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

type candidate struct {
	node     *HNSWNode
	distance float32
}

// maxHeap is a max-heap: the furthest (worst) candidate is at the top.
// Used for W (working set) to efficiently track the worst result and evict it.
type maxHeap []*candidate

func (h maxHeap) Len() int           { return len(h) }
func (h maxHeap) Less(i, j int) bool { return h[i].distance > h[j].distance }
func (h maxHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *maxHeap) Push(x interface{}) { *h = append(*h, x.(*candidate)) }
func (h *maxHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// minHeap is a min-heap: the closest (best) candidate is at the top.
// Used for C (candidate set) so we always expand the nearest unvisited node first.
type minHeap []*candidate

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].distance < h[j].distance }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *minHeap) Push(x interface{}) { *h = append(*h, x.(*candidate)) }
func (h *minHeap) Pop() interface{} {
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
	nodes       map[string]*HNSWNode // Map from key to node
	entryPoint  *HNSWNode            // Entry point for search (highest level node)
	maxLevel    int                  // Maximum layer level
	levelMult   float32              // Multiplier for level assignment (typically 1.0/ln(2.0))
	M           int                  // Maximum number of neighbors per layer
	EfConstruct int                  // Search width for construction
	Ef          int                  // Search width for search queries
}

// NewHNSWIndex creates a new HNSW index with default parameters
func NewHNSWIndex() *HNSWIndex {
	return &HNSWIndex{
		nodes:       make(map[string]*HNSWNode),
		maxLevel:    0,
		levelMult:   float32(1.0 / math.Log(2.0)),
		M:           32,    // Maximum neighbors per layer
		EfConstruct: 600,   // Construction beam width (balance quality and speed)
		Ef:          600,   // Search beam width (balance quality and speed)
	}
}

// assignLevel assigns a random level to a new node using exponential decay distribution
func (h *HNSWIndex) assignLevel() int {
	return int(-math.Log(rand.Float64()) * float64(h.levelMult))
}

// distanceBetween computes distance between two vectors
// For normalized vectors, we use negative dot product as distance
// This converts similarity maximization to distance minimization
// Both vectors should be normalized, so similarity = dot product
func distanceBetween(vec1, vec2 []float32) (float32, error) {
	sim, err := vector.DotProduct(vec1, vec2)
	if err != nil {
		return 0, err
	}
	// Return negative similarity as distance (higher similarity = lower distance)
	return -sim, nil
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
		candidates := h.searchLayerWithEf(vec, currentNearest, lc, h.EfConstruct)
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
	candidates := h.searchLayerWithEf(query, currentNearest, 0, h.Ef)

	// Extract top-k results
	results := make([]vector.SearchResult, 0, k)
	for i := 0; i < k && i < len(candidates); i++ {
		distance, _ := distanceBetween(query, candidates[i].Vector)
		// distance is negative similarity, so negate it to get actual similarity
		results = append(results, vector.SearchResult{
			Key:        candidates[i].ID,
			Similarity: -distance,
		})
	}

	return results, nil
}

// searchLayer performs a greedy search on a specific layer, returning the single closest node.
// C (candidates) is a min-heap so we always expand the nearest node first.
// W (working set) is a max-heap so we can cheaply evict the worst result.
func (h *HNSWIndex) searchLayer(query []float32, entryPoint *HNSWNode, layer int, ef int) (*HNSWNode, float32) {
	visited := make(map[*HNSWNode]bool)
	C := make(minHeap, 0) // candidates: min-heap, closest at top
	W := make(maxHeap, 0) // working set: max-heap, furthest (worst) at top

	dist, _ := distanceBetween(query, entryPoint.Vector)
	heap.Push(&C, &candidate{node: entryPoint, distance: dist})
	heap.Push(&W, &candidate{node: entryPoint, distance: dist})
	visited[entryPoint] = true

	for len(C) > 0 {
		// c = nearest candidate; f = furthest result in W
		c := C[0].distance // min of C
		f := W[0].distance // max of W (worst result)
		// If even the closest candidate is worse than the worst result, stop.
		if c > f {
			break
		}

		current := heap.Pop(&C).(*candidate)

		if current.node.Level >= layer {
			for _, neighbor := range current.node.Neighbors[layer] {
				if visited[neighbor.Node] {
					continue
				}
				visited[neighbor.Node] = true
				d, _ := distanceBetween(query, neighbor.Node.Vector)

				if d < W[0].distance || len(W) < ef {
					heap.Push(&C, &candidate{node: neighbor.Node, distance: d})
					heap.Push(&W, &candidate{node: neighbor.Node, distance: d})
					if len(W) > ef {
						heap.Pop(&W) // evict the worst
					}
				}
			}
		}
	}

	// The closest node sits at the bottom of the max-heap; drain to find it.
	var best *HNSWNode
	var bestDist float32 = float32(math.Inf(1))
	for _, c := range W {
		if c.distance < bestDist {
			bestDist = c.distance
			best = c.node
		}
	}
	if best != nil {
		return best, bestDist
	}
	return entryPoint, dist
}

// searchLayerWithEf performs greedy search and returns up to ef candidates sorted closest-first.
// C (candidates) is a min-heap; W (working set) is a max-heap.
func (h *HNSWIndex) searchLayerWithEf(query []float32, entryPoint *HNSWNode, layer int, ef int) []*HNSWNode {
	visited := make(map[*HNSWNode]bool)
	C := make(minHeap, 0) // candidates: min-heap
	W := make(maxHeap, 0) // working set: max-heap

	dist, _ := distanceBetween(query, entryPoint.Vector)
	heap.Push(&C, &candidate{node: entryPoint, distance: dist})
	heap.Push(&W, &candidate{node: entryPoint, distance: dist})
	visited[entryPoint] = true

	for len(C) > 0 {
		c := C[0].distance // nearest candidate
		f := W[0].distance // furthest result (worst in W)
		if c > f {
			break // no candidate can improve W
		}

		current := heap.Pop(&C).(*candidate)

		if current.node.Level >= layer {
			for _, neighbor := range current.node.Neighbors[layer] {
				if visited[neighbor.Node] {
					continue
				}
				visited[neighbor.Node] = true
				d, _ := distanceBetween(query, neighbor.Node.Vector)

				if d < W[0].distance || len(W) < ef {
					heap.Push(&C, &candidate{node: neighbor.Node, distance: d})
					heap.Push(&W, &candidate{node: neighbor.Node, distance: d})
					if len(W) > ef {
						heap.Pop(&W) // evict furthest
					}
				}
			}
		}
	}

	// Drain W into a slice sorted closest-first
	result := make([]*HNSWNode, len(W))
	for i := len(W) - 1; i >= 0; i-- {
		result[i] = heap.Pop(&W).(*candidate).node
	}
	return result
}

// getNeighbors returns the M nearest neighbors from candidates
// Simple greedy approach: select closest M candidates
// For distributed/diverse neighborhoods, rely on proper ef/efConstruct parameters
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
	// Sort by distance in ascending order (smaller distance = closer)
	for i := 0; i < len(neighbors); i++ {
		for j := i + 1; j < len(neighbors); j++ {
			if neighbors[j].Distance < neighbors[i].Distance {
				neighbors[i], neighbors[j] = neighbors[j], neighbors[i]
			}
		}
	}

	// Keep only top m neighbors (closest)
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
	TotalVectors      int         // Total number of vectors in index
	MaxLevel          int         // Maximum layer level in the graph
	LayerDistribution map[int]int // Number of nodes at each layer
	AverageNeighbors  float32     // Average neighbors per node
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
