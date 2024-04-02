package kvsrv

import (
	"6.5840/models"
	"6.5840/porcupine"

	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const linearizabilityCheckTimeout = 1 * time.Second

type OpLog struct {
	operations []porcupine.Operation
	sync.Mutex
}

func (log *OpLog) Append(op porcupine.Operation) {
	log.Lock()
	defer log.Unlock()
	log.operations = append(log.operations, op)
}

func (log *OpLog) Read() []porcupine.Operation {
	log.Lock()
	defer log.Unlock()
	ops := make([]porcupine.Operation, len(log.operations))
	copy(ops, log.operations)
	return ops
}

// to make sure timestamps use the monotonic clock, instead of computing
// absolute timestamps with `time.Now().UnixNano()` (which uses the wall
// clock), we measure time relative to `t0` using `time.Since(t0)`, which uses
// the monotonic clock
var t0 = time.Now()

// get/put/putappend that keep counts
func Get(cfg *config, ck *Clerk, key string, log *OpLog, cli int) string {
	start := int64(time.Since(t0))
	v := ck.Get(key)
	end := int64(time.Since(t0))
	cfg.op()
	if log != nil {
		log.Append(porcupine.Operation{
			Input:    models.KvInput{Op: 0, Key: key},
			Output:   models.KvOutput{Value: v},
			Call:     start,
			Return:   end,
			ClientId: cli,
		})
	}

	return v
}

func Put(cfg *config, ck *Clerk, key string, value string, log *OpLog, cli int) {
	start := int64(time.Since(t0))
	ck.Put(key, value)
	end := int64(time.Since(t0))
	cfg.op()
	if log != nil {
		log.Append(porcupine.Operation{
			Input:    models.KvInput{Op: 1, Key: key, Value: value},
			Output:   models.KvOutput{},
			Call:     start,
			Return:   end,
			ClientId: cli,
		})
	}
}

func Append(cfg *config, ck *Clerk, key string, value string, log *OpLog, cli int) string {
	start := int64(time.Since(t0))
	last := ck.Append(key, value)
	end := int64(time.Since(t0))
	cfg.op()
	if log != nil {
		log.Append(porcupine.Operation{
			Input:    models.KvInput{Op: 3, Key: key, Value: value},
			Output:   models.KvOutput{Value: last},
			Call:     start,
			Return:   end,
			ClientId: cli,
		})
	}
	return last
}

// a client runs the function f and then signals it is done
func run_client(t *testing.T, cfg *config, me int, ca chan bool, fn func(me int, ck *Clerk, t *testing.T)) {
	ok := false
	defer func() { ca <- ok }()
	ck := cfg.makeClient()
	fn(me, ck, t)
	ok = true
	cfg.deleteClient(ck)
}

// spawn ncli clients and wait until they are all done
func spawn_clients_and_wait(t *testing.T, cfg *config, ncli int, fn func(me int, ck *Clerk, t *testing.T)) {
	ca := make([]chan bool, ncli)
	for cli := 0; cli < ncli; cli++ {
		ca[cli] = make(chan bool)
		go run_client(t, cfg, cli, ca[cli], fn)
	}
	//log.Printf("spawn_clients_and_wait: waiting for clients")
	for cli := 0; cli < ncli; cli++ {
		ok := <-ca[cli]
		//log.Printf("spawn_clients_and_wait: client %d is done\n", cli)
		if ok == false {
			t.Fatalf("failure")
		}
	}
}

// predict effect of Append(k, val) if old value is prev.
func NextValue(prev string, val string) string {
	return prev + val
}

// check that for a specific client all known appends are present in a value,
// and in order
func checkClntAppends(t *testing.T, clnt int, v string, count int) {
	lastoff := -1
	for j := 0; j < count; j++ {
		wanted := "x " + strconv.Itoa(clnt) + " " + strconv.Itoa(j) + " y"
		off := strings.Index(v, wanted)
		if off < 0 {
			t.Fatalf("%v missing element %v in Append result %v", clnt, wanted, v)
		}
		off1 := strings.LastIndex(v, wanted)
		if off1 != off {
			t.Fatalf("duplicate element %v in Append result", wanted)
		}
		if off <= lastoff {
			t.Fatalf("wrong order for element %v in Append result", wanted)
		}
		lastoff = off
	}
}

// check that all known appends are present in a value,
// and are in order for each concurrent client.
func checkConcurrentAppends(t *testing.T, v string, counts []int) {
	nclients := len(counts)
	for i := 0; i < nclients; i++ {
		lastoff := -1
		for j := 0; j < counts[i]; j++ {
			wanted := "x " + strconv.Itoa(i) + " " + strconv.Itoa(j) + " y"
			off := strings.Index(v, wanted)
			if off < 0 {
				t.Fatalf("%v missing element %v in Append result %v", i, wanted, v)
			}
			off1 := strings.LastIndex(v, wanted)
			if off1 != off {
				t.Fatalf("duplicate element %v in Append result", wanted)
			}
			if off <= lastoff {
				t.Fatalf("wrong order for element %v in Append result", wanted)
			}
			lastoff = off
		}
	}
}

// is ov in nv?
func inHistory(ov, nv string) bool {
	return strings.Index(nv, ov) != -1
}

func randValue(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Int63()%int64(len(letterBytes))]
	}
	return string(b)
}

