# MIT 6.5840(6.824)

Labs of MIT 6.5840(6.824): Distributed Systems.

## Lab 1 MapReduce

[Experimental Requirements](http://nil.csail.mit.edu/6.5840/2024/labs/lab-mr.html)

- [x] Complete the basic requirements for MapReduce
- [x] Handling worker failures
- [x] No data competition, a big lock ensures safety
- [x] Pass lab test
- [ ] Communicate over TCP/IP and read/write files using a shared file system

[Tutorial](https://blog.csdn.net/hzf0701/article/details/138867824?spm=1001.2014.3001.5501)

```shell
❯ bash test-mr.sh
*** Starting wc test
--- wc test: PASS
*** Starting indexer test.
--- indexer test: PASS
*** Starting map parallelism test.
--- map parallelism test: PASS
*** Starting reduce parallelism test.
--- reduce parallelism test: PASS
*** Starting job count test.
--- job count test: PASS
*** Starting early exit test.
--- early exit test: PASS
*** Starting crash test.
--- crash test: PASS
*** PASSED ALL TESTS
```

## Lab 2 Key/Value Server

[Experimental Requirements](https://pdos.csail.mit.edu/6.824/labs/lab-kvsrv.html)

- [x] Complete the basic requirements for Key/Value Server
- [x] No data competition, a big lock ensures safety
- [x] Pass lab test

[Tutorial](https://blog.csdn.net/hzf0701/article/details/138904641)

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

## Lab3 Raft
[Experimental Requirements](https://pdos.csail.mit.edu/6.824/labs/lab-raft.html)

- [x] Complete the basic requirements for Raft
- [x] No data competition, a big lock ensures safety
- [x] Pass the strict test of the lab
- [x] Strictly follow experimental requirements and paper presentation
- [ ] Optimize the search for NextIndex

[Tutorial](https://blog.csdn.net/hzf0701/article/details/141108894)

![image-20240811175403947](https://raw.githubusercontent.com/HeZephyr/NewPicGoLibrary/main/img/image-20240811175403947.png)

## Lab4 Fault-tolerant Key/Value Service

[Experimental Requirements](https://pdos.csail.mit.edu/6.824/labs/lab-kvraft.html)

- [x] Integrate Raft with Key/Value Server
- [x] Implement client request handlers (Get, Put, Append)
- [x] Pass lab test including fault tolerance scenarios

[Tutorial](https://blog.csdn.net/hzf0701/article/details/141727735)

![image-20240820095009785](https://raw.githubusercontent.com/HeZephyr/NewPicGoLibrary/main/img/image-20240820095009785.png)