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
	"testing"

	"github.com/uzqw/vex/internal/storage"
	"github.com/uzqw/vex/internal/vector"
)

// generateRandomVector creates a random vector and normalizes it
func generateRandomVector(dim int) []float32 {
	vec := make([]float32, dim)
	for i := 0; i < dim; i++ {
		vec[i] = rand.Float32()*2 - 1 // Random value between -1 and 1
	}
	// Normalize the vector
	normalized, _ := vector.Normalize(vec)
	return normalized
}

// BenchmarkIndexInsert_BruteForce benchmarks insert performance with brute force index
func BenchmarkIndexInsert_BruteForce(b *testing.B) {
	dimensions := []int{128, 256, 512, 1024}

	for _, dim := range dimensions {
		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			index := storage.NewBruteForceIndex()
			vec := generateRandomVector(dim)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("vec:%d", i)
				_ = index.Insert(key, vec)
			}
		})
	}
}

// BenchmarkIndexInsert_HNSW benchmarks insert performance with HNSW index
func BenchmarkIndexInsert_HNSW(b *testing.B) {
	dimensions := []int{128, 256, 512, 1024}

	for _, dim := range dimensions {
		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			index := storage.NewHNSWIndex()
			vec := generateRandomVector(dim)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("vec:%d", i)
				_ = index.Insert(key, vec)
			}
		})
	}
}

// BenchmarkIndexSearch_BruteForce_Fixed benchmarks search with a fixed dataset size
func BenchmarkIndexSearch_BruteForce_Fixed(b *testing.B) {
	dimensions := []int{128, 256, 512, 1024}
	datasetSize := 100000 // Fixed 100k vectors

	for _, dim := range dimensions {
		b.Run(fmt.Sprintf("dim=%d-vectors=%d", dim, datasetSize), func(b *testing.B) {
			index := storage.NewBruteForceIndex()

			// Pre-populate index
			for i := 0; i < datasetSize; i++ {
				vec := generateRandomVector(dim)
				key := fmt.Sprintf("vec:%d", i)
				_ = index.Insert(key, vec)
			}

			queryVec := generateRandomVector(dim)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = index.Search(queryVec, 10)
			}
		})
	}
}

// BenchmarkIndexSearch_HNSW_Fixed benchmarks search with a fixed dataset size
func BenchmarkIndexSearch_HNSW_Fixed(b *testing.B) {
	dimensions := []int{128, 256, 512, 1024}
	datasetSize := 100000 // Fixed 100k vectors

	for _, dim := range dimensions {
		b.Run(fmt.Sprintf("dim=%d-vectors=%d", dim, datasetSize), func(b *testing.B) {
			index := storage.NewHNSWIndex()

			// Pre-populate index
			for i := 0; i < datasetSize; i++ {
				vec := generateRandomVector(dim)
				key := fmt.Sprintf("vec:%d", i)
				_ = index.Insert(key, vec)
			}

			queryVec := generateRandomVector(dim)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = index.Search(queryVec, 10)
			}
		})
	}
}

// BenchmarkIndexSearch_Comparison_128D_10K compares both algorithms on 128D with 10k vectors
func BenchmarkIndexSearch_Comparison_128D_10K(b *testing.B) {
	dim := 128
	datasetSize := 10000

	// Prepare data
	vectors := make([][]float32, datasetSize)
	for i := 0; i < datasetSize; i++ {
		vectors[i] = generateRandomVector(dim)
	}
	queryVec := generateRandomVector(dim)

	b.Run("BruteForce", func(b *testing.B) {
		index := storage.NewBruteForceIndex()
		for i := 0; i < datasetSize; i++ {
			_ = index.Insert(fmt.Sprintf("vec:%d", i), vectors[i])
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = index.Search(queryVec, 10)
		}
	})

	b.Run("HNSW", func(b *testing.B) {
		index := storage.NewHNSWIndex()
		for i := 0; i < datasetSize; i++ {
			_ = index.Insert(fmt.Sprintf("vec:%d", i), vectors[i])
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = index.Search(queryVec, 10)
		}
	})
}

