package shardkv

type KVStateMachine interface {
	Get(key string) (string, Err)
	Put(key, value string) Err
	Append(key, value string) Err
}

type ShardMemoryKV struct {
	KV          map[string]string
	shardStatus ShardStatus
}

func NewShardMemoryKV() *ShardMemoryKV {
	return &ShardMemoryKV{make(map[string]string), Serving}
}

func (shardKV *ShardMemoryKV) Get(key string) (string, Err) {
	if value, ok := shardKV.KV[key]; ok {
		return value, OK
	}
	return "", ErrNoKey
}

func (shardKV *ShardMemoryKV) Put(key, value string) Err {
	shardKV.KV[key] = value
	return OK
}

func (shardKV *ShardMemoryKV) Append(key, value string) Err {
	shardKV.KV[key] += value
	return OK
}

func (shardKV *ShardMemoryKV) deepCopy() map[string]string {
	newShard := make(map[string]string)
	for k, v := range shardKV.KV {
		newShard[k] = v
	}
	return newShard
}
