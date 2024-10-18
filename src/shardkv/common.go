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

const Debug = false

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
	default:
		panic(fmt.Sprintf("Unknown error: %d", err))
	}
}

type ShardStatus uint8

// ShardStatus type representing the state of a shard
const (
	Serving   ShardStatus = iota // shard is serving requests
	Pulling                      // shard is pulling data from other groups
	BePulling                    // shard is being pulled by other groups
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
	default:
		panic(fmt.Sprintf("Unknown ShardStatus: %d", status))
	}
}

// CommandType representing different types of commands
type CommandType uint8

const (
	Operation     CommandType = iota // Generic operation command
	Configuration                    // Configuration change command
	InsertShards                     // Command to insert shards
	DeleteShards                     // Command to delete shards
	EmptyShards                      // Command to empty shards
)

func (commandType CommandType) String() string {
	switch commandType {
	case Operation:
		return "Operation"
	case Configuration:
		return "Configuration"
	case InsertShards:
		return "InsertShards"
	case DeleteShards:
		return "DeleteShards"
	case EmptyShards:
		return "EmptyShards"
	default:
		panic(fmt.Sprintf("Unknown CommandType: %d", commandType))
	}
}

// OperationType representing various operation types
type OperationType uint8

const (
	Get OperationType = iota
	Put
	Append
)

func (op OperationType) String() string {
	switch op {
	case Get:
		return "Get"
	case Put:
		return "Put"
	case Append:
		return "Append"
	default:
		panic(fmt.Sprintf("Unknown OperationType: %d", op))
	}
}

// CommandArgs structure holding arguments for a command
type CommandArgs struct {
	Key       string
	Value     string
	Op        OperationType
	ClientId  int64
	CommandId int64
}

func (args *CommandArgs) String() string {
	return fmt.Sprintf("CommandArgs{Key: %s, Value: %s, Op: %s, ClientId: %d, CommandId: %d}", args.Key, args.Value, args.Op, args.ClientId, args.CommandId)
}

// CommandReply structure for replies from command execution
type CommandReply struct {
	Err   Err
	Value string
}

func (reply *CommandReply) String() string {
	return fmt.Sprintf("CommandReply{Err: %s, Value: %s}", reply.Err, reply.Value)
}

// OperationContext stores information about the operation's execution context
type OperationContext struct {
	MaxAppliedCommandId int64
	LastReply           *CommandReply
}

func (operationContext OperationContext) String() string {
	return fmt.Sprintf("OperationContext{MaxAppliedCommandId: %d, LastReply: %v}", operationContext.MaxAppliedCommandId, operationContext.LastReply)
}

// deepCopy creates a copy of OperationContext
func (operationContext OperationContext) deepCopy() OperationContext {
	return OperationContext{
		MaxAppliedCommandId: operationContext.MaxAppliedCommandId,
		LastReply: &CommandReply{
			Err:   operationContext.LastReply.Err,
			Value: operationContext.LastReply.Value},
	}
}

// ShardOperationArgs structure for arguments related to shard operations
type ShardOperationArgs struct {
	ConfigNum int
	ShardIDs  []int
}

func (args *ShardOperationArgs) String() string {
	return fmt.Sprintf("ShardOperationArgs{ConfigNum: %d, ShardIDs: %v}", args.ConfigNum, args.ShardIDs)
}

// ShardOperationReply structure for replies from shard operations
type ShardOperationReply struct {
	Err            Err
	ConfigNum      int
	Shards         map[int]map[string]string
	LastOperations map[int64]OperationContext
}

func (reply *ShardOperationReply) String() string {
	return fmt.Sprintf("ShardOperationReply{Err: %s, ConfigNum: %d, Shards: %v, LastOperations: %v}", reply.Err, reply.ConfigNum, reply.Shards, reply.LastOperations)
}

// Command structure representing a command to be executed
type Command struct {
	CommandType CommandType
	Data        interface{}
}

func (command Command) String() string {
	return fmt.Sprintf("Command{commandType: %s, Data: %v}", command.CommandType, command.Data)
}

// NewOperationCommand creates a new operation command from CommandArgs
func NewOperationCommand(args *CommandArgs) Command {
	return Command{Operation, *args}
}

// NewConfigurationCommand creates a new configuration command
func NewConfigurationCommand(config *shardctrler.Config) Command {
	return Command{Configuration, *config}
}

// NewInsertShardsCommand creates a new command to insert shards
func NewInsertShardsCommand(reply *ShardOperationReply) Command {
	return Command{InsertShards, *reply}
}

// NewDeleteShardsCommand creates a new command to delete shards
func NewDeleteShardsCommand(args *ShardOperationArgs) Command {
	return Command{DeleteShards, *args}
}

// NewEmptyShardsCommand creates a new command indicating no shards
func NewEmptyShardsCommand() Command {
	return Command{EmptyShards, nil}
}
