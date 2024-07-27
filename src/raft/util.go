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

type NodeState int

// Raft state
const (
	Follower NodeState = iota
	Candidate
	Leader
)

// String representation of the node state
func (state NodeState) String() string {
	switch state {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	}
	panic(fmt.Sprintf("Unknown state: %d", state))
}

// confirm concurrency safety
type lockedRand struct {
	mu   sync.Mutex
	rand *rand.Rand
}

func (r *lockedRand) Intn(n int) int {
	// avoid many goroutines to access the random number generator at the same time
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Intn(n)
}

var globalRand = &lockedRand{
	rand: rand.New(rand.NewSource(time.Now().UnixNano())),
}

const (
	HeartbeatTimeout = 125
	ElectionTimeout  = 1000
)

func StableHeartbeatTimeout() time.Duration {
	return time.Duration(HeartbeatTimeout) * time.Millisecond
}

func RandomElectionTimeOut() time.Duration {
	return time.Duration(ElectionTimeout+globalRand.Intn(ElectionTimeout)) * time.Millisecond
}

func (args RequestVoteArgs) String() string {
	return fmt.Sprintf("{Term:%v,CandidateId:%v,LastLogIdx:%v,LastLogTerm:%v}", args.Term, args.CandidateId, args.LastLogIdx, args.LastLogTerm)
}

func (args RequestVoteReply) String() string {
	return fmt.Sprintf("{Term:%v,VoteGranted:%v}", args.Term, args.VoteGranted)
}

func (args AppendEntriesArgs) String() string {
	return fmt.Sprintf("{Term:%v,LeaderId:%v,PrevLogIdx:%v,PrevLogTerm:%v,LeaderCommit:%v,Entries:%v}", args.Term, args.LeaderId, args.PrevLogIndex, args.PrevLogTerm, args.LeaderCommit, args.Entries)
}

func (args AppendEntriesReply) String() string {
	return fmt.Sprintf("{Term:%v,Success:%v}", args.Term, args.Success)
}
