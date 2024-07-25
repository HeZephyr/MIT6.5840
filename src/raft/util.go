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
type lockRand struct {
	mu   sync.Mutex
	rand *rand.Rand
}

func (r *lockRand) Intn(n int) int {
	// avoid many goroutines to access the random number generator at the same time
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Intn(n)
}

const (
	HeartbeatTimeout = 125
	ElectionTimeout  = 1000
)

func StableHeartbeatTimeout() time.Duration {
	return time.Duration(HeartbeatTimeout) * time.Millisecond
}

func RandomElectionTimeOut() time.Duration {
	return time.Duration(ElectionTimeout+rand.Intn(ElectionTimeout)) * time.Millisecond
}
