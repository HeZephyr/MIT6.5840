package kvraft

import (
	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raft"
	"sync"
	"sync/atomic"
	"time"
)

type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
}

type KVServer struct {
	mu      sync.RWMutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32 // set by Kill()

	maxraftstate int // snapshot if log grows this big
	lastApplied  int //record the last applied index to avoid duplicate apply

	// Your definitions here.
	stateMachine   KVStateMachine
	lastOperations map[int64]OperationContext
	notifyChs      map[int]chan *CommandReply
}

// the tester calls Kill() when a KVServer instance won't
// be needed again. for your convenience, we supply
// code to set rf.dead (without needing a lock),
// and a killed() method to test rf.dead in
// long-running loops. you can also add your own
// code to Kill(). you're not required to do anything
// about this, but it may be convenient (for example)
// to suppress debug output from a Kill()ed instance.
func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// Your code here, if desired.
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

func (kv *KVServer) isDuplicatedCommand(clientId, commandId int64) bool {
	operationContext, ok := kv.lastOperations[clientId]
	return ok && commandId <= operationContext.MaxAppliedCommandId
}

func (kv *KVServer) ExecuteCommand(args *CommandArgs, reply *CommandReply) {
	kv.mu.RLock()
	if args.Op != OpGet && kv.isDuplicatedCommand(args.ClientId, args.CommandId) {
		lastReply := kv.lastOperations[args.ClientId].LastReply
		reply.Value, reply.Err = lastReply.Value, lastReply.Err
		kv.mu.RUnlock()
		return
	}
	kv.mu.RUnlock()
	index, _, isLeader := kv.rf.Start(Command{args})
	if !isLeader {
		reply.Err = ErrWrongLeader
		return
	}
	kv.mu.Lock()
	ch := kv.getNotifyCh(index)
	kv.mu.Unlock()

	select {
	case result := <-ch:
		reply.Value, reply.Err = result.Value, result.Err
	case <-time.After(ExecuteTimeout):
		reply.Err = ErrTimeout
	}
	go func() {
		kv.mu.Lock()
		kv.deleteNotifyCh(index)
		kv.mu.Unlock()
	}()
}

func (kv *KVServer) getNotifyCh(index int) chan *CommandReply {
	if _, ok := kv.notifyChs[index]; !ok {
		kv.notifyChs[index] = make(chan *CommandReply, 1)
	}
	return kv.notifyChs[index]
}

func (kv *KVServer) deleteNotifyCh(index int) {
	delete(kv.notifyChs, index)
}

func (kv *KVServer) applyLogToStateMachine(command Command) *CommandReply {
	reply := new(CommandReply)
	switch command.Op {
	case OpGet:
		reply.Value, reply.Err = kv.stateMachine.Get(command.Key)
	case OpPut:
		reply.Err = kv.stateMachine.Put(command.Key, command.Value)
	case OpAppend:
		reply.Err = kv.stateMachine.Append(command.Key, command.Value)
	}
	return reply
}

func (kv *KVServer) applier() {
	for kv.killed() == false {
		select {
		case message := <-kv.applyCh:
			DPrintf("{Node %v} tries to apply message %v", kv.rf.GetId(), message)
			if message.CommandValid {
				kv.mu.Lock()
				if message.CommandIndex <= kv.lastApplied {
					DPrintf("{Node %v} discards outdated message %v because a newer snapshot which lastApplied is %v has been restored", kv.rf.GetId(), message, kv.lastApplied)
					kv.mu.Unlock()
					continue
				}
				kv.lastApplied = message.CommandIndex

				reply := new(CommandReply)
				command := message.Command.(Command) // type assertion
				if command.Op != OpGet && kv.isDuplicatedCommand(command.ClientId, command.CommandId) {
					DPrintf("{Node %v} doesn't apply duplicated message %v to stateMachine because maxAppliedCommandId is %v for client %v", kv.rf.GetId(), message, kv.lastOperations[command.ClientId], command.ClientId)
					reply = kv.lastOperations[command.ClientId].LastReply
				} else {
					reply = kv.applyLogToStateMachine(command)
					if command.Op != OpGet {
						kv.lastOperations[command.ClientId] = OperationContext{
							MaxAppliedCommandId: command.CommandId,
							LastReply:           reply,
						}
					}
				}

				// just notify related channel for currentTerm's log when node is leader
				if currentTerm, isLeader := kv.rf.GetState(); isLeader && message.CommandTerm == currentTerm {
					ch := kv.getNotifyCh(message.CommandIndex)
					ch <- reply
				}
				kv.mu.Unlock()
			}
		}
	}
}

// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Command{})
	applyCh := make(chan raft.ApplyMsg)
	// You may need initialization code here.
	kv := &KVServer{
		mu:             sync.RWMutex{},
		me:             me,
		rf:             raft.Make(servers, me, persister, applyCh),
		applyCh:        applyCh,
		dead:           0,
		maxraftstate:   maxraftstate,
		stateMachine:   &MemoryKV{KV: make(map[string]string)},
		lastOperations: make(map[int64]OperationContext),
		notifyChs:      make(map[int]chan *CommandReply),
	}
	go kv.applier()
	return kv
}
