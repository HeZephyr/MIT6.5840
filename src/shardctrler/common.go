package shardctrler

import (
	"fmt"
	"time"
)

//
// Shard controller: assigns shards to replication groups.
//
// RPC interface:
// Join(servers) -- add a set of groups (gid -> server-list mapping).
// Leave(gids) -- delete a set of groups.
// Move(shard, gid) -- hand off one shard from current owner to gid.
// Query(num) -> fetch Config # num, or latest config if num==-1.
//
// A Config (configuration) describes a set of replica groups, and the
// replica group responsible for each shard. Configs are numbered. Config
// #0 is the initial configuration, with no groups and all shards
// assigned to group 0 (the invalid group).
//
// You will need to add fields to the RPC argument structs.
//

// The number of shards.
const NShards = 10

const ExecuteTimeout = 500 * time.Millisecond

// A configuration -- an assignment of shards to groups.
// Please don't change this.
type Config struct {
	Num    int              // config number
	Shards [NShards]int     // shard -> gid
	Groups map[int][]string // gid -> servers[]
}

func DefaultConfig() Config {
	return Config{
		Groups: make(map[int][]string),
	}
}

func (cf Config) String() string {
	return fmt.Sprintf("{Num:%v,Shards:%v,Groups:%v}", cf.Num, cf.Shards, cf.Groups)
}

type OpType uint8

const (
	Join OpType = iota
	Leave
	Move
	Query
)

func (op OpType) String() string {
	switch op {
	case Join:
		return "Join"
	case Leave:
		return "Leave"
	case Move:
		return "Move"
	case Query:
		return "Query"
	default:
		panic(fmt.Sprintf("unknown operation type %d", op))
	}
}

type Err uint8

const (
	OK Err = iota
	ErrWrongLeader
	ErrTimeout
)

func (e Err) String() string {
	switch e {
	case OK:
		return "OK"
	case ErrWrongLeader:
		return "ErrWrongLeader"
	case ErrTimeout:
		return "ErrTimeout"
	default:
		panic(fmt.Sprintf("unknown error type %d", e))
	}
}

type CommandArgs struct {
	Servers   map[int][]string // for Join
	GIDs      []int            // for Leave
	Shard     int              // for Move
	GID       int              // for Move
	Num       int              // for Query
	Op        OpType
	ClientId  int64
	CommandId int64
}

func (args CommandArgs) String() string {
	switch args.Op {
	case Join:
		return fmt.Sprintf("{Servers:%v,Op:%v,ClientId:%v,CommandId:%v}", args.Servers, args.Op, args.ClientId, args.CommandId)
	case Leave:
		return fmt.Sprintf("{GIDs:%v,Op:%v,ClientId:%v,CommandId:%v}", args.GIDs, args.Op, args.ClientId, args.CommandId)
	case Move:
		return fmt.Sprintf("{Shard:%v,GID:%v,Op:%v,ClientId:%v,CommandId:%v}", args.Shard, args.GID, args.Op, args.ClientId, args.CommandId)
	case Query:
		return fmt.Sprintf("{Num:%v,Op:%v,ClientId:%v,CommandId:%v}", args.Num, args.Op, args.ClientId, args.CommandId)
	default:
		panic(fmt.Sprintf("unknown operation type %d", args.Op))
	}
}

type CommandReply struct {
	Err    Err
	Config Config
}

func (reply CommandReply) String() string {
	return fmt.Sprintf("{Err:%v,Config:%v}", reply.Err, reply.Config)
}

type OperationContext struct {
	MaxAppliedCommandId int64
	LastReply           *CommandReply
}

type Command struct {
	*CommandArgs
}
