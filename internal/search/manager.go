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

package search

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/uzqw/vex/internal/storage"
	"github.com/uzqw/vex/internal/vector"
)

// Mode controls how the Manager maintains and queries the secondary index.
type Mode int

const (
	// ModeNone always searches Storage (linear scan).
	ModeNone Mode = iota
	// ModeBruteForce keeps a BruteForceIndex in sync with Storage.
	ModeBruteForce
	// ModeHNSW keeps an HNSW index in sync with Storage.
	ModeHNSW
	// ModeAuto stays dormant (Storage search) until AutoMin vectors, then
	// builds HNSW and uses it. On index failure, falls back to Storage.
	ModeAuto
)

// IndexState describes the readiness of the secondary index.
type IndexState int

const (
	// StateDormant means no secondary index is active (auto below threshold, or none).
	StateDormant IndexState = iota
	// StateReady means the secondary index is consistent and used for search.
	StateReady
	// StateDirty means the secondary index may be inconsistent; search falls back to Storage.
	StateDirty
)

func (s IndexState) String() string {
	switch s {
	case StateDormant:
		return "dormant"
	case StateReady:
		return "ready"
	case StateDirty:
		return "dirty"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Config configures a Manager.
type Config struct {
	Mode    Mode
	AutoMin int
	// NewIndex constructs a fresh secondary index (HNSW or BruteForce).
	// Required for ModeBruteForce, ModeHNSW, and ModeAuto.
	NewIndex func() storage.Index
	// RebuildDeleteRatio triggers a full rebuild after this fraction of
	// cumulative delete/upsert operations relative to current store size.
	//
	// Semantics:
	//   - NaN (math.NaN()): use mode default (0.10 for HNSW/Auto, disabled for BruteForce)
	//   - 0: disable periodic mutation rebuild
	//   - >0: rebuild when mutations >= ratio * store.Count()
	//
	// The zero value of Config leaves this as 0.0 which means "disable" unless
	// UseDefaultRebuildRatio is true.
	RebuildDeleteRatio float64
	// UseDefaultRebuildRatio applies mode defaults when RebuildDeleteRatio is 0.
	// When false (default), 0 means disabled.
	UseDefaultRebuildRatio bool
}

// Manager coordinates Storage (source of truth) and an optional secondary index.
// All VSET/VDEL/VSEARCH/CLEAR paths should go through Manager.
type Manager struct {
	mu sync.RWMutex

	store *storage.Storage
	index storage.Index

	mode    Mode
	state   IndexState
	autoMin int

	newIndex           func() storage.Index
	rebuildDeleteRatio float64 // <=0 disables periodic rebuild
	mutationSinceBuild int     // deletes + upserts since last successful build/swap
}

// NewManager creates a Manager. Storage is always the source of truth.
// If store already contains vectors, HNSW/BruteForce (and Auto above threshold)
// perform a synchronous rebuild so Search is consistent immediately.
func NewManager(store *storage.Storage, cfg Config) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if cfg.AutoMin <= 0 {
		cfg.AutoMin = 10000
	}

	ratio := cfg.RebuildDeleteRatio
	if math.IsNaN(ratio) || (cfg.UseDefaultRebuildRatio && ratio == 0) {
		switch cfg.Mode {
		case ModeHNSW, ModeAuto:
			ratio = 0.10
		default:
			ratio = 0 // BruteForce / None: no mutation rebuild by default
		}
	}

	m := &Manager{
		store:              store,
		mode:               cfg.Mode,
		autoMin:            cfg.AutoMin,
		newIndex:           cfg.NewIndex,
		rebuildDeleteRatio: ratio,
		state:              StateDormant,
	}

	switch cfg.Mode {
	case ModeNone:
		// nothing
	case ModeBruteForce, ModeHNSW:
		if cfg.NewIndex == nil {
			return nil, fmt.Errorf("NewIndex is required for mode %v", cfg.Mode)
		}
		if store.Count() > 0 {
			if err := m.rebuildFromStoreLocked(); err != nil {
				return nil, fmt.Errorf("initial rebuild: %w", err)
			}
		} else {
			m.index = cfg.NewIndex()
			m.state = StateReady
		}
	case ModeAuto:
		if cfg.NewIndex == nil {
			return nil, fmt.Errorf("NewIndex is required for auto mode")
		}
		if store.Count() >= m.autoMin {
			if err := m.rebuildFromStoreLocked(); err != nil {
				return nil, fmt.Errorf("initial rebuild: %w", err)
			}
		} else {
			m.state = StateDormant
		}
	default:
		return nil, fmt.Errorf("unknown mode %v", cfg.Mode)
	}

	return m, nil
}

// Set upserts a vector. Storage is written first; index failures mark dirty
// and do not roll back storage. created is true when the key did not exist.
func (m *Manager) Set(key string, values []float32) (created bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, existed := m.store.Get(key)
	if err := m.store.Set(key, values); err != nil {
		return false, err
	}
	normalized, ok := m.store.Get(key)
	if !ok {
		return false, fmt.Errorf("failed to read normalized vector after set")
	}

	if existed {
		m.mutationSinceBuild++
	}

	if err := m.syncAfterSetLocked(key, normalized, existed); err != nil {
		return !existed, err
	}
	return !existed, nil
}

// SetMetadata replaces the scalar metadata for an existing key.
// A nil or empty meta clears it.
func (m *Manager) SetMetadata(key string, meta map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.SetMetadata(key, meta)
}

// GetMetadata returns the scalar metadata for key (nil when none).
func (m *Manager) GetMetadata(key string) (map[string]string, bool) {
	return m.store.GetMetadata(key)
}

// GetAllMetadata returns copies of all key->metadata pairs. Keys without
// metadata are omitted. Persistence snapshots use this alongside
// GetAllVectors.
func (m *Manager) GetAllMetadata() (map[string]map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.MetadataSnapshot(), nil
}

// Delete removes a key from storage and the secondary index.
func (m *Manager) Delete(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	deleted := m.store.Delete(key)
	if !deleted {
		return false
	}
	m.mutationSinceBuild++

	if m.index != nil && m.state == StateReady {
		if err := m.index.Delete(key); err != nil {
			m.state = StateDirty
		}
	}

	if m.mode == ModeAuto && m.store.Count() < m.autoMin {
		m.dropIndexLocked()
	} else {
		m.maybeRebuildLocked()
	}
	return true
}

// Search finds top-k neighbors. Uses secondary index only when ready; otherwise Storage.
func (m *Manager) Search(query []float32, k int) ([]vector.SearchResult, error) {
	return m.SearchFiltered(query, k, "", "")
}

// SearchFiltered finds top-k neighbors restricted to keys whose metadata has
// field == value. An empty field disables filtering. The predicate is applied
// inside the search: brute-force paths scan only matching keys; HNSW applies
// it when collecting results from the ef candidates visited at layer 0, so a
// restrictive filter can return fewer than k results.
func (m *Manager) SearchFiltered(query []float32, k int, field, value string) ([]vector.SearchResult, error) {
	var allow func(string) bool
	if field != "" {
		allow = func(key string) bool { return m.store.Match(key, field, value) }
	}

	m.mu.RLock()
	useIndex := m.index != nil && m.state == StateReady
	idx := m.index
	m.mu.RUnlock()

	if !useIndex {
		if m.mode == ModeHNSW || (m.mode == ModeAuto && idx != nil) {
			return m.searchResolved(query, k, idx, allow)
		}
		return m.storeSearch(query, k, allow)
	}

	normalized, err := vector.Normalize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize query: %w", err)
	}
	results, err := idx.SearchWhere(normalized, k, allow)
	if err != nil {
		fallback, ferr := m.fallbackSearch(query, k, idx, allow)
		if ferr != nil {
			// Query/storage error, not an index fault.
			return nil, err
		}
		// Index fault: mark dirty if still the published instance, then fall back.
		m.mu.Lock()
		if m.index == idx {
			m.state = StateDirty
		}
		m.mu.Unlock()
		return fallback, nil
	}
	return results, nil
}