// BenchmarkIndexSearch_Comparison_256D_50K compares both algorithms on 256D with 50k vectors
func BenchmarkIndexSearch_Comparison_256D_50K(b *testing.B) {
	dim := 256
	datasetSize := 50000

	// Prepare data
	vectors := make([][]float32, datasetSize)
	for i := 0; i < datasetSize; i++ {
		vectors[i] = generateRandomVector(dim)
	}
	queryVec := generateRandomVector(dim)

	b.Run("BruteForce", func(b *testing.B) {
		index := storage.NewBruteForceIndex()
		for i := 0; i < datasetSize; i++ {
			_ = index.Insert(fmt.Sprintf("vec:%d", i), vectors[i])
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = index.Search(queryVec, 10)
		}
	})

	b.Run("HNSW", func(b *testing.B) {
		index := storage.NewHNSWIndex()
		for i := 0; i < datasetSize; i++ {
			_ = index.Insert(fmt.Sprintf("vec:%d", i), vectors[i])
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = index.Search(queryVec, 10)
		}
	})
}

// BenchmarkIndexSearch_Comparison_512D_100K compares both algorithms on 512D with 100k vectors
func BenchmarkIndexSearch_Comparison_512D_100K(b *testing.B) {
	dim := 512
	datasetSize := 100000

	// Prepare data
	vectors := make([][]float32, datasetSize)
	for i := 0; i < datasetSize; i++ {
		vectors[i] = generateRandomVector(dim)
	}
	queryVec := generateRandomVector(dim)

	b.Run("BruteForce", func(b *testing.B) {
		index := storage.NewBruteForceIndex()
		for i := 0; i < datasetSize; i++ {
			_ = index.Insert(fmt.Sprintf("vec:%d", i), vectors[i])
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = index.Search(queryVec, 10)
		}
	})

	b.Run("HNSW", func(b *testing.B) {
		index := storage.NewHNSWIndex()
		for i := 0; i < datasetSize; i++ {
			_ = index.Insert(fmt.Sprintf("vec:%d", i), vectors[i])
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = index.Search(queryVec, 10)
		}
	})
}

// BenchmarkIndexSearch_Comparison_1024D_50K compares both algorithms on 1024D with 50k vectors
func BenchmarkIndexSearch_Comparison_1024D_50K(b *testing.B) {
	dim := 1024
	datasetSize := 50000

	// Prepare data
	vectors := make([][]float32, datasetSize)
	for i := 0; i < datasetSize; i++ {
		vectors[i] = generateRandomVector(dim)
	}
	queryVec := generateRandomVector(dim)

	b.Run("BruteForce", func(b *testing.B) {
		index := storage.NewBruteForceIndex()
		for i := 0; i < datasetSize; i++ {
			_ = index.Insert(fmt.Sprintf("vec:%d", i), vectors[i])
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = index.Search(queryVec, 10)
		}
	})

	b.Run("HNSW", func(b *testing.B) {
		index := storage.NewHNSWIndex()
		for i := 0; i < datasetSize; i++ {
			_ = index.Insert(fmt.Sprintf("vec:%d", i), vectors[i])
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = index.Search(queryVec, 10)
		}
	})
}

// BenchmarkIndexMemoryUsage compares memory usage between algorithms
func BenchmarkIndexMemoryUsage(b *testing.B) {
	dim := 1024
	sizes := []int{10000, 50000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("BruteForce-dim=%d-vectors=%d", dim, size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				index := storage.NewBruteForceIndex()
				for j := 0; j < size; j++ {
					vec := generateRandomVector(dim)
					_ = index.Insert(fmt.Sprintf("vec:%d", j), vec)
				}
			}
		})

		b.Run(fmt.Sprintf("HNSW-dim=%d-vectors=%d", dim, size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				index := storage.NewHNSWIndex()
				for j := 0; j < size; j++ {
					vec := generateRandomVector(dim)
					_ = index.Insert(fmt.Sprintf("vec:%d", j), vec)
				}
			}
		})
	}
}
