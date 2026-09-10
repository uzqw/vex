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

const noNode int32 = -1

// hnswNode is one vertex in the packed graph. Neighbors are indices into
// HNSWIndex.nodes, not pointers — the pointer graph scattered at ~100k GIST
// inserts (50–100k window 1199 vec/s vs 1356 bar).
type hnswNode struct {
	id        string
	level     int
	deleted   bool
	neighbors [][]hnswEdge // neighbors[layer]
}

type hnswEdge struct {
	idx  int32
	dist float32
}

type candidate struct {
	idx      int32
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
	nodes       []hnswNode // ponytail: deleted nodes leave holes; reuse slots if delete churn matters
	vecs        []float32  // packed, node i at i*dim; holes unused
	keys        map[string]int32
	entry       int32 // noNode if empty
	maxLevel    int
	dim         int
	levelMult   float32
	M           int
	EfConstruct int
	Ef          int
	rng         *rand.Rand
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
		keys:        make(map[string]int32),
		entry:       noNode,
		maxLevel:    0,
		levelMult:   float32(1.0 / math.Log(2.0)),
		M:           config.M,
		EfConstruct: config.EfConstruct,
		Ef:          config.Ef,
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

func (h *HNSWIndex) vecAt(i int32) []float32 {
	off := int(i) * h.dim
	return h.vecs[off : off+h.dim]
}

func (h *HNSWIndex) makeNeighborLists(level int) [][]hnswEdge {
	nbs := make([][]hnswEdge, level+1)
	for i := 0; i <= level; i++ {
		capn := h.M
		if i == 0 {
			capn = h.M * 2
		}
		nbs[i] = make([]hnswEdge, 0, capn)
	}
	return nbs
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

	if _, exists := h.keys[key]; exists {
		return fmt.Errorf("vector with key %q already exists", key)
	}

	if h.dim == 0 {
		h.dim = len(vec)
	}
	h.vecs = append(h.vecs, vec...) // copies; packed so graph walk is not N slice headers

	level := h.assignLevel()
	idx := int32(len(h.nodes))
	h.nodes = append(h.nodes, hnswNode{
		id:        key,
		level:     level,
		neighbors: h.makeNeighborLists(level),
	})
	stored := h.vecAt(idx)

	if h.entry == noNode {
		h.keys[key] = idx
		h.entry = idx
		h.maxLevel = level
		return nil
	}

	oldMaxLevel := h.maxLevel
	currentNearest := h.entry

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

		for _, nbIdx := range neighbors {
			dist, err := distanceBetween(stored, h.vecAt(nbIdx))
			if err != nil {
				return err
			}
			h.nodes[idx].neighbors[lc] = append(h.nodes[idx].neighbors[lc], hnswEdge{idx: nbIdx, dist: dist})

			if h.nodes[nbIdx].level >= lc {
				h.nodes[nbIdx].neighbors[lc] = append(h.nodes[nbIdx].neighbors[lc], hnswEdge{idx: idx, dist: dist})
				if err := h.pruneNeighbors(nbIdx, lc, mEffective); err != nil {
					return err
				}
			}
		}

		if len(neighbors) > 0 {
			currentNearest = neighbors[0]
		}
	}

	h.keys[key] = idx

	// Layers above oldMaxLevel stay empty; new node becomes entry point.
	if level > h.maxLevel {
		h.entry = idx
		h.maxLevel = level
	}

	return nil
}

// Search finds the top-k most similar vectors
func (h *HNSWIndex) Search(query []float32, k int) ([]vector.SearchResult, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entry == noNode {
		return []vector.SearchResult{}, nil
	}
	if h.dim > 0 && len(query) != h.dim {
		return nil, fmt.Errorf("dimension mismatch: expected %d, got %d", h.dim, len(query))
	}

	currentNearest := h.entry

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

	results := make([]vector.SearchResult, 0, k)
	for i := 0; i < k && i < len(candidates); i++ {
		n := candidates[i]
		distance, err := distanceBetween(query, h.vecAt(n))
		if err != nil {
			return nil, err
		}
		results = append(results, vector.SearchResult{
			Key:        h.nodes[n].id,
			Similarity: -distance,
		})
	}

	return results, nil
}

