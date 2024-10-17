package shardctrler

//
// Shardctrler clerk.
//

import "6.5840/labrpc"
import "crypto/rand"
import "math/big"

// Clerk represents a client that interacts with the shard controller.
type Clerk struct {
	servers []*labrpc.ClientEnd
	// Your data here.
	leaderId  int64
	clientId  int64
	commandId int64
}

// nrand generates a random 62-bit integer.
func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

// MakeClerk creates a new Clerk instance.
func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	return &Clerk{
		servers:   servers,
		leaderId:  0,
		clientId:  nrand(),
		commandId: 0,
	}
}

// Join sends a Join command to the shard controller.
func (ck *Clerk) Join(servers map[int][]string) {
	args := &CommandArgs{Op: Join, Servers: servers}
	ck.Command(args)
}

// Leave sends a Leave command to the shard controller.
func (ck *Clerk) Leave(gids []int) {
	args := &CommandArgs{Op: Leave, GIDs: gids}
	ck.Command(args)
}

// Move sends a Move command to the shard controller.
func (ck *Clerk) Move(shard int, gid int) {
	args := &CommandArgs{Op: Move, Shard: shard, GID: gid}
	ck.Command(args)
}

// Query sends a Query command to the shard controller and returns the configuration.
func (ck *Clerk) Query(num int) Config {
	args := &CommandArgs{Op: Query, Num: num}
	return ck.Command(args)
}

// Command sends a command to the shard controller.
// It tries to send the command to the current leader.
// If the leader is wrong or the call times out,
// it tries the next server in the list as the leader.
// If the command is successfully sent, it increments the command ID and returns the configuration in the reply.
func (ck *Clerk) Command(args *CommandArgs) Config {
	args.ClientId, args.CommandId = ck.clientId, ck.commandId
	for {
		var reply CommandReply
		// If the call fails due to wrong leader or timeout, try the next server as leader.
		if !ck.servers[ck.leaderId].Call("ShardCtrler.Command", args, &reply) || reply.Err == ErrWrongLeader || reply.Err == ErrTimeout {
			ck.leaderId = (ck.leaderId + 1) % int64(len(ck.servers))
		} else {
			ck.commandId++
			return reply.Config
		}
	}
}
