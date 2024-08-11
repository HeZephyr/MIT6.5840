# 【MIT 6.5840(6.824) 】Lab2:Key/Value Server 设计实现
## 实验要求

在本次 Lab 中，你将在单机上构建一个键/值服务器，以确保即使网络出现故障，每个操作也只能执行一次，并且操作是可线性化的。

客户端可以向键/值服务器发送三个不同的 RPC： `Put(key, value)` 、 `Append(key, arg)` 和 `Get(key)` 。服务器在内存中维护键/值对的`map`。键和值是字符串。 `Put(key, value)` 设置或替换`map`中给定键的值， `Append(key, arg)` 将 arg 附加到键的值并返回旧值， `Get(key)` 获取键的当前值。不存在的键的 `Get`请求应返回空字符串；对于不存在的键的 `Append` 请求应该表现为现有值是零长度字符串。每个客户端都通过`Clerk`的 `Put/Append/Get` 方法与服务器进行通信。 `Clerk` 管理与服务器的 RPC 交互。

你的服务器必须保证应用程序对`Clerk Get/Put/Append` 方法的调用是线性一致的。 如果客户端请求不是并发的，每个客户端 Get/Put/Append 调用时能够看到之前调用序列导致的状态变更。 对于并发的请求来说，返回的结果和最终状态都必须和这些操作顺序执行的结果一致。如果一些请求在时间上重叠，则它们是并发的：例如，如果客户端 X 调用 `Clerk.Put()` ，并且客户端 Y 调用 `Clerk.Append()` ，然后客户端 X 的调用 返回。 一个请求必须能够看到已完成的所有调用导致的状态变更。

一个应用实现线性一致性就像一台单机服务器一次处理一个请求的行为一样简单。 例如，如果一个客户端发起一个更新请求并从服务器获取了响应，随后从其他客户端发起的读操作可以保证能看到改更新的结果。在单台服务器上提供线性一致性是相对比较容易的。

Lab 在 `src/kvsrv` 中提供了框架代码和单元测试。你需要更改 `kvsrv/client.go`、`kvsrv/server.go` 和 `kvsrv/common.go` 文件。

## 无网络故障的KV Server

### 任务要求

此任务需要实现一个在没有丢失消息的情况下有效的解决方案。你需要在 `client.go` 中，在 Clerk 的 Put/Append/Get 方法中添加 RPC 的发送代码；并且实现 `server.go` 中 Put、Append、Get 三个 RPC handler。

当你通过了前两个测试 case：one client、many clients 时表示完成该任务。

### 设计实现

这个任务比较简单，我们只需要根据实验要求的逻辑进行实现即可。

* `server.go`

  使用`map`保存键值信息，三种操作都需要通过锁来保证互斥访问共享`map`。

  ```go
  func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
      kv.mu.Lock()
      defer kv.mu.Unlock()
      reply.Value = kv.data[args.Key]
  }
  
  func (kv *KVServer) Put(args *PutAppendArgs, reply *PutAppendReply) {
      kv.mu.Lock()
      defer kv.mu.Unlock()
      kv.data[args.Key] = args.Value
  }
  
  func (kv *KVServer) Append(args *PutAppendArgs, reply *PutAppendReply) {
      kv.mu.Lock()
      defer kv.mu.Unlock()
      oldValue := kv.data[args.Key]
      kv.data[args.Key] = oldValue + args.Value
      reply.Value = oldValue
  }
  ```

* `client.go`

  只需要添加RPC的发送代码。

  ```go
  func (ck *Clerk) Get(key string) string {
      args := &GetArgs{
          Key: key,
      }
      reply := &GetReply{}
      ck.server.Call("KVServer.Get", args, reply)
      return reply.Value
  }
  func (ck *Clerk) PutAppend(key string, value string, op string) string {
      arg := &PutAppendArgs{
          Key:   key,
          Value: value,
      }
      reply := &PutAppendReply{}
      ck.server.Call("KVServer."+op, arg, reply)
      return reply.Value
  }
  
  func (ck *Clerk) Put(key string, value string) {
      ck.PutAppend(key, value, "Put")
  }
  ```

## 可能丢弃消息的KV Server

### 任务要求

现在，您应该修改您的解决方案，以便在遇到丢失的消息（例如 RPC 请求和 RPC 回复）时继续工作。如果消息丢失，则客户端的 `ck.server.Call()` 将返回 `false` （更准确地说， `Call()` 等待响应直至超市，如果在此时间内没有响应就返回`false`）。您将面临的一个问题是 `Clerk` 可能需要多次发送 RPC，直到成功为止。但是，每次调用 `Clerk.Put()` 或 `Clerk.Append()` 应该只会导致一次执行，因此您必须确保重新发送不会导致服务器执行请求两次。

你的任务是在 `Clerk` 中添加重试逻辑，并且在 `server.go` 中来过滤重复请求。

> <center>Hint
> </center>
>
>
> 1. 您需要唯一地标识`client`操作，以确保KV Server仅执行每个操作一次。
> 2. 您必须仔细考虑`server`必须维持什么状态来处理重复的 `Get()` 、 `Put()` 和 `Append()` 请求（如果有的话）。
> 3. 您的重复检测方案应该快速释放服务器内存，例如让每个 RPC 暗示`client`已看到其前一个 RPC 的回复。可以假设`client`一次只向`Clerk`发起一次调用。

