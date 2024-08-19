package raft

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

// Debugging
const Debug = false

func DPrintf(format string, a ...interface{}) {
	if Debug {
		log.Printf(format, a...)
	}
}

const ElectionTimeout = 1000
const HeartbeatTimeout = 125

type LockedRand struct {
	mu   sync.Mutex
	rand *rand.Rand
}

func (r *LockedRand) Intn(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Intn(n)
}

var GlobalRand = &LockedRand{
	rand: rand.New(rand.NewSource(time.Now().UnixNano())),
}

func RandomElectionTimeout() time.Duration {
	return time.Duration(ElectionTimeout+GlobalRand.Intn(ElectionTimeout)) * time.Millisecond
}

func StableHeartbeatTimeout() time.Duration {
	return time.Duration(HeartbeatTimeout) * time.Millisecond
}

type ApplyMsg struct {
	// CommandTerm is not necessary because it is already applied to the state machine
	// since the application layer usually only cares about the order and content of logs
	CommandValid bool
	Command      interface{}
	CommandIndex int
	CommandTerm  int

	// For 3D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

type NodeState uint8

const (
	Follower NodeState = iota
	Candidate
	Leader
)

func (state NodeState) String() string {
	switch state {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	}
	panic("unexpected NodeState")
}

func (args RequestVoteArgs) String() string {
	return fmt.Sprintf("{Term:%v, CandidateId:%v, LastLogIndex:%v, LastLogTerm:%v}", args.Term, args.CandidateId, args.LastLogIndex, args.LastLogTerm)
}

func (reply RequestVoteReply) String() string {
	return fmt.Sprintf("{Term:%v, VoteGranted:%v}", reply.Term, reply.VoteGranted)
}

func (args AppendEntriesArgs) String() string {
	return fmt.Sprintf("{Term:%v, LeaderId:%v, PrevLogIndex:%v, PrevLogTerm:%v, LeaderCommit:%v, Entries:%v}", args.Term, args.LeaderId, args.PrevLogIndex, args.PrevLogTerm, args.LeaderCommit, args.Entries)
}

func (reply AppendEntriesReply) String() string {
	return fmt.Sprintf("{Term:%v, Success:%v, ConflictIndex:%v, ConflictTerm:%v}", reply.Term, reply.Success, reply.ConflictIndex, reply.ConflictTerm)
}

func (args InstallSnapshotArgs) String() string {
	return fmt.Sprintf("{Term:%v, LeaderId:%v, LastIncludedIndex:%v, LastIncludedTerm:%v, Data:%v}", args.Term, args.LeaderId, args.LastIncludedIndex, args.LastIncludedTerm, len(args.Data))
}

func (reply InstallSnapshotReply) String() string {
	return fmt.Sprintf("{Term:%v}", reply.Term)
}

type LogEntry struct {
	Command interface{}
	Term    int
	Index   int
}

func (rf *Raft) getLastLog() LogEntry {
	return rf.logs[len(rf.logs)-1]
}

func (rf *Raft) getFirstLog() LogEntry {
	return rf.logs[0]
}

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// shrinkEntriesArray discards the underlying array used by the entries slice
// if most of it isn't being used. This avoids holding references to a bunch of
// potentially large entries that aren't needed anymore. Simply clearing the
// entries wouldn't be safe because clients might still be using them.
func shrinkEntries(entries []LogEntry) []LogEntry {
	const lenMultiple = 2
	if cap(entries) > len(entries)*lenMultiple {
		newEntries := make([]LogEntry, len(entries))
		copy(newEntries, entries)
		return newEntries
	}
	return entries
}
