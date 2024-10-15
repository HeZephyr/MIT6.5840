package shardctrler

import "sort"

type ConfigStateMachine interface {
	Join(groups map[int][]string) Err
	Leave(gids []int) Err
	Move(shard, gid int) Err
	Query(num int) (Config, Err)
}

type MemoryConfigStateMachine struct {
	Configs []Config
}

func NewMemoryConfigStateMachine() *MemoryConfigStateMachine {
	cf := &MemoryConfigStateMachine{make([]Config, 1)}
	cf.Configs[0] = DefaultConfig()
	return cf
}

func (cf *MemoryConfigStateMachine) Join(groups map[int][]string) Err {
	// get the latest config
	lastConfig := cf.Configs[len(cf.Configs)-1]
	// create a new config based on the latest config
	newConfig := Config{len(cf.Configs), lastConfig.Shards, deepCopy(lastConfig.Groups)}
	// iterate over the list of GIDs to be added
	for gid, servers := range groups {
		// if the GID does not exist in the new configuration, add it. avoid overwriting the existing GID
		if _, ok := newConfig.Groups[gid]; !ok {
			newServers := make([]string, len(servers))
			copy(newServers, servers)
			newConfig.Groups[gid] = newServers
		}
	}
	// convert the shard allocation of the new configuration object to the mapping of "GID -> Shard List"
	group2shards := Group2Shards(newConfig)
	// load balancing is performed only when raft groups exist
	for {
		source, target := GetGIDWithMaximumShards(group2shards), GetGIDWithMinimumShards(group2shards)
		if source != 0 && len(group2shards[source])-len(group2shards[target]) <= 1 {
			break
		}
		// move the source GID group's first shard to the target GID group
		group2shards[target] = append(group2shards[target], group2shards[source][0])
		group2shards[source] = group2shards[source][1:]
	}
	var newShards [NShards]int
	// assign the shards to the GID based on the mapping of "GID -> Shard List"
	for gid, shards := range group2shards {
		for _, shard := range shards {
			newShards[shard] = gid
		}
	}
	newConfig.Shards = newShards
	cf.Configs = append(cf.Configs, newConfig)
	return OK
}

func (cf *MemoryConfigStateMachine) Leave(gids []int) Err {
	// get the latest config
	lastConfig := cf.Configs[len(cf.Configs)-1]
	// create a new config based on the latest config
	newConfig := Config{len(cf.Configs), lastConfig.Shards, deepCopy(lastConfig.Groups)}
	// convert the shard allocation of the new configuration object to the mapping of "GID -> Shard List"
	group2shards := Group2Shards(newConfig)
	// create a list to store the shards that are not assigned to any group
	orphanShards := make([]int, 0)
	// iterate over the list of GIDs to be removed
	for _, gid := range gids {
		// if the GID exists in the new configuration, remove it
		if _, ok := newConfig.Groups[gid]; ok {
			delete(newConfig.Groups, gid)
		}
		// if the GID exists in the mapping of "GID -> Shard List", remove it and append the shards to the orphanShards list
		if shards, ok := group2shards[gid]; ok {
			orphanShards = append(orphanShards, shards...)
			delete(group2shards, gid)
		}
	}
	var newShards [NShards]int
	// load balancing is performed only when raft groups exist
	if len(newConfig.Groups) > 0 {
		// assign the orphan shards to the GID with the minimum number of shards
		for _, shard := range orphanShards {
			target := GetGIDWithMinimumShards(group2shards)
			group2shards[target] = append(group2shards[target], shard)
		}
		for gid, shards := range group2shards {
			for _, shard := range shards {
				newShards[shard] = gid
			}
		}
	}
	newConfig.Shards = newShards
	cf.Configs = append(cf.Configs, newConfig)
	return OK
}

// Move moves the shard to the GID group
func (cf *MemoryConfigStateMachine) Move(shard, gid int) Err {
	// get the latest config
	lastConfig := cf.Configs[len(cf.Configs)-1]
	newConfig := Config{len(cf.Configs), lastConfig.Shards, deepCopy(lastConfig.Groups)}
	newConfig.Shards[shard] = gid
	cf.Configs = append(cf.Configs, newConfig)
	return OK
}

func (cf *MemoryConfigStateMachine) Query(num int) (Config, Err) {
	// return the latest config if the num is invalid
	if num < 0 || num >= len(cf.Configs) {
		return cf.Configs[len(cf.Configs)-1], OK
	}
	return cf.Configs[num], OK
}

func Group2Shards(config Config) map[int][]int {
	group2shards := make(map[int][]int)
	for gid := range config.Groups {
		group2shards[gid] = make([]int, 0)
	}
	for shard, gid := range config.Shards {
		group2shards[gid] = append(group2shards[gid], shard)
	}
	return group2shards
}

func GetGIDWithMinimumShards(group2shards map[int][]int) int {
	var keys []int
	for k := range group2shards {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	// find GID with minimum shards
	index, minn := -1, NShards+1
	for _, gid := range keys {
		if gid != 0 && len(group2shards[gid]) < minn {
			index, minn = gid, len(group2shards[gid])
		}
	}
	return index
}

func GetGIDWithMaximumShards(group2shards map[int][]int) int {
	// gid 0 indicates that the shard is assigned to the special group
	// By prioritizing GID 0, we handle cases where shards are assigned to this special group.
	if shards, ok := group2shards[0]; ok && len(shards) > 0 {
		return 0
	}
	// create a slice to store the keys (GIDs) from the map.
	var keys []int
	for k := range group2shards {
		keys = append(keys, k)
	}

	sort.Ints(keys)
	index, maxn := -1, -1
	for _, gid := range keys {
		if len(group2shards[gid]) > maxn {
			index, maxn = gid, len(group2shards[gid])
		}
	}
	return index
}

func deepCopy(groups map[int][]string) map[int][]string {
	newGroups := make(map[int][]string)
	for gid, servers := range groups {
		newServers := make([]string, len(servers))
		copy(newServers, servers)
		newGroups[gid] = newServers
	}
	return newGroups
}
