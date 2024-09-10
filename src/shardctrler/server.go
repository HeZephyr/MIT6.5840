package shardctrler

import (
	"6.5840/raft"
	"fmt"
	"sync/atomic"
	"time"
)
import "6.5840/labrpc"
import "sync"
import "6.5840/labgob"

type ShardCtrler struct {
	mu      sync.RWMutex
	dead    int32
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	configStateMachine ConfigStateMachine         // Config stateMachine
	lastOperations     map[int64]OperationContext // determine whether log is duplicated by recording the last commandId and response corresponding to the clientId
	notifyChannels     map[int]chan *CommandReply
}

// the tester calls Kill() when a ShardCtrler instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
func (sc *ShardCtrler) Kill() {
	atomic.StoreInt32(&sc.dead, 1)
	sc.rf.Kill()
}

func (sc *ShardCtrler) killed() bool {
	return atomic.LoadInt32(&sc.dead) == 1
}

// needed by shardkv tester
func (sc *ShardCtrler) Raft() *raft.Raft {
	return sc.rf
}

func (sc *ShardCtrler) isDuplicateRequest(clientId int64, commandId int64) bool {
	operationContext, ok := sc.lastOperations[clientId]
	return ok && commandId <= operationContext.MaxAppliedCommandId
}

func (sc *ShardCtrler) getNotifyChan(index int) chan *CommandReply {
	ch, ok := sc.notifyChannels[index]
	if !ok {
		ch = make(chan *CommandReply, 1)
		sc.notifyChannels[index] = ch
	}
	return ch
}

func (sc *ShardCtrler) removeOutdatedNotifyChan(index int) {
	delete(sc.notifyChannels, index)
}

func (sc *ShardCtrler) applyLogToStateMachine(command Command) *CommandReply {
	reply := new(CommandReply)
	switch command.Op {
	case Join:
		reply.Err = sc.configStateMachine.Join(command.Servers)
	case Leave:
		reply.Err = sc.configStateMachine.Leave(command.GIDs)
	case Move:
		reply.Err = sc.configStateMachine.Move(command.Shard, command.GID)
	case Query:
		reply.Config, reply.Err = sc.configStateMachine.Query(command.Num)
	}
	return reply
}

func (sc *ShardCtrler) Command(args *CommandArgs, reply *CommandReply) {
	sc.mu.RLock()
	if args.Op != Query && sc.isDuplicateRequest(args.ClientId, args.CommandId) {
		LastReply := sc.lastOperations[args.ClientId].LastReply
		reply.Err, reply.Config = LastReply.Err, LastReply.Config
	}
	sc.mu.RUnlock()
	index, _, isLeader := sc.rf.Start(Command{args})
	if !isLeader {
		reply.Err = ErrWrongLeader
		return
	}
	sc.mu.Lock()
	notifyChan := sc.getNotifyChan(index)
	sc.mu.Unlock()

	select {
	case result := <-notifyChan:
		reply.Err, reply.Config = result.Err, result.Config
	case <-time.After(ExecuteTimeout):
		reply.Err = ErrTimeout
	}
	go func() {
		sc.mu.Lock()
		delete(sc.notifyChannels, index)
		sc.mu.Unlock()
	}()
}

func (sc *ShardCtrler) applier() {
	for sc.killed() == false {
		select {
		case applyMsg := <-sc.applyCh:
			if applyMsg.CommandValid {
				var reply *CommandReply
				command := applyMsg.Command.(Command)
				sc.mu.Lock()
				if command.Op != Query && sc.isDuplicateRequest(command.ClientId, command.CommandId) {
					reply = sc.lastOperations[command.ClientId].LastReply
				} else {
					reply = sc.applyLogToStateMachine(command)
					if command.Op != Query {
						sc.lastOperations[command.ClientId] = OperationContext{
							MaxAppliedCommandId: command.CommandId,
							LastReply:           reply,
						}
					}
				}
				// only notify related channel for currentTerm's log when node is leader
				if currentTerm, isLeader := sc.rf.GetState(); isLeader && applyMsg.CommandTerm == currentTerm {
					notifyChan := sc.getNotifyChan(applyMsg.CommandIndex)
					notifyChan <- reply
				}
				sc.mu.Unlock()
			} else {
				panic(fmt.Sprintf("unexpected Message %v", applyMsg))
			}
		}
	}
}

// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant shardctrler service.
// me is the index of the current server in servers[].
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister) *ShardCtrler {
	labgob.Register(Command{})
	applyCh := make(chan raft.ApplyMsg)
	sc := &ShardCtrler{
		mu:                 sync.RWMutex{},
		dead:               0,
		rf:                 raft.Make(servers, me, persister, applyCh),
		applyCh:            applyCh,
		configStateMachine: NewMemoryConfigStateMachine(),
		lastOperations:     make(map[int64]OperationContext),
		notifyChannels:     make(map[int]chan *CommandReply),
	}
	// start applier goroutine to apply committed logs to stateMachine
	go sc.applier()
	DPrintf("{ShardCtrler %v} has started", sc.rf.GetId())
	return sc
}
