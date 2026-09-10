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
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"

	"github.com/uzqw/vex/internal/vector"
)

// HNSWNode represents a single vector in the HNSW graph
type HNSWNode struct {
	ID        string
	Vector    []float32
	Level     int               // Node's layer level (0 = bottom layer)
	Neighbors [][]*HNSWNeighbor // Multi-layer neighbor lists: neighbors[layer] = [neighbor1, neighbor2, ...]
	deleted   bool              // tombstone: set in Delete, checked during traversal
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
type maxHeap []candidate

func (h *maxHeap) push(x candidate) {
	*h = append(*h, x)
	for child := len(*h) - 1; child > 0; {
		parent := (child - 1) / 2
		if (*h)[parent].distance >= (*h)[child].distance {
			break
		}
		(*h)[parent], (*h)[child] = (*h)[child], (*h)[parent]
		child = parent
	}
}

func (h *maxHeap) pop() candidate {
	old := *h
	root := old[0]
	last := old[len(old)-1]
	old = old[:len(old)-1]
	if len(old) > 0 {
		old[0] = last
		siftDownMax(old, 0)
	}
	*h = old
	return root
}

func siftDownMax(h maxHeap, parent int) {
	for {
		left := parent*2 + 1
		if left >= len(h) {
			return
		}
		best := left
		right := left + 1
		if right < len(h) && h[right].distance > h[left].distance {
			best = right
		}
		if h[parent].distance >= h[best].distance {
			return
		}
		h[parent], h[best] = h[best], h[parent]
		parent = best
	}
}

// minHeap is a min-heap: the closest (best) candidate is at the top.
// Used for C (candidate set) so we always expand the nearest unvisited node first.
type minHeap []candidate

func (h *minHeap) push(x candidate) {
	*h = append(*h, x)
	for child := len(*h) - 1; child > 0; {
		parent := (child - 1) / 2
		if (*h)[parent].distance <= (*h)[child].distance {
			break
		}
		(*h)[parent], (*h)[child] = (*h)[child], (*h)[parent]
		child = parent
	}
}

func (h *minHeap) pop() candidate {
	old := *h
	root := old[0]
	last := old[len(old)-1]
	old = old[:len(old)-1]
	if len(old) > 0 {
		old[0] = last
		siftDownMin(old, 0)
	}
	*h = old
	return root
}

func siftDownMin(h minHeap, parent int) {
	for {
		left := parent*2 + 1
		if left >= len(h) {
			return
		}
		best := left
		right := left + 1
		if right < len(h) && h[right].distance < h[left].distance {
			best = right
		}
		if h[parent].distance <= h[best].distance {
			return
		}
		h[parent], h[best] = h[best], h[parent]
		parent = best
	}
}

// maxAllowedLevel caps random level assignment to avoid pathological allocations.
const maxAllowedLevel = 64

// HNSWIndex implements Hierarchical Navigable Small World algorithm.
// Search and insert are expected sub-linear in practice; cost depends on ef,
// graph degree, traversed layers, and vector dimension — not a strict O(log n).
// Space complexity: O(n * M) where M is the maximum number of neighbors per node.
type HNSWIndex struct {
	mu          sync.RWMutex
	nodes       map[string]*HNSWNode // Map from key to node
	entryPoint  *HNSWNode            // Entry point for search (highest level node)
	maxLevel    int                  // Maximum layer level
	dim         int                  // Vector dimension; 0 until first insert
	levelMult   float32              // Multiplier for level assignment (typically 1.0/ln(2.0))
	M           int                  // Maximum number of neighbors per layer (layer > 0)
	EfConstruct int                  // Search width for construction
	Ef          int                  // Search width for search queries
	rng         *rand.Rand           // Per-index RNG, avoids global rand lock contention
}

// HNSWConfig controls HNSW graph density and search breadth.
type HNSWConfig struct {
	M           int
	EfConstruct int
	Ef          int
	Seed        int64
}

const (
	DefaultHNSWM           = 16
	DefaultHNSWEfConstruct = 64 // 600 made GIST1M insert ~135 vec/s; search ef stays 600
	DefaultHNSWEf          = 600
)

// DefaultHNSWConfig returns the default HNSW configuration.
func DefaultHNSWConfig() HNSWConfig {
	return HNSWConfig{
		M:           DefaultHNSWM,
		EfConstruct: DefaultHNSWEfConstruct,
		Ef:          DefaultHNSWEf,
		Seed:        rand.Int63(),
	}
}

func (c HNSWConfig) withDefaults() HNSWConfig {
	defaults := DefaultHNSWConfig()
	if c.M <= 0 {
		c.M = defaults.M
	}
	if c.EfConstruct <= 0 {
		c.EfConstruct = defaults.EfConstruct
	}
	if c.Ef <= 0 {
		c.Ef = defaults.Ef
	}
	if c.Seed == 0 {
		c.Seed = defaults.Seed
	}
	return c
}

// NewHNSWIndex creates a new HNSW index with default parameters
func NewHNSWIndex() *HNSWIndex {
	return NewHNSWIndexWithConfig(DefaultHNSWConfig())
}

// NewHNSWIndexWithConfig creates a new HNSW index with custom parameters.
func NewHNSWIndexWithConfig(config HNSWConfig) *HNSWIndex {
	config = config.withDefaults()
	return &HNSWIndex{
		nodes:       make(map[string]*HNSWNode),
		maxLevel:    0,
		levelMult:   float32(1.0 / math.Log(2.0)),
		M:           config.M,           // Maximum neighbors per layer (layer > 0); layer 0 uses 2*M
		EfConstruct: config.EfConstruct, // Construction beam width
		Ef:          config.Ef,          // Search beam width
		rng:         rand.New(rand.NewSource(config.Seed)),
	}
}

// assignLevel assigns a random level to a new node using exponential decay distribution.
// Uses r in (0,1] so Log never sees 0.
func (h *HNSWIndex) assignLevel() int {
	// h.rng is only called inside Insert which holds the write lock, so no extra sync needed
	return computeLevel(h.rng.Float64(), h.levelMult, maxAllowedLevel)
}

// computeLevel is a pure helper for level assignment (testable with edge inputs).
func computeLevel(u float64, levelMult float32, capLevel int) int {
	// Map u in [0,1) to r in (0,1]: avoid Log(0).
	r := 1.0 - u
	if r <= 0 {
		r = math.SmallestNonzeroFloat64
	}
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return 0
	}
	level := int(-math.Log(r) * float64(levelMult))
	if level < 0 {
		level = 0
	}
	if capLevel > 0 && level > capLevel {
		level = capLevel
	}
	return level
}