// storeSearch is Storage.Search with an optional key predicate.
func (m *Manager) storeSearch(query []float32, k int, allow func(string) bool) ([]vector.SearchResult, error) {
	if allow == nil {
		return m.store.Search(query, k)
	}
	normalizedQuery, err := vector.Normalize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize query: %w", err)
	}
	h := &vector.TopKHeap{}
	heap.Init(h)
	for _, key := range m.store.GetAllKeys() {
		if !allow(key) {
			continue
		}
		vec, ok := m.store.Get(key)
		if !ok || vec == nil {
			continue
		}
		similarity, err := vector.DotProduct(normalizedQuery, vec)
		if err != nil {
			return nil, err
		}
		if h.Len() < k {
			heap.Push(h, vector.SearchResult{Key: key, Similarity: similarity})
		} else if similarity > (*h)[0].Similarity {
			heap.Pop(h)
			heap.Push(h, vector.SearchResult{Key: key, Similarity: similarity})
		}
	}
	results := make([]vector.SearchResult, h.Len())
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(h).(vector.SearchResult)
	}
	return results, nil
}

// Clear wipes storage and the secondary index.
func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store.Clear()
	// Drop without Clear so in-flight Search on the old pointer can finish.
	m.index = nil
	m.state = StateDormant
	m.mutationSinceBuild = 0
	if m.mode == ModeBruteForce || m.mode == ModeHNSW {
		m.index = m.newIndex()
		m.state = StateReady
	}
}