// Basic test is as follows: one or more clients submitting Append/Get
// operations to the server for some period of time.  After the period
// is over, test checks that all appended values are present and in
// order for a particular key.  If unreliable is set, RPCs may fail.
func GenericTest(t *testing.T, nclients int, unreliable bool, randomkeys bool) {
	const (
		NITER = 3
		TIME  = 1
	)

	title := "Test: "
	if unreliable {
		// the network drops RPC requests and replies.
		title = title + "unreliable net, "
	}
	if randomkeys {
		title = title + "random keys, "
	}
	if nclients > 1 {
		title = title + "many clients"
	} else {
		title = title + "one client"
	}
	cfg := make_config(t, unreliable)
	defer cfg.cleanup()

	cfg.begin(title)
	opLog := &OpLog{}

	ck := cfg.makeClient()

	done_clients := int32(0)
	clnts := make([]chan int, nclients)
	for i := 0; i < nclients; i++ {
		clnts[i] = make(chan int)
	}
	for i := 0; i < NITER; i++ {
		//log.Printf("Iteration %v\n", i)
		atomic.StoreInt32(&done_clients, 0)
		go spawn_clients_and_wait(t, cfg, nclients, func(cli int, myck *Clerk, t *testing.T) {
			j := 0
			defer func() {
				clnts[cli] <- j
			}()
			last := "" // only used when not randomkeys
			if !randomkeys {
				Put(cfg, myck, strconv.Itoa(cli), last, opLog, cli)
			}
			for atomic.LoadInt32(&done_clients) == 0 {
				var key string
				if randomkeys {
					key = strconv.Itoa(rand.Intn(nclients))
				} else {
					key = strconv.Itoa(cli)
				}
				nv := "x " + strconv.Itoa(cli) + " " + strconv.Itoa(j) + " y"
				if (rand.Int() % 1000) < 500 {
					//log.Printf("%d: client new append %v\n", cli, nv)
					l := Append(cfg, myck, key, nv, opLog, cli)
					if !randomkeys {
						if j > 0 {
							o := "x " + strconv.Itoa(cli) + " " + strconv.Itoa(j-1) + " y"
							if !inHistory(o, l) {
								t.Fatalf("error: old %v not in return\n%v\n", o, l)
							}
						}
						if inHistory(nv, l) {
							t.Fatalf("error: new value %v in returned values\n%v\n", nv, l)
						}
						last = NextValue(last, nv)
					}
					j++
				} else if randomkeys && (rand.Int()%1000) < 100 {
					// we only do this when using random keys, because it would break the
					// check done after Get() operations
					Put(cfg, myck, key, nv, opLog, cli)
					j++
				} else {
					//log.Printf("%d: client new get %v\n", cli, key)
					v := Get(cfg, myck, key, opLog, cli)
					// the following check only makes sense when we're not using random keys
					if !randomkeys && v != last {
						t.Fatalf("get wrong value, key %v, wanted:\n%v\n, got\n%v\n", key, last, v)
					}
				}
			}
		})

		time.Sleep(TIME * time.Second)

		atomic.StoreInt32(&done_clients, 1) // tell clients to quit

		for i := 0; i < nclients; i++ {
			j := <-clnts[i]
			// if j < 10 {
			// 	log.Printf("Warning: client %d managed to perform only %d put operations in 1 sec?\n", i, j)
			// }
			key := strconv.Itoa(i)
			//log.Printf("Check %v for client %d\n", j, i)
			v := Get(cfg, ck, key, opLog, 0)
			if !randomkeys {
				checkClntAppends(t, i, v, j)
			}
		}

	}

	res, info := porcupine.CheckOperationsVerbose(models.KvModel, opLog.Read(), linearizabilityCheckTimeout)
	if res == porcupine.Illegal {
		file, err := ioutil.TempFile("", "*.html")
		if err != nil {
			fmt.Printf("info: failed to create temp file for visualization")
		} else {
			err = porcupine.Visualize(models.KvModel, info, file)
			if err != nil {
				fmt.Printf("info: failed to write history visualization to %s\n", file.Name())
			} else {
				fmt.Printf("info: wrote history visualization to %s\n", file.Name())
			}
		}
		t.Fatal("history is not linearizable")
	} else if res == porcupine.Unknown {
		fmt.Println("info: linearizability check timed out, assuming history is ok")
	}

	cfg.end()
}