// adaptiveEf is currently a pass-through. Earlier versions scaled ef down for
// high-dimensional vectors, but that hurt recall@10 on 1024D/1536D workloads.
func (h *HNSWIndex) adaptiveEf(baseEf, dim int) int {
	return baseEf
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

	// Dimension check before mutating the graph.
	if h.dim == 0 {
		if len(vec) == 0 {
			return fmt.Errorf("vector dimension must be > 0")
		}
	} else if len(vec) != h.dim {
		return fmt.Errorf("dimension mismatch: expected %d, got %d", h.dim, len(vec))
	}

	// Check if node already exists
	if _, exists := h.nodes[key]; exists {
		return fmt.Errorf("vector with key %q already exists", key)
	}

	// Own a copy so callers cannot mutate graph data.
	stored := append([]float32(nil), vec...)

	// Create new node
	level := h.assignLevel()
	newNode := &HNSWNode{
		ID:        key,
		Vector:    stored,
		Level:     level,
		Neighbors: make([][]*HNSWNeighbor, level+1),
	}
	for i := 0; i <= level; i++ {
		newNode.Neighbors[i] = make([]*HNSWNeighbor, 0)
	}

	// If this is the first node
	if h.entryPoint == nil {
		h.dim = len(stored)
		h.nodes[key] = newNode
		h.entryPoint = newNode
		h.maxLevel = level
		return nil
	}

	oldMaxLevel := h.maxLevel
	currentNearest := h.entryPoint

	// Greedy descent on layers that already exist in the graph.
	// Layers above oldMaxLevel must not be searched or linked yet.
	for lc := oldMaxLevel; lc > level; lc-- {
		var err error
		currentNearest, _, err = h.searchLayer(stored, currentNearest, lc, 1)
		if err != nil {
			return err
		}
	}

	// Connect only on layers that both the new node and the existing graph share.
	startLayer := level
	if startLayer > oldMaxLevel {
		startLayer = oldMaxLevel
	}

	for lc := startLayer; lc >= 0; lc-- {
		mEffective := h.M
		if lc == 0 {
			mEffective = h.M * 2
		}

		candidates, err := h.searchLayerWithEf(stored, currentNearest, lc, h.adaptiveEf(h.EfConstruct, len(stored)))
		if err != nil {
			return err
		}
		neighbors, err := h.selectNeighborsHeuristic(stored, candidates, mEffective)
		if err != nil {
			return err
		}

		for _, neighbor := range neighbors {
			dist, err := distanceBetween(stored, neighbor.Vector)
			if err != nil {
				return err
			}
			newNode.Neighbors[lc] = append(newNode.Neighbors[lc], &HNSWNeighbor{
				Node:     neighbor,
				Distance: dist,
			})

			if neighbor.Level >= lc {
				neighbor.Neighbors[lc] = append(neighbor.Neighbors[lc], &HNSWNeighbor{
					Node:     newNode,
					Distance: dist,
				})
				if err := h.pruneNeighbors(neighbor, lc, mEffective); err != nil {
					return err
				}
			}
		}

		if len(neighbors) > 0 {
			currentNearest = neighbors[0]
		}
	}

	h.nodes[key] = newNode

	// Layers above oldMaxLevel stay empty; new node becomes entry point.
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
	if h.dim > 0 && len(query) != h.dim {
		return nil, fmt.Errorf("dimension mismatch: expected %d, got %d", h.dim, len(query))
	}

	// Search from top layer to layer 0
	currentNearest := h.entryPoint

	for lc := h.maxLevel; lc > 0; lc-- {
		var err error
		currentNearest, _, err = h.searchLayer(query, currentNearest, lc, 1)
		if err != nil {
			return nil, err
		}
	}

	// Search at layer 0 with ef parameter. ef must cover k, otherwise
	// recall@k is capped by the candidate list size.
	searchEf := h.adaptiveEf(h.Ef, len(query))
	if searchEf < k {
		searchEf = k
	}
	candidates, err := h.searchLayerWithEf(query, currentNearest, 0, searchEf)
	if err != nil {
		return nil, err
	}

	// Extract top-k results
	results := make([]vector.SearchResult, 0, k)
	for i := 0; i < k && i < len(candidates); i++ {
		distance, err := distanceBetween(query, candidates[i].Vector)
		if err != nil {
			return nil, err
		}
		// distance is negative similarity, so negate it to get actual similarity
		results = append(results, vector.SearchResult{
			Key:        candidates[i].ID,
			Similarity: -distance,
		})
	}

	return results, nil
}

