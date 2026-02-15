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

// DataDistribution type for vector generation
type DataDistribution int

const (
	DistributionRandom DataDistribution = iota
	DistributionClustered
)

// RecallTestConfig defines parameters for a recall test
type RecallTestConfig struct {
	Name           string
	Vectors        int
	Dimension      int
	KValues        []int
	NumQueries     int
	Seed           int64
	Distribution   DataDistribution
	NumClusters    int // For clustered distribution
	ClusterSpread  float32 // Radius/spread of each cluster (0.0-1.0)
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

// generateClusteredVectors generates vectors in clusters
// This produces more realistic data with local structure
func generateClusteredVectors(numVectors, numClusters, dimension int, clusterSpread float32, rng *rand.Rand) [][]float32 {
	// Generate cluster centers
	clusterCenters := make([][]float32, numClusters)
	for i := range numClusters {
		center := generateTestVector(dimension, rng)
		clusterCenters[i] = center
	}

	vectors := make([][]float32, numVectors)
	vectorsPerCluster := (numVectors + numClusters - 1) / numClusters

	for i := range numVectors {
		clusterIdx := i / vectorsPerCluster
		if clusterIdx >= numClusters {
			clusterIdx = numClusters - 1
		}

		// Start with cluster center
		vec := make([]float32, dimension)
		copy(vec, clusterCenters[clusterIdx])

		// Add noise proportional to clusterSpread
		for j := range dimension {
			noise := (rng.Float32()*2 - 1) * clusterSpread
			vec[j] += noise
		}

		// Normalize
		normalized, _ := vector.Normalize(vec)
		vectors[i] = normalized
	}

	return vectors
}

// buildTestIndices creates HNSW and BruteForce indices with identical data
func buildTestIndices(numVectors, dimension int, seed int64) (
	*storage.HNSWIndex,
	*storage.BruteForceIndex,
	[][]float32,
	error,
) {
	return buildTestIndicesWithDistribution(numVectors, dimension, seed, DistributionRandom, 0, 0)
}

// buildTestIndicesWithDistribution creates indices with specified data distribution
func buildTestIndicesWithDistribution(numVectors, dimension int, seed int64,
	distribution DataDistribution, numClusters int, clusterSpread float32) (
	*storage.HNSWIndex,
	*storage.BruteForceIndex,
	[][]float32,
	error,
) {
	hnsw := storage.NewHNSWIndex()
	bruteForce := storage.NewBruteForceIndex()

	rng := rand.New(rand.NewSource(seed))

	// Generate vectors based on distribution type
	var vectors [][]float32
	if distribution == DistributionClustered {
		vectors = generateClusteredVectors(numVectors, numClusters, dimension, clusterSpread, rng)
	} else {
		vectors = make([][]float32, numVectors)
		for i := range numVectors {
			vectors[i] = generateTestVector(dimension, rng)
		}
	}

	// Insert vectors into both indices
	for i := range numVectors {
		vec := vectors[i]
		// Make a copy to ensure no aliasing issues
		vecCopy := make([]float32, len(vec))
		copy(vecCopy, vec)

		key := fmt.Sprintf(TestVectorKeyFmt, i)

		if err := hnsw.Insert(key, vec); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to insert into HNSW: %v", err)
		}
		if err := bruteForce.Insert(key, vecCopy); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to insert into BruteForce: %v", err)
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

	// Minimum acceptable recall threshold; fail below this.
	const minRecall = 0.90

	for _, config := range configs {
		t.Run(config.Name, func(t *testing.T) {
			hnsw, bruteForce, _, err := buildTestIndices(config.Vectors, config.Dimension, config.Seed)
			if err != nil {
				t.Fatalf("failed to build indices: %v", err)
			}

			t.Logf("%d vectors, %d dims, %d queries", config.Vectors, config.Dimension, config.NumQueries)

			recallByK := make(map[int][]float64)
			for _, k := range config.KValues {
				recallByK[k] = make([]float64, 0, config.NumQueries)
			}

			queryRng := rand.New(rand.NewSource(config.Seed + QuerySeedOffset))
			for q := 0; q < config.NumQueries; q++ {
				queryVec := generateTestVector(config.Dimension, queryRng)

				hnswResults, err := hnsw.Search(queryVec, 100)
				if err != nil {
					t.Fatalf("HNSW search failed: %v", err)
				}
				bruteForceResults, err := bruteForce.Search(queryVec, 100)
				if err != nil {
					t.Fatalf("BruteForce search failed: %v", err)
				}

				for _, k := range config.KValues {
					recallByK[k] = append(recallByK[k], calculateRecallAtK(hnswResults, bruteForceResults, k))
				}
			}

			var totalRecall float64
			var totalCount int
			for _, k := range config.KValues {
				var sum float64
				for _, r := range recallByK[k] {
					sum += r
				}
				avg := sum / float64(len(recallByK[k]))
				totalRecall += sum
				totalCount += len(recallByK[k])

				status := "PASS"
				if avg < minRecall {
					status = "FAIL"
				}
				t.Logf("recall@%-3d = %.2f%%  %s", k, avg*100, status)
				if avg < minRecall {
					t.Errorf("recall@%d = %.4f is below threshold %.2f", k, avg, minRecall)
				}
			}

			if totalCount > 0 {
				t.Logf("avg recall  = %.2f%%", totalRecall/float64(totalCount)*100)
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

// TestHNSWParameterDiagnosis diagnoses the impact of M, EfConstruct, and Ef on recall
func TestHNSWParameterDiagnosis(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping parameter diagnosis in short mode")
	}

	// Parameter configurations to test (more aggressive parameters)
	paramConfigs := []struct {
		name        string
		M           int
		EfConstruct int
		Ef          int
	}{
		{"M32-ef800", 32, 800, 800},
		{"M64-ef1600", 64, 1600, 1600},
		{"M96-ef2400", 96, 2400, 2400},
		{"M128-ef4000", 128, 4000, 4000},
		{"M256-ef8000", 256, 8000, 8000},
	}

	// Test on 10K-256D (the problematic configuration)
	config := RecallTestConfig{
		Name:       "10K-256D",
		Vectors:    10000,
		Dimension:  256,
		KValues:    []int{1, 10},
		NumQueries: 20,
		Seed:       DefaultSeed,
	}

	t.Logf("\n=== HNSW Parameter Impact Diagnosis ===\n")
	t.Logf("Configuration: %s (%d vectors, %d dimensions, %d queries)\n\n",
		config.Name, config.Vectors, config.Dimension, config.NumQueries)

	t.Logf("%-18s | recall@1 | recall@10 | avg recall\n", "Parameters")
	t.Logf("%-18s | -------- | --------- | ----------\n", "")

	for _, param := range paramConfigs {
		// Build indices with custom parameters
		hnsw := storage.NewHNSWIndex()
		hnsw.M = param.M
		hnsw.EfConstruct = param.EfConstruct
		hnsw.Ef = param.Ef

		bruteForce := storage.NewBruteForceIndex()
		rng := rand.New(rand.NewSource(config.Seed))

		// Build indices
		for i := range config.Vectors {
			vec := generateTestVector(config.Dimension, rng)
			vecCopy := make([]float32, len(vec))
			copy(vecCopy, vec)

			key := fmt.Sprintf(TestVectorKeyFmt, i)

			if err := hnsw.Insert(key, vec); err != nil {
				t.Fatalf("failed to insert into HNSW: %v", err)
			}
			if err := bruteForce.Insert(key, vecCopy); err != nil {
				t.Fatalf("failed to insert into BruteForce: %v", err)
			}
		}

		// Run queries
		recallByK := make(map[int][]float64)
		for _, k := range config.KValues {
			recallByK[k] = make([]float64, 0, config.NumQueries)
		}

		queryRng := rand.New(rand.NewSource(config.Seed + QuerySeedOffset))
		for q := 0; q < config.NumQueries; q++ {
			queryVec := generateTestVector(config.Dimension, queryRng)

			hnswResults, err := hnsw.Search(queryVec, 100)
			if err != nil {
				t.Fatalf("HNSW search failed: %v", err)
			}

			bruteForceResults, err := bruteForce.Search(queryVec, 100)
			if err != nil {
				t.Fatalf("BruteForce search failed: %v", err)
			}

			for _, k := range config.KValues {
				recall := calculateRecallAtK(hnswResults, bruteForceResults, k)
				recallByK[k] = append(recallByK[k], recall)
			}
		}

		// Calculate averages
		var totalRecall float64
		var totalCount int
		recall1Avg := 0.0
		recall10Avg := 0.0

		for _, k := range config.KValues {
			var sum float64
			for _, r := range recallByK[k] {
				sum += r
				totalRecall += r
				totalCount++
			}
			avg := sum / float64(len(recallByK[k]))

			if k == 1 {
				recall1Avg = avg
			} else if k == 10 {
				recall10Avg = avg
			}
		}

		avgRecall := 0.0
		if totalCount > 0 {
			avgRecall = totalRecall / float64(totalCount)
		}

		t.Logf("%-18s | %8.2f%% | %9.2f%% | %10.2f%%\n",
			param.name,
			recall1Avg*100,
			recall10Avg*100,
			avgRecall*100)
	}
}

// TestHNSWRecallWithClusteredData tests recall with realistic clustered vectors
func TestHNSWRecallWithClusteredData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping clustered data tests in short mode")
	}

	// Clustered data configurations (more realistic)
	configs := []RecallTestConfig{
		{
			Name: "10K-256D-Clustered(10)", Vectors: 10000, Dimension: 256,
			KValues: []int{1, 5, 10, 50, 100}, NumQueries: 50, Seed: DefaultSeed,
			Distribution: DistributionClustered, NumClusters: 10, ClusterSpread: 0.3,
		},
		{
			Name: "10K-256D-Clustered(50)", Vectors: 10000, Dimension: 256,
			KValues: []int{1, 5, 10, 50, 100}, NumQueries: 50, Seed: DefaultSeed,
			Distribution: DistributionClustered, NumClusters: 50, ClusterSpread: 0.3,
		},
		{
			Name: "10K-256D-Clustered(100)", Vectors: 10000, Dimension: 256,
			KValues: []int{1, 5, 10, 50, 100}, NumQueries: 50, Seed: DefaultSeed,
			Distribution: DistributionClustered, NumClusters: 100, ClusterSpread: 0.3,
		},
	}

	// Expected recall thresholds for clustered data (should be much better)
	idealThresholds := map[int]float64{
		1:   0.85,
		5:   0.90,
		10:  0.92,
		50:  0.95,
		100: 0.97,
	}

	minimumThresholds := map[int]float64{
		1:   0.50, // Realistic minimum for clustered data
		5:   0.60,
		10:  0.70,
		50:  0.80,
		100: 0.85,
	}

	t.Logf("\n=== HNSW Recall with Clustered Data ===\n")

	for _, config := range configs {
		t.Run(config.Name, func(t *testing.T) {
			// Build indices with clustered data
			hnsw, bruteForce, trainVectors, err := buildTestIndicesWithDistribution(
				config.Vectors, config.Dimension, config.Seed,
				config.Distribution, config.NumClusters, config.ClusterSpread,
			)
			if err != nil {
				t.Fatalf("failed to build indices: %v", err)
			}

			t.Logf("Configuration: %d vectors, %d dimensions, %d clusters, spread=%.2f",
				config.Vectors, config.Dimension, config.NumClusters, config.ClusterSpread)

			// Track recall by K
			recallByK := make(map[int][]float64)
			for _, k := range config.KValues {
				recallByK[k] = make([]float64, 0, config.NumQueries)
			}

			// Generate query vectors by perturbing training vectors
			// This ensures queries are in the same distribution as training data
			queryRng := rand.New(rand.NewSource(config.Seed + QuerySeedOffset))
			for q := 0; q < config.NumQueries; q++ {
				// Pick a random training vector and add small perturbation
				baseVecIdx := queryRng.Intn(len(trainVectors))
				baseVec := trainVectors[baseVecIdx]

				// Add small noise to the base vector
				queryVec := make([]float32, len(baseVec))
				copy(queryVec, baseVec)
				for i := range queryVec {
					// Add small perturbation (0-5% of vector magnitude)
					noise := (queryRng.Float32()*2 - 1) * 0.05
					queryVec[i] += noise
				}

				// Normalize the perturbed vector
				queryVec, _ = vector.Normalize(queryVec)

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

				ideal := idealThresholds[k]
				minimum := minimumThresholds[k]

				var status string
				if avgRecall >= ideal {
					status = "✓✓ (ideal)"
				} else if avgRecall >= minimum {
					status = "✓ (acceptable)"
				} else {
					status = "✗ (BROKEN)"
				}

				t.Logf("recall@%-3d = %.4f (%.2f%%) [ideal: %.2f%%, min: %.2f%%] %s",
					k, avgRecall, avgRecall*100, ideal*100, minimum*100, status)

				// Fail if recall drops below minimum threshold
				if avgRecall < minimum {
					t.Errorf("recall@%d = %.4f is below minimum %.4f", k, avgRecall, minimum)
				}
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