// Test one client
func TestBasic2(t *testing.T) {
	GenericTest(t, 1, false, false)
}

// Test many clients
func TestConcurrent2(t *testing.T) {
	GenericTest(t, 5, false, false)
}

// Test: unreliable net, many clients
func TestUnreliable2(t *testing.T) {
	GenericTest(t, 5, true, false)
}

// Test: unreliable net, many clients, one key
func TestUnreliableOneKey2(t *testing.T) {
	cfg := make_config(t, true)
	defer cfg.cleanup()

	ck := cfg.makeClient()

	cfg.begin("Test: concurrent append to same key, unreliable")

	Put(cfg, ck, "k", "", nil, -1)

	const nclient = 5
	const upto = 10
	spawn_clients_and_wait(t, cfg, nclient, func(me int, myck *Clerk, t *testing.T) {
		n := 0
		for n < upto {
			nv := "x " + strconv.Itoa(me) + " " + strconv.Itoa(n) + " y"
			ov := Append(cfg, myck, "k", nv, nil, -1)
			n++
			// log.Printf("%d: append nv %v ov %v\n", me, nv, ov)
			if inHistory(nv, ov) {
				t.Fatalf("error: nv %v in returned values\n%v\n", nv, ov)
			}
		}
	})

	var counts []int
	for i := 0; i < nclient; i++ {
		counts = append(counts, upto)
	}

	vx := Get(cfg, ck, "k", nil, -1)
	checkConcurrentAppends(t, vx, counts)

	cfg.end()
}

const (
	MiB = 1 << 20
)

func TestMemGet2(t *testing.T) {
	const MEM = 10 // in MiB

	cfg := make_config(t, true)
	defer cfg.cleanup()

	ck0 := cfg.makeClient()
	ck1 := cfg.makeClient()

	cfg.begin("Test: memory use get")

	rdVal := randValue(MiB * MEM)
	ck0.Put("k", rdVal)

	if v := ck0.Get("k"); len(v) != len(rdVal) {
		t.Fatalf("error: incorrect len %d\n", len(v))
	}
	if v := ck1.Get("k"); len(v) != len(rdVal) {
		t.Fatalf("error: incorrect len %d\n", len(v))
	}

	ck0.Put("k", "0")

	runtime.GC()
	var st runtime.MemStats

	runtime.ReadMemStats(&st)
	m := st.HeapAlloc / MiB
	if m >= MEM {
		t.Fatalf("error: server using too much memory %d\n", m)
	}

	cfg.end()
}

func TestMemPut2(t *testing.T) {
	const MEM = 10 // in MiB

	cfg := make_config(t, false)
	defer cfg.cleanup()

	cfg.begin("Test: memory use put")

	ck0 := cfg.makeClient()
	ck1 := cfg.makeClient()

	rdVal := randValue(MiB * MEM)
	ck0.Put("k", rdVal)
	ck1.Put("k", "")

	runtime.GC()

	var st runtime.MemStats
	runtime.ReadMemStats(&st)
	m := st.HeapAlloc / MiB
	if m >= MEM {
		t.Fatalf("error: server using too much memory %d\n", m)
	}
	cfg.end()
}

