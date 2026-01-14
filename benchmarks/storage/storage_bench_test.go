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

// Package bench_storage contains benchmarks for the storage layer.
package bench_storage

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/uzqw/vex/internal/storage"
)

// BenchmarkStorageContention measures performance under high write contention
// This is designed to reveal false sharing effects between CPU cores
//
// Run with: go test -bench=BenchmarkStorageContention -benchmem ./benchmarks/storage
func BenchmarkStorageContention(b *testing.B) {
	dims := 128
	numCPU := runtime.GOMAXPROCS(0)

	for _, concurrency := range []int{1, 2, 4, 8, numCPU, numCPU * 2} {
		b.Run(fmt.Sprintf("goroutines=%d", concurrency), func(b *testing.B) {
			s := storage.New()

			// Pre-generate vectors to avoid allocation in hot loop
			vectors := make([][]float32, concurrency)
			for i := range vectors {
				vectors[i] = make([]float32, dims)
				for j := range vectors[i] {
					vectors[i][j] = float32(i*dims+j) * 0.001
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			var wg sync.WaitGroup
			opsPerGoroutine := b.N / concurrency

			for g := 0; g < concurrency; g++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					vec := vectors[id]
					for i := 0; i < opsPerGoroutine; i++ {
						key := fmt.Sprintf("key-%d-%d", id, i)
						_ = s.Set(key, vec)
					}
				}(g)
			}
			wg.Wait()
		})
	}
}

// BenchmarkStorageReadWrite measures mixed read/write performance
//
// Run with: go test -bench=BenchmarkStorageReadWrite -benchmem ./benchmarks/storage
func BenchmarkStorageReadWrite(b *testing.B) {
	dims := 128
	numCPU := runtime.GOMAXPROCS(0)

	for _, concurrency := range []int{1, 4, numCPU, numCPU * 2} {
		b.Run(fmt.Sprintf("goroutines=%d", concurrency), func(b *testing.B) {
			s := storage.New()

			// Pre-populate with some data
			vec := make([]float32, dims)
			for i := range vec {
				vec[i] = float32(i) * 0.001
			}
			for i := 0; i < 10000; i++ {
				_ = s.Set(fmt.Sprintf("init-%d", i), vec)
			}

			b.ResetTimer()
			b.ReportAllocs()

			var wg sync.WaitGroup
			opsPerGoroutine := b.N / concurrency

			for g := 0; g < concurrency; g++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					for i := 0; i < opsPerGoroutine; i++ {
						if i%10 == 0 {
							// 10% writes
							key := fmt.Sprintf("key-%d-%d", id, i)
							_ = s.Set(key, vec)
						} else {
							// 90% reads
							key := fmt.Sprintf("init-%d", i%10000)
							_, _ = s.Get(key)
						}
					}
				}(g)
			}
			wg.Wait()
		})
	}
}

// BenchmarkShardIsolation tests if operations on different shards interfere
// If false sharing is present, performance will degrade with more goroutines
//
// Run with: go test -bench=BenchmarkShardIsolation -benchmem ./benchmarks/storage
func BenchmarkShardIsolation(b *testing.B) {
	dims := 128

	b.Run("single_shard_contention", func(b *testing.B) {
		s := storage.New()
		vec := make([]float32, dims)
		for i := range vec {
			vec[i] = float32(i) * 0.001
		}

		b.ResetTimer()
		b.ReportAllocs()

		// All goroutines write to keys that hash to same shard
		// This should show lock contention
		var wg sync.WaitGroup
		numG := runtime.GOMAXPROCS(0)
		opsPerG := b.N / numG

		for g := 0; g < numG; g++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for i := 0; i < opsPerG; i++ {
					// Same prefix = likely same shard
					key := fmt.Sprintf("same-shard-%d", i)
					_ = s.Set(key, vec)
				}
			}(g)
		}
		wg.Wait()
	})

	b.Run("distributed_shards", func(b *testing.B) {
		s := storage.New()
		vec := make([]float32, dims)
		for i := range vec {
			vec[i] = float32(i) * 0.001
		}

		b.ResetTimer()
		b.ReportAllocs()

		// Each goroutine uses unique prefix = different shards
		// Should have less contention
		var wg sync.WaitGroup
		numG := runtime.GOMAXPROCS(0)
		opsPerG := b.N / numG

		for g := 0; g < numG; g++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for i := 0; i < opsPerG; i++ {
					// Unique prefix per goroutine = distributed across shards
					key := fmt.Sprintf("g%d-key-%d", id, i)
					_ = s.Set(key, vec)
				}
			}(g)
		}
		wg.Wait()
	})
}
