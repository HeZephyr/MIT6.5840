package shardkv

import (
	"6.5840/labrpc"
	"6.5840/shardctrler"
	"bytes"
	"fmt"
	"sync/atomic"
	"time"
)
import "6.5840/raft"
import "sync"
import "6.5840/labgob"

// ShardKV represents a sharded key/value store with Raft consensus.
type ShardKV struct {
	mu      sync.RWMutex       // mutex for synchronizing access to shared resources
	dead    int32              // set by Kill(), indicates if the server is killed
	rf      *raft.Raft         // raft instance for consensus
	applyCh chan raft.ApplyMsg // channel for applying raft messages

	makeEnd func(string) *labrpc.ClientEnd // function to create a client end to communicate with other groups
	gid     int                            // group id of the server
	sc      *shardctrler.Clerk             // client to communicate with the shardctrler

	maxRaftState int // snapshot if log grows this big
	lastApplied  int // index of the last applied log entry to prevent stateMachine from rolling back

	lastConfig    shardctrler.Config // the last configuration received from the shardctrler
	currentConfig shardctrler.Config // the current configuration of the cluster

	stateMachine   map[int]*Shard             // KV State Machines
	lastOperations map[int64]OperationContext // determine whether log is duplicated by (clientId, commandId)
	notifyChans    map[int]chan *CommandReply // notify the client when the command is applied
}

// Command handles client command requests.
func (kv *ShardKV) Command(args *CommandArgs, reply *CommandReply) {
	kv.mu.RLock()
	// if the command is the duplicated, return result directly without raft layer's participation
	if args.Op != Get && kv.isDuplicateRequest(args.ClientId, args.CommandId) {
		lastReply := kv.lastOperations[args.ClientId].LastReply
		reply.Value, reply.Err = lastReply.Value, lastReply.Err
		kv.mu.RUnlock()
		return
	}

	// check if the server can serve the requested shard.
	if !kv.canServe(key2shard(args.Key)) {
		reply.Err = ErrWrongGroup
		kv.mu.RUnlock()
		return
	}
	kv.mu.RUnlock()
	kv.Execute(NewOperationCommand(args), reply)
}

// Execute processes a command and returns the result via the reply parameter.
func (kv *ShardKV) Execute(command Command, reply *CommandReply) {
	// do not hold lock to improve throughput
	// when KVServer holds the lock to take snapshot, underlying raft can still commit raft logs
	index, _, isLeader := kv.rf.Start(command)
	if !isLeader {
		reply.Err = ErrWrongLeader
		return
	}
	defer DPrintf("{Node %v}{Group %v} Execute Command %v with CommandReply %v", kv.rf.GetId(), kv.gid, command, reply)
	kv.mu.Lock()
	notifyChan := kv.getNotifyChan(index)
	kv.mu.Unlock()

	// wait for the result or timeout.
	select {
	case result := <-notifyChan:
		reply.Value, reply.Err = result.Value, result.Err
	case <-time.After(ExecuteTimeout):
		reply.Err = ErrTimeout
	}
	// release notifyChan to reduce memory footprint
	// why asynchronously? to improve throughput, here is no need to block client request
	go func() {
		kv.mu.Lock()
		kv.removeOutdatedNotifyChan(index)
		kv.mu.Unlock()
	}()
}

// GetShardsData handles the retrieval of shard data from the leader server.
func (kv *ShardKV) GetShardsData(args *ShardOperationArgs, reply *ShardOperationReply) {
	// only pull shards from leader
	if _, isLeader := kv.rf.GetState(); !isLeader {
		reply.Err = ErrWrongLeader
		return
	}

	kv.mu.RLock()
	defer kv.mu.RUnlock()
	defer DPrintf("{Node %v}{Group %v} processes PullTaskRequest %v with PullTaskReply %v", kv.rf.GetId(), kv.gid, args, reply)

	// Check if the current configuration is ready for the requested operation.
	if kv.currentConfig.Num < args.ConfigNum {
		reply.Err = ErrNotReady
		return
	}

	// Prepare to return the shard data.
	reply.Shards = make(map[int]map[string]string)
	for _, shardID := range args.ShardIDs {
		reply.Shards[shardID] = kv.stateMachine[shardID].deepCopy()
	}

	// Copy the last operation contexts for each client.
	reply.LastOperations = make(map[int64]OperationContext)
	for clientId, operationContext := range kv.lastOperations {
		reply.LastOperations[clientId] = operationContext.deepCopy()
	}

	// set the configuration number and return OK
	reply.ConfigNum, reply.Err = args.ConfigNum, OK
}