var visitedPool = sync.Pool{New: func() any { return make(map[*HNSWNode]bool, 128) }}

func acquireVisited() map[*HNSWNode]bool {
	m := visitedPool.Get().(map[*HNSWNode]bool)
	clear(m)
	return m
}

func releaseVisited(m map[*HNSWNode]bool) {
	visitedPool.Put(m)
}

// searchLayer performs a greedy search on a specific layer, returning the single closest node.
// C (candidates) is a min-heap so we always expand the nearest node first.
// W (working set) is a max-heap so we can cheaply evict the worst result.
func (h *HNSWIndex) searchLayer(query []float32, entryPoint *HNSWNode, layer int, ef int) (*HNSWNode, float32, error) {
	visited := acquireVisited()
	defer releaseVisited(visited)
	C := make(minHeap, 0, ef) // candidates: min-heap, closest at top
	W := make(maxHeap, 0, ef) // working set: max-heap, furthest (worst) at top

	dist, err := distanceBetween(query, entryPoint.Vector)
	if err != nil {
		return nil, 0, err
	}
	C.push(candidate{node: entryPoint, distance: dist})
	W.push(candidate{node: entryPoint, distance: dist})
	visited[entryPoint] = true

	// Track best inline instead of scanning W at the end
	best, bestDist := entryPoint, dist

	for len(C) > 0 {
		c := C[0].distance // min of C (nearest candidate)
		f := W[0].distance // max of W (worst result in working set)
		if c > f {
			break
		}

		current := C.pop()

		if current.node.Level >= layer {
			for _, neighbor := range current.node.Neighbors[layer] {
				nb := neighbor.Node
				if visited[nb] || nb.deleted {
					continue
				}
				visited[nb] = true
				d, err := distanceBetween(query, nb.Vector)
				if err != nil {
					return nil, 0, err
				}

				if d < W[0].distance || len(W) < ef {
					C.push(candidate{node: nb, distance: d})
					W.push(candidate{node: nb, distance: d})
					if len(W) > ef {
						W.pop()
					}
					if d < bestDist {
						bestDist = d
						best = nb
					}
				}
			}
		}
	}

	return best, bestDist, nil
}

