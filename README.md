# Vex

[![codecov](https://codecov.io/gh/uzqw/vex/branch/main/graph/badge.svg)](https://codecov.io/gh/uzqw/vex)
[![CI](https://github.com/uzqw/vex/actions/workflows/ci.yml/badge.svg)](https://github.com/uzqw/vex/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

A production-grade, high-performance vector database built with Go. Features SIMD-optimized operations (7.8x-9.1x speedup), HNSW indexing for fast approximate search, and RDB-style snapshot persistence. Designed for real-world embedding workloads (768D-2048D dimensions).

## Features

- **SIMD-Optimized Vector Operations**: 7.8x-9.1x speedup with AVX2/AVX512 assembly for production dimensions
- **HNSW Index**: Hierarchical Navigable Small World algorithm for fast approximate nearest neighbor search
- **Snapshot Persistence**: RDB-style persistence with compression and automatic scheduling
- **High-Performance Storage**: 32-way sharded in-memory storage with lock-free metrics
- **RESP Protocol**: Compatible with Redis protocol for easy integration
- **Observability**: Built-in metrics, structured logging, and request tracing
- **Production-Ready**: Graceful shutdown, automatic snapshots, comprehensive error handling

## Architecture Highlights

- **SIMD Assembly Optimizations**: AVX2/AVX512 instructions for 7.8x-9.1x faster dot products (768D-2048D)
- **HNSW Algorithm**: Multi-layer hierarchical graph for O(log N) search complexity
- **Snapshot Persistence**: Atomic RDB-style snapshots with Snappy compression
- **32-way Sharding**: Reduces lock contention with CPU cache-line padding
- **Optimized Vector Search**: Normalized vectors enable dot-product computation for cosine similarity
- **Production Features**: Graceful shutdown, automatic snapshots, memory monitoring

## Prerequisites

- **Go 1.22+** - [Download](https://go.dev/dl/)
- **Git** - For cloning and version info
- **Make** - For build commands (optional)

## Quick Start

### Build and Run

```bash
# Build everything
make build

# Run the server
make run

# Run with JSON logging
make run-json

# Run with debug logging
make run-debug
```

### Using Docker

```bash
# Build the image
docker build -t vex .

# Run the container
docker run -p 6379:6379 vex
```

### Install from Source

```bash
# Install vex-server binary to $GOPATH/bin
go install github.com/uzqw/vex/cmd/vex-server@latest
```

### Using the Server

Connect using any RESP protocol client (like redis-cli) or netcat:

```bash
# Using netcat
nc localhost 6379

# Using redis-cli
redis-cli -p 6379
```

### Installing redis-cli

If you don't have `redis-cli` installed, you can install it with:

**Ubuntu/Debian:**
```bash
sudo apt update && sudo apt install redis-tools
```

**macOS (Homebrew):**
```bash
brew install redis
```

**Arch Linux:**
```bash
sudo pacman -S redis
```

## Commands

### Basic Commands

- `PING [message]` - Test connection
- `ECHO message` - Echo back a message
- `STATS` / `INFO` - Get vex statistics
- `QUIT` - Close connection

### Vector Commands

#### VSET - Store a vector

```
VSET key "[0.1, 0.2, 0.3, ...]"
```

Example:
```
VSET vec:1 "[0.12, 0.33, 0.95]"
+OK
```

#### VGET - Retrieve a vector

```
VGET key
```

Example:
```
VGET vec:1
$26
[0.120000, 0.330000, 0.950000]
```

#### VDEL - Delete a vector

```
VDEL key
```

Returns `:1` if deleted, `:0` if key didn't exist.

#### VSEARCH - Find similar vectors

```
VSEARCH "[0.1, 0.2, 0.3, ...]" k
```

Example (find top 5 similar vectors):
```
VSEARCH "[0.12, 0.33, 0.95]" 5
*2
$5
vec:9
$5
vec:3
```

#### CLEAR - Remove all vectors

```
CLEAR
+OK
```

### Persistence Commands

#### BGSAVE - Create background snapshot

```
BGSAVE
+Background saving started
```

Creates a snapshot asynchronously without blocking operations.

#### SAVE - Create blocking snapshot

```
SAVE
+OK
```

Creates a snapshot synchronously (blocks until complete).

#### INFO persistence - Get snapshot information

```
INFO persistence
${JSON}
{
  "last_snapshot_time": "2026-01-30T16:00:00Z",
  "snapshot_in_progress": false,
  "total_snapshots": 42,
  "snapshot_errors": 0
}
```

### Stats Command

Get real-time vex metrics:

```
STATS
${JSON}
{
  "goroutines": 54,
  "total_commands": 125000,
  "active_connections": 12,
  "total_keys": 50000,
  "memory_usage_mb": 245.3,
  "uptime": "1h20m15s",
  "qps": 12500.5
}
```

## Benchmarking

Vex includes comprehensive performance benchmarks at both the unit and integration levels.

### Unit Benchmarks (Micro-level)

Test individual components in isolation:

```bash
# Run all unit benchmarks
make bench-unit

# Or directly with Go
go test -bench=. -benchmem ./benchmarks/...
```

### Integration Benchmarks (End-to-End)

Test the complete system with realistic workloads. Requires a running server:

```bash
# Terminal 1: Start the server
make run

# Terminal 2: Run benchmarks
make bench-integration
```

### Complete Benchmark Suite

Run both unit and integration benchmarks:

```bash
# Run all benchmarks
./benchmarks/run-all.sh

# Or using make
make bench
```

### Detailed Usage

For comprehensive documentation on benchmarking, see [benchmarks/README.md](benchmarks/README.md).

#### Unit Benchmark Examples

```bash
# Run storage benchmarks
go test -bench=BenchmarkStorage -benchmem ./benchmarks/storage/

# Run with CPU profiling
go test -bench=. -benchmem -cpuprofile=cpu.prof ./benchmarks/storage/
go tool pprof -http=:8080 cpu.prof

# Compare with previous results
go install golang.org/x/perf/cmd/benchstat@latest
go test -bench=. -benchmem ./benchmarks/storage/ > new.txt
benchstat old.txt new.txt
```

#### Integration Benchmark Examples

```bash
# Custom insert benchmark
make benchmark-custom ARGS="-mode=insert -concurrency=100 -n=200000 -dim=256"

# Custom search benchmark
make benchmark-custom ARGS="-mode=search -concurrency=50 -n=100000"

# Or run directly
go run cmd/vex-benchmark/main.go -mode=insert -concurrency=50 -n=100000
```

### Example Integration Benchmark Output

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

### Make Targets for Benchmarking

```bash
make bench              # Run all benchmarks (unit + integration)
make bench-all          # Run all unit benchmarks
make bench-unit         # Run unit benchmarks only
make bench-storage      # Run storage layer benchmarks
make bench-integration  # Run integration benchmarks (requires running server)
make benchmark          # Legacy: insert benchmark
make benchmark-search   # Legacy: search benchmark
make benchmark-custom   # Custom benchmark with parameters
```

## Development

### Project Structure

```
vex/
├── benchmarks/              # Performance benchmarks
│   ├── storage/             # Storage layer benchmarks (HNSW, brute force)
│   ├── README.md            # Comprehensive benchmark guide
│   └── run-all.sh           # Run all benchmarks script
├── cmd/
│   ├── vex-server/          # Main server entry point
│   └── vex-benchmark/       # Integration benchmark tool
├── internal/
│   ├── protocol/            # RESP protocol parsing
│   ├── storage/             # Sharded vector storage
│   │   ├── index.go         # Index interface
│   │   ├── index_hnsw.go    # HNSW implementation
│   │   └── persistence/     # Snapshot persistence layer
│   ├── vector/              # Vector computation
│   │   ├── asm/             # AVX2/AVX512 assembly (goat-generated)
│   │   └── c/               # C source for SIMD (from Weaviate)
│   └── metrics/             # Performance metrics
├── pkg/
│   └── logger/              # Structured logging
├── docs/
│   ├── README.md            # Documentation index
│   ├── PERFORMANCE.md       # SIMD benchmarks and results
│   ├── BUILD.md             # Assembly compilation guide
│   ├── PERSISTENCE.md       # Persistence design
│   └── LICENSES.md          # License compliance
├── Dockerfile.goat          # Docker image for goat tool
└── README.md                # This file
```

### Available Make Targets

```bash
make help           # Show all available commands
make build          # Build binaries
make run            # Run server
make test           # Run tests
make test-coverage  # Run tests with coverage
make fmt            # Format code
make vet            # Run go vet
make lint           # Run golangci-lint
make test-race      # Run tests with race detector
make install-tools  # Install dev tools
make clean          # Clean build artifacts
```

### Running Tests

```bash
# Run all tests
make test

# Run with coverage report
make test-coverage
```

## Configuration

### Server Flags

- `-host` - Host to bind to (default: "0.0.0.0")
- `-port` - Port to listen on (default: "6379")
- `-log-format` - Log format: "text" or "json" (default: "text")
- `-log-level` - Log level: "debug", "info", "warn", "error" (default: "info")

### Persistence Configuration

Persistence can be configured via environment variables:

- `VEX_PERSISTENCE_ENABLED` - Enable persistence (default: false)
- `VEX_DATA_DIR` - Data directory for snapshots (default: "./data")
- `VEX_SNAPSHOT_SECONDS` - Auto-snapshot interval in seconds (default: 300)
- `VEX_KEEP_SNAPSHOTS` - Number of snapshots to retain (default: 3)

Example:
```bash
VEX_PERSISTENCE_ENABLED=true VEX_SNAPSHOT_SECONDS=600 ./vex-server
```

### Benchmark Flags

- `-host` - Server host (default: "localhost")
- `-port` - Server port (default: "6379")
- `-concurrency` - Number of concurrent connections (default: 50)
- `-n` - Total number of operations (default: 100000)
- `-mode` - Benchmark mode: "insert" or "search" (default: "insert")
- `-dim` - Vector dimension (default: 128)

## Performance Characteristics

### Vector Operations (SIMD-Optimized)

| Dimension | Speedup | Use Case |
|-----------|---------|----------|
| 768D      | 7.8x ⚡⚡⚡ | BERT, sentence transformers |
| 1024D     | 8.5x ⚡⚡⚡⚡ | CLIP, ResNet image embeddings |
| 1536D     | **9.1x** ⚡⚡⚡⚡ | OpenAI text-embedding-ada-002 |
| 2048D     | 9.0x ⚡⚡⚡⚡ | Large multi-modal models |

*Tested on Intel Core Ultra 9 285H with AVX2 support*

See [docs/PERFORMANCE.md](docs/PERFORMANCE.md) for detailed benchmarks.

### System Performance

- **Throughput**: 80,000+ QPS for inserts on modern hardware
- **Search Latency**: P99 < 2ms with HNSW index
- **Concurrency**: Scales linearly with CPU cores (32-way sharding)
- **Memory**: ~4 bytes per dimension per vector (float32)
- **Snapshot Save**: ~10s for 1M vectors (1024D, compressed)
- **Snapshot Load**: ~50s for 1M vectors (1024D, compressed)

## Use Cases

- Semantic search applications
- Recommendation systems
- Similarity detection
- Embedding storage and retrieval
- Development and testing of vector-based systems

## Limitations

- **In-Memory First**: Primary storage is in-memory; persistence uses periodic snapshots (no WAL yet)
- **Single Node**: No clustering or replication support
- **No Authentication**: No built-in auth mechanism (use network isolation)
- **Cosine Similarity Only**: Only cosine similarity distance metric is supported
- **AMD64 Only**: SIMD optimizations require AMD64 with AVX2 (ARM64 support planned)

## Documentation

Comprehensive documentation is available in the [docs/](docs/) directory:

- **[docs/README.md](docs/README.md)** - Documentation index and quick reference
- **[docs/PERFORMANCE.md](docs/PERFORMANCE.md)** - SIMD optimization benchmarks and analysis
- **[docs/BUILD.md](docs/BUILD.md)** - Guide for compiling C to assembly with goat
- **[docs/PERSISTENCE.md](docs/PERSISTENCE.md)** - Persistence design and implementation
- **[docs/LICENSES.md](docs/LICENSES.md)** - License compliance (BSD-3 + Apache 2.0)

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

### Third-Party Code

Vex includes SIMD optimization code derived from [Weaviate](https://github.com/weaviate/weaviate) (BSD-3-Clause), which is fully compatible with Apache 2.0. See [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md) and [NOTICE](NOTICE) for details.

### Copyright

Copyright © 2025 uzqw

### Summary

- You can use this software for commercial and non-commercial purposes
- You must include a copy of the license and copyright notice
- You must document significant changes made to the code
- The software is provided "as is" without warranties or liabilities

For full details, please refer to the [Apache 2.0 License](http://www.apache.org/licenses/LICENSE-2.0).

## Contributing

Key implementation principles:

1. **Performance First**: SIMD optimizations, efficient algorithms (HNSW), lock-free operations
2. **Production-Ready**: Persistence, graceful shutdown, comprehensive error handling
3. **Observability**: Structured logging, atomic metrics, detailed benchmarks
4. **Code Quality**: Comprehensive tests, race detection, proper documentation

When contributing:
- Follow existing code style and patterns
- Add tests for new functionality
- Update documentation (especially performance benchmarks)
- Run `make test` and `make test-race` before submitting
- For SIMD changes, see [docs/BUILD.md](docs/BUILD.md)

## Acknowledgments

- **[Weaviate](https://github.com/weaviate/weaviate)**: SIMD optimization C code (BSD-3-Clause)
- **[goat](https://github.com/mmcloughlin/goat)**: C to Go assembly conversion tool
- **[hnswlib](https://github.com/nmslib/hnswlib)**: HNSW algorithm reference implementation

Built following production-grade practices for high-performance Go services.
