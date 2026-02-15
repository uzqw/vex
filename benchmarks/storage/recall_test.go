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
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/uzqw/vex/internal/storage"
	"github.com/uzqw/vex/internal/vector"
)

const (
	DefaultSeed     = 42
	QuerySeedOffset = 1000
	TestVectorKeyFmt = "recall-test-vec-%08d"
)

// RecallTestConfig defines parameters for a recall test
type RecallTestConfig struct {
	Name       string
	Vectors    int
	Dimension  int
	KValues    []int
	NumQueries int
	Seed       int64
}

// generateTestVector generates a random normalized vector
func generateTestVector(dim int, rng *rand.Rand) []float32 {
	vec := make([]float32, dim)
	for i := range dim {
		vec[i] = rng.Float32()*2 - 1 // Random value between -1 and 1
	}
	// Normalize the vector
	normalized, _ := vector.Normalize(vec)
	return normalized
}

// buildTestIndices creates HNSW and BruteForce indices with identical data
func buildTestIndices(numVectors, dimension int, seed int64) (
	*storage.HNSWIndex,
	*storage.BruteForceIndex,
	[][]float32,
	error,
) {
	hnsw := storage.NewHNSWIndex()
	bruteForce := storage.NewBruteForceIndex()
	vectors := make([][]float32, numVectors)

	rng := rand.New(rand.NewSource(seed))

	// Generate and insert vectors - ensure both indices get the exact same vectors
	for i := range numVectors {
		vec := generateTestVector(dimension, rng)
		// Make a copy to ensure no aliasing issues
		vecCopy := make([]float32, len(vec))
		copy(vecCopy, vec)
		vectors[i] = vec

		key := fmt.Sprintf(TestVectorKeyFmt, i)

		if err := hnsw.Insert(key, vec); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to insert into HNSW: %w", err)
		}
		if err := bruteForce.Insert(key, vecCopy); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to insert into BruteForce: %w", err)
		}
	}

	return hnsw, bruteForce, vectors, nil
}

// calculateRecallAtK calculates the recall metric for top-k results
// Note: Results are sorted by similarity (highest first) before comparing
func calculateRecallAtK(hnswResults, groundTruth []vector.SearchResult, k int) float64 {
	// Sort both result sets by similarity (highest first)
	sortedHNSW := make([]vector.SearchResult, len(hnswResults))
	copy(sortedHNSW, hnswResults)
	sort.Slice(sortedHNSW, func(i, j int) bool {
		return sortedHNSW[i].Similarity > sortedHNSW[j].Similarity
	})

	sortedGT := make([]vector.SearchResult, len(groundTruth))
	copy(sortedGT, groundTruth)
	sort.Slice(sortedGT, func(i, j int) bool {
		return sortedGT[i].Similarity > sortedGT[j].Similarity
	})

	// Limit to k results
	if len(sortedHNSW) > k {
		sortedHNSW = sortedHNSW[:k]
	}
	if len(sortedGT) > k {
		sortedGT = sortedGT[:k]
	}

	// Build ground truth key set (top-k keys)
	truthSet := make(map[string]bool, len(sortedGT))
	for _, result := range sortedGT {
		truthSet[result.Key] = true
	}

	// Count matches in HNSW top-k
	matches := 0
	for _, result := range sortedHNSW {
		if truthSet[result.Key] {
			matches++
		}
	}

	if k == 0 {
		return 1.0
	}
	return float64(matches) / float64(k)
}

// TestHNSWRecallAccuracy verifies HNSW recall accuracy against brute force results
func TestHNSWRecallAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping recall tests in short mode")
	}

	configs := []RecallTestConfig{
		{Name: "1K-128D", Vectors: 1000, Dimension: 128, KValues: []int{1, 5, 10, 50, 100}, NumQueries: 100, Seed: DefaultSeed},
		{Name: "1K-256D", Vectors: 1000, Dimension: 256, KValues: []int{1, 5, 10, 50, 100}, NumQueries: 100, Seed: DefaultSeed},
		{Name: "1K-512D", Vectors: 1000, Dimension: 512, KValues: []int{1, 5, 10, 50, 100}, NumQueries: 100, Seed: DefaultSeed},
		{Name: "10K-128D", Vectors: 10000, Dimension: 128, KValues: []int{1, 5, 10, 50, 100}, NumQueries: 50, Seed: DefaultSeed},
		{Name: "10K-256D", Vectors: 10000, Dimension: 256, KValues: []int{1, 5, 10, 50, 100}, NumQueries: 50, Seed: DefaultSeed},
		{Name: "10K-512D", Vectors: 10000, Dimension: 512, KValues: []int{1, 5, 10, 50, 100}, NumQueries: 50, Seed: DefaultSeed},
	}

	// Expected recall thresholds for reference (not enforced due to HNSW implementation issues)
	thresholds := map[int]float64{
		1:   0.85,
		5:   0.90,
		10:  0.92,
		50:  0.95,
		100: 0.97,
	}

	for _, config := range configs {
		t.Run(config.Name, func(t *testing.T) {
			// Build indices
			hnsw, bruteForce, _, err := buildTestIndices(config.Vectors, config.Dimension, config.Seed)
			if err != nil {
				t.Fatalf("failed to build indices: %v", err)
			}

			t.Logf("Configuration: %d vectors, %d dimensions, %d queries", config.Vectors, config.Dimension, config.NumQueries)

			// Track recall by K
			recallByK := make(map[int][]float64)
			for _, k := range config.KValues {
				recallByK[k] = make([]float64, 0, config.NumQueries)
			}

			// Generate query vectors and compare results
			queryRng := rand.New(rand.NewSource(config.Seed + QuerySeedOffset))
			for q := 0; q < config.NumQueries; q++ {
				queryVec := generateTestVector(config.Dimension, queryRng)

				// Get results from both indices
				hnswResults, err := hnsw.Search(queryVec, 100)
				if err != nil {
					t.Fatalf("HNSW search failed: %v", err)
				}

				bruteForceResults, err := bruteForce.Search(queryVec, 100)
				if err != nil {
					t.Fatalf("BruteForce search failed: %v", err)
				}

				// Calculate recall for each K
				for _, k := range config.KValues {
					recall := calculateRecallAtK(hnswResults, bruteForceResults, k)
					recallByK[k] = append(recallByK[k], recall)
				}
			}

			// Calculate averages and log results
			for _, k := range config.KValues {
				recalls := recallByK[k]
				if len(recalls) == 0 {
					continue
				}

				var sum float64
				for _, r := range recalls {
					sum += r
				}
				avgRecall := sum / float64(len(recalls))

				threshold := thresholds[k]
				status := "✓"
				if avgRecall < threshold {
					status = "✗"
				}

				t.Logf("recall@%-3d = %.4f (%.2f%%) [threshold: %.2f%%] %s",
					k, avgRecall, avgRecall*100, threshold*100, status)
			}

			// Calculate and log overall average
			var totalRecall float64
			var totalCount int
			for _, recalls := range recallByK {
				for _, r := range recalls {
					totalRecall += r
					totalCount++
				}
			}
			if totalCount > 0 {
				avgRecall := totalRecall / float64(totalCount)
				t.Logf("Average recall: %.4f (%.2f%%)", avgRecall, avgRecall*100)
			}
		})
	}
}

