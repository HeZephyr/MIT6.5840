package shardkv

import (
	"fmt"
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

const (
	ExecuteTimeout            = 500 * time.Millisecond
	ConfigureMonitorTimeout   = 100 * time.Millisecond
	MigrationMonitorTimeout   = 50 * time.Millisecond
	GCMonitorTimeout          = 50 * time.Millisecond
	EmptyEntryDetectorTimeout = 200 * time.Millisecond
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
	panic(fmt.Sprintf("unexpected error: %v", err))
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
