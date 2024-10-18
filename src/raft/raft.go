package raft

import (
	"6.5840/labgob"
	"bytes"
	"sort"
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
	applyCond      *sync.Cond    // condition variable for apply goroutine
	replicatorCond []*sync.Cond  // condition variable for replicator goroutine
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

func (rf *Raft) GetRaftStateSize() int {
	return rf.persister.RaftStateSize()
}

func (rf *Raft) GetId() int {
	return rf.me
}

func (rf *Raft) encodeState() []byte {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.logs)
	return w.Bytes()
}

func (rf *Raft) persist() {
	rf.persister.SaveStateAndSnapshot(rf.encodeState(), nil)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm, votedFor int
	var logs []LogEntry
	if d.Decode(&currentTerm) != nil || d.Decode(&votedFor) != nil || d.Decode(&logs) != nil {
		DPrintf("{Node %v} fails to decode persisted state", rf.me)
	}
	rf.currentTerm, rf.votedFor, rf.logs = currentTerm, votedFor, logs
	rf.lastApplied, rf.commitIndex = rf.getFirstLog().Index, rf.getFirstLog().Index
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	snapshotIndex := rf.getFirstLog().Index
	if index <= snapshotIndex || index > rf.getLastLog().Index {
		DPrintf("{Node %v} rejects replacing log with snapshotIndex %v as current snapshotIndex %v is larger in term %v", rf.me, index, snapshotIndex, rf.currentTerm)
		return
	}
	// remove log entries up to index
	rf.logs = shrinkEntries(rf.logs[index-snapshotIndex:])
	rf.logs[0].Command = nil
	rf.persister.SaveStateAndSnapshot(rf.encodeState(), snapshot)
	DPrintf("{Node %v}'s state is {state %v,term %v,commitIndex %v,lastApplied %v,firstLog %v,lastLog %v} after accepting the snapshot with index %v", rf.me, rf.state, rf.currentTerm, rf.commitIndex, rf.lastApplied, rf.getFirstLog(), rf.getLastLog(), index)
}

func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// outdated snapshot
	if lastIncludedIndex <= rf.commitIndex {
		DPrintf("{Node %v} rejects outdated snapshot with lastIncludeIndex %v as current commitIndex %v is larger in term %v", rf.me, lastIncludedIndex, rf.commitIndex, rf.currentTerm)
		return false
	}
	// need dummy entry at index 0
	if lastIncludedIndex > rf.getLastLog().Index {
		rf.logs = make([]LogEntry, 1)
	} else {
		rf.logs = shrinkEntries(rf.logs[lastIncludedIndex-rf.getFirstLog().Index:])
		rf.logs[0].Command = nil
	}
	rf.logs[0].Term, rf.logs[0].Index = lastIncludedTerm, lastIncludedIndex
	rf.commitIndex, rf.lastApplied = lastIncludedIndex, lastIncludedIndex
	rf.persister.SaveStateAndSnapshot(rf.encodeState(), snapshot)

	DPrintf("{Node %v}'s state is {state %v,term %v,commitIndex %v,lastApplied %v,firstLog %v,lastLog %v} after accepting the snapshot which lastIncludedTerm is %v, lastIncludedIndex is %v", rf.me, rf.state, rf.currentTerm, rf.commitIndex, rf.lastApplied, rf.getFirstLog(), rf.getLastLog(), lastIncludedTerm, lastIncludedIndex)
	return true
}

func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.state != Leader {
		return -1, -1, false
	}
	// first append log entry for itself
	newLogIndex := rf.getLastLog().Index + 1
	rf.logs = append(rf.logs, LogEntry{
		Term:    rf.currentTerm,
		Command: command,
		Index:   newLogIndex,
	})
	rf.persist()
	rf.matchIndex[rf.me], rf.nextIndex[rf.me] = newLogIndex, newLogIndex+1
	DPrintf("{Node %v} starts agreement on a new log entry with command %v in term %v", rf.me, command, rf.currentTerm)
	// then broadcast to all peers to append log entry
	rf.BroadcastHeartbeat(false)
	// return the new log index and term, and whether this server is the leader
	return newLogIndex, rf.currentTerm, true
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
	rf.persist()
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
							rf.BroadcastHeartbeat(true)
						}
					} else if reply.Term > rf.currentTerm {
						rf.ChangeState(Follower)
						rf.currentTerm, rf.votedFor = reply.Term, -1
						rf.persist()
					}
				}
			}
		}(peer)
	}
}

