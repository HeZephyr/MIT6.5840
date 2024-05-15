package kvsrv

type MessageType int

const (
	Modify = iota
	Report
)

// Put or Append
type PutAppendArgs struct {
	Key   string
	Value string
	// You'll have to add definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	MessageType MessageType // Modify or Report
	MessageID   int64       // Unique ID for each message
}

type PutAppendReply struct {
	Value string
}

type GetArgs struct {
	Key string
	// You'll have to add definitions here.
}

type GetReply struct {
	Value string
}