// searchLayerWithEf performs greedy search and returns up to ef candidates sorted closest-first.
// C (candidates) is a min-heap; W (working set) is a max-heap.
func (h *HNSWIndex) searchLayerWithEf(query []float32, entryPoint *HNSWNode, layer int, ef int) ([]*HNSWNode, error) {
	visited := acquireVisited()
	defer releaseVisited(visited)
	C := make(minHeap, 0, ef) // candidates: min-heap
	W := make(maxHeap, 0, ef) // working set: max-heap

	dist, err := distanceBetween(query, entryPoint.Vector)
	if err != nil {
		return nil, err
	}
	C.push(candidate{node: entryPoint, distance: dist})
	W.push(candidate{node: entryPoint, distance: dist})
	visited[entryPoint] = true

	for len(C) > 0 {
		c := C[0].distance // nearest candidate
		f := W[0].distance // furthest result (worst in W)
		if c > f {
			break // no candidate can improve W
		}

		current := C.pop()

		if current.node.Level >= layer {
			for _, neighbor := range current.node.Neighbors[layer] {
				nb := neighbor.Node
				if visited[nb] || nb.deleted {
					continue
				}
				visited[nb] = true
				d, err := distanceBetween(query, nb.Vector)
				if err != nil {
					return nil, err
				}

				if d < W[0].distance || len(W) < ef {
					C.push(candidate{node: nb, distance: d})
					W.push(candidate{node: nb, distance: d})
					if len(W) > ef {
						W.pop() // evict furthest
					}
				}
			}
		}
	}

	// Drain W into a slice sorted closest-first, skipping any tombstoned nodes.
	result := make([]*HNSWNode, len(W))
	write := len(W)
	for i := len(W) - 1; i >= 0; i-- {
		n := W.pop().node
		if !n.deleted {
			write--
			result[write] = n
		}
	}
	return result[write:], nil
}

