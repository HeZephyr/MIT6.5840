package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"sync"
	"sync/atomic"
	"time"

	//	"6.5840/labgob"
	"6.5840/labrpc"
)

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.RWMutex        // Lock to protect shared access to this peer's state, to use RWLock for better performance
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Persistent state on all servers(Updated on stable storage before responding to RPCs)
	currentTerm int        // latest term server has seen(initialized to 0 on first boot, increases monotonically)
	votedFor    int        // candidateId that received vote in current term(or null if none)
	logs        []LogEntry // log entries; each entry contains command for state machine, and term when entry was received by leader(first index is 1)

	// Volatile state on all servers
	commitIndex int // index of highest log entry known to be committed(initialized to 0, increases monotonically)
	lastApplied int // index of highest log entry applied to state machine(initialized to 0, increases monotonically)

	// Volatile state on leaders(Reinitialized after election)
	nextIndex  []int // for each server, index of the next log entry to send to that server(initialized to leader last log index + 1)
	matchIndex []int // for each server, index of highest log entry known to be replicated on server(initialized to 0, increases monotonically)

	// other properties
	state          NodeState     // current state of the server
	electionTimer  *time.Timer   // timer for election timeout
	heartbeatTimer *time.Timer   // timer for heartbeat
	applyCh        chan ApplyMsg // channel to send apply message to service
}

func (rf *Raft) ChangeState(state NodeState) {
	if rf.state == state {
		return
	}
	DPrintf("{Node %v} changes state from %v to %v", rf.me, rf.state, state)
	rf.state = state
	switch state {
	case Follower:
		rf.electionTimer.Reset(RandomElectionTimeout())
		rf.heartbeatTimer.Stop() // stop heartbeat
	case Candidate:
	case Leader:
		rf.electionTimer.Stop() // stop election
		rf.heartbeatTimer.Reset(StableHeartbeatTimeout())
	}
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.RLock()
	defer rf.mu.RUnlock()
	return rf.currentTerm, rf.state == Leader
}

func (rf *Raft) persist() {
	// Your code here (3C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// raftstate := w.Bytes()
	// rf.persister.Save(raftstate, nil)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (3C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (3D).

}

func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (3B).

	return index, term, isLeader
}

func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func (rf *Raft) StartElection() {
	rf.votedFor = rf.me
	args := rf.genRequestVoteArgs()
	grantedVotes := 1
	DPrintf("{Node %v} starts election with RequestVoteArgs %v", rf.me, args)
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		go func(peer int) {
			reply := new(RequestVoteReply)
			if rf.sendRequestVote(peer, args, reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()
				DPrintf("{Node %v} receives RequestVoteReply %v from {Node %v} after sending RequestVoteArgs %v", rf.me, reply, peer, args)
				if args.Term == rf.currentTerm && rf.state == Candidate {
					if reply.VoteGranted {
						grantedVotes += 1
						// check over half of the votes
						if grantedVotes > len(rf.peers)/2 {
							DPrintf("{Node %v} receives over half of the votes", rf.me)
							rf.ChangeState(Leader)
							rf.BroadcastHeartbeat()
						}
					} else if reply.Term > rf.currentTerm {
						rf.ChangeState(Follower)
						rf.currentTerm, rf.votedFor = reply.Term, -1
					}
				}
			}
		}(peer)
	}
}

func (rf *Raft) BroadcastHeartbeat() {
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		go func(peer int) {
			// send heartbeat
			rf.mu.RLock()
			if rf.state != Leader {
				rf.mu.RUnlock()
				return
			}
			// send heartbeat
			args := rf.genAppendEntriesArgs()
			rf.mu.RUnlock()
			reply := new(AppendEntriesReply)
			if rf.sendAppendEntries(peer, args, reply) {
				rf.mu.Lock()
				if args.Term == rf.currentTerm && rf.state == Leader {
					if !reply.Success {
						// handle failure
						if reply.Term > rf.currentTerm {
							rf.ChangeState(Follower)
							rf.currentTerm, rf.votedFor = reply.Term, -1
						}
					}
				}
				rf.mu.Unlock()
			}
		}(peer)
	}
}

func (rf *Raft) ticker() {
	for rf.killed() == false {
		select {
		case <-rf.electionTimer.C:
			// start election
			rf.mu.Lock()
			rf.ChangeState(Candidate)
			rf.currentTerm += 1
			// start election
			rf.StartElection()
			rf.electionTimer.Reset(RandomElectionTimeout()) // reset election timer in case of split vote
			rf.mu.Unlock()
		case <-rf.heartbeatTimer.C:
			// send heartbeat
			rf.mu.Lock()
			if rf.state == Leader {
				// should send heartbeat
				rf.BroadcastHeartbeat()
				rf.heartbeatTimer.Reset(StableHeartbeatTimeout())
			}
			rf.mu.Unlock()
		}
	}
}

func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{
		mu:             sync.RWMutex{},
		peers:          peers,
		persister:      persister,
		me:             me,
		dead:           0,
		currentTerm:    0,
		votedFor:       -1,
		logs:           make([]LogEntry, 1), // dummy entry at index 0
		commitIndex:    0,
		lastApplied:    0,
		nextIndex:      make([]int, len(peers)),
		matchIndex:     make([]int, len(peers)),
		state:          Follower,
		electionTimer:  time.NewTimer(RandomElectionTimeout()),
		heartbeatTimer: time.NewTimer(StableHeartbeatTimeout()),
		applyCh:        applyCh,
	}

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()

	return rf
}
