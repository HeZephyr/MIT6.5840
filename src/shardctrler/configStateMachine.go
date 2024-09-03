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
		// if the GID does not exist in the new configuration, add it
		newServers := make([]string, len(servers))
		copy(newServers, servers)
		newConfig.Groups[gid] = newServers
	}
	// convert the shard allocation of the new configuration object to the mapping of "GID -> Shard List"
	s2g := Group2Shards(newConfig)
	// load balancing is performed only when raft groups exist
	for {
		source, target := GetGIDWithMaximumShards(s2g), GetGIDWithMinimumShards(s2g)
		if source != 0 && len(s2g[source])-len(s2g[target]) <= 1 {
			break
		}
		// move the source GID group's first shard to the target GID group
		s2g[target] = append(s2g[target], s2g[source][0])
		s2g[source] = s2g[source][1:]
	}
	var newShards [NShards]int
	// assign the shards to the GID based on the mapping of "GID -> Shard List"
	for gid, shards := range s2g {
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
	s2g := Group2Shards(newConfig)
	// create a list to store the shards that are not assigned to any group
	orphanShards := make([]int, 0)
	// iterate over the list of GIDs to be removed
	for _, gid := range gids {
		// if the GID exists in the new configuration, remove it
		if _, ok := newConfig.Groups[gid]; ok {
			delete(newConfig.Groups, gid)
		}
		// if the GID exists in the mapping of "GID -> Shard List", remove it and append the shards to the orphanShards list
		if shards, ok := s2g[gid]; ok {
			orphanShards = append(orphanShards, shards...)
			delete(s2g, gid)
		}
	}
	var newShards [NShards]int
	// load balancing is performed only when raft groups exist
	if len(newConfig.Groups) > 0 {
		// assign the orphan shards to the GID with the minimum number of shards
		for _, shard := range orphanShards {
			target := GetGIDWithMinimumShards(s2g)
			s2g[target] = append(s2g[target], shard)
		}
		for gid, shards := range s2g {
			for _, shard := range shards {
				newShards[shard] = gid
			}
		}
	}
	newConfig.Shards = newShards
	cf.Configs = append(cf.Configs, newConfig)
	return OK
}

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
	s2g := make(map[int][]int)
	for gid := range config.Groups {
		s2g[gid] = make([]int, 0)
	}
	for shard, gid := range config.Shards {
		s2g[gid] = append(s2g[gid], shard)
	}
	return s2g
}

func GetGIDWithMinimumShards(s2g map[int][]int) int {
	var keys []int
	for k := range s2g {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	// find GID with minimum shards
	index, minn := -1, NShards+1
	for _, gid := range keys {
		if gid != 0 && len(s2g[gid]) < minn {
			index, minn = gid, len(s2g[gid])
		}
	}
	return index
}

func GetGIDWithMaximumShards(s2g map[int][]int) int {
	if shards, ok := s2g[0]; ok && len(shards) > 0 {
		return 0
	}
	var keys []int
	for k := range s2g {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	index, maxn := -1, -1
	for _, gid := range keys {
		if gid != 0 && len(s2g[gid]) > maxn {
			index, maxn = gid, len(s2g[gid])
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
