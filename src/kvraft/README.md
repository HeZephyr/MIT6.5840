# MIT 6.5840(6.824) Lab 4:Fault-tolerant Key/Value Service 设计实现

## 1 实验要求

<font color="red">本实验旨在利用lab 3中的Raft库，构建一个具备容错能力的键值存储服务</font>。服务将作为一个复制状态机，由多个服务器组成，各服务器通过Raft协议同步数据库状态。即使在部分故障或网络隔离的情况下，只要大多数服务器正常，服务仍需继续响应客户端请求。在lab 4完成后，你将实现图中Raft交互的所有部分（Clerk、Service和Raft）。

![](https://raw.githubusercontent.com/HeZephyr/NewPicGoLibrary/main/img/image-20240811160802518.png)

客户端通过Clerk与键值服务交互，发送RPC请求，支持Put、Append和Get三种操作。服务需确保这些操作线性化，如果逐个调用，这些方法应表现得好像系统只有一个状态副本，每个调用都应观察到前序调用序列对状态的修改。对于并发调用，返回值和最终状态必须与操作按某种顺序逐个执行时相同。如果调用在时间上重叠，则认为是并发调用。

为单一服务器提供线性化相对容易，但如果服务是复制的，则较为困难，因为所有服务器必须为并发请求选择相同的执行顺序，避免使用过时的状态回复客户端，并在故障恢复时以保留所有确认的客户端更新为前提。Raft 作者的[博士论文](https://web.stanford.edu/~ouster/cgi-bin/papers/OngaroPhD.pdf)的第 6.3 小节介绍了如何实现线性化语义，在知乎上也有关于这方面的[讨论]((https://www.zhihu.com/question/278551592))，可以参考 dragonboat 作者的回答。

实验分为两个阶段：A阶段实现基于Raft的键值服务，不使用快照；B阶段则集成快照功能，优化日志管理。

我的实验代码仓库：https://github.com/HeZephyr/MIT6.5840/tree/main/src/kvraft，已通过压力测试，代码严格遵守上述按要求实现。

<font color="red">注意：下述所贴代码为了简洁以及分块，进行了一定程度的删减，如果需要复现，可以前往仓库。</font>

## 2 实验设计

### 2.1 思路

lab4需要我们基于lab3实现的Raft，实现一个可用的KV服务，这意味着我们需要保证线性一致性（要求从外部观察者的角度来看，所有操作都按照某个全局顺序执行，并且结果与这些操作按该顺序串行执行的结果相同）。尽管 Raft 共识算法本身支持线性化语义，但要真正保证线性化语义在整个系统中生效，仍然需要上层服务的配合。

例如，在下面这张图中：x初始值为0，client1发送put请求(x,1)，client2发送put请求(x,2)，并在put请求前后发送get请求，此时如果put请求因为超时不断重发，如果在client2的put请求之后才被应用，则导致最后client2读到的是1，RaftKV的结果也是1，这就违背了线性一致性。

![image-20240822144442922](https://raw.githubusercontent.com/HeZephyr/NewPicGoLibrary/main/img/image-20240822144442922.png)

这是因为当客户端向服务端提交command时，服务端在Raft层中同步、提交并应用后，客户端因为没有收到请求回复，会重试此操作，这种重试机制会导致相同的命令被执行多次。<font color="red">注意，这里讨论的都是写请求，因为读请求不会改变系统状态，可以重复执行多次。</font>

为了解决重复执行命令导致线性一致性破坏的问题，Raft 作者提出了一种解决方案：客户端为每个命令分配一个唯一的序列号。状态机会记录每个客户端的最新序列号及其对应的执行结果。如果一个命令的序列号已经被处理过，则系统会直接返回先前的结果，而不会重新执行该命令。这样可以确保每个命令只被应用到状态机一次，避免了重复执行可能带来的线性一致性问题。

在这个lab中，我们可以按照如下机制具体实现：

1. **客户端命令唯一化**：每个客户端发送给服务端的每个`command`请求都携带一个由`ClientId`和`CommandId`组成的二元组。`ClientId`是客户端的唯一标识符，`CommandId`是一个递增的整数，用于唯一标识客户端发出的每一个命令。
2. **服务器端状态记录**：在服务器端，维护一个映射表，这个映射表以`ClientId`作为主键，其值是一个结构体包含：
    - 最近执行的来自该客户端的`CommandId`。
    - 对应的命令执行结果。
3. **重复命令检测与处理**：
    - 当一个新命令到达时，首先检查映射表中是否存在对应的`ClientId`条目。
    - 如果存在，则比较新命令的`CommandId`与映射表中记录的`CommandId`。
        - 如果新命令的`CommandId`小于或等于记录的`CommandId`，则说明这是一个重复命令，服务器可以直接返回之前存储的结果。
        - 如果新命令的`CommandId`大于记录的`CommandId`，则说明这是新的命令，服务器应该正常处理这个命令，并更新映射表中对应`ClientId`的`CommandId`及结果。
    - 如果不存在对应的`ClientId`条目，则将此命令视为首次出现的命令进行处理，并添加一个新的条目到映射表中。

### 2.2 lab4A：无快照

整体的时序图如下所示：

![image-20240829224033039](https://raw.githubusercontent.com/HeZephyr/NewPicGoLibrary/main/img/image-20240829224033039.png)

#### 2.2.1 客户端

对于客户端，需要有`(clientId, commandId)`来标识唯一命令，对于`clientId`，通过lab提供的随机数生成器`nrand`生成即可，对于`commandId`，可以采用递增的方式进行管理。这意味着每当客户端发送一个新的命令时，`commandId`都会递增一次，从而确保每个命令都有一个唯一的标识符，这样也需要保证如果这条命令没处理完（请求的server不是leader或者请求超时）需重复执行的时候，不能改变commandId。

```go
type CommandArgs struct {
	Key       string
	Value     string
	Op        OpType
	ClientId  int64
	CommandId int64
}

type CommandReply struct {
	Err   Err
	Value string
}

type Clerk struct {
	servers []*labrpc.ClientEnd
	leaderId  int
	clientId  int64
	commandId int64
}


func (ck *Clerk) Get(key string) string {
	return ck.ExecuteCommand(&CommandArgs{Key: key, Op: OpGet})
}

func (ck *Clerk) Put(key string, value string) {
	ck.ExecuteCommand(&CommandArgs{Key: key, Value: value, Op: OpPut})
}

func (ck *Clerk) Append(key string, value string) {
	ck.ExecuteCommand(&CommandArgs{Key: key, Value: value, Op: OpAppend})
}

func (ck *Clerk) ExecuteCommand(args *CommandArgs) string {
	args.ClientId, args.CommandId = ck.clientId, ck.commandId
	for {
		reply := new(CommandReply)
		if !ck.servers[ck.leaderId].Call("KVServer.ExecuteCommand", args, reply) || reply.Err == ErrWrongLeader || reply.Err == ErrTimeout {
			ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
			continue
		}
		ck.commandId += 1
		return reply.Value
	}
}
```

#### 2.2.2 服务端

`KVServer`结构体被设计成一个基于Raft一致性协议实现的键值存储服务。为了确保客户端请求的幂等性，并且能够正确地处理来自客户端的重复请求，`lastOperations`映射表用于跟踪每个客户端（由`clientId`标识）的最后已应用的`commandId`以及相应的`reply`。这使得服务器能够在接收到重复请求时返回之前的结果而无需再次执行相同的命令。

状态机`stateMachine`在此处被实现为内存中的键值对存储`MemoryKV`，这意味着所有的键值对数据都保存在内存中，这对于快速读写操作是非常有效的，但可能不是持久化存储的最佳选择，因为如果服务器重启或崩溃，所有数据都会丢失。

`lastApplied`字段被用来记录最后应用到状态机的日志条目的索引，以此来避免处理那些已经被应用过的过期日志条目。

`notifyChs`是一个映射，它的键是日志条目的索引，值是一个`channel`。用于通知Raft的处理结果（机即复制到大多数副本并且应用到状态机之后）。

```go
type KVServer struct {
	mu      sync.RWMutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32 // set by Kill()

	maxraftstate int // snapshot if log grows this big
	lastApplied  int //record the last applied index to avoid duplicate apply

	stateMachine   KVStateMachine
	lastOperations map[int64]OperationContext
	notifyChs      map[int]chan *CommandReply
}

type KVStateMachine interface {
	Get(key string) (string, Err)
	Put(key, value string) Err
	Append(key, value string) Err
}

type OperationContext struct {
	MaxAppliedCommandId int64
	LastReply           *CommandReply
}
```

`ExecuteCommand`RPC实现如下，这段首先检查是否不是Get请求且为重复的命令，如果是则返回上次的结果，否则通过Raft的`Start`方法复制并应用日志，如果`Start`方法返回结果告知当前server不是Leader，则返回`ErrWrongLeader`，否则，去注册一个channel去阻塞等待执行结果（因为Start返回只是代表日志被复制到大多数节点中，有没有应用还不知道），这个执行结果由`applier`协程push。

```go
func (kv *KVServer) ExecuteCommand(args *CommandArgs, reply *CommandReply) {
	kv.mu.RLock()
	if args.Op != OpGet && kv.isDuplicatedCommand(args.ClientId, args.CommandId) {
		lastReply := kv.lastOperations[args.ClientId].LastReply
		reply.Value, reply.Err = lastReply.Value, lastReply.Err
		kv.mu.RUnlock()
		return
	}
	kv.mu.RUnlock()
	index, _, isLeader := kv.rf.Start(Command{args})
	if !isLeader {
		reply.Err = ErrWrongLeader
		return
	}
	kv.mu.Lock()
	ch := kv.getNotifyCh(index)
	kv.mu.Unlock()

	select {
	case result := <-ch:
		reply.Value, reply.Err = result.Value, result.Err
	case <-time.After(ExecuteTimeout):
		reply.Err = ErrTimeout
	}
	go func() {
		kv.mu.Lock()
		kv.deleteNotifyCh(index)
		kv.mu.Unlock()
	}()
}
```

#### 2.2.3 applier

`applier`协程实现如下，主要是监控`applyCh`，根据Raft的应用结果来进行响应处理，需要注意的就是检测是否为重复的命令，如果不是，则需要应用到状态机，并保存最近的响应结果。最后，如果当前节点是领导者，并且该日志条目属于当前任期，则通知相关的客户端。

```go
func (kv *KVServer) applier() {
	for kv.killed() == false {
		select {
		case message := <-kv.applyCh:
			DPrintf("{Node %v} tries to apply message %v", kv.rf.GetId(), message)
			if message.CommandValid {
				kv.mu.Lock()
				if message.CommandIndex <= kv.lastApplied {
					DPrintf("{Node %v} discards outdated message %v because a newer snapshot which lastApplied is %v has been restored", kv.rf.GetId(), message, kv.lastApplied)
					kv.mu.Unlock()
					continue
				}
				kv.lastApplied = message.CommandIndex

				reply := new(CommandReply)
				command := message.Command.(Command) // type assertion
				if command.Op != OpGet && kv.isDuplicatedCommand(command.ClientId, command.CommandId) {
					DPrintf("{Node %v} doesn't apply duplicated message %v to stateMachine because maxAppliedCommandId is %v for client %v", kv.rf.GetId(), message, kv.lastOperations[command.ClientId], command.ClientId)
					reply = kv.lastOperations[command.ClientId].LastReply
				} else {
					reply = kv.applyLogToStateMachine(command)
					if command.Op != OpGet {
						kv.lastOperations[command.ClientId] = OperationContext{
							MaxAppliedCommandId: command.CommandId,
							LastReply:           reply,
						}
					}
				}

				// just notify related channel for currentTerm's log when node is leader
				if currentTerm, isLeader := kv.rf.GetState(); isLeader && message.CommandTerm == currentTerm {
					ch := kv.getNotifyCh(message.CommandIndex)
					ch <- reply
				}
				kv.mu.Unlock()
			}
		}
	}
}
```

### 2.2 lab4B：有快照

实现了lab4A，lab4B就好做了，只需要修改`applier`，每次应用了`command`之后，都需要检查是否达到`maxraftstate`，如果达到，则调用`snapshot`来制作快照，需要注意，快照中，不仅需要保存状态机的状态，还需要包含用来去重的`lastOperations`，这也是为了防止应用快照后的节点成为leader后，由于没有`lastOperations`导致重复执行命令。

然后，`applyCh`中还有Leader发来的快照，我们需要进行验证，如果有效，则需要更新相应的状态，具体实现代码如下：

```go
func (kv *KVServer) applier() {
	for kv.killed() == false {
		select {
		case message := <-kv.applyCh:
			DPrintf("{Node %v} tries to apply message %v", kv.rf.GetId(), message)
			if message.CommandValid {
				kv.mu.Lock()
				if message.CommandIndex <= kv.lastApplied {
					DPrintf("{Node %v} discards outdated message %v because a newer snapshot which lastApplied is %v has been restored", kv.rf.GetId(), message, kv.lastApplied)
					kv.mu.Unlock()
					continue
				}
				kv.lastApplied = message.CommandIndex

				reply := new(CommandReply)
				command := message.Command.(Command) // type assertion
				if command.Op != OpGet && kv.isDuplicatedCommand(command.ClientId, command.CommandId) {
					DPrintf("{Node %v} doesn't apply duplicated message %v to stateMachine because maxAppliedCommandId is %v for client %v", kv.rf.GetId(), message, kv.lastOperations[command.ClientId], command.ClientId)
					reply = kv.lastOperations[command.ClientId].LastReply
				} else {
					reply = kv.applyLogToStateMachine(command)
					if command.Op != OpGet {
						kv.lastOperations[command.ClientId] = OperationContext{
							MaxAppliedCommandId: command.CommandId,
							LastReply:           reply,
						}
					}
				}

				// just notify related channel for currentTerm's log when node is leader
				if currentTerm, isLeader := kv.rf.GetState(); isLeader && message.CommandTerm == currentTerm {
					ch := kv.getNotifyCh(message.CommandIndex)
					ch <- reply
				}
				if kv.needSnapshot() {
					kv.takeSnapshot(message.CommandIndex)
				}

				kv.mu.Unlock()
			} else if message.SnapshotValid {
				kv.mu.Lock()
				if kv.rf.CondInstallSnapshot(message.SnapshotTerm, message.SnapshotIndex, message.Snapshot) {
					kv.restoreStateFromSnapshot(message.Snapshot)
					kv.lastApplied = message.SnapshotIndex
				}
				kv.mu.Unlock()
			} else {
				panic(fmt.Sprintf("Invalid ApplyMsg %v", message))
			}
		}
	}
}
```

## 3 压测结果

网上提供了一个[测试脚本](https://gist.github.com/JJGO/0d73540ef7cc2f066cb535156b7cbdab)，功能强大。我的压测结果如下所示：

![image-20240820095009785](https://raw.githubusercontent.com/HeZephyr/NewPicGoLibrary/main/img/image-20240820095009785.png)