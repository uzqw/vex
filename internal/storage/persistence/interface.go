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

import (
	"context"
	"time"
)

// Snapshotter defines the interface for creating and loading snapshots
type Snapshotter interface {
	// Save creates a snapshot of the current state
	Save(ctx context.Context) error

	// Load restores state from the latest snapshot
	Load(ctx context.Context) error

	// GetLastSnapshotInfo returns information about the last snapshot
	GetLastSnapshotInfo() (*SnapshotInfo, error)
}

// SnapshotInfo contains metadata about a snapshot
type SnapshotInfo struct {
	Version     string    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	VectorCount int       `json:"vector_count"`
	Dimension   int       `json:"dimension"`
	IndexType   string    `json:"index_type"`
	SizeBytes   int64     `json:"size_bytes"`
	Checksum    string    `json:"checksum"`
	DurationMs  int64     `json:"duration_ms"`
	Compressed  bool      `json:"compressed"`
	Compression string    `json:"compression,omitempty"`
}

// Config holds persistence configuration
type Config struct {
	// Enable persistence
	Enabled bool

	// Data directory path
	DataDir string

	// Snapshot interval in seconds (0 = manual only)
	SnapshotSeconds int

	// Keep N most recent snapshots
	KeepSnapshots int

	// Compression algorithm: "none", "snappy", "gzip"
	Compression string

	// Enable checksum verification
	Checksum bool
}

// DefaultConfig returns default persistence configuration
func DefaultConfig() Config {
	return Config{
		Enabled:         false, // Disabled by default for backward compatibility
		DataDir:         "./data",
		SnapshotSeconds: 300, // 5 minutes
		KeepSnapshots:   3,
		Compression:     "snappy",
		Checksum:        true,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.DataDir == "" {
		return ErrInvalidConfig
	}
	if c.KeepSnapshots < 1 {
		return ErrInvalidConfig
	}
	return nil
}

// Manager manages persistence operations including scheduling
type Manager struct {
	config    Config
	snapshot  Snapshotter
	scheduler *Scheduler
	stats     *Stats
}

// NewManager creates a new persistence manager
func NewManager(config Config, snapshot Snapshotter) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	m := &Manager{
		config:   config,
		snapshot: snapshot,
		stats:    NewStats(),
	}

	// Create scheduler if auto-snapshot is enabled
	if config.Enabled && config.SnapshotSeconds > 0 {
		m.scheduler = NewScheduler(
			time.Duration(config.SnapshotSeconds)*time.Second,
			m.snapshot,
			m.stats,
		)
	}

	return m, nil
}

// Start begins the persistence manager (starts scheduler if configured)
func (m *Manager) Start(ctx context.Context) error {
	if !m.config.Enabled {
		return nil
	}

	// Load existing snapshot on startup
	if err := m.snapshot.Load(ctx); err != nil {
		// Log warning but don't fail startup
		// Allow starting with empty database
		return err
	}

	// Start background scheduler
	if m.scheduler != nil {
		m.scheduler.Start(ctx)
	}

	return nil
}

// Stop gracefully stops the persistence manager
func (m *Manager) Stop(ctx context.Context) error {
	if m.scheduler != nil {
		m.scheduler.Stop()
	}
	return nil
}

// TriggerSnapshot manually triggers a snapshot
func (m *Manager) TriggerSnapshot(ctx context.Context) error {
	return m.snapshot.Save(ctx)
}

// GetStats returns persistence statistics
func (m *Manager) GetStats() *Stats {
	return m.stats
}

// Stats tracks persistence statistics
type Stats struct {
	LastSnapshotTime     time.Time
	LastSnapshotDuration time.Duration
	SnapshotInProgress   bool
	TotalSnapshots       int64
	SnapshotErrors       int64
	LastError            error
}

// NewStats creates a new Stats instance
func NewStats() *Stats {
	return &Stats{}
}
