# Persistence Package

Snapshot-based persistence for Vex vector database (RDB-style, similar to Redis).

## Quick Start

```go
import (
    "context"
    "github.com/uzqw/vex/internal/storage"
    "github.com/uzqw/vex/internal/storage/persistence"
)

// 1. Create storage
store := storage.New()

// 2. Configure persistence
config := persistence.Config{
    Enabled:         true,
    DataDir:         "./data",
    SnapshotSeconds: 300,      // 5 minutes
    KeepSnapshots:   3,        // Keep 3 snapshots
    Compression:     "snappy", // Fast compression
    Checksum:        true,
}

// 3. Setup persistence
adapter := persistence.NewStorageAdapter(store)
snapshotter := persistence.NewVectorSnapshot(config, adapter)
manager, _ := persistence.NewManager(config, snapshotter)

// 4. Start (loads snapshot + starts scheduler)
ctx := context.Background()
manager.Start(ctx)

// 5. Trigger manual snapshot
manager.TriggerSnapshot(ctx)

// 6. Graceful shutdown
manager.Stop(ctx)
```

## Architecture

```
Storage (in-memory)
    ↓
StorageAdapter (implements VectorDataSource)
    ↓
VectorSnapshot (implements Snapshotter)
    ↓
Manager (orchestration + scheduling)
```

## Files Created

```
data/
└── latest/
    ├── metadata.json   # Snapshot info
    └── vectors.rdb     # Binary vector data
```

## Configuration Options

| Field | Default | Description |
|-------|---------|-------------|
| `Enabled` | `false` | Enable persistence |
| `DataDir` | `"./data"` | Data directory path |
| `SnapshotSeconds` | `300` | Auto-snapshot interval (0=disabled) |
| `KeepSnapshots` | `3` | Number of snapshots to keep |
| `Compression` | `"snappy"` | Compression: "none", "snappy", "gzip" |
| `Checksum` | `true` | Enable checksum verification |

## API

### Manager Methods

```go
// Start loads snapshot and starts scheduler
func (m *Manager) Start(ctx context.Context) error

// Stop gracefully shuts down
func (m *Manager) Stop(ctx context.Context) error

// TriggerSnapshot manually creates a snapshot
func (m *Manager) TriggerSnapshot(ctx context.Context) error

// GetStats returns persistence statistics
func (m *Manager) GetStats() *Stats
```

### Snapshotter Interface

```go
type Snapshotter interface {
    Save(ctx context.Context) error
    Load(ctx context.Context) error
    GetLastSnapshotInfo() (*SnapshotInfo, error)
}
```

## File Format

### metadata.json
```json
{
  "version": "v1",
  "created_at": "2026-01-30T16:00:00Z",
  "vector_count": 10000,
  "dimension": 128,
  "index_type": "memory",
  "size_bytes": 532000,
  "checksum": "sha256:abc123...",
  "duration_ms": 1234,
  "compressed": true,
  "compression": "snappy"
}
```

### vectors.rdb (binary)
```
[Header: 16 bytes]
  Magic: "VEX\0" (4)
  Version: uint32 (4)
  Count: uint64 (8)

[Entries: N vectors]
  KeyLen: uint16 (2)
  Key: string (variable)
  Vector: []float32 (dim * 4)
```

## Testing

```bash
# Run tests
go test ./internal/storage/persistence/...

# Run with race detector
go test -race ./internal/storage/persistence/...

# Benchmark
go test -bench=. ./internal/storage/persistence/...
```

## Performance

| Vectors | Dimension | Save Time | Load Time | Size (compressed) | Use Case |
|---------|-----------|-----------|-----------|-------------------|----------|
| 10K     | 1536D     | ~50ms     | ~250ms    | ~60 MB            | OpenAI embeddings |
| 100K    | 1024D     | ~1s       | ~5s       | ~400 MB           | CLIP embeddings |
| 1M      | 1024D     | ~10s      | ~50s      | ~2.5 GB           | Large production |

**Note**: Production workloads typically use 768D-2048D dimensions. Smaller dimensions are not representative of real-world use cases.

## Error Handling

```go
// Corrupted snapshot -> logs warning, starts empty
// Missing snapshot -> logs info, starts empty
// Checksum mismatch -> returns error
// Disk full -> returns error
```

## Monitoring

### Metrics (via GetStats())
- `LastSnapshotTime` - When last snapshot completed
- `LastSnapshotDuration` - How long it took
- `SnapshotInProgress` - Boolean flag
- `TotalSnapshots` - Success count
- `SnapshotErrors` - Error count
- `LastError` - Most recent error

### Logs
```
INFO: Snapshot started (background)
INFO: Snapshot completed in 1.23s (1M vectors, 532 MB)
ERROR: Snapshot failed: disk full
WARN: No snapshot found, starting with empty database
```

## Future Enhancements (v0.4.0)

- WAL (Write-Ahead Log) support
- HNSW index persistence
- Incremental snapshots
- Point-in-time recovery
- S3/GCS cloud storage support

## See Also

- [Design doc](../../../docs/PERSISTENCE.md) - Detailed design
- [Integration example](example_integration.go) - Usage examples
- [Tests](snapshot_test.go) - Test cases
