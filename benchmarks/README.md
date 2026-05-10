# Benchmarks

This directory contains performance benchmarks for Vex, organized into unit-level benchmarks and integration-level benchmarks.

## Overview

Vex provides two types of performance tests:

1. **Unit Benchmarks** (`./benchmarks/`): Isolated component-level performance tests
2. **Integration Benchmarks** (`cmd/vex-benchmark/`): End-to-end system performance tests

## Unit Benchmarks

Unit benchmarks measure the performance of individual components (storage, vector operations, protocol parsing) in isolation.

### Running Unit Benchmarks

Run all unit benchmarks:

```bash
make bench-unit
```

Or directly with Go:

```bash
go test -bench=. -benchmem ./benchmarks/...
```

### Storage Layer Benchmarks (`./storage/`)

Measures the performance of the sharded vector storage system.

```bash
# Run all storage benchmarks
go test -bench=BenchmarkStorage -benchmem ./benchmarks/storage/

# Run specific benchmark
go test -bench=BenchmarkShardIsolation -benchmem ./benchmarks/storage/

# Run with detailed CPU profile
go test -bench=. -benchmem -cpuprofile=cpu.prof ./benchmarks/storage/
```

#### Available Storage Benchmarks

- **BenchmarkStorageContention**: Measures write performance under contention with varying concurrency levels (1, 2, 4, 8, NumCPU, NumCPU*2)
  - Tests sequential writes to the same shard
  - Reveals lock contention effects

- **BenchmarkStorageReadWrite**: Measures mixed read/write workload performance
  - 90% reads, 10% writes
  - Tests realistic production scenarios
  - Varies concurrency levels

- **BenchmarkShardIsolation**: Tests shard distribution effectiveness
  - `single_shard_contention`: All operations go to same shard
  - `distributed_shards`: Operations distributed across shards
  - Validates that sharding reduces lock contention

### Interpreting Results

A typical output looks like:

```
BenchmarkStorageContention/goroutines=1-16
  598203   2115 ns/op   552 B/op   3 allocs/op
BenchmarkStorageContention/goroutines=4-16
  892145   1345 ns/op   551 B/op   3 allocs/op
BenchmarkStorageContention/goroutines=8-16
  1298456   912 ns/op   549 B/op   3 allocs/op
```

Key metrics:
- **ns/op**: Nanoseconds per operation (lower is better)
- **B/op**: Bytes allocated per operation
- **allocs/op**: Number of allocations per operation

### Performance Characteristics

Expected results on modern hardware (Intel Core i7/i9):

| Metric | Target | Typical |
|--------|--------|---------|
| Set operation | < 1 µs | 0.5-1.0 µs |
| Get operation | < 500 ns | 200-400 ns |
| Search (100 vectors) | < 50 µs | 20-40 µs |
| Memory per vector | ~5 bytes/dimension | 512B (128D) |

## Integration Benchmarks

Integration benchmarks test the entire system end-to-end, including network I/O and protocol handling.

### Prerequisites

1. Start the Vex server:

```bash
make run
# or
go run ./cmd/vex-server/main.go
```

2. In another terminal, run benchmarks:

```bash
make bench-integration
```

### Running Specific Integration Benchmarks

Insert benchmark (100k operations, 50 concurrent connections):

```bash
go run ./cmd/vex-benchmark/main.go -mode=insert -concurrency=50 -n=100000
```

Search benchmark (50k operations):

```bash
go run ./cmd/vex-benchmark/main.go -mode=search -concurrency=50 -n=50000
```

Custom benchmark with parameters:

```bash
go run ./cmd/vex-benchmark/main.go \
  -mode=insert \
  -concurrency=100 \
  -n=200000 \
  -dim=256
```

### Comparing Search Modes

Run the server with the search mode you want to test, then run the same
benchmark command against each mode:

```bash
# Terminal 1
go run ./cmd/vex-server/main.go -index=auto

# Terminal 2
go run ./cmd/vex-benchmark/main.go \
  -mode=search -concurrency=8 -n=10000 -dim=128 \
  -prepare-n=10000 -warmup=1000 -k=10
```

Available server search modes:

- `-index=none`: sharded full-scan storage search
- `-index=bruteforce`: brute-force index search
- `-index=hnsw`: HNSW index search
- `-index=auto`: sharded full scan below `-auto-index-min-vectors`, HNSW above it

### Benchmark Flags

- `-host`: Server host (default: "localhost")
- `-port`: Server port (default: "6379")
- `-concurrency`: Number of concurrent connections (default: 50)
- `-n`: Total number of measured operations (default: 100000)
- `-mode`: Benchmark mode: "insert" or "search" (default: "insert")
- `-dim`: Vector dimension (default: 128)
- `-prepare-n`: Number of vectors to load before search benchmarks (default: 1000)
- `-warmup`: Number of warmup operations to run before measuring (default: 0)
- `-k`: Top-k value for VSEARCH (default: 10)
- `-seed`: Random seed for deterministic vectors (default: 42)
- `-key-prefix`: Key prefix used for generated vectors (default: "vec")