// Count returns the number of vectors in storage.
func (m *Manager) Count() int {
	return m.store.Count()
}

// GetDimension returns the expected vector dimension (0 if unset).
// Named for persistence.VectorDataSource.
func (m *Manager) GetDimension() int {
	return m.store.Dimension()
}

// GetAllKeys returns all keys currently in storage.
func (m *Manager) GetAllKeys() []string {
	return m.store.GetAllKeys()
}

// GetAllVectors returns copies of every vector, resolving bodies that
// HNSW/Auto modes keep only in the packed index. Persistence snapshots must
// go through this rather than reading Storage directly.
func (m *Manager) GetAllVectors() (map[string][]float32, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotLocked(), nil
}

// SetAllVectors bulk-restores vectors through Set so storage and the
// secondary index stay consistent. Used by persistence recovery.
func (m *Manager) SetAllVectors(vectors map[string][]float32) error {
	return m.SetAllVectorsWithMetadata(vectors, nil)
}

// SetAllVectorsWithMetadata restores vectors and their scalar metadata.
// Implements persistence.MetadataDataSource.
func (m *Manager) SetAllVectorsWithMetadata(vectors map[string][]float32, meta map[string]map[string]string) error {
	for key, vec := range vectors {
		if _, err := m.Set(key, vec); err != nil {
			return fmt.Errorf("failed to set vector %s: %w", key, err)
		}
		if fields, ok := meta[key]; ok {
			if err := m.store.SetMetadata(key, fields); err != nil {
				return fmt.Errorf("failed to set metadata %s: %w", key, err)
			}
		}
	}
	return nil
}

// Get returns a vector. HNSW/Auto may keep the body in the packed index.
func (m *Manager) Get(key string) ([]float32, bool) {
	v, ok := m.store.Get(key)
	if !ok {
		return nil, false
	}
	if v != nil {
		return v, true
	}
	m.mu.RLock()
	idx := m.index
	m.mu.RUnlock()
	if idx != nil {
		if vec, ok := idx.Get(key); ok {
			return vec, true
		}
	}
	return m.store.Get(key)
}

// State returns the current index readiness (for tests/stats).
func (m *Manager) State() IndexState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// Mode returns the configured mode.
func (m *Manager) Mode() Mode {
	return m.mode
}

// IndexCount returns secondary index size, or -1 if none.
func (m *Manager) IndexCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.index == nil {
		return -1
	}
	return m.index.Count()
}

// Rebuild rebuilds the secondary index from a storage snapshot (if mode uses one).
func (m *Manager) Rebuild() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rebuildFromStoreLocked()
}

func (m *Manager) syncAfterSetLocked(key string, normalized []float32, existed bool) error {
	switch m.mode {
	case ModeNone:
		return nil

	case ModeBruteForce, ModeHNSW:
		if m.state != StateReady || m.index == nil {
			_ = m.rebuildFromStoreLocked() // storage already committed
			return nil
		}
		if existed {
			if err := m.index.Delete(key); err != nil {
				m.state = StateDirty
				return nil
			}
		}
		if err := m.index.Insert(key, normalized); err != nil {
			m.state = StateDirty
			return nil // storage already committed
		}
		if m.mode == ModeHNSW {
			m.store.DropVector(key)
		}
		m.maybeRebuildLocked()
		return nil

	case ModeAuto:
		count := m.store.Count()
		if count < m.autoMin {
			if m.index != nil {
				m.dropIndexLocked()
			}
			return nil
		}
		if m.state != StateReady || m.index == nil {
			_ = m.rebuildFromStoreLocked() // storage already committed
			return nil
		}
		if existed {
			if err := m.index.Delete(key); err != nil {
				m.state = StateDirty
				return nil
			}
		}
		if err := m.index.Insert(key, normalized); err != nil {
			m.state = StateDirty
			return nil
		}
		m.store.DropVector(key)
		m.maybeRebuildLocked()
		return nil
	}
	return nil
}

