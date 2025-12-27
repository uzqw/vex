# Vex Architecture Design Specification (v3.0)

This document serves as the **architecture design specification for Vex v3.0 - Production-Grade Enhancement**.

Compared to previous versions, v3.0 emphasizes **I/O robustness**, **built-in benchmarking**, and **multi-dimensional observability**. This embodies the "engineering mindset" senior interviewers look for: not just functional, but production-ready, stress-tested, and measurable.

---

# Project: Vex Design Spec (v3.0)

## 1. Project Vision

Build a **production-grade, lightweight in-memory vector database**.

**Core Differentiators**: Beyond core functionality, emphasize **high-throughput I/O handling**, **observability (metrics & tracing)**, and **verifiable performance benchmarks**.

## 2. Core Architecture Design

### 2.1 Directory Structure

Introduces `cmd/benchmark` and `internal/metrics` as key additions.

```text
Vex/
├── cmd/
│   ├── server/             # Main server entry point
│   │   └── main.go
│   └── benchmark/          # Performance testing tool (key addition)
│       └── main.go
├── internal/
│   ├── protocol/           # RESP protocol parsing (I/O)
│   ├── storage/            # Storage engine (Sharded Map)
│   ├── vector/             # Vector computation core
│   └── metrics/            # Performance monitoring (key addition)
├── pkg/
│   └── logger/             # Structured logging wrapper
└── docs/
    └── design_spec.md
```

---

## 3. Key Module Details

### 3.1 I/O Subsystem & Protocol (Robust I/O Layer)

This is a common pitfall in junior projects. Must handle TCP packet coalescence, incomplete packets, and malformed data.

**Input Handling**:

- Use `bufio.Reader` for buffered I/O to reduce syscall overhead
- **Protocol Guard**: Strictly validate RESP format (`*` prefix, `$` length markers). On invalid format, not only error but **close connection** to prevent protocol corruption
- **Vector Parsing**: User vectors are JSON strings like `"[0.1, 0.2]"`
  - *Optimization*: `json.Unmarshal` may be slow. Implement a simple `FastVectorParser` in `internal/protocol` that manually parses comma-separated values as a performance highlight

**Output Handling**:

- Wrap `RESPWriter` struct
- Support pipelining: if client sends 10 commands at once, process all then flush output buffer once instead of 10 network I/O operations

### 3.2 Observability System (Deep Observability)

Beyond logging, implement a "dashboard" mentality.

**1. Structured Logging**:

- Introduce `RequestID`: Each connection gets a UUID, and all logs within that connection carry this ID
- Example: `INFO msg="query executed" trace_id=abc-123 latency=5ms hits=10`

**2. Built-in Metrics**:

- Implement atomic counters in `internal/metrics` using `sync/atomic` (no external Prometheus dependency, reducing deployment complexity)
- **Core Metrics**:
  - `TotalCommands`: Total requests processed
  - `ActiveConnections`: Current open connections
  - `TotalKeys`: Total vectors stored
  - `MemoryUsage`: Approximate memory usage
- **Exposure**: Custom `INFO` or `STATS` command returning JSON-formatted statistics

### 3.3 Storage & Compute

**Sharding**: Maintain 32-shard + cache-line padding design (prevents False Sharing)

**Vector Engine**:
- Auto-normalize vectors during `VSET` operation. This allows cosine similarity to be computed as simple dot product, yielding 30% speedup

---

## 4. Benchmark Suite

Evidence beats claims. Don't just say "I'm fast" — provide tools for others to measure.

Implement a CLI tool in `cmd/benchmark/main.go`:

**Parameters**:

- `-concurrency`: Number of concurrent goroutines (default: 50)
- `-n`: Total operations (default: 100,000)
- `-mode`: `insert` (writes) or `search` (queries)
- `-dim`: Vector dimensionality (default: 128)

**Sample Output**:

```text
=== Vex Benchmark ===
Mode:        Insert
Concurrency: 50
Total Ops:   100,000
---
Total Time:  1.25s
QPS:         80,000 ops/sec  <-- Core metric
Avg Latency: 0.5ms
P99 Latency: 2.1ms           <-- Core metric
```

---

## 5. Interaction Protocol Specification

Define strict input/output contracts for consistency.

### Scenario 1: Vector Insert (Standard Flow)

- **Client**: `VSET vec:1 "[0.12, 0.33, 0.95]"`
- **Server Processing**:
  1. Parse RESP → extract key and JSON string
  2. Parse JSON → `[]float32`
  3. Check dimension consistency (e.g., 128-dim)
  4. Store in appropriate shard
- **Server Output**: `+OK\r\n`
- **Metrics**: Increment `TotalKeys` and `TotalCommands`

### Scenario 2: Vector Search (Search Flow)

- **Client**: `VSEARCH "[0.12, 0.33, 0.95]" 5` (top-5)
- **Server Processing**:
  1. Parse query vector
  2. **Concurrent scan**: Launch N goroutines to scan different shards in parallel, computing distances
  3. **Merge**: Use min-heap to collect top-K results across all shards
- **Server Output** (RESP Array):
  ```text
  *2
  $5
  vec:9
  $5
  vec:3
  ```
- **Logs**: `INFO cmd=VSEARCH latency=3ms result_count=2`

### Scenario 3: System Status (Observability Flow)

- **Client**: `STATS`
- **Server Output** (RESP Bulk String - JSON):
  ```json
  {
    "goroutines": 54,
    "qps": 12000,
    "keys": 50000,
    "uptime": "1h20m"
  }
  ```

---

## 6. Implementation Guide

Follow this specification when implementing Vex. Key principles:

1. **Robust I/O**: Proper RESP protocol handling with error recovery
2. **Observability**: Structured logging and atomic metrics (no external dependencies)
3. **Performance**: Lock-free operations where possible, efficient vector computation
4. **Production-Ready**: Graceful shutdown, memory monitoring, comprehensive error handling
5. **Benchmarkable**: Built-in performance testing tools with latency percentiles