// selectNeighborsHeuristic implements HNSW paper Algorithm 4.
// Instead of returning the raw M closest candidates, it enforces spatial diversity:
// a candidate e is kept only if it is closer to the query than to every already-selected
// neighbor r. This prevents "shadowing" — where a cluster of very similar nodes all
// connect to the same query region, leaving other directions of the graph unreachable.
//
// keepPrunedConnections=true: if the heuristic leaves fewer than m neighbors we backfill
// from the discarded set so the graph stays well-connected.
func (h *HNSWIndex) selectNeighborsHeuristic(query []float32, candidates []*HNSWNode, m int) ([]*HNSWNode, error) {
	if len(candidates) <= m {
		return candidates, nil
	}

	// Limit the working set to 3*M closest candidates.
	// Running the full O(ef*M*dim) heuristic on all ef candidates is expensive
	// in high dimensions; 3*M gives enough diversity headroom while keeping
	// construction time bounded.
	workSet := candidates
	if len(workSet) > m*3 {
		workSet = workSet[:m*3]
	}

	// candidates are already sorted closest-first from searchLayerWithEf
	selected := make([]*HNSWNode, 0, m)
	discarded := make([]*HNSWNode, 0, len(workSet))

	for _, e := range workSet {
		if len(selected) >= m {
			break
		}
		distQE, err := distanceBetween(query, e.Vector)
		if err != nil {
			return nil, err
		}

		// e is dominated if any already-selected neighbor r is closer to e than q is
		dominated := false
		for _, r := range selected {
			distER, err := distanceBetween(e.Vector, r.Vector)
			if err != nil {
				return nil, err
			}
			if distER < distQE {
				dominated = true
				break
			}
		}

		if !dominated {
			selected = append(selected, e)
		} else {
			discarded = append(discarded, e)
		}
	}

	// keepPrunedConnections: backfill with discarded so we reach m if possible
	for _, e := range discarded {
		if len(selected) >= m {
			break
		}
		selected = append(selected, e)
	}

	return selected, nil
}

// pruneNeighbors trims a node's neighbor list to at most m closest entries.
func (h *HNSWIndex) pruneNeighbors(node *HNSWNode, layer int, m int) error {
	neighbors := node.Neighbors[layer]
	if len(neighbors) <= m {
		return nil
	}

	sort.Slice(neighbors, func(i, j int) bool {
		return neighbors[i].Distance < neighbors[j].Distance
	})

	node.Neighbors[layer] = neighbors[:m]
	return nil
}

// Delete removes a vector from the index and repairs all neighbor lists that
// referenced it, so no dangling pointers remain in the graph.
func (h *HNSWIndex) Delete(key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	node, exists := h.nodes[key]
	if !exists {
		return fmt.Errorf("vector with key %q not found", key)
	}

	// Mark as deleted so concurrent/future traversals skip it immediately
	node.deleted = true

	// fix 6: remove this node from every neighbor's adjacency list
	for layer := 0; layer <= node.Level; layer++ {
		for _, nb := range node.Neighbors[layer] {
			if nb.Node.Level < layer {
				continue
			}
			list := nb.Node.Neighbors[layer]
			newList := list[:0]
			for _, n := range list {
				if n.Node != node {
					newList = append(newList, n)
				}
			}
			nb.Node.Neighbors[layer] = newList
		}
	}

	delete(h.nodes, key)
	if len(h.nodes) == 0 {
		h.dim = 0
	}

	// If the deleted node was the entry point, elect a new one at the highest level
	if h.entryPoint == node {
		h.entryPoint = nil
		h.maxLevel = 0
		for _, n := range h.nodes {
			if h.entryPoint == nil || n.Level > h.entryPoint.Level {
				h.entryPoint = n
				h.maxLevel = n.Level
			}
		}
	}

	return nil
}

// Clear removes all vectors from the index
func (h *HNSWIndex) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodes = make(map[string]*HNSWNode)
	h.entryPoint = nil
	h.maxLevel = 0
	h.dim = 0
}

// CheckLayerInvariant verifies every neighbor at layer L has Level >= L.
// Intended for tests.
func (h *HNSWIndex) CheckLayerInvariant() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, node := range h.nodes {
		for layer := 0; layer <= node.Level; layer++ {
			for _, nb := range node.Neighbors[layer] {
				if nb.Node.Level < layer {
					return fmt.Errorf("node %q layer %d neighbor %q has level %d", node.ID, layer, nb.Node.ID, nb.Node.Level)
				}
			}
		}
	}
	return nil
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
