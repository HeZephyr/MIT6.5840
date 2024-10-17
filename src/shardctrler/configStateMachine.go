package shardctrler

import "sort"

// ConfigStateMachine defines an interface for a configuration state machine, including methods for operating on configurations.
type ConfigStateMachine interface {
	Join(groups map[int][]string) Err // add new groups to the configuration, where groups is a map from group ID to a list of servers
	Leave(gids []int) Err             // leave specified groups from the configuration, where gids is a list of group IDs
	Move(shard, gid int) Err          // move a specified shard to a specified group, where shard is the shard ID and gid is the group ID
	Query(num int) (Config, Err)      // query a specified configuration, where num is the configuration number, if num is not valid, return the latest configuration
}

// MemoryConfigStateMachine is a memory - based implementation of the ConfigStateMachine interface.
type MemoryConfigStateMachine struct {
	Configs []Config
}

// NewMemoryConfigStateMachine creates a new MemoryConfigStateMachine instance and initializes it with a default configuration.
func NewMemoryConfigStateMachine() *MemoryConfigStateMachine {
	cf := &MemoryConfigStateMachine{make([]Config, 1)}
	cf.Configs[0] = DefaultConfig()
	return cf
}

// Join adds new groups to the configuration.
func (cf *MemoryConfigStateMachine) Join(groups map[int][]string) Err {
	lastConfig := cf.Configs[len(cf.Configs)-1]
	// create a new configuration based on the last configuration
	newConfig := Config{
		len(cf.Configs),
		lastConfig.Shards,
		deepCopy(lastConfig.Groups),
	}
	for gid, servers := range groups {
		// if the group does not exist in the new configuration, add it
		if _, ok := newConfig.Groups[gid]; !ok {
			newServers := make([]string, len(servers))
			copy(newServers, servers)
			newConfig.Groups[gid] = newServers
		}
	}
	group2Shards := Group2Shards(newConfig)
	for {
		// load balance the shards among the groups
		source, target := GetGIDWithMaximumShards(group2Shards), GetGIDWithMinimumShards(group2Shards)
		if source != 0 && len(group2Shards[source])-len(group2Shards[target]) <= 1 {
			break
		}
		group2Shards[target] = append(group2Shards[target], group2Shards[source][0])
		group2Shards[source] = group2Shards[source][1:]
	}
	// update the shard assignment in the new configuration
	var newShards [NShards]int
	for gid, shards := range group2Shards {
		for _, shard := range shards {
			newShards[shard] = gid
		}
	}
	newConfig.Shards = newShards
	cf.Configs = append(cf.Configs, newConfig)
	return OK
}

// Leave removes specified groups from the configuration.
func (cf *MemoryConfigStateMachine) Leave(gids []int) Err {
	lastConifg := cf.Configs[len(cf.Configs)-1]
	// create a new configuration based on the last configuration
	newConfig := Config{
		len(cf.Configs),
		lastConifg.Shards,
		deepCopy(lastConifg.Groups),
	}
	group2Shards := Group2Shards(newConfig)
	// used to store the orphan shards (i.e., shards owned by
	orphanShards := make([]int, 0)
	for _, gid := range gids {
		// if the group exists in the new configuration, remove it
		if _, ok := newConfig.Groups[gid]; ok {
			delete(newConfig.Groups, gid)
		}
		// if the group owns any shards, remove them and add them to the orphan shards
		if shards, ok := group2Shards[gid]; ok {
			delete(group2Shards, gid)
			orphanShards = append(orphanShards, shards...)
		}
	}

	var newShards [NShards]int
	if len(newConfig.Groups) > 0 {
		// re-allocate orphan shards to the remaining groups
		for _, shard := range orphanShards {
			gid := GetGIDWithMinimumShards(group2Shards)
			newShards[shard] = gid
			group2Shards[gid] = append(group2Shards[gid], shard)
		}

		// update the shard assignment in the new configuration
		for gid, shards := range group2Shards {
			for _, shard := range shards {
				newShards[shard] = gid
			}
		}
	}
	newConfig.Shards = newShards
	cf.Configs = append(cf.Configs, newConfig)
	return OK
}

// Move moves a specified shard to a specified group.
func (cf *MemoryConfigStateMachine) Move(shard, gid int) Err {
	lastConfig := cf.Configs[len(cf.Configs)-1]
	// create a new configuration based on the last configuration
	newConfig := Config{
		len(cf.Configs),
		lastConfig.Shards,
		deepCopy(lastConfig.Groups),
	}
	// update the shard assignment in the new configuration
	newConfig.Shards[shard] = gid
	cf.Configs = append(cf.Configs, newConfig)
	return OK
}

// Query queries a specified configuration.
func (cf *MemoryConfigStateMachine) Query(num int) (Config, Err) {
	// if the configuration number is not valid, return the latest configuration
	if num < 0 || num >= len(cf.Configs) {
		return cf.Configs[len(cf.Configs)-1], OK
	}
	return cf.Configs[num], OK
}

// Group2Shards assigns each shard to the corresponding group.
func Group2Shards(config Config) map[int][]int {
	group2Shards := make(map[int][]int)
	for gid := range config.Groups {
		group2Shards[gid] = make([]int, 0)
	}
	for shard, gid := range config.Shards {
		group2Shards[gid] = append(group2Shards[gid], shard)
	}
	return group2Shards
}

// GetGIDWithMinimumShards returns the group ID with the minimum number of shards.
func GetGIDWithMinimumShards(group2Shards map[int][]int) int {
	// get all the group IDs
	var gids []int
	for gid := range group2Shards {
		gids = append(gids, gid)
	}
	sort.Ints(gids)
	index, minShards := -1, NShards+1
	// find the group ID with the minimum number of shards
	for _, gid := range gids {
		// don't consider the special group 0
		if gid != 0 && len(group2Shards[gid]) < minShards {
			index, minShards = gid, len(group2Shards[gid])
		}
	}
	return index
}

// GetGIDWithMaximumShards returns the group ID with the maximum number of shards
func GetGIDWithMaximumShards(group2Shards map[int][]int) int {
	// group indicate the not assigned group, always choose gid 0 if there is any shard not assigned
	if shards, ok := group2Shards[0]; ok && len(shards) != 0 {
		return 0
	}

	var gids []int
	for gid := range group2Shards {
		gids = append(gids, gid)
	}
	sort.Ints(gids)
	index, maxShards := -1, -1
	// find the group ID with the maximum number of shards
	for _, gid := range gids {
		if len(group2Shards[gid]) > maxShards {
			index, maxShards = gid, len(group2Shards[gid])
		}
	}
	return index
}

// deepCopy creates a deep copy of the groups map.
func deepCopy(groups map[int][]string) map[int][]string {
	newGroups := make(map[int][]string)
	for gid, servers := range groups {
		newServers := make([]string, len(servers))
		copy(newServers, servers)
		newGroups[gid] = newServers
	}
	return newGroups
}
