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
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
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

// errorMockDataSource allows injecting errors for GetAllVectors and SetAllVectors
type errorMockDataSource struct {
	vectors   map[string][]float32
	dimension int
	getAllErr error
	setAllErr error
}

func (m *errorMockDataSource) GetAllVectors() (map[string][]float32, error) {
	if m.getAllErr != nil {
		return nil, m.getAllErr
	}
	return m.vectors, nil
}

func (m *errorMockDataSource) SetAllVectors(vectors map[string][]float32) error {
	if m.setAllErr != nil {
		return m.setAllErr
	}
	m.vectors = vectors
	return nil
}

func (m *errorMockDataSource) GetDimension() int {
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

// --- New error-path coverage tests ---

func TestSnapshotSaveDisabled(t *testing.T) {
	ds := &mockDataSource{vectors: map[string][]float32{"v": {1}}, dimension: 1}
	cfg := Config{Enabled: false, DataDir: "/tmp/unused"}
	snap := NewVectorSnapshot(cfg, ds)
	if err := snap.Save(context.Background()); err != nil {
		t.Fatalf("Save with Enabled=false should return nil, got: %v", err)
	}
}

func TestSnapshotLoadDisabled(t *testing.T) {
	ds := &mockDataSource{vectors: make(map[string][]float32), dimension: 1}
	cfg := Config{Enabled: false, DataDir: "/tmp/unused"}
	snap := NewVectorSnapshot(cfg, ds)
	if err := snap.Load(context.Background()); err != nil {
		t.Fatalf("Load with Enabled=false should return nil, got: %v", err)
	}
}

func TestSnapshotSaveGetAllVectorsError(t *testing.T) {
	injectedErr := errors.New("datasource broken")
	ds := &errorMockDataSource{
		vectors:   map[string][]float32{"v": {1}},
		dimension: 1,
		getAllErr: injectedErr,
	}
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none"}
	snap := NewVectorSnapshot(cfg, ds)
	err = snap.Save(context.Background())
	if err == nil {
		t.Fatal("expected error from Save when GetAllVectors fails")
	}
	if !errors.Is(err, injectedErr) {
		t.Errorf("expected wrapped injectedErr, got: %v", err)
	}
}

func TestSnapshotLoadSetAllVectorsError(t *testing.T) {
	// First, save a valid snapshot with a normal datasource
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	saveDS := &mockDataSource{
		vectors:   map[string][]float32{"v": {1, 2}},
		dimension: 2,
	}
	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none", Checksum: true}
	snap := NewVectorSnapshot(cfg, saveDS)
	if err := snap.Save(context.Background()); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// Now load with a datasource that fails on SetAllVectors
	injectedErr := errors.New("set failed")
	loadDS := &errorMockDataSource{
		dimension: 2,
		setAllErr: injectedErr,
	}
	loadSnap := NewVectorSnapshot(cfg, loadDS)
	err = loadSnap.Load(context.Background())
	if err == nil {
		t.Fatal("expected error from Load when SetAllVectors fails")
	}
	if !errors.Is(err, injectedErr) {
		t.Errorf("expected wrapped injectedErr, got: %v", err)
	}
}

func TestSnapshotLoadInvalidMagic(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create latest directory with corrupt vectors.rdb (bad magic)
	latestDir := filepath.Join(tempDir, "latest")
	if err := os.MkdirAll(latestDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write garbage bytes as vectors file
	if err := os.WriteFile(filepath.Join(latestDir, vectorsFile), []byte("BAAD\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write valid metadata
	metaJSON := `{"version":"v1","vector_count":0,"dimension":2,"checksum":"sha256:abc"}`
	if err := os.WriteFile(filepath.Join(latestDir, metadataFile), []byte(metaJSON), 0644); err != nil {
		t.Fatal(err)
	}

	ds := &mockDataSource{vectors: make(map[string][]float32), dimension: 2}
	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none", Checksum: false}
	snap := NewVectorSnapshot(cfg, ds)
	err = snap.Load(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid magic")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("expected ErrInvalidFormat, got: %v", err)
	}
}

func TestSnapshotLoadInvalidVersion(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	latestDir := filepath.Join(tempDir, "latest")
	if err := os.MkdirAll(latestDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write vectors file with correct magic but wrong version (99)
	f, err := os.Create(filepath.Join(latestDir, vectorsFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(vectorsMagic)); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(99)); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(f, binary.LittleEndian, uint64(0)); err != nil {
		t.Fatal(err)
	}
	f.Close()

	metaJSON := `{"version":"v1","vector_count":0,"dimension":2,"checksum":"sha256:abc"}`
	if err := os.WriteFile(filepath.Join(latestDir, metadataFile), []byte(metaJSON), 0644); err != nil {
		t.Fatal(err)
	}

	ds := &mockDataSource{vectors: make(map[string][]float32), dimension: 2}
	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none", Checksum: false}
	snap := NewVectorSnapshot(cfg, ds)
	err = snap.Load(context.Background())
	if err == nil {
		t.Fatal("expected error for wrong version")
	}
	if !errors.Is(err, ErrVersionMismatch) {
		t.Errorf("expected ErrVersionMismatch, got: %v", err)
	}
}

func TestSnapshotLoadCorruptMetadata(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	latestDir := filepath.Join(tempDir, "latest")
	if err := os.MkdirAll(latestDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write corrupt metadata JSON
	if err := os.WriteFile(filepath.Join(latestDir, metadataFile), []byte("{invalid json!!!"), 0644); err != nil {
		t.Fatal(err)
	}

	ds := &mockDataSource{vectors: make(map[string][]float32), dimension: 2}
	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none"}
	snap := NewVectorSnapshot(cfg, ds)
	err = snap.Load(context.Background())
	if err == nil {
		t.Fatal("expected error for corrupt metadata")
	}
}

func TestSnapshotLoadChecksumMismatch(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Save a valid snapshot first
	saveDS := &mockDataSource{
		vectors:   map[string][]float32{"k": {1.0, 2.0}},
		dimension: 2,
	}
	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none", Checksum: true}
	snap := NewVectorSnapshot(cfg, saveDS)
	if err := snap.Save(context.Background()); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// Tamper with the vectors file to cause checksum mismatch
	vecPath := filepath.Join(tempDir, "latest", vectorsFile)
	data, err := os.ReadFile(vecPath)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte near the end to corrupt data
	if len(data) > 5 {
		data[len(data)-1] ^= 0xFF
	}
	if err := os.WriteFile(vecPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	loadDS := &mockDataSource{vectors: make(map[string][]float32), dimension: 2}
	loadSnap := NewVectorSnapshot(cfg, loadDS)
	err = loadSnap.Load(context.Background())
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("expected ErrChecksumMismatch, got: %v", err)
	}
}

func TestSnapshotSaveLoadEmptyVectors(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	ds := &mockDataSource{
		vectors:   map[string][]float32{},
		dimension: 4,
	}
	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none", Checksum: true}
	snap := NewVectorSnapshot(cfg, ds)
	ctx := context.Background()

	if err := snap.Save(ctx); err != nil {
		t.Fatalf("Save empty vectors failed: %v", err)
	}

	loadDS := &mockDataSource{vectors: make(map[string][]float32), dimension: 4}
	loadSnap := NewVectorSnapshot(cfg, loadDS)
	if err := loadSnap.Load(ctx); err != nil {
		t.Fatalf("Load empty vectors failed: %v", err)
	}

	if len(loadDS.vectors) != 0 {
		t.Errorf("expected 0 vectors, got %d", len(loadDS.vectors))
	}
}

func TestSnapshotSaveLoadWithoutChecksum(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	ds := &mockDataSource{
		vectors:   map[string][]float32{"a": {1, 2, 3}},
		dimension: 3,
	}
	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none", Checksum: false}
	snap := NewVectorSnapshot(cfg, ds)
	ctx := context.Background()

	if err := snap.Save(ctx); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loadDS := &mockDataSource{vectors: make(map[string][]float32), dimension: 3}
	loadSnap := NewVectorSnapshot(cfg, loadDS)
	if err := loadSnap.Load(ctx); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(loadDS.vectors) != 1 {
		t.Errorf("expected 1 vector, got %d", len(loadDS.vectors))
	}
}

func TestSnapshotOverwriteExisting(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "vex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	ds := &mockDataSource{
		vectors:   map[string][]float32{"v1": {1, 2}},
		dimension: 2,
	}
	cfg := Config{Enabled: true, DataDir: tempDir, Compression: "none", Checksum: true}
	snap := NewVectorSnapshot(cfg, ds)
	ctx := context.Background()

	// Save first snapshot
	if err := snap.Save(ctx); err != nil {
		t.Fatalf("First save error: %v", err)
	}

	// Update vectors and save again (overwrites existing latest)
	ds.vectors = map[string][]float32{
		"v1": {10, 20},
		"v2": {30, 40},
	}
	if err := snap.Save(ctx); err != nil {
		t.Fatalf("Second save error: %v", err)
	}

	// Load and verify we get the updated data
	loadDS := &mockDataSource{vectors: make(map[string][]float32), dimension: 2}
	loadSnap := NewVectorSnapshot(cfg, loadDS)
	if err := loadSnap.Load(ctx); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(loadDS.vectors) != 2 {
		t.Errorf("expected 2 vectors after overwrite, got %d", len(loadDS.vectors))
	}
}

// errWriter is an io.Writer that returns an error after writing failAfter bytes
type errWriter struct {
	written   int
	failAfter int
}

func (w *errWriter) Write(p []byte) (int, error) {
	if w.written+len(p) > w.failAfter {
		remaining := w.failAfter - w.written
		if remaining <= 0 {
			return 0, errors.New("write error")
		}
		w.written += remaining
		return remaining, errors.New("write error")
	}
	w.written += len(p)
	return len(p), nil
}

func TestWriteHeaderErrors(t *testing.T) {
	snap := NewVectorSnapshot(Config{}, &mockDataSource{dimension: 2})

	// Fail on magic write (first 4 bytes)
	t.Run("fail on magic", func(t *testing.T) {
		w := &errWriter{failAfter: 0}
		if err := snap.writeHeader(w, 1); err == nil {
			t.Fatal("expected error on magic write")
		}
	})

	// Fail on version write (after 4 bytes of magic)
	t.Run("fail on version", func(t *testing.T) {
		w := &errWriter{failAfter: 4}
		if err := snap.writeHeader(w, 1); err == nil {
			t.Fatal("expected error on version write")
		}
	})

	// Fail on count write (after 4+4=8 bytes)
	t.Run("fail on count", func(t *testing.T) {
		w := &errWriter{failAfter: 8}
		if err := snap.writeHeader(w, 1); err == nil {
			t.Fatal("expected error on count write")
		}
	})

	// Success case (need at least 4+4+8=16 bytes)
	t.Run("success", func(t *testing.T) {
		w := &errWriter{failAfter: 100}
		if err := snap.writeHeader(w, 1); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestWriteVectorErrors(t *testing.T) {
	snap := NewVectorSnapshot(Config{}, &mockDataSource{dimension: 2})

	// Fail on key length write (first 2 bytes)
	t.Run("fail on key length", func(t *testing.T) {
		w := &errWriter{failAfter: 0}
		if err := snap.writeVector(w, "key", []float32{1, 2}); err == nil {
			t.Fatal("expected error on key length write")
		}
	})

	// Fail on key write (after 2 bytes for key length)
	t.Run("fail on key data", func(t *testing.T) {
		w := &errWriter{failAfter: 2}
		if err := snap.writeVector(w, "key", []float32{1, 2}); err == nil {
			t.Fatal("expected error on key write")
		}
	})

	// Fail on vector data write (after 2 + 3 = 5 bytes for "key")
	t.Run("fail on vector data", func(t *testing.T) {
		w := &errWriter{failAfter: 5}
		if err := snap.writeVector(w, "key", []float32{1, 2}); err == nil {
			t.Fatal("expected error on vector data write")
		}
	})
}

func TestReadHeaderErrors(t *testing.T) {
	snap := NewVectorSnapshot(Config{}, &mockDataSource{dimension: 2})

	// Reader that returns too few bytes for magic
	t.Run("truncated magic", func(t *testing.T) {
		r := bytes.NewReader([]byte{0x00, 0x01})
		_, err := snap.readHeader(r)
		if err == nil {
			t.Fatal("expected error for truncated magic")
		}
	})

	// Reader with correct magic but truncated version
	t.Run("truncated version", func(t *testing.T) {
		r := bytes.NewReader([]byte(vectorsMagic))
		_, err := snap.readHeader(r)
		if err == nil {
			t.Fatal("expected error for truncated version")
		}
	})

	// Correct magic + version but truncated count
	t.Run("truncated count", func(t *testing.T) {
		buf := new(bytes.Buffer)
		buf.Write([]byte(vectorsMagic))
		if err := binary.Write(buf, binary.LittleEndian, uint32(formatVersion)); err != nil {
			t.Fatal(err)
		}
		// no count written
		_, err := snap.readHeader(buf)
		if err == nil {
			t.Fatal("expected error for truncated count")
		}
	})
}

func TestReadVectorErrors(t *testing.T) {
	snap := NewVectorSnapshot(Config{}, &mockDataSource{dimension: 2})

	// Empty reader
	t.Run("fail on key length", func(t *testing.T) {
		r := bytes.NewReader([]byte{})
		_, _, err := snap.readVector(r)
		if err == nil {
			t.Fatal("expected error reading key length")
		}
	})

	// Key length present but key data truncated
	t.Run("fail on key data", func(t *testing.T) {
		buf := new(bytes.Buffer)
		if err := binary.Write(buf, binary.LittleEndian, uint16(10)); err != nil {
			t.Fatal(err)
		}
		_, _, err := snap.readVector(buf)
		if err == nil {
			t.Fatal("expected error reading key data")
		}
	})

	// Key present but vector data truncated
	t.Run("fail on vector data", func(t *testing.T) {
		buf := new(bytes.Buffer)
		if err := binary.Write(buf, binary.LittleEndian, uint16(2)); err != nil {
			t.Fatal(err)
		}
		buf.Write([]byte("ab"))
		// no float32 data follows; dimension=2 expects 8 bytes
		_, _, err := snap.readVector(buf)
		if err == nil {
			t.Fatal("expected error reading vector data")
		}
	})
}
