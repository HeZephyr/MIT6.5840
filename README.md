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