// DeleteShardsData handles the deletion of shard data from the leader server.
func (kv *ShardKV) DeleteShardsData(args *ShardOperationArgs, reply *ShardOperationReply) {
	// only delete shards when role is leader
	if _, isLeader := kv.rf.GetState(); !isLeader {
		reply.Err = ErrWrongLeader
		return
	}
	defer DPrintf("{Node %v}{Group %v} processes GCTaskRequest %v with GCTaskReply %v", kv.rf.GetId(), kv.gid, args, reply)
	kv.mu.RLock()
	// Check if the current configuration is greater than the requested one.
	if kv.currentConfig.Num > args.ConfigNum {
		DPrintf("{Node %v}{Group %v} encounters duplicated shards deletion request %v when currentConfig is %v", kv.rf.GetId(), kv.gid, args, kv.currentConfig)
		reply.Err = OK
		kv.mu.RUnlock()
		return
	}
	kv.mu.RUnlock()

	// execute the command to delete shards
	commandReply := new(CommandReply)
	kv.Execute(NewDeleteShardsCommand(args), commandReply)
	reply.Err = commandReply.Err
}

// applier continuously applies commands from the Raft log to the state machine.
func (kv *ShardKV) applier() {
	for !kv.killed() {
		select {
		// wait for a new message in the apply channel
		case message := <-kv.applyCh:
			DPrintf("{Node %v}{Group %v} tries to apply message %v", kv.rf.GetId(), kv.gid, message)
			if message.CommandValid {
				kv.mu.Lock()
				// check if the command has already been applied.
				if message.CommandIndex <= kv.lastApplied {
					DPrintf("{Node %v}{Group %v} discards outdated message %v because a newer snapshot which lastApplied is %v has been applied", kv.rf.GetId(), kv.gid, message, kv.lastApplied)
					kv.mu.Unlock()
					continue
				}
				// update the last applied index
				kv.lastApplied = message.CommandIndex

				reply := new(CommandReply)
				// type assert the command from the message.
				command := message.Command.(Command)
				switch command.CommandType {
				case Operation:
					// extract the operation data and apply the operation to the state machine
					operation := command.Data.(CommandArgs)
					reply = kv.applyOperation(&operation)
				case Configuration:
					// extract the configuration data and apply the configuration to the state machine
					nextConfig := command.Data.(shardctrler.Config)
					reply = kv.applyConfiguration(&nextConfig)
				case InsertShards:
					// extract the shard insertion data and apply the insertion to the state machine
					shardsInfo := command.Data.(ShardOperationReply)
					reply = kv.applyInsertShards(&shardsInfo)
				case DeleteShards:
					// extract the shard deletion data and apply the deletion to the state machine
					shardsInfo := command.Data.(ShardOperationArgs)
					reply = kv.applyDeleteShards(&shardsInfo)
				case EmptyShards:
					// apply empty shards to the state machine, to prevent the state machine from rolling back
					reply = kv.applyEmptyShards()
				}

				// only notify the related channel for currentTerm's log when node is Leader
				if currentTerm, isLeader := kv.rf.GetState(); isLeader && message.CommandTerm == currentTerm {
					notifyChan := kv.getNotifyChan(message.CommandIndex)
					notifyChan <- reply
				}

				// take snapshot if needed
				if kv.needSnapshot() {
					kv.takeSnapshot(message.CommandIndex)
				}
				kv.mu.Unlock()
			} else if message.SnapshotValid {
				// restore the state machine from the snapshot
				kv.mu.Lock()
				if kv.rf.CondInstallSnapshot(message.SnapshotTerm, message.SnapshotIndex, message.Snapshot) {
					kv.restoreSnapshot(message.Snapshot)
					kv.lastApplied = message.SnapshotIndex
				}
				kv.mu.Unlock()
			} else {
				panic(fmt.Sprintf("{Node %v}{Group %v} invalid apply message %v", kv.rf.GetId(), kv.gid, message))
			}
		}
	}
}