var visitedPool = sync.Pool{New: func() any { return make(map[int32]bool, 128) }}

func acquireVisited() map[int32]bool {
	m := visitedPool.Get().(map[int32]bool)
	clear(m)
	return m
}

func releaseVisited(m map[int32]bool) {
	visitedPool.Put(m)
}

// searchLayer performs a greedy search on a specific layer, returning the single closest node.
// C (candidates) is a min-heap so we always expand the nearest node first.
// W (working set) is a max-heap so we can cheaply evict the worst result.
func (h *HNSWIndex) searchLayer(query []float32, entry int32, layer int, ef int) (int32, float32, error) {
	visited := acquireVisited()
	defer releaseVisited(visited)
	C := make(minHeap, 0, ef)
	W := make(maxHeap, 0, ef)

	dist, err := distanceBetween(query, h.vecAt(entry))
	if err != nil {
		return noNode, 0, err
	}
	C.push(candidate{idx: entry, distance: dist})
	W.push(candidate{idx: entry, distance: dist})
	visited[entry] = true

	best, bestDist := entry, dist

	for len(C) > 0 {
		c := C[0].distance
		f := W[0].distance
		if c > f {
			break
		}

		current := C.pop()

		if h.nodes[current.idx].level >= layer {
			for _, e := range h.nodes[current.idx].neighbors[layer] {
				if visited[e.idx] || h.nodes[e.idx].deleted {
					continue
				}
				visited[e.idx] = true
				d, err := distanceBetween(query, h.vecAt(e.idx))
				if err != nil {
					return noNode, 0, err
				}

				if d < W[0].distance || len(W) < ef {
					C.push(candidate{idx: e.idx, distance: d})
					W.push(candidate{idx: e.idx, distance: d})
					if len(W) > ef {
						W.pop()
					}
					if d < bestDist {
						bestDist = d
						best = e.idx
					}
				}
			}
		}
	}

	return best, bestDist, nil
}

