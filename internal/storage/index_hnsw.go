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
	"math"
	"math/rand"
	"sync"

	"github.com/uzqw/vex/internal/vector"
)

// HNSWNode represents a single vector in the HNSW graph
type HNSWNode struct {
	ID        string
	Vector    []float32
	Level     int               // Node's layer level (0 = bottom layer)
	Neighbors [][]*HNSWNeighbor // Multi-layer neighbor lists: neighbors[layer] = [neighbor1, neighbor2, ...]
}

// HNSWNeighbor represents a link to another node with precomputed distance
type HNSWNeighbor struct {
	Node     *HNSWNode
	Distance float32
}

// candidateHeap is a max-heap of candidates (used in HNSW search)
type candidateHeap []*candidate

type candidate struct {
	node     *HNSWNode
	distance float32
}

// Heap interface implementation for candidateHeap
func (h candidateHeap) Len() int           { return len(h) }
func (h candidateHeap) Less(i, j int) bool { return h[i].distance > h[j].distance } // max-heap
func (h candidateHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *candidateHeap) Push(x interface{}) {
	*h = append(*h, x.(*candidate))
}

func (h *candidateHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// HNSWIndex implements Hierarchical Navigable Small World algorithm.
// Time complexity: O(log n) for search, O(log n) for insert (amortized)
// Space complexity: O(n * M) where M is the maximum number of neighbors per node
type HNSWIndex struct {
	mu          sync.RWMutex
	nodes       map[string]*HNSWNode  // Map from key to node
	entryPoint  *HNSWNode             // Entry point for search (highest level node)
	maxLevel    int                   // Maximum layer level
	levelMult   float32               // Multiplier for level assignment (typically 1.0/ln(2.0))
	M           int                   // Maximum number of neighbors per layer
	efConstruct int                   // Search width for construction
	ef          int                   // Search width for search queries
}

// NewHNSWIndex creates a new HNSW index with default parameters
func NewHNSWIndex() *HNSWIndex {
	return &HNSWIndex{
		nodes:       make(map[string]*HNSWNode),
		maxLevel:    0,
		levelMult:   float32(1.0 / math.Log(2.0)),
		M:           6,   // Default maximum neighbors per layer
		efConstruct: 200, // Default construction beam width
		ef:          200, // Default search beam width
	}
}

// assignLevel assigns a random level to a new node using exponential decay distribution
func (h *HNSWIndex) assignLevel() int {
	return int(-math.Log(rand.Float64()) * float64(h.levelMult))
}

// distanceBetween computes cosine similarity between two vectors
// Both vectors should be normalized, so similarity = dot product
func distanceBetween(vec1, vec2 []float32) (float32, error) {
	return vector.DotProduct(vec1, vec2)
}

// Insert adds a new vector to the HNSW index
func (h *HNSWIndex) Insert(key string, vec []float32) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if node already exists
	if _, exists := h.nodes[key]; exists {
		return fmt.Errorf("vector with key %q already exists", key)
	}

	// Create new node
	level := h.assignLevel()
	newNode := &HNSWNode{
		ID:        key,
		Vector:    vec,
		Level:     level,
		Neighbors: make([][]*HNSWNeighbor, level+1),
	}
	for i := 0; i <= level; i++ {
		newNode.Neighbors[i] = make([]*HNSWNeighbor, 0)
	}

	h.nodes[key] = newNode

	// If this is the first node
	if h.entryPoint == nil {
		h.entryPoint = newNode
		h.maxLevel = level
		return nil
	}

	// Find nearest neighbors at all levels
	currentNearest := h.entryPoint

	// Search from top to target layer
	for lc := h.maxLevel; lc > level; lc-- {
		currentNearest, _ = h.searchLayer(vec, currentNearest, lc, 1)
	}

	// Insert node at all levels from level to 0
	for lc := level; lc >= 0; lc-- {
		candidates := h.searchLayerWithEf(vec, currentNearest, lc, h.efConstruct)
		neighbors := h.getNeighbors(candidates, h.M)

		// Add bidirectional links
		for _, neighbor := range neighbors {
			dist, _ := distanceBetween(vec, neighbor.Vector)
			newNode.Neighbors[lc] = append(newNode.Neighbors[lc], &HNSWNeighbor{
				Node:     neighbor,
				Distance: dist,
			})

			// Prune neighbors of the existing node (only if neighbor has this layer)
			if neighbor.Level >= lc {
				neighbor.Neighbors[lc] = append(neighbor.Neighbors[lc], &HNSWNeighbor{
					Node:     newNode,
					Distance: dist,
				})
				h.pruneNeighbors(neighbor, lc, h.M)
			}
		}

		// Update current nearest for next layer
		if len(neighbors) > 0 {
			currentNearest = neighbors[0]
		}
	}

	// Update entry point if new node's level is higher
	if level > h.maxLevel {
		h.entryPoint = newNode
		h.maxLevel = level
	}

	return nil
}

