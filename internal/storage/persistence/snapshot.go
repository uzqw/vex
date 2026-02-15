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
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/golang/snappy"
)

const (
	// File names
	metadataFile = "metadata.json"
	vectorsFile  = "vectors.rdb"

	// Magic numbers for file format verification
	vectorsMagic = "VEX\x00"

	// Current format version
	formatVersion = 1
)

// VectorSnapshot implements Snapshotter for vector data
type VectorSnapshot struct {
	config     Config
	dataSource VectorDataSource
}

// VectorDataSource provides access to vector data for snapshotting
type VectorDataSource interface {
	// GetAllVectors returns all vectors as a map
	GetAllVectors() (map[string][]float32, error)

	// SetAllVectors replaces all vectors (used during recovery)
	SetAllVectors(vectors map[string][]float32) error

	// GetDimension returns the vector dimension
	GetDimension() int
}

// NewVectorSnapshot creates a new vector snapshot handler
func NewVectorSnapshot(config Config, dataSource VectorDataSource) *VectorSnapshot {
	return &VectorSnapshot{
		config:     config,
		dataSource: dataSource,
	}
}

// Save creates a snapshot of vector data
func (s *VectorSnapshot) Save(ctx context.Context) error {
	if !s.config.Enabled {
		return nil
	}

	start := time.Now()

	// Create temporary directory for atomic snapshot
	tempDir := filepath.Join(s.config.DataDir, fmt.Sprintf("temp-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir) // Clean up on error

	// Get all vectors from data source
	vectors, err := s.dataSource.GetAllVectors()
	if err != nil {
		return fmt.Errorf("failed to get vectors: %w", err)
	}

	slog.Info("Starting snapshot", "vector_count", len(vectors))

	// Save vector data
	vectorsPath := filepath.Join(tempDir, vectorsFile)
	checksum, size, err := s.saveVectors(vectorsPath, vectors)
	if err != nil {
		return fmt.Errorf("failed to save vectors: %w", err)
	}

	// Create metadata
	metadata := &SnapshotInfo{
		Version:     fmt.Sprintf("v%d", formatVersion),
		CreatedAt:   time.Now(),
		VectorCount: len(vectors),
		Dimension:   s.dataSource.GetDimension(),
		IndexType:   "memory", // Will be "hnsw" in phase 2
		SizeBytes:   size,
		Checksum:    fmt.Sprintf("sha256:%x", checksum),
		DurationMs:  time.Since(start).Milliseconds(),
		Compressed:  s.config.Compression != "none",
		Compression: s.config.Compression,
	}

	// Save metadata
	metadataPath := filepath.Join(tempDir, metadataFile)
	if err := s.saveMetadata(metadataPath, metadata); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	// Atomic move: rename temp directory to latest
	latestDir := filepath.Join(s.config.DataDir, "latest")

	// Remove old latest if exists
	if err := os.RemoveAll(latestDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old snapshot: %w", err)
	}

	// Rename temp to latest
	if err := os.Rename(tempDir, latestDir); err != nil {
		return fmt.Errorf("failed to finalize snapshot: %w", err)
	}

	duration := time.Since(start)
	slog.Info("Snapshot completed",
		"vector_count", len(vectors),
		"size_bytes", size,
		"duration", duration,
		"compressed", metadata.Compressed,
	)

	return nil
}

// Load restores vector data from the latest snapshot
func (s *VectorSnapshot) Load(ctx context.Context) error {
	if !s.config.Enabled {
		return nil
	}

	latestDir := filepath.Join(s.config.DataDir, "latest")

	// Check if snapshot exists
	if _, err := os.Stat(latestDir); os.IsNotExist(err) {
		slog.Info("No snapshot found, starting with empty database")
		return nil
	}

	slog.Info("Loading snapshot from disk")
	start := time.Now()

	// Load metadata
	metadataPath := filepath.Join(latestDir, metadataFile)
	metadata, err := s.loadMetadata(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	// Load vectors
	vectorsPath := filepath.Join(latestDir, vectorsFile)
	vectors, checksum, err := s.loadVectors(vectorsPath)
	if err != nil {
		return fmt.Errorf("failed to load vectors: %w", err)
	}

	// Verify checksum if enabled
	if s.config.Checksum {
		expectedChecksum := fmt.Sprintf("sha256:%x", checksum)
		if expectedChecksum != metadata.Checksum {
			return fmt.Errorf("%w: expected %s, got %s",
				ErrChecksumMismatch, metadata.Checksum, expectedChecksum)
		}
	}

	// Verify vector count
	if len(vectors) != metadata.VectorCount {
		slog.Warn("Vector count mismatch",
			"expected", metadata.VectorCount,
			"actual", len(vectors),
		)
	}

	// Restore vectors to data source
	if err := s.dataSource.SetAllVectors(vectors); err != nil {
		return fmt.Errorf("failed to restore vectors: %w", err)
	}

	duration := time.Since(start)
	slog.Info("Snapshot loaded",
		"vector_count", len(vectors),
		"duration", duration,
		"snapshot_age", time.Since(metadata.CreatedAt),
	)

	return nil
}

// GetLastSnapshotInfo returns metadata about the last snapshot, or nil if none exists.
func (s *VectorSnapshot) GetLastSnapshotInfo() (*SnapshotInfo, error) {
	latestDir := filepath.Join(s.config.DataDir, "latest")
	metadataPath := filepath.Join(latestDir, metadataFile)

	info, err := s.loadMetadata(metadataPath)
	if errors.Is(err, ErrSnapshotNotFound) {
		return nil, nil
	}
	return info, err
}

// saveVectors writes vectors to disk in binary format
func (s *VectorSnapshot) saveVectors(path string, vectors map[string][]float32) ([]byte, int64, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	// Create hasher for checksum
	hasher := sha256.New()

	// Create writer (potentially compressed)
	var writer io.Writer
	if s.config.Compression == "snappy" {
		writer = snappy.NewBufferedWriter(io.MultiWriter(file, hasher))
	} else {
		writer = io.MultiWriter(file, hasher)
	}

	// Write header
	if err := s.writeHeader(writer, len(vectors)); err != nil {
		return nil, 0, err
	}

	// Write each vector
	for key, vec := range vectors {
		if err := s.writeVector(writer, key, vec); err != nil {
			return nil, 0, err
		}
	}

	// Flush compression buffer if needed
	if sw, ok := writer.(*snappy.Writer); ok {
		if err := sw.Close(); err != nil {
			return nil, 0, err
		}
	}

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}

	return hasher.Sum(nil), stat.Size(), nil
}

// writeHeader writes the file header
func (s *VectorSnapshot) writeHeader(w io.Writer, count int) error {
	// Magic number
	if _, err := w.Write([]byte(vectorsMagic)); err != nil {
		return err
	}

	// Version
	if err := binary.Write(w, binary.LittleEndian, uint32(formatVersion)); err != nil {
		return err
	}

	// Vector count
	if err := binary.Write(w, binary.LittleEndian, uint64(count)); err != nil {
		return err
	}

	return nil
}

// writeVector writes a single vector entry
func (s *VectorSnapshot) writeVector(w io.Writer, key string, vec []float32) error {
	// Key length
	if err := binary.Write(w, binary.LittleEndian, uint16(len(key))); err != nil {
		return err
	}

	// Key
	if _, err := w.Write([]byte(key)); err != nil {
		return err
	}

	// Vector data
	if err := binary.Write(w, binary.LittleEndian, vec); err != nil {
		return err
	}

	return nil
}

// loadVectors reads vectors from disk
func (s *VectorSnapshot) loadVectors(path string) (map[string][]float32, []byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	// Create hasher for checksum verification
	hasher := sha256.New()

	// Create reader (potentially compressed)
	var reader io.Reader
	if s.config.Compression == "snappy" {
		reader = snappy.NewReader(io.TeeReader(file, hasher))
	} else {
		reader = io.TeeReader(file, hasher)
	}

	// Read and verify header
	count, err := s.readHeader(reader)
	if err != nil {
		return nil, nil, err
	}

	// Read vectors
	vectors := make(map[string][]float32, count)
	for i := 0; i < count; i++ {
		key, vec, err := s.readVector(reader)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read vector %d: %w", i, err)
		}
		vectors[key] = vec
	}

	return vectors, hasher.Sum(nil), nil
}

// readHeader reads and validates the file header
func (s *VectorSnapshot) readHeader(r io.Reader) (int, error) {
	// Read magic
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		return 0, err
	}
	if string(magic) != vectorsMagic {
		return 0, ErrInvalidFormat
	}

	// Read version
	var version uint32
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return 0, err
	}
	if version != formatVersion {
		return 0, fmt.Errorf("%w: expected %d, got %d", ErrVersionMismatch, formatVersion, version)
	}

	// Read count
	var count uint64
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return 0, err
	}

	return int(count), nil
}

// readVector reads a single vector entry
func (s *VectorSnapshot) readVector(r io.Reader) (string, []float32, error) {
	// Read key length
	var keyLen uint16
	if err := binary.Read(r, binary.LittleEndian, &keyLen); err != nil {
		return "", nil, err
	}

	// Read key
	keyBytes := make([]byte, keyLen)
	if _, err := io.ReadFull(r, keyBytes); err != nil {
		return "", nil, err
	}
	key := string(keyBytes)

	// Read vector dimension (we need to know it from metadata)
	// For now, read until we can't anymore or use stored dimension
	// This is a simplification - in production, dimension should be in header
	dim := s.dataSource.GetDimension()
	vec := make([]float32, dim)
	if err := binary.Read(r, binary.LittleEndian, vec); err != nil {
		return "", nil, err
	}

	return key, vec, nil
}

// saveMetadata writes snapshot metadata as JSON
func (s *VectorSnapshot) saveMetadata(path string, metadata *SnapshotInfo) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// loadMetadata reads snapshot metadata from JSON
func (s *VectorSnapshot) loadMetadata(path string) (*SnapshotInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrSnapshotNotFound
		}
		return nil, err
	}

	var metadata SnapshotInfo
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &metadata, nil
}