### 方案设计

根据提示，我们可以为`Put`和`Append`消息添加标识ID（`Get`消息只需不断重试，不会有影响），这里我们还需要用到`sync.Map`用于在键/值服务器中跟踪处理过的请求ID，以防止重复处理请求。每当服务器接收到一个新的RPC请求时，它会检查请求ID是否已存在于`sync.Map`中。如果存在，则表明该请求已经处理过，服务器可以跳过重复的处理，直接返回之前处理过的值。否则，服务器会记录该请求ID处理请求，并将回复结果记录。这种机制确保了操作的幂等性，避免了由于网络故障或重试机制导致的重复执行。

当然，还需要考虑一个问题，就是服务器会不断积压处理过的请求ID信息，所以我们需要快速释放服务器内存，即让`Client`通知`Server`这个任务操作已经完成，删除相关的记录信息。故我们还需要给消息结构添加一个`Type`字段标识为`Modify`还是`Report`。

整个流程图如下所示：

![image-20240515111905159](https://raw.githubusercontent.com/unique-pure/NewPicGoLibrary/main/img/KV_Server_Example_2.png)

### 代码实现

实验代码实现仓库：https://github.com/unique-pure/MIT6.5840/tree/main/src/kvsrv，实验代码已通过实验测试。

* `common.go`

  ```go
  type MessageType int
  
  const (
      Modify = iota
      Report
  )
  
  // Put or Append
  type PutAppendArgs struct {
      Key   string
      Value string
      MessageType MessageType // Modify or Report
      MessageID   int64       // Unique ID for each message
  }
  ```

* `server.go`

  ```go
  type KVServer struct {
      mu sync.Mutex
  
      data   map[string]string
      record sync.Map
  }
  
  func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
      kv.mu.Lock()
      defer kv.mu.Unlock()
      reply.Value = kv.data[args.Key]
  }
  
  func (kv *KVServer) Put(args *PutAppendArgs, reply *PutAppendReply) {
      if args.MessageType == Report {
          kv.record.Delete(args.MessageID)
      }
      res, ok := kv.record.Load(args.MessageID)
      if ok {
          reply.Value = res.(string) // 重复请求，返回之前的结果
          return
      }
      kv.mu.Lock()
      old := kv.data[args.Key]
      kv.data[args.Key] = args.Value
      reply.Value = old
      kv.mu.Unlock()
  
      kv.record.Store(args.MessageID, old) // 记录请求
  }
  
  func (kv *KVServer) Append(args *PutAppendArgs, reply *PutAppendReply) {
      if args.MessageType == Report {
          kv.record.Delete(args.MessageID)
      }
      res, ok := kv.record.Load(args.MessageID)
      if ok {
          reply.Value = res.(string) // 重复请求，返回之前的结果
          return
      }
      kv.mu.Lock()
      old := kv.data[args.Key]
      kv.data[args.Key] = old + args.Value
      reply.Value = old
      kv.mu.Unlock()
  
      kv.record.Store(args.MessageID, old) // 记录请求
  }
  ```

* `client.go`

  ```go
  func (ck *Clerk) Get(key string) string {
      args := &GetArgs{
          Key: key,
      }
      reply := &GetReply{}
      for !ck.server.Call("KVServer.Get", args, reply) {
      } // keep trying forever
      return reply.Value
  }
  func (ck *Clerk) PutAppend(key string, value string, op string) string {
      MessageID := nrand()
      arg := &PutAppendArgs{
          Key:         key,
          Value:       value,
          MessageID:   MessageID,
          MessageType: Modify,
      }
      reply := &PutAppendReply{}
      for !ck.server.Call("KVServer."+op, arg, reply) {
      }
      arg = &PutAppendArgs{
          MessageType: Report,
          MessageID:   MessageID,
      }
      for !ck.server.Call("KVServer."+op, arg, reply) {
      }
      return reply.Value
  }
  ```

```shell
❯ go test
Test: one client
  ... Passed -- t  3.3 nrpc 20037 ops 13359
Test: many clients ...
  ... Passed -- t  3.7 nrpc 85009 ops 56718
Test: unreliable net, many clients ...
  ... Passed -- t  3.3 nrpc  1161 ops  632
Test: concurrent append to same key, unreliable ...
  ... Passed -- t  0.4 nrpc   131 ops   52
Test: memory use get ...
  ... Passed -- t  0.6 nrpc     8 ops    0
Test: memory use put ...
  ... Passed -- t  0.3 nrpc     4 ops    0
Test: memory use append ...
  ... Passed -- t  0.5 nrpc     4 ops    0
Test: memory use many put clients ...
  ... Passed -- t 36.7 nrpc 200000 ops    0
Test: memory use many get client ...
  ... Passed -- t 22.6 nrpc 100002 ops    0
Test: memory use many appends ...
2024/05/15 12:48:26 m0 411000 m1 1550088
  ... Passed -- t  2.6 nrpc  2000 ops    0
PASS
ok      6.5840/kvsrv    75.329s
```