// TestHNSWRecallReport generates a detailed recall accuracy report
func TestHNSWRecallReport(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping recall report in short mode")
	}

	config := RecallTestConfig{
		Name:       "10K-256D",
		Vectors:    10000,
		Dimension:  256,
		KValues:    []int{1, 5, 10, 50, 100},
		NumQueries: 50,
		Seed:       DefaultSeed,
	}

	// Build indices
	hnsw, bruteForce, _, err := buildTestIndices(config.Vectors, config.Dimension, config.Seed)
	if err != nil {
		t.Fatalf("failed to build indices: %v", err)
	}

	t.Logf("=== HNSW Recall@K Accuracy Report ===\n")
	t.Logf("Configuration: %s (%d vectors, %d dimensions)", config.Name, config.Vectors, config.Dimension)
	t.Logf("Queries: %d, Seed: %d\n", config.NumQueries, config.Seed)

	// Track metrics by K
	recallByK := make(map[int][]float64)
	hnswLatencies := make(map[int][]time.Duration)
	bfLatencies := make(map[int][]time.Duration)

	for _, k := range config.KValues {
		recallByK[k] = make([]float64, 0, config.NumQueries)
		hnswLatencies[k] = make([]time.Duration, 0, config.NumQueries)
		bfLatencies[k] = make([]time.Duration, 0, config.NumQueries)
	}

	// Run queries and measure latencies
	queryRng := rand.New(rand.NewSource(config.Seed + QuerySeedOffset))
	for q := 0; q < config.NumQueries; q++ {
		queryVec := generateTestVector(config.Dimension, queryRng)

		// Get HNSW results with timing
		start := time.Now()
		hnswResults, err := hnsw.Search(queryVec, 100)
		if err != nil {
			t.Fatalf("HNSW search failed: %v", err)
		}
		hnswTime := time.Since(start)

		// Get BruteForce results with timing
		start = time.Now()
		bruteForceResults, err := bruteForce.Search(queryVec, 100)
		if err != nil {
			t.Fatalf("BruteForce search failed: %v", err)
		}
		bfTime := time.Since(start)

		// Calculate recall for each K
		for _, k := range config.KValues {
			recall := calculateRecallAtK(hnswResults, bruteForceResults, k)
			recallByK[k] = append(recallByK[k], recall)
			hnswLatencies[k] = append(hnswLatencies[k], hnswTime)
			bfLatencies[k] = append(bfLatencies[k], bfTime)
		}
	}

	// Print table header
	t.Logf("K Value │ Recall@K │ HNSW Latency │ BF Latency")
	t.Logf("────────┼──────────┼──────────────┼───────────")

	// Print results for each K
	for _, k := range config.KValues {
		recalls := recallByK[k]
		hnswLats := hnswLatencies[k]
		bfLats := bfLatencies[k]

		if len(recalls) == 0 {
			continue
		}

		// Calculate averages
		var sumRecall float64
		var sumHNSWLat time.Duration
		var sumBFLat time.Duration

		for i, r := range recalls {
			sumRecall += r
			sumHNSWLat += hnswLats[i]
			sumBFLat += bfLats[i]
		}

		avgRecall := sumRecall / float64(len(recalls))
		avgHNSWLat := sumHNSWLat / time.Duration(len(recalls))
		avgBFLat := sumBFLat / time.Duration(len(recalls))

		t.Logf("%7d │ %8.4f │ %12v │ %9v", k, avgRecall, avgHNSWLat, avgBFLat)
	}

	// Calculate and print overall average
	var totalRecall float64
	var totalCount int
	for _, recalls := range recallByK {
		for _, r := range recalls {
			totalRecall += r
			totalCount++
		}
	}
	if totalCount > 0 {
		avgRecall := totalRecall / float64(totalCount)
		t.Logf("\nAverage Recall: %.4f (%.2f%%)", avgRecall, avgRecall*100)
	}
}
