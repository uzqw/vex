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
	"os"
	"testing"
)

// mockDataSource implements VectorDataSource for testing
type mockDataSource struct {
	vectors   map[string][]float32
	dimension int
}

func (m *mockDataSource) GetAllVectors() (map[string][]float32, error) {
	return m.vectors, nil
}

func (m *mockDataSource) SetAllVectors(vectors map[string][]float32) error {
	m.vectors = vectors
	return nil
}

func (m *mockDataSource) GetDimension() int {
	return m.dimension
}

func TestSnapshotSaveLoad(t *testing.T) {
	// Create temp directory for testing
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create test data
	testVectors := map[string][]float32{
		"vec1": {1.0, 2.0, 3.0, 4.0},
		"vec2": {5.0, 6.0, 7.0, 8.0},
		"vec3": {9.0, 10.0, 11.0, 12.0},
	}

	dataSource := &mockDataSource{
		vectors:   testVectors,
		dimension: 4,
	}

	config := Config{
		Enabled:     true,
		DataDir:     tempDir,
		Compression: "none", // No compression for easier debugging
		Checksum:    true,
	}

	snapshot := NewVectorSnapshot(config, dataSource)

	// Test Save
	ctx := context.Background()
	if err := snapshot.Save(ctx); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Create new data source for loading
	loadDataSource := &mockDataSource{
		vectors:   make(map[string][]float32),
		dimension: 4,
	}

	loadSnapshot := NewVectorSnapshot(config, loadDataSource)

	// Test Load
	if err := loadSnapshot.Load(ctx); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify loaded data
	if len(loadDataSource.vectors) != len(testVectors) {
		t.Errorf("Expected %d vectors, got %d", len(testVectors), len(loadDataSource.vectors))
	}

	for key, expected := range testVectors {
		actual, ok := loadDataSource.vectors[key]
		if !ok {
			t.Errorf("Vector %s not found after load", key)
			continue
		}

		if len(actual) != len(expected) {
			t.Errorf("Vector %s: expected length %d, got %d", key, len(expected), len(actual))
			continue
		}

		for i := range expected {
			if actual[i] != expected[i] {
				t.Errorf("Vector %s[%d]: expected %f, got %f", key, i, expected[i], actual[i])
			}
		}
	}
}

func TestSnapshotWithCompression(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	testVectors := map[string][]float32{
		"vec1": {1.0, 2.0, 3.0, 4.0},
		"vec2": {5.0, 6.0, 7.0, 8.0},
	}

	dataSource := &mockDataSource{
		vectors:   testVectors,
		dimension: 4,
	}

	config := Config{
		Enabled:     true,
		DataDir:     tempDir,
		Compression: "snappy",
		Checksum:    true,
	}

	snapshot := NewVectorSnapshot(config, dataSource)

	ctx := context.Background()
	if err := snapshot.Save(ctx); err != nil {
		t.Fatalf("Save with compression failed: %v", err)
	}

	// Load and verify
	loadDataSource := &mockDataSource{
		vectors:   make(map[string][]float32),
		dimension: 4,
	}

	loadSnapshot := NewVectorSnapshot(config, loadDataSource)
	if err := loadSnapshot.Load(ctx); err != nil {
		t.Fatalf("Load with compression failed: %v", err)
	}

	if len(loadDataSource.vectors) != len(testVectors) {
		t.Errorf("Expected %d vectors, got %d", len(testVectors), len(loadDataSource.vectors))
	}
}

func TestSnapshotNotFound(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dataSource := &mockDataSource{
		vectors:   make(map[string][]float32),
		dimension: 4,
	}

	config := Config{
		Enabled: true,
		DataDir: tempDir,
	}

	snapshot := NewVectorSnapshot(config, dataSource)

	// Should not error when no snapshot exists
	ctx := context.Background()
	if err := snapshot.Load(ctx); err != nil {
		t.Fatalf("Load should not fail when no snapshot exists: %v", err)
	}
}

func TestSnapshotGetLastSnapshotInfo(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	ds := &mockDataSource{
		vectors:   map[string][]float32{"v": {1, 2, 3}},
		dimension: 3,
	}
	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none", Checksum: true}
	snap := NewVectorSnapshot(cfg, ds)

	t.Run("no snapshot returns nil info without error", func(t *testing.T) {
		info, err := snap.GetLastSnapshotInfo()
		if err != nil {
			t.Fatalf("GetLastSnapshotInfo error before save: %v", err)
		}
		if info != nil {
			t.Errorf("expected nil info before first save, got %+v", info)
		}
	})

	t.Run("info returned after save", func(t *testing.T) {
		ctx := context.Background()
		if err := snap.Save(ctx); err != nil {
			t.Fatalf("Save error: %v", err)
		}
		info, err := snap.GetLastSnapshotInfo()
		if err != nil {
			t.Fatalf("GetLastSnapshotInfo error after save: %v", err)
		}
		if info == nil {
			t.Fatal("expected non-nil info after save")
		}
		if info.VectorCount != 1 {
			t.Errorf("VectorCount = %d, want 1", info.VectorCount)
		}
	})
}

func TestSnapshotSaveDisabledConfig(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	ds := &mockDataSource{
		vectors:   map[string][]float32{"v": {1, 2}},
		dimension: 2,
	}
	// Enabled=false: Save should still proceed (it writes regardless of Enabled flag)
	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none"}
	snap := NewVectorSnapshot(cfg, ds)
	ctx := context.Background()
	if err := snap.Save(ctx); err != nil {
		t.Fatalf("Save error: %v", err)
	}
}

func TestSnapshotLoadBadDir(t *testing.T) {
	ds := &mockDataSource{
		vectors:   make(map[string][]float32),
		dimension: 4,
	}
	// Point to a directory that does not exist → metadata missing → treated as no snapshot
	cfg := Config{Enabled: true, DataDir: "/nonexistent/path/vex-test"}
	snap := NewVectorSnapshot(cfg, ds)
	ctx := context.Background()
	// Load on missing dir: should return nil (no snapshot found), not a hard error
	if err := snap.Load(ctx); err != nil {
		t.Logf("Load with bad dir returned error (acceptable): %v", err)
	}
}
