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

package storage

import (
	"container/heap"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"

	"github.com/uzqw/vex/internal/vector"
)

const (
	// ShardCount is the number of shards to distribute keys across
	// 32 is a good balance between concurrency and memory overhead
	ShardCount = 32

	// CacheLineSize is typically 64 bytes on modern CPUs
	// We pad each shard to prevent false sharing between CPU cores
	CacheLineSize = 64
)

// shard represents a single shard with its own lock
// The padding prevents false sharing when different cores access different shards
type shard struct {
	mu   sync.RWMutex
	data map[string][]float32
	_    [CacheLineSize - 16]byte // Padding to prevent false sharing (adjust based on struct size)
}

// Storage is a sharded, thread-safe in-memory vector storage
// Uses multiple shards with individual locks to reduce lock contention
type Storage struct {
	shards [ShardCount]*shard
	dim    atomic.Int32 // Expected vector dimension (0 means not set yet), lock-free
}

// New creates a new Storage instance
func New() *Storage {
	s := &Storage{}
	for i := 0; i < ShardCount; i++ {
		s.shards[i] = &shard{
			data: make(map[string][]float32),
		}
	}
	return s
}

// getShard returns the shard for a given key
func (s *Storage) getShard(key string) *shard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return s.shards[h.Sum32()%ShardCount]
}

// Set stores a vector with the given key
// Automatically normalizes the vector for optimized cosine similarity computation
func (s *Storage) Set(key string, values []float32) error {
	// Check dimension consistency using atomic operations (lock-free)
	dim := int(s.dim.Load())
	if dim == 0 {
		// Try to set dimension atomically; if another goroutine set it first, use theirs
		s.dim.CompareAndSwap(0, int32(len(values)))
		dim = int(s.dim.Load())
	}
	if len(values) != dim {
		return fmt.Errorf("dimension mismatch: expected %d, got %d", dim, len(values))
	}

	// Normalize the vector for optimized cosine similarity
	normalized, err := vector.Normalize(values)
	if err != nil {
		return fmt.Errorf("failed to normalize vector: %w", err)
	}

	shard := s.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.data[key] = normalized
	return nil
}

// Get retrieves a vector by key
func (s *Storage) Get(key string) ([]float32, bool) {
	shard := s.getShard(key)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	val, ok := shard.data[key]
	return val, ok
}

// Delete removes a vector by key
func (s *Storage) Delete(key string) bool {
	shard := s.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	_, exists := shard.data[key]
	if exists {
		delete(shard.data, key)
	}
	return exists
}

// Count returns the total number of vectors stored
func (s *Storage) Count() int {
	count := 0
	for i := 0; i < ShardCount; i++ {
		shard := s.shards[i]
		shard.mu.RLock()
		count += len(shard.data)
		shard.mu.RUnlock()
	}
	return count
}

// Search finds the top-K most similar vectors to the query vector
// Uses concurrent scanning across shards for better performance
func (s *Storage) Search(query []float32, k int) ([]vector.SearchResult, error) {
	// Normalize query vector for optimized comparison with stored normalized vectors
	normalizedQuery, err := vector.Normalize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize query: %w", err)
	}

	// Channel to collect results from each shard
	type shardResult struct {
		results []vector.SearchResult
		err     error
	}
	resultChan := make(chan shardResult, ShardCount)

	// Launch concurrent search across all shards
	var wg sync.WaitGroup
	for i := 0; i < ShardCount; i++ {
		wg.Add(1)
		go func(shardIdx int) {
			defer wg.Done()

			shard := s.shards[shardIdx]
			shard.mu.RLock()
			defer shard.mu.RUnlock()

			var results []vector.SearchResult
			for key, vec := range shard.data {
				// Since both vectors are normalized, dot product = cosine similarity
				similarity, err := vector.DotProduct(normalizedQuery, vec)
				if err != nil {
					resultChan <- shardResult{err: err}
					return
				}

				results = append(results, vector.SearchResult{
					Key:        key,
					Similarity: similarity,
				})
			}

			resultChan <- shardResult{results: results}
		}(i)
	}

	// Wait for all goroutines to finish
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Merge results using a min-heap to maintain top-K
	h := &vector.TopKHeap{}
	heap.Init(h)

	for result := range resultChan {
		if result.err != nil {
			return nil, result.err
		}

		for _, res := range result.results {
			if h.Len() < k {
				heap.Push(h, res)
			} else if res.Similarity > (*h)[0].Similarity {
				// Replace the minimum if we found a better match
				heap.Pop(h)
				heap.Push(h, res)
			}
		}
	}

	// Extract results and sort in descending order of similarity
	results := make([]vector.SearchResult, h.Len())
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(h).(vector.SearchResult)
	}

	return results, nil
}

// Clear removes all vectors from storage
func (s *Storage) Clear() {
	for i := 0; i < ShardCount; i++ {
		shard := s.shards[i]
		shard.mu.Lock()
		shard.data = make(map[string][]float32)
		shard.mu.Unlock()
	}
	s.dim.Store(0)
}

// Dimension returns the expected vector dimension (0 if no vectors stored yet)
func (s *Storage) Dimension() int {
	return int(s.dim.Load())
}