// applyOperation applies a given operation to the KV state machine.
func (kv *ShardKV) applyOperation(operation *CommandArgs) *CommandReply {
	reply := new(CommandReply)
	shardID := key2shard(operation.Key)

	// check if the server can serve the requested shard.
	if !kv.canServe(shardID) {
		reply.Err = ErrWrongGroup
	} else {
		// check if the operation is duplicated(only for non-Get operations)
		if operation.Op != Get && kv.isDuplicateRequest(operation.ClientId, operation.CommandId) {
			DPrintf("{Node %v}{Group %v} does not apply duplicated commandId %v to stateMachine because maxAppliedCommandId is %v for clientId %v", kv.rf.GetId(), kv.gid, operation.CommandId, kv.lastOperations[operation.ClientId].MaxAppliedCommandId, operation.ClientId)
			lastReply := kv.lastOperations[operation.ClientId].LastReply
			reply.Value, reply.Err = lastReply.Value, lastReply.Err
		} else {
			// apply the operation to the state machine
			reply = kv.applyLogToStateMachine(operation, shardID)
			// update the last operation context for the client if the operation is not a Get operation
			if operation.Op != Get {
				kv.lastOperations[operation.ClientId] = OperationContext{
					operation.CommandId,
					reply,
				}
			}
		}
	}
	return reply
}

// applyConfiguration applies a new configuration to the shard.
func (kv *ShardKV) applyConfiguration(nextConfig *shardctrler.Config) *CommandReply {
	reply := new(CommandReply)
	// check if the new configuration is the next in line.
	if nextConfig.Num == kv.currentConfig.Num+1 {
		DPrintf("{Node %v}{Group %v} updates currentConfig from %v to %v", kv.rf.GetId(), kv.gid, kv.currentConfig, nextConfig)
		// update the shard status based on the new configuration.
		kv.updateShardStatus(nextConfig)
		// save the last configuration.
		kv.lastConfig = kv.currentConfig
		// update the current configuration.
		kv.currentConfig = *nextConfig
		reply.Err = OK
	} else {
		DPrintf("{Node %v}{Group %v} discards outdated configuration %v when currentConfig is %v", kv.rf.GetId(), kv.gid, nextConfig, kv.currentConfig)
		reply.Err = ErrOutDated
	}
	return reply
}

// applyInsertShards applies the insertion of shard data.
func (kv *ShardKV) applyInsertShards(shardsInfo *ShardOperationReply) *CommandReply {
	reply := new(CommandReply)
	// check if the configuration number matches the current one.
	if shardsInfo.ConfigNum == kv.currentConfig.Num {
		DPrintf("{Node %v}{Group %v} accepts shards insertion %v when currentConfig is %v", kv.rf.GetId(), kv.gid, shardsInfo, kv.currentConfig)
		for shardID, shardData := range shardsInfo.Shards {
			shard := kv.stateMachine[shardID]
			// only pull if the shard is in the Pulling state.
			if shard.Status == Pulling {
				for key, value := range shardData {
					shard.Put(key, value)
				}
				// update the shard status to Garbage Collecting.
				shard.Status = GCing
			} else {
				DPrintf("{Node %v}{Group %v} encounters duplicated shards insertion %v when currentConfig is %v", kv.rf.GetId(), kv.gid, shardsInfo, kv.currentConfig)
				break
			}
		}
		// update last operations with the provided contexts.
		for clientId, operationContext := range shardsInfo.LastOperations {
			if lastOperation, ok := kv.lastOperations[clientId]; !ok || lastOperation.MaxAppliedCommandId < operationContext.MaxAppliedCommandId {
				kv.lastOperations[clientId] = operationContext
			}
		}
	} else {
		DPrintf("{Node %v}{Group %v} discards outdated shards insertion %v when currentConfig is %v", kv.rf.GetId(), kv.gid, shardsInfo, kv.currentConfig)
		reply.Err = ErrOutDated
	}
	return reply
}

