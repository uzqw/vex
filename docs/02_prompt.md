# Role

You are a senior Go architect with 10+ years of experience, specializing in high-performance storage systems and AI infrastructure.

# Goal

Implement a lightweight, Redis protocol-compatible **in-memory vector database** from scratch using Go. The project is named `Vex`.

**Target Audience**: Technical interview preparation (targeting senior engineer positions) and open-source education.

**Key Focus Areas**: High-concurrency architecture, RESP protocol parsing, and vector similarity computation.

# Tech Stack

- **Language**: Go (Golang) 1.25+
- **Protocol**: RESP (Redis Serialization Protocol) - enables direct connection via redis-cli
- **Storage**: Pure in-memory (with optional WAL persistence for future versions)
- **Vector Index**:
  1. **Phase 1**: Flat search with concurrent acceleration
  2. **Phase 2**: HNSW (Hierarchical Navigable Small World) index interface

# Core Features & Requirements (MVP Version)

## 1. Network Layer (TCP Server)

- Use Go's native `net` package
- Implement **Goroutine-per-connection** model for high-concurrency handling
- Build a custom RESP parser (no third-party libraries - demonstrate wheel-building capability)

## 2. Data Model (Data Schema)

- Support standard KV operations: `SET key value`, `GET key`
- **Core differentiation**: Support vector storage
- New command: `VSET key [0.12, 0.34, 0.55, ...]` (store vector)
- New command: `VSEARCH vector k` (find top-k most similar vectors)

## 3. Core Algorithm (Vector Engine)

- Implement **Euclidean Distance** and **Cosine Similarity** computation functions
- **Performance requirement**: Efficient computation with consideration for floating-point optimization in Go (SIMD-aware comments)

## 4. Concurrency Control

- **Avoid** a single global lock (`sync.Mutex`)
- Design a **sharded storage** structure: distribute data across 16-32 shards, each with independent locking to reduce contention

# Output Request

Please provide the following in stages:

1. **Project Directory Structure**: Follow Go Standard Layout
2. **Core Interface Definitions**: Particularly `StorageEngine` and `VectorIndex` interfaces
3. **Key Implementation**:
   - Sharded map implementation with per-shard locking
   - Vector similarity computation functions
   - Basic RESP parser skeleton
4. **Interview Highlights**: Explain how the design addresses "high-performance" and "AI-scale" requirements