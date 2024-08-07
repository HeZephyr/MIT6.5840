package raft

type RequestVoteArgs struct {
	// Your data here (3A, 3B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

type RequestVoteReply struct {
	// Your data here (3A).
	Term        int
	VoteGranted bool
}

func (rf *Raft) genRequestVoteArgs() *RequestVoteArgs {
	args := &RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: rf.getLastLog().Index,
		LastLogTerm:  rf.getLastLog().Term,
	}
	return args
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer DPrintf("{Node %v}'s state is {state %v, term %v}} after processing RequestVote,  RequestVoteArgs %v and RequestVoteReply %v ", rf.me, rf.state, rf.currentTerm, args, reply)
	// Reply false if term < currentTerm(§5.1)
	// if the term is same as currentTerm, and the votedFor is not null and not the candidateId, then reject the vote(§5.2)
	if args.Term < rf.currentTerm || (args.Term == rf.currentTerm && rf.votedFor != -1 && rf.votedFor != args.CandidateId) {
		reply.Term, reply.VoteGranted = rf.currentTerm, false
		return
	}

	if args.Term > rf.currentTerm {
		rf.ChangeState(Follower)
		rf.currentTerm, rf.votedFor = args.Term, -1
	}
	rf.votedFor = args.CandidateId
	rf.electionTimer.Reset(RandomElectionTimeout())
	reply.Term, reply.VoteGranted = rf.currentTerm, true
}

func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	LeaderCommit int
	Entries      []LogEntry
}

type AppendEntriesReply struct {
	Term    int
	Success bool
}

func (rf *Raft) genAppendEntriesArgs() *AppendEntriesArgs {
	args := &AppendEntriesArgs{
		Term:     rf.currentTerm,
		LeaderId: rf.me,
	}
	return args
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	// Your code here (3A, 3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer DPrintf("{Node %v}'s state is {state %v, term %v}} after processing AppendEntries,  AppendEntriesArgs %v and AppendEntriesReply %v ", rf.me, rf.state, rf.currentTerm, args, reply)

	// Reply false if term < currentTerm(§5.1)
	if args.Term < rf.currentTerm {
		reply.Term, reply.Success = rf.currentTerm, false
		return
	}
	// indicate the peer is the leader
	if args.Term > rf.currentTerm {
		rf.currentTerm, rf.votedFor = args.Term, -1
	}

	rf.ChangeState(Follower)
	rf.electionTimer.Reset(RandomElectionTimeout())

	reply.Term, reply.Success = rf.currentTerm, true
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}
