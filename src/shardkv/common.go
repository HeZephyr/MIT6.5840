package shardkv

import (
	"6.5840/shardctrler"
	"fmt"
	"log"
	"time"
)

//
// Sharded key/value server.
// Lots of replica groups, each running Raft.
// Shardctrler decides which group serves each shard.
// Shardctrler may change shard assignment from time to time.
//
// You will have to modify these definitions.
//

const Debug = true

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

const (
	ExecuteTimeout              = 500 * time.Millisecond
	ConfigurationMonitorTimeout = 100 * time.Millisecond
	MigrationMonitorTimeout     = 50 * time.Millisecond
	GCMonitorTimeout            = 50 * time.Millisecond
	EmptyEntryDetectorTimeout   = 200 * time.Millisecond
)

type Err uint8

const (
	OK Err = iota
	ErrNoKey
	ErrWrongGroup
	ErrWrongLeader
	ErrOutDated
	ErrTimeout
	ErrNotReady
)

func (err Err) String() string {
	switch err {
	case OK:
		return "OK"
	case ErrNoKey:
		return "ErrNoKey"
	case ErrWrongGroup:
		return "ErrWrongGroup"
	case ErrWrongLeader:
		return "ErrWrongLeader"
	case ErrOutDated:
		return "ErrOutDated"
	case ErrTimeout:
		return "ErrTimeout"
	case ErrNotReady:
		return "ErrNotReady"
	}
	panic(fmt.Sprintf("unexpected error: %d", err))
}

type OpType uint8

const (
	Get OpType = iota
	Put
	Append
)

func (op OpType) String() string {
	switch op {
	case Get:
		return "Get"
	case Put:
		return "Put"
	case Append:
		return "Append"
	}
	panic(fmt.Sprintf("unexpected operation: %d", op))
}

type CommandArgs struct {
	Key       string
	Value     string
	Op        OpType
	ClientId  int64
	CommandId int64
}

func (args CommandArgs) String() string {
	return fmt.Sprintf("{Key: %v, Value: %v, Op: %v, ClientId: %v, CommandId: %v}", args.Key, args.Value, args.Op, args.ClientId, args.CommandId)
}

type CommandReply struct {
	Err   Err
	Value string
}

func (reply CommandReply) String() string {
	return fmt.Sprintf("{Err: %v, Value: %v}", reply.Err, reply.Value)
}

type ShardStatus uint8

const (
	Serving ShardStatus = iota
	Pulling
	BePulling
	GCing
)

func (status ShardStatus) String() string {
	switch status {
	case Serving:
		return "Serving"
	case Pulling:
		return "Pulling"
	case BePulling:
		return "BePulling"
	case GCing:
		return "GCing"
	}
	panic(fmt.Sprintf("unexpected ShardStatus %d", status))
}

type OperationContext struct {
	MaxAppliedCommandId int64
	LastReply           *CommandReply
}

func (operationContext OperationContext) String() string {
	return fmt.Sprintf("{MaxAppliedCommandId: %v, LastReply: %v}", operationContext.MaxAppliedCommandId, operationContext.LastReply)
}

func (operationContext OperationContext) deepCopy() OperationContext {
	return OperationContext{
		MaxAppliedCommandId: operationContext.MaxAppliedCommandId,
		LastReply:           &CommandReply{operationContext.LastReply.Err, operationContext.LastReply.Value},
	}
}

type CommandType uint8

const (
	Operation     CommandType = iota // such as Get, Put, Append
	Configuration                    // such as Join, Leave, Move
	InsertShards                     // insert shards into the shardkv
	DeleteShards                     // delete shards from the shardkv
	EmptyEntry                       // serves as a placeholder in the log or for consistency purposes.
)

func (op CommandType) String() string {
	switch op {
	case Operation:
		return "Operation"
	case Configuration:
		return "Configuration"
	case InsertShards:
		return "InsertShards"
	case DeleteShards:
		return "DeleteShards"
	case EmptyEntry:
		return "EmptyEntry"
	}
	panic(fmt.Sprintf("unexpected CommandType %d", op))
}

type Command struct {
	Op   CommandType
	Data interface{}
}

func (command Command) String() string {
	return fmt.Sprintf("{Type:%v,Data:%v}", command.Op, command.Data)
}

func NewOperationCommand(args *CommandArgs) Command {
	return Command{Operation, *args}
}

func NewConfigurationCommand(config *shardctrler.Config) Command {
	return Command{Configuration, *config}
}

func NewInsertShardsCommand(reply *ShardOperationReply) Command {
	return Command{InsertShards, *reply}
}

func NewDeleteShardsCommand(args *ShardOperationArgs) Command {
	return Command{DeleteShards, *args}
}

func NewEmptyEntryCommand() Command {
	return Command{EmptyEntry, nil}
}

type ShardOperationArgs struct {
	ConfigNum int
	ShardIds  []int
}

func (args ShardOperationArgs) String() string {
	return fmt.Sprintf("{ConfigNum: %v, ShardIds: %v}", args.ConfigNum, args.ShardIds)
}

type ShardOperationReply struct {
	Err            Err
	ConfigNum      int
	Shards         map[int]map[string]string
	LastOperations map[int64]OperationContext
}

func (reply ShardOperationReply) String() string {
	return fmt.Sprintf("{Err: %v, ConfigNum: %v, Shards: %v, LastOperations: %v}", reply.Err, reply.ConfigNum, reply.Shards, reply.LastOperations)
}
