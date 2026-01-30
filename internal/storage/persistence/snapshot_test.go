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
