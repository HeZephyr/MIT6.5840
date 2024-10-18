package shardkv

// Shard represents a key-value store with its status.
type Shard struct {
	KV     map[string]string // Map to store key-value pairs
	Status ShardStatus       // Current status of the shard
}

// NewShard creates and initializes a new Shard instance.
func NewShard() *Shard {
	return &Shard{
		KV:     make(map[string]string),
		Status: Serving,
	}
}

func (shard *Shard) Get(key string) (string, Err) {
	if value, ok := shard.KV[key]; ok {
		return value, OK
	}
	return "", ErrNoKey
}

func (shard *Shard) Put(key, value string) Err {
	shard.KV[key] = value
	return OK
}

func (shard *Shard) Append(key, value string) Err {
	shard.KV[key] += value
	return OK
}

// deepCopy creates a copy of the shard's key-value pairs.
// Returns a new map with all key-value pairs from the shard.
func (shard *Shard) deepCopy() map[string]string {
	newShard := make(map[string]string)
	for k, v := range shard.KV {
		newShard[k] = v
	}
	return newShard
}