// applyDeleteShards applies the deletion of shard data.
func (kv *ShardKV) applyDeleteShards(shardsInfo *ShardOperationArgs) *CommandReply {
	// check if the configuration number matches.
	if shardsInfo.ConfigNum == kv.currentConfig.Num {
		DPrintf("{Node %v}{Group %v}'s shards status are %v before accepting shards deletion %v when currentConfig is %v", kv.rf.GetId(), kv.gid, kv.stateMachine, shardsInfo, kv.currentConfig)
		for _, shardID := range shardsInfo.ShardIDs {
			// get the shard to delete.
			shard := kv.stateMachine[shardID]
			if shard.Status == GCing { // if the shard is being garbage collected, update the status to Serving.
				shard.Status = Serving
			} else if shard.Status == BePulling { // if the shard is being pulled, reset the shard to a new one
				kv.stateMachine[shardID] = NewShard()
			} else { // exit if the shard is not in the expected state.
				DPrintf("{Node %v}{Group %v} encounters duplicated shards deletion %v when currentConfig is %v", kv.rf.GetId(), kv.gid, shardsInfo, kv.currentConfig)
				break
			}
		}
		DPrintf("{Node %v}{Group %v}'s shards status are %v after accepting shards deletion %v when currentConfig is %v", kv.rf.GetId(), kv.gid, kv.stateMachine, shardsInfo, kv.currentConfig)
		return &CommandReply{Err: OK}
	}
	DPrintf("{Node %v}{Group %v} discards outdated shards deletion %v when currentConfig is %v", kv.rf.GetId(), kv.gid, shardsInfo, kv.currentConfig)
	return &CommandReply{Err: OK}
}

// applyEmptyShards handles the case for empty shards. This is to prevent the state machine from rolling back.
func (kv *ShardKV) applyEmptyShards() *CommandReply {
	return &CommandReply{Err: OK}
}

// updateShardStatus updates the status of shards based on the next configuration.
func (kv *ShardKV) updateShardStatus(nextConfig *shardctrler.Config) {
	for shardID := 0; shardID < shardctrler.NShards; shardID++ {
		// check if the shard's group changes from current to next configuration.
		// The shard is not the responsibility of this gid, but the gid in the next configuration is responsible for this shard, so need to pull the shard.
		if kv.currentConfig.Shards[shardID] != kv.gid && nextConfig.Shards[shardID] == kv.gid {
			// Get the new group ID.
			gid := kv.currentConfig.Shards[shardID]
			// skip the shard if the group is 0, because it means the shard is not assigned to any group.
			if gid != 0 {
				kv.stateMachine[shardID].Status = Pulling
			}
		}
		// check if the shard's group changes from next to current configuration.
		// The shard is the responsibility of this gid, but the gid in the next configuration is not responsible for this shard, so need to be pulled by other group.
		if kv.currentConfig.Shards[shardID] == kv.gid && nextConfig.Shards[shardID] != kv.gid {
			// Get the new group ID.
			gid := nextConfig.Shards[shardID]
			if gid != 0 {
				kv.stateMachine[shardID].Status = BePulling
			}
		}
	}
}

// applyLogToStateMachine applies the operation log to the state machine.
func (kv *ShardKV) applyLogToStateMachine(operation *CommandArgs, shardID int) *CommandReply {
	reply := new(CommandReply)
	switch operation.Op {
	case Get:
		reply.Value, reply.Err = kv.stateMachine[shardID].Get(operation.Key)
	case Put:
		reply.Err = kv.stateMachine[shardID].Put(operation.Key, operation.Value)
	case Append:
		reply.Err = kv.stateMachine[shardID].Append(operation.Key, operation.Value)
	}
	return reply
}

