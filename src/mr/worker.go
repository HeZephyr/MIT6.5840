package mr

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)
import "log"
import "net/rpc"
import "hash/fnv"

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

// generate the intermediate files for reduce tasks
func generateFileName(r int, NMap int) []string {
	var fileName []string
	for TaskID := 0; TaskID < NMap; TaskID++ {
		fileName = append(fileName, fmt.Sprintf("mr-%d-%d", TaskID, r))
	}
	return fileName
}

// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {
	for {
		args := MessageSend{}
		reply := MessageReply{}
		call("Coordinator.RequestTask", &args, &reply)

		switch reply.TaskType {
		case MapTask:
			HandleMapTask(&reply, mapf)
		case ReduceTask:
			HandleReduceTask(&reply, reducef)
		case Wait:
			time.Sleep(1 * time.Second)
		case Exit:
			os.Exit(0)
		default:
			time.Sleep(1 * time.Second)
		}
	}
}

func HandleMapTask(reply *MessageReply, mapf func(string, string) []KeyValue) {
	// open the file
	file, err := os.Open(reply.TaskFile)
	if err != nil {
		log.Fatalf("cannot open %v", reply.TaskFile)
		return
	}
	// read the file, get the content
	content, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("cannot read %v", reply.TaskFile)
		return
	}
	file.Close()

	// call the map function to get the key-value pairs
	kva := mapf(reply.TaskFile, string(content))
	// create intermediate files
	intermediate := make([][]KeyValue, reply.NReduce)
	for _, kv := range kva {
		r := ihash(kv.Key) % reply.NReduce
		intermediate[r] = append(intermediate[r], kv)
	}

	// write the intermediate files
	for r, kva := range intermediate {
		oname := fmt.Sprintf("mr-%v-%v", reply.TaskID, r)
		ofile, err := os.CreateTemp("", oname)
		if err != nil {
			log.Fatalf("cannot create tempfile %v", oname)
		}
		enc := json.NewEncoder(ofile)
		for _, kv := range kva {
			// write the key-value pairs to the intermediate file
			enc.Encode(kv)
		}
		ofile.Close()
		// Atomic file renaming：rename the tempfile to the final intermediate file
		os.Rename(ofile.Name(), oname)
	}

	// send the task completion message to the coordinator
	args := MessageSend{
		TaskID:              reply.TaskID,
		TaskCompletedStatus: MapTaskCompleted,
	}
	call("Coordinator.ReportTask", &args, &MessageReply{})
}
func HandleReduceTask(reply *MessageReply, reducef func(string, []string) string) {
	// load the intermediate files
	var intermediate []KeyValue
	// get the intermediate file names
	intermediateFiles := generateFileName(reply.TaskID, reply.NMap)
	// fmt.Println(intermediateFiles)
	for _, filename := range intermediateFiles {
		file, err := os.Open(filename)
		if err != nil {
			log.Fatalf("cannot open %v", filename)
			return
		}
		// decode the intermediate file
		dec := json.NewDecoder(file)
		for {
			kv := KeyValue{}
			if err := dec.Decode(&kv); err == io.EOF {
				break
			}
			intermediate = append(intermediate, kv)
		}
		file.Close()
	}

	// sort the intermediate key-value pairs by key
	sort.Slice(intermediate, func(i, j int) bool {
		return intermediate[i].Key < intermediate[j].Key
	})

	// write the key-value pairs to the output file
	oname := fmt.Sprintf("mr-out-%v", reply.TaskID)
	ofile, err := os.Create(oname)
	if err != nil {
		log.Fatalf("cannot create %v", oname)
		return
	}
	for i := 0; i < len(intermediate); {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		var values []string
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}

		// call the reduce function to get the output
		output := reducef(intermediate[i].Key, values)

		// write the key-value pairs to the output file
		fmt.Fprintf(ofile, "%v %v\n", intermediate[i].Key, output)

		i = j
	}
	ofile.Close()
	// rename the output file to the final output file
	os.Rename(ofile.Name(), oname)

	// send the task completion message to the coordinator
	args := MessageSend{
		TaskID:              reply.TaskID,
		TaskCompletedStatus: ReduceTaskCompleted,
	}
	call("Coordinator.ReportTask", &args, &MessageReply{})
}

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