func TestMemAppend2(t *testing.T) {
	const MEM = 10 // in MiB

	cfg := make_config(t, false)
	defer cfg.cleanup()

	cfg.begin("Test: memory use append")

	ck0 := cfg.makeClient()
	ck1 := cfg.makeClient()

	rdVal0 := randValue(MiB * MEM)
	ck0.Append("k", rdVal0)
	rdVal1 := randValue(MiB * MEM)
	ck1.Append("k", rdVal1)

	runtime.GC()
	var st runtime.MemStats
	runtime.ReadMemStats(&st)
	m := st.HeapAlloc / MiB
	if m > 3*MEM {
		t.Fatalf("error: server using too much memory %d\n", m)
	}
	cfg.end()
}

func TestMemPutManyClients(t *testing.T) {
	const (
		NCLIENT = 100_000
		MEM     = 1000
	)

	cfg := make_config(t, false)
	defer cfg.cleanup()

	v := randValue(MEM)

	cks := make([]*Clerk, NCLIENT)
	for i, _ := range cks {
		cks[i] = cfg.makeClient()
	}

	// allow threads started by labrpc to start
	time.Sleep(1 * time.Second)

	cfg.begin("Test: memory use many put clients")

	runtime.GC()
	runtime.GC()

	var st runtime.MemStats
	runtime.ReadMemStats(&st)
	m0 := st.HeapAlloc

	for i := 0; i < NCLIENT; i++ {
		cks[i].Put("k", v)
	}

	runtime.GC()
	time.Sleep(1 * time.Second)
	runtime.GC()

	runtime.ReadMemStats(&st)
	m1 := st.HeapAlloc
	f := (float64(m1) - float64(m0)) / NCLIENT
	if m1 > m0+(NCLIENT*200) {
		t.Fatalf("error: server using too much memory %d %d (%.2f per client)\n", m0, m1, f)
	}

	for _, ck := range cks {
		cfg.deleteClient(ck)
	}

	cfg.end()
}

func TestMemGetManyClients(t *testing.T) {
	const (
		NCLIENT = 100_000
	)

	cfg := make_config(t, false)
	defer cfg.cleanup()

	cfg.begin("Test: memory use many get client")

	ck := cfg.makeClient()
	ck.Put("0", "")
	cfg.deleteClient(ck)

	cks := make([]*Clerk, NCLIENT)
	for i, _ := range cks {
		cks[i] = cfg.makeClient()
	}

	// allow threads started by labrpc to start
	time.Sleep(1 * time.Second)

	runtime.GC()
	runtime.GC()

	var st runtime.MemStats
	runtime.ReadMemStats(&st)
	m0 := st.HeapAlloc

	for i := 0; i < NCLIENT; i++ {
		cks[i].Get("0")
	}

	runtime.GC()

	time.Sleep(1 * time.Second)

	runtime.GC()

	runtime.ReadMemStats(&st)
	m1 := st.HeapAlloc
	f := (float64(m1) - float64(m0)) / NCLIENT
	if m1 >= m0+NCLIENT*10 {
		t.Fatalf("error: server using too much memory m0 %d m1 %d (%.2f per client)\n", m0, m1, f)
	}

	for _, ck := range cks {
		cfg.deleteClient(ck)
	}

	cfg.end()
}

func TestMemManyAppends(t *testing.T) {
	const (
		N   = 1000
		MEM = 1000
	)

	cfg := make_config(t, false)
	defer cfg.cleanup()

	cfg.begin("Test: memory use many appends")

	ck := cfg.makeClient()
	rdVal := randValue(MEM)

	runtime.GC()
	runtime.GC()

	var st runtime.MemStats
	runtime.ReadMemStats(&st)
	m0 := st.HeapAlloc

	for i := 0; i < N; i++ {
		ck.Append("k", rdVal)
	}

	runtime.GC()

	time.Sleep(1 * time.Second)

	runtime.GC()

	runtime.ReadMemStats(&st)
	m1 := st.HeapAlloc
	if m1 >= 3*MEM*N {
		t.Fatalf("error: server using too much memory m0 %d m1 %d\n", m0, m1)
	}

	log.Printf("m0 %d m1 %d\n", m0, m1)

	cfg.deleteClient(ck)
	cfg.end()
}