// Search finds the top-k most similar vectors
func (h *HNSWIndex) Search(query []float32, k int) ([]vector.SearchResult, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entryPoint == nil {
		return []vector.SearchResult{}, nil
	}

	// Search from top layer to layer 0
	currentNearest := h.entryPoint

	for lc := h.maxLevel; lc > 0; lc-- {
		currentNearest, _ = h.searchLayer(query, currentNearest, lc, 1)
	}

	// Search at layer 0 with ef parameter
	candidates := h.searchLayerWithEf(query, currentNearest, 0, h.ef)

	// Extract top-k results
	results := make([]vector.SearchResult, 0, k)
	for i := 0; i < k && i < len(candidates); i++ {
		distance, _ := distanceBetween(query, candidates[i].Vector)
		results = append(results, vector.SearchResult{
			Key:        candidates[i].ID,
			Similarity: distance,
		})
	}

	return results, nil
}

// searchLayer performs a greedy search on a specific layer
func (h *HNSWIndex) searchLayer(query []float32, entryPoint *HNSWNode, layer int, ef int) (*HNSWNode, float32) {
	visited := make(map[*HNSWNode]bool)
	candidates := make(candidateHeap, 0)
	w := make(candidateHeap, 0)

	distance, _ := distanceBetween(query, entryPoint.Vector)
	heap.Push(&candidates, &candidate{node: entryPoint, distance: distance})
	heap.Push(&w, &candidate{node: entryPoint, distance: distance})
	visited[entryPoint] = true

	for len(candidates) > 0 {
		lowerBound := candidates[0].distance
		if lowerBound > w[len(w)-1].distance {
			break
		}

		current := heap.Pop(&candidates).(*candidate)

		if current.distance > lowerBound {
			continue
		}

		// Check neighbors at this layer
		if current.node.Level >= layer {
			for _, neighbor := range current.node.Neighbors[layer] {
				if !visited[neighbor.Node] {
					visited[neighbor.Node] = true
					distance, _ := distanceBetween(query, neighbor.Node.Vector)

					if distance > w[0].distance || len(w) < ef {
						heap.Push(&candidates, &candidate{node: neighbor.Node, distance: distance})
						heap.Push(&w, &candidate{node: neighbor.Node, distance: distance})

						if len(w) > ef {
							heap.Pop(&w)
						}
					}
				}
			}
		}
	}

	// Return the closest node
	if len(w) > 0 {
		return w[len(w)-1].node, w[len(w)-1].distance
	}
	return entryPoint, distance
}