func (kv *ShardKV) needSnapshot() bool {
	return kv.maxRaftState != -1 && kv.rf.GetRaftStateSize() >= kv.maxRaftState
}

func (kv *ShardKV) takeSnapshot(index int) {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(kv.lastConfig)
	e.Encode(kv.currentConfig)
	e.Encode(kv.stateMachine)
	e.Encode(kv.lastOperations)
	data := w.Bytes()
	kv.rf.Snapshot(index, data)
}

func (kv *ShardKV) restoreSnapshot(snapshot []byte) {
	// restore the state machine from the snapshot. If the snapshot is nil or empty, initialize the state machines.
	if snapshot == nil || len(snapshot) < 1 {
		kv.initStateMachines()
		return
	}
	r := bytes.NewBuffer(snapshot)
	d := labgob.NewDecoder(r)
	var stateMachine map[int]*Shard
	var lastOperations map[int64]OperationContext
	var lastConfig shardctrler.Config
	var currentConfig shardctrler.Config
	if d.Decode(&lastConfig) != nil ||
		d.Decode(&currentConfig) != nil ||
		d.Decode(&stateMachine) != nil ||
		d.Decode(&lastOperations) != nil {
		DPrintf("{Node %v}{Group %v} fails to restore state machine from snapshot", kv.rf.GetId(), kv.gid)
	}
	kv.lastConfig, kv.currentConfig, kv.stateMachine, kv.lastOperations = lastConfig, currentConfig, stateMachine, lastOperations
}

func (kv *ShardKV) initStateMachines() {
	for shardID := 0; shardID < shardctrler.NShards; shardID++ {
		if _, ok := kv.stateMachine[shardID]; !ok {
			kv.stateMachine[shardID] = NewShard()
		}
	}
}

// each RPC imply that the client has seen the reply for its previous RPC
// therefore, we only need to determine whether the latest commandId of a clientId meets the criteria
func (kv *ShardKV) isDuplicateRequest(clientId int64, commandId int64) bool {
	operationContext, ok := kv.lastOperations[clientId]
	return ok && commandId <= operationContext.MaxAppliedCommandId
}

// canServe checks if the server can serve the shard.
func (kv *ShardKV) canServe(shardID int) bool {
	return kv.currentConfig.Shards[shardID] == kv.gid && (kv.stateMachine[shardID].Status == Serving || kv.stateMachine[shardID].Status == GCing)
}

// getNotifyChan returns the notify channel for the given index.
func (kv *ShardKV) getNotifyChan(index int) chan *CommandReply {
	if _, ok := kv.notifyChans[index]; !ok {
		kv.notifyChans[index] = make(chan *CommandReply, 1)
	}
	return kv.notifyChans[index]
}

// removeNotifyChan removes the notify channel for the given index.
func (kv *ShardKV) removeOutdatedNotifyChan(index int) {
	delete(kv.notifyChans, index)
}

// Checks if the next configuration can be performed.
// If all shards are in Serving status, it queries and applies the next configuration.
func (kv *ShardKV) configurationAction() {
	canPerformNextConfig := true
	kv.mu.RLock()
	// If any shard is not in the Serving status, the next configuration cannot be applied
	for _, shard := range kv.stateMachine {
		if shard.Status != Serving {
			canPerformNextConfig = false
			DPrintf("{Node %v}{Group %v} will not try to fetch latest configuration because shards status are %v when currentConfig is %v", kv.rf.GetId(), kv.gid, kv.stateMachine, kv.currentConfig)
			break
		}
	}
	currentConfigNum := kv.currentConfig.Num
	kv.mu.RUnlock()
	// Query and apply the next configuration if allowed
	if canPerformNextConfig {
		nextConfig := kv.sc.Query(currentConfigNum + 1)
		// Ensure the queried configuration is the next one
		if nextConfig.Num == currentConfigNum+1 {
			DPrintf("{Node %v}{Group %v} fetches latest configuration %v when currentConfigNum is %v", kv.rf.GetId(), kv.gid, nextConfig, currentConfigNum)
			kv.Execute(NewConfigurationCommand(&nextConfig), new(CommandReply))
		}
	}
}

