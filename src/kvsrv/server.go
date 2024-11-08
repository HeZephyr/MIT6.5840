package kvsrv

import (
	"log"
	"sync"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

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
		return
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
		return
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

func StartKVServer() *KVServer {
	kv := new(KVServer)

	kv.data = make(map[string]string)
	return kv
}
