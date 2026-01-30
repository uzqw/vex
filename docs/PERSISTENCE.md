# Persistence Design (Snapshot-based)

## Overview

Vex implements RDB-style snapshot persistence similar to Redis. This provides data durability while maintaining high performance for in-memory operations.

## Architecture

```
┌─────────────────────────────────────────┐
│          Storage Layer                  │
│  (In-memory: Shards + HNSW Index)      │
└───────────────┬─────────────────────────┘
                │
                │ Snapshot Trigger
                ▼
┌─────────────────────────────────────────┐
│      Persistence Manager                │
│  - Background snapshot scheduler        │
│  - Atomic snapshot creation             │
│  - Recovery on startup                  │
└───────────────┬─────────────────────────┘
                │
                ▼
┌─────────────────────────────────────────┐
│          Snapshot Files                 │
│  - vectors.rdb    (vector data)         │
│  - hnsw.rdb      (index structure)      │
│  - metadata.json (version, timestamp)   │
└─────────────────────────────────────────┘
```

## File Format

### 1. Snapshot Metadata (`metadata.json`)
```json
{
  "version": "0.2.0",
  "created_at": "2026-01-30T16:00:00Z",
  "vector_count": 10000,
  "dimension": 128,
  "index_type": "hnsw",
  "checksum": "sha256:abc123..."
}
```

### 2. Vector Data (`vectors.rdb`)
Binary format optimized for fast loading:
```
[Header: 16 bytes]
  - Magic: "VEX\0" (4 bytes)
  - Version: uint32 (4 bytes)
  - Count: uint64 (8 bytes)

[Vector Entries: N * (key_len + key + dim * 4) bytes]
  For each vector:
    - Key length: uint16 (2 bytes)
    - Key: string (variable)
    - Vector: []float32 (dim * 4 bytes)
```

### 3. HNSW Index (`hnsw.rdb`)
```
[Header: 24 bytes]
  - Magic: "HNSW" (4 bytes)
  - Version: uint32 (4 bytes)
  - Node count: uint64 (8 bytes)
  - Max level: uint32 (4 bytes)
  - Entry point: uint32 (4 bytes)

[Node Entries]
  For each node:
    - Key: string
    - Level: uint32
    - Connections per level: [][]string (serialized)
```

## Snapshot Strategy

### Trigger Conditions
1. **Time-based**: Every N seconds (default: 300s / 5 minutes)
2. **Write-based**: After N write operations (default: disabled)
3. **Manual**: Via API call (`BGSAVE` command)

### Process Flow
```
1. Create temp directory: data/temp-{timestamp}/
2. Fork snapshot operation (goroutine)
3. Serialize vector data → temp/vectors.rdb.tmp
4. Serialize HNSW index → temp/hnsw.rdb.tmp
5. Write metadata → temp/metadata.json
6. Verify checksums
7. Atomic rename: temp/ → data/latest/
8. Clean up old snapshots (keep last N)
```

### Copy-on-Write Strategy
```go
// Take snapshot of current state without blocking writes
snapshot := &Snapshot{
    vectors: s.copyVectors(),  // Read-locked copy
    index:   s.index.snapshot(), // HNSW snapshot
}

// Serialize in background
go snapshot.saveToDisk(path)
```

## Recovery

### Startup Flow
```
1. Check if data directory exists
2. Load metadata.json
3. Verify checksums
4. Load vectors.rdb → Storage shards
5. Load hnsw.rdb → Rebuild HNSW index
6. Validate vector count matches
7. Mark as ready
```

### Error Handling
- Corrupted snapshot → Try previous snapshot
- Missing files → Start with empty database
- Checksum mismatch → Log error, skip loading

## Configuration

```go
type PersistenceConfig struct {
    // Enable persistence
    Enabled bool

    // Data directory path
    DataDir string

    // Snapshot interval in seconds (0 = disabled)
    SnapshotSeconds int

    // Keep N most recent snapshots
    KeepSnapshots int

    // Compression (gzip, snappy, none)
    Compression string
}
```

### Default Configuration
```yaml
persistence:
  enabled: true
  data_dir: "./data"
  snapshot_seconds: 300  # 5 minutes
  keep_snapshots: 3
  compression: "snappy"  # Fast compression
```

## API

