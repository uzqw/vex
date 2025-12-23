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
	"strings"
	"testing"
	"time"
)

func TestGlobal(t *testing.T) {
	g := Global()
	if g == nil {
		t.Fatal("Global() returned nil")
	}

	// Should return same instance
	g2 := Global()
	if g != g2 {
		t.Error("Global() should return the same instance")
	}
}

func TestStatsCommands(t *testing.T) {
	s := &Stats{startTime: time.Now()}

	initial := s.GetTotalCommands()
	s.IncrementCommands()
	s.IncrementCommands()
	s.IncrementCommands()

	got := s.GetTotalCommands() - initial
	if got != 3 {
		t.Errorf("After 3 increments, got %d, want 3", got)
	}
}

func TestStatsActiveConnections(t *testing.T) {
	s := &Stats{startTime: time.Now()}

	s.IncrementActiveConnections()
	s.IncrementActiveConnections()
	if s.GetActiveConnections() != 2 {
		t.Errorf("GetActiveConnections() = %d, want 2", s.GetActiveConnections())
	}

	s.DecrementActiveConnections()
	if s.GetActiveConnections() != 1 {
		t.Errorf("GetActiveConnections() after decrement = %d, want 1", s.GetActiveConnections())
	}
}

func TestStatsKeys(t *testing.T) {
	s := &Stats{startTime: time.Now()}

	s.IncrementKeys()
	s.IncrementKeys()
	s.IncrementKeys()

	if s.GetTotalKeys() != 3 {
		t.Errorf("GetTotalKeys() = %d, want 3", s.GetTotalKeys())
	}

	s.DecrementKeys()
	if s.GetTotalKeys() != 2 {
		t.Errorf("GetTotalKeys() after decrement = %d, want 2", s.GetTotalKeys())
	}
}

func TestStatsMemoryUsage(t *testing.T) {
	s := &Stats{startTime: time.Now()}

	s.SetMemoryUsage(1024 * 1024 * 100) // 100 MB

	if s.GetMemoryUsage() != 104857600 {
		t.Errorf("GetMemoryUsage() = %d, want 104857600", s.GetMemoryUsage())
	}
}

func TestStatsUptime(t *testing.T) {
	s := &Stats{startTime: time.Now().Add(-time.Second * 5)}

	uptime := s.GetUptime()
	if uptime < time.Second*4 || uptime > time.Second*6 {
		t.Errorf("GetUptime() = %v, expected around 5s", uptime)
	}
}

func TestSnapshot(t *testing.T) {
	s := &Stats{startTime: time.Now().Add(-time.Second * 10)}

	s.IncrementCommands()
	s.IncrementCommands()
	s.IncrementActiveConnections()
	s.IncrementKeys()
	s.SetMemoryUsage(1024 * 1024)

	snapshot := s.Snapshot()

	if snapshot.TotalCommands < 2 {
		t.Errorf("Snapshot.TotalCommands = %d, want >= 2", snapshot.TotalCommands)
	}
	if snapshot.ActiveConnections != 1 {
		t.Errorf("Snapshot.ActiveConnections = %d, want 1", snapshot.ActiveConnections)
	}
	if snapshot.TotalKeys < 1 {
		t.Errorf("Snapshot.TotalKeys = %d, want >= 1", snapshot.TotalKeys)
	}
	if snapshot.MemoryUsageMB < 0.9 || snapshot.MemoryUsageMB > 1.1 {
		t.Errorf("Snapshot.MemoryUsageMB = %f, want ~1.0", snapshot.MemoryUsageMB)
	}
	if snapshot.Goroutines <= 0 {
		t.Error("Snapshot.Goroutines should be > 0")
	}
	if snapshot.QPS <= 0 {
		t.Error("Snapshot.QPS should be > 0")
	}
	if snapshot.Uptime == "" {
		t.Error("Snapshot.Uptime should not be empty")
	}
}

func TestJSON(t *testing.T) {
	s := &Stats{startTime: time.Now()}

	s.IncrementCommands()
	s.IncrementActiveConnections()
	s.IncrementKeys()
	s.SetMemoryUsage(1024)

	jsonStr, err := s.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	// Verify it's valid JSON
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("JSON() returned invalid JSON: %v", err)
	}

	// Check required fields exist
	requiredFields := []string{"goroutines", "total_commands", "active_connections", "total_keys", "memory_usage_mb", "uptime", "qps"}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("JSON() missing field: %s", field)
		}
	}

	// Verify pretty printing
	if !strings.Contains(jsonStr, "\n") {
		t.Error("JSON() should be pretty printed with newlines")
	}
}
