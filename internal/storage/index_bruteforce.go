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
	"sync"

	"github.com/uzqw/vex/internal/vector"
)

// BruteForceIndex implements a simple linear scan search without any indexing structure.
// Useful for small datasets or as a baseline for comparison.
// Time complexity: O(n*d) where n is number of vectors and d is dimensionality.
type BruteForceIndex struct {
	mu   sync.RWMutex
	data map[string][]float32 // key -> normalized vector
}

// NewBruteForceIndex creates a new brute force index
func NewBruteForceIndex() *BruteForceIndex {
	return &BruteForceIndex{
		data: make(map[string][]float32),
	}
}

// Insert adds a normalized vector to the index
func (bf *BruteForceIndex) Insert(key string, vec []float32) error {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	bf.data[key] = vec
	return nil
}

// Search scans all vectors and returns top-k results
func (bf *BruteForceIndex) Search(query []float32, k int) ([]vector.SearchResult, error) {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	if len(bf.data) == 0 {
		return []vector.SearchResult{}, nil
	}

	// Use a min-heap to maintain top-k results
	h := &vector.TopKHeap{}
	heap.Init(h)

	// Scan all vectors
	for key, vec := range bf.data {
		// Calculate similarity (both vectors are normalized, so dot product = cosine similarity)
		similarity, err := vector.DotProduct(query, vec)
		if err != nil {
			return nil, fmt.Errorf("failed to compute similarity: %w", err)
		}

		// Maintain top-k heap
		if h.Len() < k {
			heap.Push(h, vector.SearchResult{
				Key:        key,
				Similarity: similarity,
			})
		} else if similarity > (*h)[0].Similarity {
			// Replace minimum if we found a better match
			heap.Pop(h)
			heap.Push(h, vector.SearchResult{
				Key:        key,
				Similarity: similarity,
			})
		}
	}

	// Extract results in descending order
	results := make([]vector.SearchResult, h.Len())
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(h).(vector.SearchResult)
	}

	return results, nil
}

// Delete removes a vector from the index
func (bf *BruteForceIndex) Delete(key string) error {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	delete(bf.data, key)
	return nil
}

// Clear removes all vectors from the index
func (bf *BruteForceIndex) Clear() {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	bf.data = make(map[string][]float32)
}

// Count returns the number of vectors in the index
func (bf *BruteForceIndex) Count() int {
	bf.mu.RLock()
	defer bf.mu.RUnlock()
	return len(bf.data)
}
