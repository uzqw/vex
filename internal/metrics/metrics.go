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

package metrics

import (
	"encoding/json"
	"runtime"
	"sync/atomic"
	"time"
)

// Stats holds all system metrics using atomic operations for thread-safety
// This design avoids mutex overhead and provides lock-free performance monitoring
type Stats struct {
	// Core counters
	totalCommands     atomic.Uint64 // Total number of commands processed
	activeConnections atomic.Int64  // Current number of active connections
	totalKeys         atomic.Uint64 // Total number of keys stored
	memoryUsage       atomic.Uint64 // Approximate memory usage in bytes

	// Timing
	startTime time.Time // Server start time for uptime calculation
}

// Global stats instance
var global = &Stats{
	startTime: time.Now(),
}

// Global returns the global stats instance
func Global() *Stats {
	return global
}

// IncrementCommands increments the total command counter
func (s *Stats) IncrementCommands() {
	s.totalCommands.Add(1)
}

// IncrementActiveConnections increments the active connection counter
func (s *Stats) IncrementActiveConnections() {
	s.activeConnections.Add(1)
}

// DecrementActiveConnections decrements the active connection counter
func (s *Stats) DecrementActiveConnections() {
	s.activeConnections.Add(-1)
}

// IncrementKeys increments the total keys counter
func (s *Stats) IncrementKeys() {
	s.totalKeys.Add(1)
}

// DecrementKeys decrements the total keys counter
func (s *Stats) DecrementKeys() {
	s.totalKeys.Add(^uint64(0)) // Atomic decrement by 1
}

// SetMemoryUsage sets the approximate memory usage
func (s *Stats) SetMemoryUsage(bytes uint64) {
	s.memoryUsage.Store(bytes)
}

// GetTotalCommands returns the total number of commands processed
func (s *Stats) GetTotalCommands() uint64 {
	return s.totalCommands.Load()
}

// GetActiveConnections returns the current number of active connections
func (s *Stats) GetActiveConnections() int64 {
	return s.activeConnections.Load()
}

// GetTotalKeys returns the total number of keys stored
func (s *Stats) GetTotalKeys() uint64 {
	return s.totalKeys.Load()
}

// GetMemoryUsage returns the approximate memory usage in bytes
func (s *Stats) GetMemoryUsage() uint64 {
	return s.memoryUsage.Load()
}

// GetUptime returns the server uptime duration
func (s *Stats) GetUptime() time.Duration {
	return time.Since(s.startTime)
}

// Snapshot represents a point-in-time view of all metrics
type Snapshot struct {
	Goroutines        int     `json:"goroutines"`
	TotalCommands     uint64  `json:"total_commands"`
	ActiveConnections int64   `json:"active_connections"`
	TotalKeys         uint64  `json:"total_keys"`
	MemoryUsageMB     float64 `json:"memory_usage_mb"`
	Uptime            string  `json:"uptime"`
	QPS               float64 `json:"qps"` // Queries per second
}

// Snapshot creates a consistent snapshot of all metrics
func (s *Stats) Snapshot() *Snapshot {
	uptime := s.GetUptime()
	totalCommands := s.GetTotalCommands()

	// Calculate QPS (queries per second) based on total commands and uptime
	var qps float64
	if uptime.Seconds() > 0 {
		qps = float64(totalCommands) / uptime.Seconds()
	}

	return &Snapshot{
		Goroutines:        runtime.NumGoroutine(),
		TotalCommands:     totalCommands,
		ActiveConnections: s.GetActiveConnections(),
		TotalKeys:         s.GetTotalKeys(),
		MemoryUsageMB:     float64(s.GetMemoryUsage()) / 1024 / 1024,
		Uptime:            uptime.String(),
		QPS:               qps,
	}
}

// JSON returns the metrics snapshot as a JSON string
func (s *Stats) JSON() (string, error) {
	snapshot := s.Snapshot()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