// searchLayerWithEf performs greedy search and returns multiple candidates sorted by similarity
func (h *HNSWIndex) searchLayerWithEf(query []float32, entryPoint *HNSWNode, layer int, ef int) []*HNSWNode {
	visited := make(map[*HNSWNode]bool)
	candidates := make(candidateHeap, 0)
	w := make(candidateHeap, 0)

	distance, _ := distanceBetween(query, entryPoint.Vector)
	heap.Push(&candidates, &candidate{node: entryPoint, distance: distance})
	heap.Push(&w, &candidate{node: entryPoint, distance: distance})
	visited[entryPoint] = true

	for len(candidates) > 0 {
		lowerBound := candidates[0].distance
		if lowerBound > w[0].distance {
			break
		}

		current := heap.Pop(&candidates).(*candidate)

		if current.distance > lowerBound {
			continue
		}

		// Check neighbors
		if current.node.Level >= layer {
			for _, neighbor := range current.node.Neighbors[layer] {
				if !visited[neighbor.Node] {
					visited[neighbor.Node] = true
					distance, _ := distanceBetween(query, neighbor.Node.Vector)

					if distance > w[0].distance || len(w) < ef {
						heap.Push(&candidates, &candidate{node: neighbor.Node, distance: distance})
						heap.Push(&w, &candidate{node: neighbor.Node, distance: distance})

						if len(w) > ef {
							heap.Pop(&w)
						}
					}
				}
			}
		}
	}

	// Convert heap to sorted result
	result := make([]*HNSWNode, len(w))
	for i := len(w) - 1; i >= 0; i-- {
		result[i] = heap.Pop(&w).(*candidate).node
	}
	return result
}

// getNeighbors returns the M nearest neighbors from candidates
func (h *HNSWIndex) getNeighbors(candidates []*HNSWNode, m int) []*HNSWNode {
	if len(candidates) <= m {
		return candidates
	}
	return candidates[:m]
}

// pruneNeighbors prunes the neighbor list to maintain size constraint
func (h *HNSWIndex) pruneNeighbors(node *HNSWNode, layer int, m int) {
	neighbors := node.Neighbors[layer]
	if len(neighbors) <= m {
		return
	}

	// Keep the m closest neighbors by distance
	// Sort by distance in descending order (higher similarity first)
	for i := 0; i < len(neighbors); i++ {
		for j := i + 1; j < len(neighbors); j++ {
			if neighbors[j].Distance > neighbors[i].Distance {
				neighbors[i], neighbors[j] = neighbors[j], neighbors[i]
			}
		}
	}

	// Keep only top m neighbors (highest similarity)
	node.Neighbors[layer] = neighbors[:m]
}

// Delete removes a vector from the index (marks as deleted, actual removal not implemented)
func (h *HNSWIndex) Delete(key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.nodes, key)
	return nil
}

// Clear removes all vectors from the index
func (h *HNSWIndex) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodes = make(map[string]*HNSWNode)
	h.entryPoint = nil
	h.maxLevel = 0
}

// Count returns the number of vectors in the index
func (h *HNSWIndex) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.nodes)
}

// IndexStats contains statistics about the HNSW index
type IndexStats struct {
	TotalVectors    int            // Total number of vectors in index
	MaxLevel        int            // Maximum layer level in the graph
	LayerDistribution map[int]int   // Number of nodes at each layer
	AverageNeighbors float32       // Average neighbors per node
}

// GetStats returns statistical information about the index
func (h *HNSWIndex) GetStats() IndexStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := IndexStats{
		TotalVectors:      len(h.nodes),
		MaxLevel:          h.maxLevel,
		LayerDistribution: make(map[int]int),
	}

	// Count nodes at each layer
	var totalNeighbors int
	for _, node := range h.nodes {
		for level := 0; level <= node.Level; level++ {
			stats.LayerDistribution[level]++
			totalNeighbors += len(node.Neighbors[level])
		}
	}

	// Calculate average neighbors
	totalNodes := 0
	for _, count := range stats.LayerDistribution {
		totalNodes += count
	}
	if totalNodes > 0 {
		stats.AverageNeighbors = float32(totalNeighbors) / float32(totalNodes)
	}

	return stats
}
