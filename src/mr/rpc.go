package mr

//
// RPC definitions.
//
// remember to capitalize all names.
//

import (
	"os"
)
import "strconv"

type TaskType int

const (
	MapTask = iota
	ReduceTask
	Wait
	Exit
)

type TaskCompletedStatus int

const (
	MapTaskCompleted = iota
	MapTaskFailed
	ReduceTaskCompleted
	ReduceTaskFailed
)

// MessageSend is the struct of message send
type MessageSend struct {
	TaskID              int                 // task id
	TaskCompletedStatus TaskCompletedStatus // task completed status
}

// MessageReply is the struct of message reply
type MessageReply struct {
	TaskID   int      // task id
	TaskType TaskType // task type, map or reduce or wait or exit
	TaskFile string   // task file name
	NReduce  int      // reduce number, indicate the number of reduce tasks
	NMap     int      // map number, indicate the number of map tasks
}

// Cook up a unique-ish UNIX-domain socket name
// in /var/tmp, for the coordinator.
// Can't use the current directory since
// Athena AFS doesn't support UNIX-domain sockets.
func coordinatorSock() string {
	s := "/var/tmp/5840-mr-"
	s += strconv.Itoa(os.Getuid())
	return s
}