// Executes the migration task to pull shard data from other groups.
func (kv *ShardKV) migrationAction() {
	kv.mu.RLock()
	gid2Shards := kv.getShardIDsByStatus(Pulling)
	var wg sync.WaitGroup

	// Create pull tasks for each group (GID)
	for gid, shardIDs := range gid2Shards {
		DPrintf("{Node %v}{Group %v} starts a PullTask to get shards %v from group %v when config is %v", kv.rf.GetId(), kv.gid, shardIDs, gid, kv.currentConfig)
		wg.Add(1)
		go func(servers []string, configNum int, shardIDs []int) {
			defer wg.Done()
			pullTaskArgs := ShardOperationArgs{configNum, shardIDs}
			// Try to pull shard data from each server in the group
			for _, server := range servers {
				pullTaskReply := new(ShardOperationReply)
				srv := kv.makeEnd(server)
				if srv.Call("ShardKV.GetShardsData", &pullTaskArgs, pullTaskReply) && pullTaskReply.Err == OK {
					//Pulling data from these servers
					DPrintf("{Node %v}{Group %v} gets a PullTaskReply %v and tries to commit it when currentConfigNum is %v", kv.rf.GetId(), kv.gid, pullTaskReply, configNum)
					kv.Execute(NewInsertShardsCommand(pullTaskReply), new(CommandReply))
				}
			}
		}(kv.lastConfig.Groups[gid], kv.currentConfig.Num, shardIDs)
	}
	kv.mu.RUnlock()
	wg.Wait() // Wait for all pull tasks to complete
}

// Executes garbage collection (GC) tasks to delete shard data from other groups.
func (kv *ShardKV) gcAction() {
	kv.mu.RLock()
	// Get the group that was previously responsible for these shards and clean up the shards that are no longer responsible.
	gid2Shards := kv.getShardIDsByStatus(GCing)
	var wg sync.WaitGroup

	// Create GC tasks for each group (GID)
	for gid, shardIDs := range gid2Shards {
		DPrintf("{Node %v}{Group %v} starts a GCTask to delete shards %v from group %v when config is %v", kv.rf.GetId(), kv.gid, shardIDs, gid, kv.currentConfig)
		wg.Add(1)
		go func(servers []string, configNum int, shardIDs []int) {
			defer wg.Done()
			gcTaskArgs := ShardOperationArgs{configNum, shardIDs}
			// Try to delete shard data from each server in the group
			for _, server := range servers {
				gcTaskReply := new(ShardOperationReply)
				srv := kv.makeEnd(server)
				if srv.Call("ShardKV.DeleteShardsData", &gcTaskArgs, gcTaskReply) && gcTaskReply.Err == OK {
					DPrintf("{Node %v}{Group %v} deletes shards %v in remote group successfully when currentConfigNum is %v", kv.rf.GetId(), kv.gid, shardIDs, configNum)
					kv.Execute(NewDeleteShardsCommand(&gcTaskArgs), new(CommandReply))
				}
			}
		}(kv.lastConfig.Groups[gid], kv.currentConfig.Num, shardIDs)
	}
	kv.mu.RUnlock()
	wg.Wait() // Wait for all GC tasks to complete
}

// Ensures that a log entry is present in the current term to keep the log active.
func (kv *ShardKV) checkEntryInCurrentTermAction() {
	// If no log entry exists in the current term, execute an empty command
	if !kv.rf.HasLogInCurrentTerm() {
		kv.Execute(NewEmptyShardsCommand(), new(CommandReply))
	}
}

// Monitor a specific action and repeatedly execute it in a fixed time interval if the server is the leader.
func (kv *ShardKV) Monitor(action func(), timeout time.Duration) {
	for !kv.killed() {
		if _, isLeader := kv.rf.GetState(); isLeader {
			action()
		}
		time.Sleep(timeout)
	}
}