func (rf *Raft) BroadcastHeartbeat(isHeartbeat bool) {
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		if isHeartbeat {
			// should send heartbeat to all peers immediately
			go rf.replicateOnceRound(peer)
		} else {
			// just need to signal replicator to send log entries to peer
			rf.replicatorCond[peer].Signal()
		}
	}
}

func (rf *Raft) isLogUpToDate(index, term int) bool {
	lastLog := rf.getLastLog()
	return term > lastLog.Term || (term == lastLog.Term && index >= lastLog.Index)
}

func (rf *Raft) isLogMatched(index, term int) bool {
	return index <= rf.getLastLog().Index && term == rf.logs[index-rf.getFirstLog().Index].Term
}

func (rf *Raft) advanceCommitIndexForLeader() {
	n := len(rf.matchIndex)
	sortMatchIndex := make([]int, n)
	copy(sortMatchIndex, rf.matchIndex)
	sort.Ints(sortMatchIndex)
	// get the index of the log entry with the highest index that is known to be replicated on a majority of servers
	newCommitIndex := sortMatchIndex[n-(n/2+1)]
	if newCommitIndex > rf.commitIndex {
		if rf.isLogMatched(newCommitIndex, rf.currentTerm) {
			DPrintf("{Node %v} advances commitIndex from %v to %v in term %v", rf.me, rf.commitIndex, newCommitIndex, rf.currentTerm)
			rf.commitIndex = newCommitIndex
			rf.applyCond.Signal()
		}
	}
}

func (rf *Raft) replicateOnceRound(peer int) {
	rf.mu.RLock()
	if rf.state != Leader {
		rf.mu.RUnlock()
		return
	}
	prevLogIndex := rf.nextIndex[peer] - 1
	if prevLogIndex < rf.getFirstLog().Index {
		// only send InstallSnapshot RPC
		args := rf.genInstallSnapshotArgs()
		rf.mu.RUnlock()
		reply := new(InstallSnapshotReply)
		if rf.sendInstallSnapshot(peer, args, reply) {
			rf.mu.Lock()
			if rf.state == Leader && rf.currentTerm == args.Term {
				if reply.Term > rf.currentTerm {
					rf.ChangeState(Follower)
					rf.currentTerm, rf.votedFor = reply.Term, -1
					rf.persist()
				} else {
					rf.nextIndex[peer] = args.LastIncludedIndex + 1
					rf.matchIndex[peer] = args.LastIncludedIndex
				}
			}
			rf.mu.Unlock()
			DPrintf("{Node %v} sends InstallSnapshotArgs %v to {Node %v} and receives InstallSnapshotReply %v", rf.me, args, peer, reply)
		}
	} else {
		args := rf.genAppendEntriesArgs(prevLogIndex)
		rf.mu.RUnlock()
		reply := new(AppendEntriesReply)
		if rf.sendAppendEntries(peer, args, reply) {
			rf.mu.Lock()
			if args.Term == rf.currentTerm && rf.state == Leader {
				if !reply.Success {
					if reply.Term > rf.currentTerm {
						// indicate current server is not the leader
						rf.ChangeState(Follower)
						rf.currentTerm, rf.votedFor = reply.Term, -1
						rf.persist()
					} else if reply.Term == rf.currentTerm {
						// decrease nextIndex and retry
						rf.nextIndex[peer] = reply.ConflictIndex
						// TODO: optimize the nextIndex finding, maybe use binary search
						if reply.ConflictTerm != -1 {
							firstLogIndex := rf.getFirstLog().Index
							for index := args.PrevLogIndex - 1; index >= firstLogIndex; index-- {
								if rf.logs[index-firstLogIndex].Term == reply.ConflictTerm {
									rf.nextIndex[peer] = index
									break
								}
							}
						}
					}
				} else {
					rf.matchIndex[peer] = args.PrevLogIndex + len(args.Entries)
					rf.nextIndex[peer] = rf.matchIndex[peer] + 1
					// advance commitIndex if possible
					rf.advanceCommitIndexForLeader()
				}
			}
			rf.mu.Unlock()
			DPrintf("{Node %v} sends AppendEntriesArgs %v to {Node %v} and receives AppendEntriesReply %v", rf.me, args, peer, reply)
		}
	}
}