### Commands
```bash
# Trigger background snapshot
BGSAVE

# Trigger blocking snapshot (for testing)
SAVE

# Get last snapshot info
INFO persistence

# Disable auto-snapshot
CONFIG SET save ""
```

### Response Format
```json
{
  "last_snapshot_time": "2026-01-30T16:00:00Z",
  "last_snapshot_duration_ms": 1234,
  "snapshot_in_progress": false,
  "total_snapshots": 3
}
```

## Performance Impact

### Write Path
- **No impact**: Snapshots run in background
- **Memory**: Temporary copy during snapshot (~2x memory peak)
- **CPU**: <5% during snapshot

### Read Path
- **No impact**: Reads always from memory

### Startup Time
| Vector Count | Dimension | Load Time | Use Case |
|--------------|-----------|-----------|----------|
| 10K          | 1536D     | ~250ms    | Small production dataset (OpenAI embeddings) |
| 100K         | 1024D     | ~5s       | Medium production dataset (CLIP embeddings) |
| 1M           | 1024D     | ~50s      | Large production dataset |
| 10M          | 768D      | ~8min     | Very large production dataset |

**Note**: Load times scale linearly with data size. Use SSD storage for optimal performance.

### Disk Space
```
Uncompressed: vector_count * (key_size + dim * 4) bytes
Compressed (snappy): ~60-70% of uncompressed

Production examples:
- 1M vectors, 1024D, 20-byte keys
  = 1M * (20 + 1024*4) = 4.1 GB uncompressed
  = ~2.5 GB compressed (snappy)

- 1M vectors, 1536D, 20-byte keys (OpenAI embeddings)
  = 1M * (20 + 1536*4) = 6.2 GB uncompressed
  = ~3.7 GB compressed (snappy)
```

## Implementation Phases

### Phase 1: Basic Snapshot (Week 1)
- [x] Design interfaces
- [ ] Implement vector serialization
- [ ] Implement basic recovery
- [ ] Add snapshot command
- [ ] Unit tests

### Phase 2: HNSW Persistence (Week 1-2)
- [ ] Serialize HNSW graph structure
- [ ] Deserialize and rebuild index
- [ ] Verify index correctness after recovery
- [ ] Integration tests

### Phase 3: Production Features (Week 2)
- [ ] Background snapshot scheduler
- [ ] Compression support
- [ ] Checksum verification
- [ ] Snapshot rotation
- [ ] Metrics and monitoring

### Phase 4: Optimization (Week 3)
- [ ] Parallel serialization
- [ ] Incremental snapshots
- [ ] Memory-mapped loading
- [ ] Benchmark and tune

## Testing Strategy

### Unit Tests
```go
func TestSnapshotSaveLoad(t *testing.T)
func TestCorruptedSnapshot(t *testing.T)
func TestConcurrentSnapshotAndWrites(t *testing.T)
func TestSnapshotRotation(t *testing.T)
```

### Integration Tests
```go
func TestFullRecovery(t *testing.T)
func TestLargeDatasetSnapshot(t *testing.T)
func TestCrashRecovery(t *testing.T)
```

### Benchmark Tests
```go
func BenchmarkSnapshotCreation(b *testing.B)
func BenchmarkSnapshotLoad(b *testing.B)
```

## Monitoring

### Metrics to Track
```go
snapshot_last_success_timestamp
snapshot_last_duration_seconds
snapshot_size_bytes
snapshot_errors_total
snapshot_in_progress (gauge)
```

### Logs
```
INFO: Snapshot started (background)
INFO: Snapshot completed in 1.23s (1000000 vectors, 532 MB)
ERROR: Snapshot failed: disk full
WARN: Old snapshot corrupted, using previous backup
```

## Future Enhancements (v0.4.0+)

1. **WAL Support**: Add write-ahead log for zero data loss
2. **Incremental Snapshots**: Only save changed data
3. **Distributed Snapshots**: Snapshot across cluster nodes
4. **Cloud Storage**: S3/GCS/Azure Blob support
5. **Point-in-Time Recovery**: Snapshot + WAL replay

## References

- Redis RDB format: https://github.com/redis/redis/blob/unstable/src/rdb.c
- Weaviate persistence: https://weaviate.io/developers/weaviate/concepts/storage
- HNSW serialization: Based on hnswlib format
