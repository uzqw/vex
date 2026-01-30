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
	"log/slog"
	"sync"
	"time"
)

// Scheduler handles periodic snapshot creation
type Scheduler struct {
	interval    time.Duration
	snapshotter Snapshotter
	stats       *Stats

	ticker  *time.Ticker
	stopCh  chan struct{}
	wg      sync.WaitGroup
	running bool
	mu      sync.Mutex
}

// NewScheduler creates a new snapshot scheduler
func NewScheduler(interval time.Duration, snapshotter Snapshotter, stats *Stats) *Scheduler {
	return &Scheduler{
		interval:    interval,
		snapshotter: snapshotter,
		stats:       stats,
		stopCh:      make(chan struct{}),
	}
}

// Start begins the periodic snapshot scheduler
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	s.running = true
	s.ticker = time.NewTicker(s.interval)

	s.wg.Add(1)
	go s.run(ctx)

	slog.Info("Snapshot scheduler started", "interval", s.interval)
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	close(s.stopCh)
	s.ticker.Stop()
	s.wg.Wait()
	s.running = false

	slog.Info("Snapshot scheduler stopped")
}

// run is the main scheduler loop
func (s *Scheduler) run(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-s.stopCh:
			return
		case <-s.ticker.C:
			s.triggerSnapshot(ctx)
		}
	}
}

// triggerSnapshot executes a snapshot and updates stats
func (s *Scheduler) triggerSnapshot(ctx context.Context) {
	// Check if snapshot is already in progress
	if s.stats.SnapshotInProgress {
		slog.Warn("Skipping scheduled snapshot: another snapshot is in progress")
		return
	}

	start := time.Now()
	s.stats.SnapshotInProgress = true

	slog.Info("Starting scheduled snapshot")

	err := s.snapshotter.Save(ctx)

	duration := time.Since(start)
	s.stats.SnapshotInProgress = false
	s.stats.LastSnapshotTime = time.Now()
	s.stats.LastSnapshotDuration = duration

	if err != nil {
		s.stats.SnapshotErrors++
		s.stats.LastError = err
		slog.Error("Scheduled snapshot failed", "error", err, "duration", duration)
	} else {
		s.stats.TotalSnapshots++
		slog.Info("Scheduled snapshot completed", "duration", duration)
	}
}