func (rf *Raft) needReplicating(peer int) bool {
	rf.mu.RLock()
	defer rf.mu.RUnlock()
	// check the logs of peer is behind the leader
	return rf.state == Leader && rf.matchIndex[peer] < rf.getLastLog().Index
}

func (rf *Raft) replicator(peer int) {
	rf.replicatorCond[peer].L.Lock()
	defer rf.replicatorCond[peer].L.Unlock()
	for rf.killed() == false {
		for !rf.needReplicating(peer) {
			rf.replicatorCond[peer].Wait()
		}
		// send log entries to peer
		rf.replicateOnceRound(peer)
	}
}

func (rf *Raft) ticker() {
	for rf.killed() == false {
		select {
		case <-rf.electionTimer.C:
			rf.mu.Lock()
			rf.ChangeState(Candidate)
			rf.currentTerm += 1
			rf.persist()
			// start election
			rf.StartElection()
			rf.electionTimer.Reset(RandomElectionTimeout()) // reset election timer in case of split vote
			rf.mu.Unlock()
		case <-rf.heartbeatTimer.C:
			rf.mu.Lock()
			if rf.state == Leader {
				// should send heartbeat
				rf.BroadcastHeartbeat(true)
				rf.heartbeatTimer.Reset(StableHeartbeatTimeout())
			}
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) applier() {
	for rf.killed() == false {
		rf.mu.Lock()
		// check the commitIndex is advanced
		for rf.commitIndex <= rf.lastApplied {
			// need to wait for the commitIndex to be advanced
			rf.applyCond.Wait()
		}

		// apply log entries to state machine
		firstLogIndex, commitIndex, lastApplied := rf.getFirstLog().Index, rf.commitIndex, rf.lastApplied
		entries := make([]LogEntry, commitIndex-lastApplied)
		copy(entries, rf.logs[lastApplied-firstLogIndex+1:commitIndex-firstLogIndex+1])
		rf.mu.Unlock()
		// send the apply message to applyCh for service/State Machine Replica
		for _, entry := range entries {
			rf.applyCh <- ApplyMsg{
				CommandValid: true,
				Command:      entry.Command,
				CommandIndex: entry.Index,
				CommandTerm:  entry.Term,
			}
		}
		rf.mu.Lock()
		DPrintf("{Node %v} applies log entries from index %v to %v in term %v", rf.me, lastApplied+1, commitIndex, rf.currentTerm)
		// use commitIndex rather than rf.commitIndex because rf.commitIndex may change during the Unlock() and Lock()
		rf.lastApplied = Max(rf.lastApplied, commitIndex)
		rf.mu.Unlock()
	}
}

// used by upper layer to detect whether there are any logs in current term
func (rf *Raft) HasLogInCurrentTerm() bool {
	rf.mu.RLock()
	defer rf.mu.RUnlock()
	return rf.getLastLog().Term == rf.currentTerm
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
		replicatorCond: make([]*sync.Cond, len(peers)),
	}

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())
	// should use mu to protect applyCond, avoid other goroutine to change the critical section
	rf.applyCond = sync.NewCond(&rf.mu)
	// initialize nextIndex and matchIndex, and start replicator goroutine
	for peer := range peers {
		rf.matchIndex[peer], rf.nextIndex[peer] = 0, rf.getLastLog().Index+1
		if peer != rf.me {
			rf.replicatorCond[peer] = sync.NewCond(&sync.Mutex{})
			// start replicator goroutine to send log entries to peer
			go rf.replicator(peer)
		}
	}
	// start ticker goroutine to start elections
	go rf.ticker()
	// start apply goroutine to apply log entries to state machine
	go rf.applier()
	return rf
}
