package shardctrler

//
// Shardctrler clerk.
//

import "6.5840/labrpc"
import "crypto/rand"
import "math/big"

type Clerk struct {
	servers []*labrpc.ClientEnd
	// Your data here.
	leaderId  int64
	clientId  int64
	commandId int64
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	return &Clerk{
		servers:   servers,
		leaderId:  0,
		clientId:  nrand(),
		commandId: 0,
	}
}

func (ck *Clerk) Join(servers map[int][]string) {
	args := &CommandArgs{Op: Join, Servers: servers}
	ck.Command(args)
}

func (ck *Clerk) Leave(gids []int) {
	args := &CommandArgs{Op: Leave, GIDs: gids}
	ck.Command(args)
}

func (ck *Clerk) Move(shard int, gid int) {
	args := &CommandArgs{Op: Move, Shard: shard, GID: gid}
	ck.Command(args)
}

func (ck *Clerk) Query(num int) Config {
	args := &CommandArgs{Op: Query, Num: num}
	return ck.Command(args)
}

func (ck *Clerk) Command(args *CommandArgs) Config {
	args.ClientId, args.CommandId = ck.clientId, ck.commandId
	for {
		var reply CommandReply
		if !ck.servers[ck.leaderId].Call("ShardCtrler.Command", args, &reply) || reply.Err == ErrWrongLeader || reply.Err == ErrTimeout {
			ck.leaderId = (ck.leaderId + 1) % int64(len(ck.servers))
		} else {
			ck.commandId++
			return reply.Config
		}
	}
}