// searchLayerWithEf performs greedy search and returns up to ef candidates sorted closest-first.
// C (candidates) is a min-heap; W (working set) is a max-heap.
func (h *HNSWIndex) searchLayerWithEf(query []float32, entry int32, layer int, ef int) ([]int32, error) {
	visited := acquireVisited()
	defer releaseVisited(visited)
	C := make(minHeap, 0, ef)
	W := make(maxHeap, 0, ef)

	dist, err := distanceBetween(query, h.vecAt(entry))
	if err != nil {
		return nil, err
	}
	C.push(candidate{idx: entry, distance: dist})
	W.push(candidate{idx: entry, distance: dist})
	visited[entry] = true

	for len(C) > 0 {
		c := C[0].distance
		f := W[0].distance
		if c > f {
			break
		}

		current := C.pop()

		if h.nodes[current.idx].level >= layer {
			for _, e := range h.nodes[current.idx].neighbors[layer] {
				if visited[e.idx] || h.nodes[e.idx].deleted {
					continue
				}
				visited[e.idx] = true
				d, err := distanceBetween(query, h.vecAt(e.idx))
				if err != nil {
					return nil, err
				}

				if d < W[0].distance || len(W) < ef {
					C.push(candidate{idx: e.idx, distance: d})
					W.push(candidate{idx: e.idx, distance: d})
					if len(W) > ef {
						W.pop()
					}
				}
			}
		}
	}

	result := make([]int32, len(W))
	write := len(W)
	for i := len(W) - 1; i >= 0; i-- {
		n := W.pop().idx
		if !h.nodes[n].deleted {
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
func (h *HNSWIndex) selectNeighborsHeuristic(query []float32, candidates []int32, m int) ([]int32, error) {
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

	selected := make([]int32, 0, m)
	discarded := make([]int32, 0, len(workSet))

	for _, e := range workSet {
		if len(selected) >= m {
			break
		}
		distQE, err := distanceBetween(query, h.vecAt(e))
		if err != nil {
			return nil, err
		}

		dominated := false
		for _, r := range selected {
			distER, err := distanceBetween(h.vecAt(e), h.vecAt(r))
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

	for _, e := range discarded {
		if len(selected) >= m {
			break
		}
		selected = append(selected, e)
	}

	return selected, nil
}

// pruneNeighbors trims a node's neighbor list to at most m closest entries.
func (h *HNSWIndex) pruneNeighbors(idx int32, layer int, m int) error {
	neighbors := h.nodes[idx].neighbors[layer]
	if len(neighbors) <= m {
		return nil
	}

	sort.Slice(neighbors, func(i, j int) bool {
		return neighbors[i].dist < neighbors[j].dist
	})

	h.nodes[idx].neighbors[layer] = neighbors[:m]
	return nil
}

// Delete removes a vector from the index and repairs all neighbor lists that
// referenced it, so no dangling pointers remain in the graph.
func (h *HNSWIndex) Delete(key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	idx, exists := h.keys[key]
	if !exists {
		return fmt.Errorf("vector with key %q not found", key)
	}

	h.nodes[idx].deleted = true

	for layer := 0; layer <= h.nodes[idx].level; layer++ {
		for _, e := range h.nodes[idx].neighbors[layer] {
			if h.nodes[e.idx].level < layer {
				continue
			}
			list := h.nodes[e.idx].neighbors[layer]
			newList := list[:0]
			for _, n := range list {
				if n.idx != idx {
					newList = append(newList, n)
				}
			}
			h.nodes[e.idx].neighbors[layer] = newList
		}
	}

	delete(h.keys, key)
	if len(h.keys) == 0 {
		h.dim = 0
	}

	if h.entry == idx {
		h.entry = noNode
		h.maxLevel = 0
		for i := range h.nodes {
			if h.nodes[i].deleted {
				continue
			}
			if h.entry == noNode || h.nodes[i].level > h.nodes[h.entry].level {
				h.entry = int32(i)
				h.maxLevel = h.nodes[i].level
			}
		}
	}

	return nil
}

// Clear removes all vectors from the index
func (h *HNSWIndex) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodes = nil
	h.vecs = nil
	h.keys = make(map[string]int32)
	h.entry = noNode
	h.maxLevel = 0
	h.dim = 0
}

// CheckLayerInvariant verifies every neighbor at layer L has Level >= L.
// Intended for tests.
func (h *HNSWIndex) CheckLayerInvariant() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for i := range h.nodes {
		node := h.nodes[i]
		if node.deleted {
			continue
		}
		for layer := 0; layer <= node.level; layer++ {
			for _, e := range node.neighbors[layer] {
				if h.nodes[e.idx].level < layer {
					return fmt.Errorf("node %q layer %d neighbor %q has level %d", node.id, layer, h.nodes[e.idx].id, h.nodes[e.idx].level)
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
	return len(h.keys)
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
		TotalVectors:      len(h.keys),
		MaxLevel:          h.maxLevel,
		LayerDistribution: make(map[int]int),
	}

	var totalNeighbors int
	for i := range h.nodes {
		node := h.nodes[i]
		if node.deleted {
			continue
		}
		for level := 0; level <= node.level; level++ {
			stats.LayerDistribution[level]++
			totalNeighbors += len(node.neighbors[level])
		}
	}

	totalNodes := 0
	for _, count := range stats.LayerDistribution {
		totalNodes += count
	}
	if totalNodes > 0 {
		stats.AverageNeighbors = float32(totalNeighbors) / float32(totalNodes)
	}

	return stats
}
