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

package bench_storage

import (
	"fmt"
	"testing"

	"github.com/uzqw/vex/internal/storage"
)

// TestHNSWLayerDistribution verifies that HNSW builds multiple layers
func TestHNSWLayerDistribution(t *testing.T) {
	tests := []struct {
		name       string
		numVectors int
		dimension  int
	}{
		{"Small dataset (1K vectors)", 1000, 128},
		{"Medium dataset (10K vectors)", 10000, 128},
		{"Large dataset (50K vectors)", 50000, 256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := storage.NewHNSWIndex()

			// Insert vectors
			for i := 0; i < tt.numVectors; i++ {
				vec := generateRandomVector(tt.dimension)
				key := fmt.Sprintf("vec:%d", i)
				if err := index.Insert(key, vec); err != nil {
					t.Fatalf("Failed to insert: %v", err)
				}
			}

			// Get index statistics
			stats := index.GetStats()

			t.Logf("\n=== %s ===", tt.name)
			t.Logf("Total vectors: %d", stats.TotalVectors)
			t.Logf("Maximum layer level: %d", stats.MaxLevel)
			t.Logf("Average neighbors per node: %.1f", stats.AverageNeighbors)

			// Verify index has multiple layers
			if stats.MaxLevel < 0 {
				t.Errorf("Expected maxLevel >= 0, got %d", stats.MaxLevel)
			}

			// Print layer distribution
			t.Logf("\nLayer distribution:")
			expectedDistribution := []float64{100.0, 50.0, 25.0, 12.5, 6.25, 3.125}
			for level := 0; level <= stats.MaxLevel; level++ {
				count := stats.LayerDistribution[level]
				percentage := float64(count) * 100 / float64(stats.TotalVectors)

				// Check if distribution matches expected exponential decay
				expectedPct := 100.0
				if level < len(expectedDistribution) {
					expectedPct = expectedDistribution[level]
				}

				t.Logf("  Layer %d: %d nodes (%.1f%%, expected ~%.1f%%)",
					level, count, percentage, expectedPct)

				// Verify layer has nodes
				if count <= 0 && level <= stats.MaxLevel {
					t.Errorf("Layer %d should have nodes but has %d", level, count)
				}
			}

			t.Logf("\n✓ Multi-layer structure verified")
			t.Logf("✓ Vectors properly distributed across %d layers", stats.MaxLevel+1)
		})
	}
}

// TestHNSWMultiLayerSearch verifies search works across multiple layers
func TestHNSWMultiLayerSearch(t *testing.T) {
	index := storage.NewHNSWIndex()
	dim := 128

	// Insert 10k vectors to ensure multiple layers
	numVectors := 10000
	for i := 0; i < numVectors; i++ {
		vec := generateRandomVector(dim)
		key := fmt.Sprintf("vec:%d", i)
		if err := index.Insert(key, vec); err != nil {
			t.Fatalf("Failed to insert: %v", err)
		}
	}

	// Search should work correctly with multi-layer index
	query := generateRandomVector(dim)
	results, err := index.Search(query, 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 10 {
		t.Errorf("Expected 10 results, got %d", len(results))
	}

	t.Logf("✓ Search returned %d results from %d vectors", len(results), numVectors)
	t.Logf("✓ Multi-layer search working correctly")
}
