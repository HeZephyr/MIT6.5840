package shardctrler

import (
	"6.5840/raft"
	"sync/atomic"
	"time"
)
import "6.5840/labrpc"
import "sync"
import "6.5840/labgob"

type ShardCtrler struct {
	mu      sync.RWMutex
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	// Your data here.
	dead           int32 // set by Kill()
	stateMachine   ConfigStateMachine
	lastOperations map[int64]OperationContext
	notifyChans    map[int]chan *CommandReply
}

func (sc *ShardCtrler) Command(args *CommandArgs, reply *CommandReply) {
	sc.mu.RLock()
	if args.Op != Query && sc.isDuplicateRequest(args.ClientId, args.CommandId) {
		LastReply := sc.lastOperations[args.ClientId].LastReply
		reply.Config, reply.Err = LastReply.Config, LastReply.Err
		sc.mu.RUnlock()
		return
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
		reply.Config, reply.Err = result.Config, result.Err
	case <-time.After(ExecuteTimeout):
		reply.Err = ErrTimeout
	}
	go func() {
		sc.mu.Lock()
		sc.removeOutdatedNotifyChan(index)
		sc.mu.Unlock()
	}()
}

func (sc *ShardCtrler) isDuplicateRequest(clientId int64, commandId int64) bool {
	OperationContext, ok := sc.lastOperations[clientId]
	return ok && commandId <= OperationContext.MaxAppliedCommandId
}

func (sc *ShardCtrler) getNotifyChan(index int) chan *CommandReply {
	notifyChan, ok := sc.notifyChans[index]
	if !ok {
		notifyChan = make(chan *CommandReply, 1)
		sc.notifyChans[index] = notifyChan
	}
	return notifyChan
}

func (sc *ShardCtrler) removeOutdatedNotifyChan(index int) {
	delete(sc.notifyChans, index)
}

func (sc *ShardCtrler) applier() {
	for !sc.killed() {
		select {
		case message := <-sc.applyCh:
			if message.CommandValid {
				reply := new(CommandReply)
				command := message.Command.(Command)
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

				if currentTerm, isLeader := sc.rf.GetState(); isLeader && message.CommandTerm == currentTerm {
					notifyChan := sc.getNotifyChan(message.CommandIndex)
					notifyChan <- reply
				}
				sc.mu.Unlock()
			}
		}
	}
}

func (sc *ShardCtrler) applyLogToStateMachine(command Command) *CommandReply {
	reply := new(CommandReply)
	switch command.Op {
	case Join:
		reply.Err = sc.stateMachine.Join(command.Servers)
	case Leave:
		reply.Err = sc.stateMachine.Leave(command.GIDs)
	case Move:
		reply.Err = sc.stateMachine.Move(command.Shard, command.GID)
	case Query:
		reply.Config, reply.Err = sc.stateMachine.Query(command.Num)
	}
	return reply
}

// the tester calls Kill() when a ShardCtrler instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
func (sc *ShardCtrler) Kill() {
	sc.rf.Kill()
	// Your code here, if desired.
	atomic.StoreInt32(&sc.dead, 1)
}

func (sc *ShardCtrler) killed() bool {
	return atomic.LoadInt32(&sc.dead) == 1
}

// needed by shardkv tester
func (sc *ShardCtrler) Raft() *raft.Raft {
	return sc.rf
}

// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant shardctrler service.
// me is the index of the current server in servers[].
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister) *ShardCtrler {
	labgob.Register(Command{})
	apply := make(chan raft.ApplyMsg)

	sc := &ShardCtrler{
		rf:             raft.Make(servers, me, persister, apply),
		applyCh:        apply,
		stateMachine:   NewMemoryConfigStateMachine(),
		lastOperations: make(map[int64]OperationContext),
		notifyChans:    make(map[int]chan *CommandReply),
		dead:           0,
	}
	go sc.applier()
	return sc
}
