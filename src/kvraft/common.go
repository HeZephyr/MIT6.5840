package kvraft

import (
	"fmt"
	"log"
	"time"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

const ExecuteTimeout = 1000 * time.Millisecond

type Err uint8

const (
	Ok Err = iota
	ErrNoKey
	ErrWrongLeader
	ErrTimeout
)

func (err Err) String() string {
	switch err {
	case Ok:
		return "Ok"
	case ErrNoKey:
		return "ErrNoKey"
	case ErrWrongLeader:
		return "ErrWrongLeader"
	case ErrTimeout:
		return "ErrTimeout"
	}
	panic(fmt.Sprintf("unexpected Err %d", err))
}

type OpType uint8

const (
	OpPut OpType = iota
	OpAppend
	OpGet
)

func (opType OpType) String() string {
	switch opType {
	case OpPut:
		return "Put"
	case OpAppend:
		return "Append"
	case OpGet:
		return "Get"
	}
	panic(fmt.Sprintf("unexpected OpType %d", opType))
}

type CommandArgs struct {
	Key       string
	Value     string
	Op        OpType
	ClientId  int64
	CommandId int64
}

func (args CommandArgs) String() string {
	return fmt.Sprintf("{Key:%v, Value:%v, Op:%v, ClientId:%v, Id:%v}", args.Key, args.Value, args.Op, args.ClientId, args.CommandId)
}

type CommandReply struct {
	Err   Err
	Value string
}

func (reply CommandReply) String() string {
	return fmt.Sprintf("{Err:%v, Value:%v}", reply.Err, reply.Value)
}

type OperationContext struct {
	MaxAppliedCommandId int64
	LastReply           *CommandReply
}

type Command struct {
	*CommandArgs
}
