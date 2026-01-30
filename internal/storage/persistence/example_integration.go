// Copyright 2025 uzqw
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package persistence

// This file contains example code showing how to integrate persistence
// into the Vex server. This is for documentation purposes.

/*
Example integration in cmd/vex-server/main.go:

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/uzqw/vex/internal/storage"
	"github.com/uzqw/vex/internal/storage/persistence"
)

func main() {
	// Create storage
	store := storage.New()

	// Configure persistence
	persistConfig := persistence.Config{
		Enabled:         true,
		DataDir:         "./data",
		SnapshotSeconds: 300,  // Snapshot every 5 minutes
		KeepSnapshots:   3,    // Keep last 3 snapshots
		Compression:     "snappy",
		Checksum:        true,
	}

	// Create storage adapter
	adapter := persistence.NewStorageAdapter(store)

	// Create snapshot handler
	snapshotter := persistence.NewVectorSnapshot(persistConfig, adapter)

	// Create persistence manager
	manager, err := persistence.NewManager(persistConfig, snapshotter)
	if err != nil {
		log.Fatalf("Failed to create persistence manager: %v", err)
	}

	// Start persistence (loads snapshot and starts scheduler)
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		log.Printf("Warning: failed to load snapshot: %v", err)
		// Continue with empty database
	}

	// ... setup server, handlers, etc ...

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")

	// Create final snapshot before exit
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := manager.TriggerSnapshot(shutdownCtx); err != nil {
		log.Printf("Warning: failed to create final snapshot: %v", err)
	}

	if err := manager.Stop(shutdownCtx); err != nil {
		log.Printf("Warning: failed to stop persistence manager: %v", err)
	}

	log.Println("Shutdown complete")
}
```

Example with manual snapshot trigger (e.g., RESP command handler):

```go
func handleBGSAVE(manager *persistence.Manager) error {
	// Trigger background snapshot
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := manager.TriggerSnapshot(ctx); err != nil {
			log.Printf("Snapshot failed: %v", err)
		}
	}()

	return nil // Return immediately, snapshot runs in background
}

func handleINFO(manager *persistence.Manager) (map[string]interface{}, error) {
	stats := manager.GetStats()
	info, err := manager.GetSnapshot().GetLastSnapshotInfo()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"last_snapshot_time":     stats.LastSnapshotTime,
		"last_snapshot_duration": stats.LastSnapshotDuration,
		"snapshot_in_progress":   stats.SnapshotInProgress,
		"total_snapshots":        stats.TotalSnapshots,
		"snapshot_errors":        stats.SnapshotErrors,
		"last_error":             stats.LastError,
		"snapshot_info":          info,
	}, nil
}
```

Example with configuration from environment:

```go
func loadPersistenceConfig() persistence.Config {
	config := persistence.DefaultConfig()

	if enabled := os.Getenv("VEX_PERSISTENCE_ENABLED"); enabled == "true" {
		config.Enabled = true
	}

	if dataDir := os.Getenv("VEX_DATA_DIR"); dataDir != "" {
		config.DataDir = dataDir
	}

	if interval := os.Getenv("VEX_SNAPSHOT_SECONDS"); interval != "" {
		if seconds, err := strconv.Atoi(interval); err == nil {
			config.SnapshotSeconds = seconds
		}
	}

	return config
}
```

*/