// Returns a map of GID to shard IDs for shards with a specified status.
func (kv *ShardKV) getShardIDsByStatus(status ShardStatus) map[int][]int {
	gid2shardIDs := make(map[int][]int)
	for shardID, shard := range kv.stateMachine {
		// Filter shards with the given status
		if shard.Status == status {
			// Find the last gid responsible for the shard and pull data from that gid
			gid := kv.lastConfig.Shards[shardID]
			if gid != 0 {
				// Group shard IDs by GID
				if _, ok := gid2shardIDs[gid]; !ok {
					gid2shardIDs[gid] = make([]int, 0)
				}
				gid2shardIDs[gid] = append(gid2shardIDs[gid], shardID)
			}
		}
	}
	return gid2shardIDs
}

// the tester calls Kill() when a ShardKV instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
func (kv *ShardKV) Kill() {
	DPrintf("{Node %v}{Group %v} is killed", kv.rf.GetId(), kv.gid)
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// Your code here, if desired.
}

// killed returns true if the server is killed.
func (kv *ShardKV) killed() bool {
	return atomic.LoadInt32(&kv.dead) == 1
}

// servers[] contains the ports of the servers in this group.
//
// me is the index of the current server in servers[].
//
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
//
// the k/v server should snapshot when Raft's saved state exceeds
// maxraftstate bytes, in order to allow Raft to garbage-collect its
// log. if maxraftstate is -1, you don't need to snapshot.
//
// gid is this group's GID, for interacting with the shardctrler.
//
// pass ctrlers[] to shardctrler.MakeClerk() so you can send
// RPCs to the shardctrler.
//
// make_end(servername) turns a server name from a
// Config.Groups[gid][i] into a labrpc.ClientEnd on which you can
// send RPCs. You'll need this to send RPCs to other groups.
//
// look at client.go for examples of how to use ctrlers[]
// and make_end() to send RPCs to the group owning a specific shard.
//
// StartServer() must return quickly, so it should start goroutines
// for any long-running work.
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int, gid int, ctrlers []*labrpc.ClientEnd, makeEnd func(string) *labrpc.ClientEnd) *ShardKV {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Command{})
	labgob.Register(CommandArgs{})
	labgob.Register(shardctrler.Config{})
	labgob.Register(ShardOperationArgs{})
	labgob.Register(ShardOperationReply{})

	// Create a channel to receive messages applied by Raft.
	applyCh := make(chan raft.ApplyMsg)

	kv := &ShardKV{
		dead:           0,
		rf:             raft.Make(servers, me, persister, applyCh),
		applyCh:        applyCh,
		makeEnd:        makeEnd,
		gid:            gid,
		sc:             shardctrler.MakeClerk(ctrlers),
		maxRaftState:   maxraftstate,
		lastApplied:    0,
		lastConfig:     shardctrler.DefaultConfig(),
		currentConfig:  shardctrler.DefaultConfig(),
		stateMachine:   make(map[int]*Shard),
		lastOperations: make(map[int64]OperationContext),
		notifyChans:    make(map[int]chan *CommandReply),
	}

	// Restore any snapshot data stored in persister to recover the state.
	kv.restoreSnapshot(persister.ReadSnapshot())

	go kv.applier()

	// Start several monitoring routines that periodically perform specific actions:
	go kv.Monitor(kv.configurationAction, ConfigurationMonitorTimeout)         // Monitor configuration changes.
	go kv.Monitor(kv.migrationAction, MigrationMonitorTimeout)                 // Monitor shard migration.
	go kv.Monitor(kv.gcAction, GCMonitorTimeout)                               // Monitor garbage collection of old shard data.
	go kv.Monitor(kv.checkEntryInCurrentTermAction, EmptyEntryDetectorTimeout) // Monitor Raft log entries in the current term.

	DPrintf("{Node %v}{Group %v} started", kv.rf.GetId(), kv.gid)
	return kv
}