func (m *Manager) maybeRebuildLocked() {
	if m.state == StateDirty {
		_ = m.rebuildFromStoreLocked()
		return
	}
	if m.state != StateReady || m.index == nil {
		return
	}
	if m.rebuildDeleteRatio <= 0 {
		return
	}
	n := m.store.Count()
	if n == 0 {
		return
	}
	if float64(m.mutationSinceBuild) >= float64(n)*m.rebuildDeleteRatio {
		_ = m.rebuildFromStoreLocked()
	}
}

// dropIndexLocked unpublishes the secondary index without Clear() so concurrent
// Search calls holding the old pointer can finish safely.
func (m *Manager) dropIndexLocked() {
	m.restoreBodiesLocked()
	m.index = nil
	m.state = StateDormant
	m.mutationSinceBuild = 0
}

// rebuildFromStoreLocked builds a fresh index from a consistent snapshot and swaps it in.
// The previous index is not Clear()'d so in-flight Search on the old pointer remains valid.
func (m *Manager) rebuildFromStoreLocked() error {
	if m.mode == ModeNone {
		return nil
	}
	if m.mode == ModeAuto && m.store.Count() < m.autoMin {
		m.dropIndexLocked()
		return nil
	}
	if m.newIndex == nil {
		m.state = StateDirty
		return fmt.Errorf("no index factory configured")
	}

	snap := m.snapshotLocked()
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	newIdx := m.newIndex()
	for _, key := range keys {
		if err := newIdx.Insert(key, snap[key]); err != nil {
			// Keep a still-usable index; only mark dirty if there was none.
			if m.index == nil || m.state != StateReady {
				m.state = StateDirty
			}
			return fmt.Errorf("rebuild insert %q: %w", key, err)
		}
	}
	// Atomic publish: re-point only; do not Clear the old index.
	m.index = newIdx
	m.state = StateReady
	m.mutationSinceBuild = 0
	if m.mode == ModeHNSW || m.mode == ModeAuto {
		for _, key := range keys {
			m.store.DropVector(key)
		}
	}
	return nil
}

func (m *Manager) fallbackSearch(query []float32, k int, idx storage.Index, allow func(string) bool) ([]vector.SearchResult, error) {
	if m.mode == ModeHNSW || m.mode == ModeAuto {
		return m.searchResolved(query, k, idx, allow)
	}
	return m.storeSearch(query, k, allow)
}

func (m *Manager) vecFor(key string, idx storage.Index) ([]float32, bool) {
	vec, ok := m.store.Get(key)
	if !ok {
		return nil, false
	}
	if vec != nil {
		return vec, true
	}
	if idx != nil {
		if v, ok := idx.Get(key); ok {
			return v, true
		}
	}
	return m.store.Get(key)
}

func (m *Manager) searchResolved(query []float32, k int, idx storage.Index, allow func(string) bool) ([]vector.SearchResult, error) {
	normalizedQuery, err := vector.Normalize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize query: %w", err)
	}
	h := &vector.TopKHeap{}
	heap.Init(h)
	for _, key := range m.store.GetAllKeys() {
		if allow != nil && !allow(key) {
			continue
		}
		vec, ok := m.vecFor(key, idx)
		if !ok {
			continue
		}
		similarity, err := vector.DotProduct(normalizedQuery, vec)
		if err != nil {
			return nil, err
		}
		if h.Len() < k {
			heap.Push(h, vector.SearchResult{Key: key, Similarity: similarity})
		} else if k > 0 && similarity > (*h)[0].Similarity {
			heap.Pop(h)
			heap.Push(h, vector.SearchResult{Key: key, Similarity: similarity})
		}
	}
	results := make([]vector.SearchResult, h.Len())
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(h).(vector.SearchResult)
	}
	return results, nil
}

func (m *Manager) snapshotLocked() map[string][]float32 {
	keys := m.store.GetAllKeys()
	out := make(map[string][]float32, len(keys))
	for _, key := range keys {
		vec, ok := m.vecFor(key, m.index)
		if !ok {
			continue
		}
		copied := make([]float32, len(vec))
		copy(copied, vec)
		out[key] = copied
	}
	return out
}

func (m *Manager) restoreBodiesLocked() {
	if m.index == nil {
		return
	}
	for _, key := range m.store.GetAllKeys() {
		v, ok := m.store.Get(key)
		if !ok || v != nil {
			continue
		}
		vec, ok := m.index.Get(key)
		if !ok {
			continue
		}
		_ = m.store.Set(key, vec)
	}
}