### Integration Benchmark Output

```
=== Vex Benchmark ===
Mode:        insert
Concurrency: 50
Total Ops:   100000
---
Total Time:    1.25s
QPS:           80000 ops/sec
Success:       100000
Errors:        0

Latency Statistics:
  Min:         245µs
  Avg:         625µs
  P50:         580µs
  P95:         1.2ms
  P99:         2.1ms
  Max:         5.3ms
```

## Performance Profiling

### CPU Profiling

Profile a unit benchmark:

```bash
go test -bench=BenchmarkStorageContention -benchmem -cpuprofile=cpu.prof ./benchmarks/storage/
go tool pprof -http=:8080 cpu.prof
```

Profile integration benchmark:

```bash
# Run server with CPU profiling
go run ./cmd/vex-server/main.go &

# In another terminal, create a CPU profile
go tool pprof http://localhost:6379/debug/pprof/profile
```

### Memory Profiling

Benchmark with memory profiling:

```bash
go test -bench=BenchmarkStorageContention -benchmem -memprofile=mem.prof ./benchmarks/storage/
go tool pprof mem.prof

# In pprof:
(pprof) top10
(pprof) list main.Set
```

### Go's Built-in Pprof

The server includes built-in pprof endpoints (if compiled with profiling support):

```bash
# CPU profile (30 seconds)
go tool pprof http://localhost:6379/debug/pprof/profile?seconds=30

# Memory profile
go tool pprof http://localhost:6379/debug/pprof/heap

# Goroutine profile
go tool pprof http://localhost:6379/debug/pprof/goroutine
```

## Automated Benchmarking

Run all benchmarks (unit + integration):

```bash
make bench
```

This will:
1. Run all unit benchmarks from `./benchmarks/`
2. Run integration benchmarks from `cmd/vex-benchmark/` (requires running server)

## Benchmark Best Practices

### Writing Benchmarks

1. **Reset timer after setup**: Use `b.ResetTimer()` after preparation
2. **Report allocations**: Use `b.ReportAllocs()` to track memory
3. **Use sub-benchmarks**: Use `b.Run()` for multiple test cases
4. **Avoid random operations**: Pre-generate test data
5. **Test realistic scenarios**: Mix reads and writes, vary concurrency

### Running Benchmarks

1. **Run multiple times**: Results vary between runs
2. **Disable other apps**: Reduce system noise
3. **Use consistent hardware**: Compare results on same machine
4. **Run with `-benchtime`**: Longer tests for stable results

```bash
# Run 3 times with 5-second duration each
for i in {1..3}; do
  go test -bench=. -benchtime=5s ./benchmarks/storage/
done
```

5. **Use benchstat for comparison**:

```bash
go install golang.org/x/perf/cmd/benchstat@latest

# Save baseline
go test -bench=. -benchmem ./benchmarks/storage/ > old.txt

# Make changes, then compare
go test -bench=. -benchmem ./benchmarks/storage/ > new.txt
benchstat old.txt new.txt
```

## Continuous Benchmarking

### GitHub Actions

Enable benchmark tracking in CI/CD:

```yaml
name: Benchmarks
on: [push, pull_request]

jobs:
  benchmark:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      - run: go test -bench=. -benchmem ./benchmarks/...
```

### Regression Detection

Track performance regressions with benchstat:

```bash
# Compare main vs feature branch
git checkout main
go test -bench=. -benchmem ./benchmarks/storage/ > baseline.txt

git checkout feature-branch
go test -bench=. -benchmem ./benchmarks/storage/ > current.txt

benchstat baseline.txt current.txt
```

## Performance Optimization Tips

### Reducing Lock Contention

- Use sharding to distribute lock pressure
- Keep critical sections small
- Prefer RWMutex for read-heavy workloads
- Consider lock-free data structures

### Reducing Allocations

- Pre-allocate slices and maps
- Reuse buffers
- Minimize interface{} conversions
- Use sync.Pool for temporary objects

### Vector Operations

- Normalize vectors at storage time (pre-computation)
- Use dot product instead of cosine similarity for normalized vectors
- Cache computed norms
- Batch operations when possible

## Troubleshooting

### High latency variance

- Check system load with `top` or `htop`
- Run benchmarks with CPU isolation
- Disable dynamic CPU scaling: `echo performance | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor`

### Memory pressure

- Monitor with `watch -n1 free -h`
- Increase `vm.max_map_count` if needed: `echo 262144 | sudo tee /proc/sys/vm/max_map_count`

### Inconsistent results

- Disable frequency scaling
- Close background applications
- Run tests with higher `-benchtime` for stability

## See Also

- [Go Benchmark Documentation](https://pkg.go.dev/testing#hdr-Benchmarks)
- [benchstat](https://golang.org/x/perf/cmd/benchstat)
- [pprof Documentation](https://github.com/google/pprof/tree/main/doc)
- [Main README](../README.md)
- [Design Specification](../docs/01_v3_design_spec.md)
