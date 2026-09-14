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

	// rebuildGen increments whenever the published index is dropped or
	// cleared (dropIndexLocked, Clear); an in-flight rebuild whose captured
	// gen no longer matches abandons its swap instead of publishing a
	// stale index.
	rebuildGen int
	// rebuilding is true while a background rebuild builds a private index.
	// rebuildPending collects keys mutated during that build; the swap folds
	// them into the new index so it is consistent at publish time.
	rebuilding     bool
	rebuildPending map[string]struct{}

	// snapActive is true while SnapshotVectors streams phase 1. Mutators
	// then record every mutated key in snapMutated so the snapshot can
	// re-read those keys at its end barrier and emit an internally
	// consistent view without ever holding the lock for the whole copy.
	snapActive  bool
	snapMutated map[string]struct{}

	// writeMu serializes the full writer sequence — store write through
	// secondary-index sync — so index mutations land in the same order as
	// their store writes (a same-key upsert can never leave the index with
	// the previous value) and Delete cannot interleave with an in-flight
	// Set's index insert. Writers therefore serialize on the HNSW
	// single-writer ceiling, but the manager lock is free for readers
	// while an insert runs. Always acquire writeMu before mu.
	writeMu sync.Mutex
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
			if err := m.Rebuild(); err != nil {
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
			if err := m.Rebuild(); err != nil {
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

// Set upserts a vector. Storage is written first under a brief manager
// lock, then the expensive secondary-index insert runs WITHOUT the
// manager lock (writeMu keeps writer order stable) and a second brief
// lock pass drops the storage body and services rebuild bookkeeping.
// Index failures mark dirty and do not roll back storage. created is
// true when the key did not exist.
func (m *Manager) Set(key string, values []float32) (created bool, err error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	// Phase 1: commit to storage and log the mutation under the manager
	// lock, then capture the index-sync decision and release so readers
	// never wait on the insert that follows.
	m.mu.Lock()
	_, existed := m.store.Get(key)
	if err := m.store.Set(key, values); err != nil {
		m.mu.Unlock()
		return false, err
	}
	if m.snapActive {
		m.snapMutated[key] = struct{}{}
	}
	if m.rebuilding {
		// Fold this mutation into the in-flight rebuild at its swap.
		m.rebuildPending[key] = struct{}{}
	}
	normalized, ok := m.store.Get(key)
	if !ok {
		m.mu.Unlock()
		return false, fmt.Errorf("failed to read normalized vector after set")
	}
	if existed {
		m.mutationSinceBuild++
	}
	idx, doInsert := m.setSyncPlanLocked()
	m.mu.Unlock()

	if !doInsert {
		return !existed, nil
	}

	// Phase 2: index work without the manager lock. idx stays safe to
	// mutate because published indexes are only mutated by writeMu
	// holders (Set, Delete); rebuild swaps only re-point m.index.
	if existed {
		if err := idx.Delete(key); err != nil {
			m.mu.Lock()
			if m.index == idx {
				m.state = StateDirty
			}
			m.mu.Unlock()
			return !existed, nil
		}
	}
	insertErr := idx.Insert(key, normalized)

	// Phase 3: publish bookkeeping under a brief lock. If idx was replaced
	// while the insert ran (rebuild swap, auto-shrink drop, Clear), the
	// fold/swap machinery already applied this mutation to the new index
	// and this pass must not touch state or bodies.
	m.mu.Lock()
	if m.index == idx {
		if insertErr != nil {
			m.state = StateDirty
		} else {
			if m.mode == ModeHNSW || m.mode == ModeAuto {
				// The published index now holds the body; drop the storage copy.
				m.store.DropVector(key)
			}
			m.maybeRebuildLocked()
		}
	}
	m.mu.Unlock()
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

// Delete removes a key from storage and the secondary index. It holds
// writeMu so its store+index delete cannot interleave with an in-flight
// Set's index insert on the same key.
func (m *Manager) Delete(key string) bool {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	deleted := m.store.Delete(key)
	if !deleted {
		return false
	}
	if m.snapActive {
		m.snapMutated[key] = struct{}{}
	}
	if m.rebuilding {
		m.rebuildPending[key] = struct{}{}
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
			return m.searchResolved(query, k, allow)
		}
		return m.storeSearch(query, k, allow)
	}

	normalized, err := vector.Normalize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize query: %w", err)
	}
	results, err := idx.SearchWhere(normalized, k, allow)
	if err != nil {
		fallback, ferr := m.fallbackSearch(query, k, allow)
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
	if allow == nil && (m.mode == ModeNone || m.mode == ModeBruteForce) {
		// These modes never drop storage bodies into the index, so the raw
		// storage scan is safe. HNSW/Auto must resolve through vecForLive:
		// a rebuild swap may drop bodies while this scan runs.
		return m.store.Search(query, k)
	}
	normalizedQuery, err := vector.Normalize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize query: %w", err)
	}
	h := &vector.TopKHeap{}
	heap.Init(h)
	var scanErr error
	m.store.Scan(func(key string) {
		if scanErr != nil || (allow != nil && !allow(key)) {
			return
		}
		vec, ok := m.vecForLive(key)
		if !ok {
			return
		}
		similarity, err := vector.DotProduct(normalizedQuery, vec)
		if err != nil {
			scanErr = err
			return
		}
		if h.Len() < k {
			heap.Push(h, vector.SearchResult{Key: key, Similarity: similarity})
		} else if similarity > (*h)[0].Similarity {
			heap.Pop(h)
			heap.Push(h, vector.SearchResult{Key: key, Similarity: similarity})
		}
	})
	if scanErr != nil {
		return nil, scanErr
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
	if m.snapActive {
		// Log every key so an in-flight snapshot's end barrier drops them
		// all instead of resurrecting pre-Clear vectors.
		for _, key := range m.store.GetAllKeys() {
			m.snapMutated[key] = struct{}{}
		}
	}
	m.rebuildGen++
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

// SnapshotVectors streams an internally consistent snapshot of every
// vector without materializing the whole library or holding the manager
// lock for the copy. fn(key, vec) is called for every vector; a key mutated
// while the stream runs may be emitted more than once, with later
// emissions superseding earlier ones. deleted(key) is called for keys
// removed while the stream ran and supersedes any earlier emission of
// that key. The emitted state equals the library state at the snapshot's
// end barrier: phase 1 streams the start key set without the manager
// lock, mutations are logged meanwhile, and a brief write-locked barrier
// re-reads only the mutated keys. Writes therefore stall only for that
// bounded pass, never for the full-library copy. vec is only valid for
// the duration of the call. Returns the number of distinct keys emitted.
func (m *Manager) SnapshotVectors(fn func(key string, vec []float32) error, deleted func(key string) error) (int, error) {
	m.mu.Lock()
	if m.snapActive {
		m.mu.Unlock()
		return 0, fmt.Errorf("snapshot already in progress")
	}
	m.snapActive = true
	m.snapMutated = make(map[string]struct{})
	keys := m.store.GetAllKeys()
	m.mu.Unlock()

	for _, key := range keys {
		vec, ok := m.vecForLive(key)
		if !ok {
			continue
		}
		if err := fn(key, vec); err != nil {
			m.endSnapshot()
			return 0, err
		}
	}

	// End barrier: stop mutation logging and re-read every mutated key
	// under one brief write lock, so the emitted state equals the store
	// at this point. Corrections are copied and delivered after releasing
	// the lock so disk I/O in fn never blocks writers.
	m.mu.Lock()
	m.snapActive = false
	mutated := m.snapMutated
	m.snapMutated = nil
	count := m.store.Count()
	type correction struct {
		key string
		vec []float32
	}
	corrections := make([]correction, 0, len(mutated))
	var removed []string
	for key := range mutated {
		if vec, ok := m.vecFor(key, m.index); ok {
			copied := make([]float32, len(vec))
			copy(copied, vec)
			corrections = append(corrections, correction{key, copied})
		} else {
			removed = append(removed, key)
		}
	}
	m.mu.Unlock()

	for _, c := range corrections {
		if err := fn(c.key, c.vec); err != nil {
			return 0, err
		}
	}
	for _, key := range removed {
		if err := deleted(key); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// endSnapshot aborts an in-flight streaming snapshot barrier.
func (m *Manager) endSnapshot() {
	m.mu.Lock()
	m.snapActive = false
	m.snapMutated = nil
	m.mu.Unlock()
}

// GetAllVectors returns copies of every vector, resolving bodies that
// HNSW/Auto modes keep only in the packed index. Persistence snapshots
// must go through this rather than reading Storage directly.
func (m *Manager) GetAllVectors() (map[string][]float32, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotLocked(), nil
}

// SetAllVectors bulk-restores vectors. Used by persistence recovery.
func (m *Manager) SetAllVectors(vectors map[string][]float32) error {
	return m.SetAllVectorsWithMetadata(vectors, nil)
}

// SetAllVectorsWithMetadata restores vectors and their scalar metadata.
// Implements persistence.MetadataDataSource.
//
// Bulk path: the whole store write commits under one m.mu hold, then the
// index is rebuilt once via the leg-1 background-rebuild machinery
// instead of a full Set (three lock round-trips plus an index insert)
// per vector. Recovery runs before the server accepts traffic, so
// snapActive/rebuilding cannot be true; the guards below keep that
// assumption honest if a future caller restores into a live manager.
func (m *Manager) SetAllVectorsWithMetadata(vectors map[string][]float32, meta map[string]map[string]string) error {
	if len(vectors) == 0 {
		return nil
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	// Commit every store write under one manager lock.
	m.mu.Lock()
	for key, vec := range vectors {
		if _, existed := m.store.Get(key); existed {
			m.mutationSinceBuild++
		}
		if err := m.store.Set(key, vec); err != nil {
			m.mu.Unlock()
			return fmt.Errorf("failed to set vector %s: %w", key, err)
		}
		if m.snapActive {
			m.snapMutated[key] = struct{}{}
		}
		if m.rebuilding {
			m.rebuildPending[key] = struct{}{}
		}
		if fields, ok := meta[key]; ok {
			if err := m.store.SetMetadata(key, fields); err != nil {
				m.mu.Unlock()
				return fmt.Errorf("failed to set metadata %s: %w", key, err)
			}
		}
	}
	m.mu.Unlock()

	// Index work: one synchronous rebuild covers the whole just-committed
	// store in a single deterministic pass — cheaper than N index inserts
	// and keeps HNSW build order sorted. Rebuild() is a no-op for ModeNone
	// and drops the index for Auto below threshold, so it needs no
	// per-mode branching here. It returns with a searchable index,
	// matching the old per-vector Set behavior.
	return m.Rebuild()
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

// Rebuild rebuilds the secondary index from a consistent storage snapshot.
// The key set is captured under a brief lock and the new index is built by
// streaming vectors one by one without holding the manager lock — reads
// and writes keep running on the old index — and the result is swapped in
// atomically. Mutations issued while the build runs are folded in before
// the swap. Returns an error if a rebuild is already in progress.
func (m *Manager) Rebuild() error {
	m.mu.Lock()
	if m.mode == ModeNone {
		m.mu.Unlock()
		return nil
	}
	if m.rebuilding {
		m.mu.Unlock()
		return fmt.Errorf("rebuild already in progress")
	}
	if m.mode == ModeAuto && m.store.Count() < m.autoMin {
		m.dropIndexLocked()
		m.mu.Unlock()
		return nil
	}
	if m.newIndex == nil {
		m.state = StateDirty
		m.mu.Unlock()
		return fmt.Errorf("no index factory configured")
	}
	m.rebuilding = true
	m.rebuildPending = make(map[string]struct{})
	run := &rebuildRun{
		gen:     m.rebuildGen,
		keys:    m.sortedKeysLocked(),
		pending: m.rebuildPending,
	}
	m.mu.Unlock()
	return m.runRebuild(run)
}

// setSyncPlanLocked captures, while the caller holds m.mu, what index
// work Set must do on the returned index after releasing the manager
// lock. doInsert is false when the mutation path already handled the
// index (rebuild started, dormant auto, or no index mode); then no
// per-key work remains.
func (m *Manager) setSyncPlanLocked() (idx storage.Index, doInsert bool) {
	switch m.mode {
	case ModeNone:
		return nil, false

	case ModeBruteForce, ModeHNSW:
		if m.state != StateReady || m.index == nil {
			m.startRebuildLocked() // storage already committed
			return nil, false
		}
		return m.index, true

	case ModeAuto:
		if m.store.Count() < m.autoMin {
			if m.index != nil {
				m.dropIndexLocked()
			}
			return nil, false
		}
		if m.state != StateReady || m.index == nil {
			m.startRebuildLocked() // storage already committed
			return nil, false
		}
		return m.index, true
	}
	return nil, false
}

func (m *Manager) maybeRebuildLocked() {
	if m.rebuilding {
		// An in-flight rebuild publishes a consistent index at its swap;
		// mutations are folded in via rebuildPending.
		return
	}
	if m.state == StateDirty {
		m.startRebuildLocked()
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
		m.startRebuildLocked()
	}
}

// dropIndexLocked unpublishes the secondary index without Clear() so concurrent
// Search calls holding the old pointer can finish safely.
func (m *Manager) dropIndexLocked() {
	m.rebuildGen++
	m.restoreBodiesLocked()
	m.index = nil
	m.state = StateDormant
	m.mutationSinceBuild = 0
}

// rebuildRun carries one rebuild's inputs from its start to its swap.
type rebuildRun struct {
	// gen is rebuildGen at start; a mismatch means the index was dropped
	// or cleared during the build and the swap must be abandoned.
	gen int
	// keys is the sorted key set captured when the rebuild started. Bodies
	// are streamed from the live store/index during the build instead of
	// copying the whole library under the lock.
	keys []string
	// pending holds keys mutated while the build runs (guarded by m.mu).
	pending map[string]struct{}
}

// sortedKeysLocked returns all store keys in deterministic order so HNSW
// construction is reproducible. Caller holds m.mu.
func (m *Manager) sortedKeysLocked() []string {
	keys := m.store.GetAllKeys()
	sort.Strings(keys)
	return keys
}

// startRebuildLocked captures the storage key set under the already-held
// write lock and launches the index build in the background, so reads and
// writes keep running on the old index while the new one is built. Caller
// holds m.mu.
func (m *Manager) startRebuildLocked() {
	if m.mode == ModeNone || m.rebuilding {
		return
	}
	if m.mode == ModeAuto && m.store.Count() < m.autoMin {
		m.dropIndexLocked()
		return
	}
	if m.newIndex == nil {
		m.state = StateDirty
		return
	}
	m.rebuilding = true
	m.rebuildPending = make(map[string]struct{})
	run := &rebuildRun{
		gen:     m.rebuildGen,
		keys:    m.sortedKeysLocked(),
		pending: m.rebuildPending,
	}
	go func() { _ = m.runRebuild(run) }()
}

// runRebuild streams the captured key set into a private index without
// holding the manager lock, then briefly takes the write lock to fold
// mutations that happened during the build into the new index and publish
// the swap. The previous index is not Clear()'d so in-flight Search on the
// old pointer remains valid. Called synchronously by Rebuild, in the
// background by startRebuildLocked.
func (m *Manager) runRebuild(run *rebuildRun) error {
	newIdx := m.newIndex()
	// Presize the packed vector array when the count is known; the store
	// dimension is already fixed for any non-empty rebuild.
	if p, ok := newIdx.(interface{ Presize(n, dim int) }); ok {
		p.Presize(len(run.keys), m.store.Dimension())
	}
	for _, key := range run.keys {
		vec, ok := m.vecForLive(key)
		if !ok {
			continue // deleted while building; the swap fold drops it
		}
		if err := newIdx.Insert(key, vec); err != nil {
			m.mu.Lock()
			m.rebuildFailedLocked()
			m.mu.Unlock()
			return fmt.Errorf("rebuild insert %q: %w", key, err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rebuildGen != run.gen {
		// The index was dropped or cleared while building (auto shrink,
		// Clear); the snapshot is stale, so abandon the swap.
		m.rebuilding = false
		m.rebuildPending = nil
		return nil
	}
	for key := range run.pending {
		if vec, ok := m.vecFor(key, m.index); ok {
			_ = newIdx.Delete(key) // upsert: replace any snapshot copy
			if err := newIdx.Insert(key, vec); err != nil {
				m.rebuildFailedLocked()
				return fmt.Errorf("rebuild insert %q: %w", key, err)
			}
		} else {
			_ = newIdx.Delete(key)
		}
	}
	// Atomic publish: re-point only; do not Clear the old index.
	m.index = newIdx
	m.state = StateReady
	m.mutationSinceBuild = 0
	m.rebuilding = false
	m.rebuildPending = nil
	if m.mode == ModeHNSW || m.mode == ModeAuto {
		for _, key := range run.keys {
			m.store.DropVector(key)
		}
		for key := range run.pending {
			m.store.DropVector(key)
		}
	}
	return nil
}

// rebuildFailedLocked discards a failed rebuild and keeps the currently
// published index usable; only marks dirty when there was none ready.
// Caller holds m.mu.
func (m *Manager) rebuildFailedLocked() {
	if m.index == nil || m.state != StateReady {
		m.state = StateDirty
	}
	m.rebuilding = false
	m.rebuildPending = nil
}

func (m *Manager) fallbackSearch(query []float32, k int, allow func(string) bool) ([]vector.SearchResult, error) {
	if m.mode == ModeHNSW || m.mode == ModeAuto {
		return m.searchResolved(query, k, allow)
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

// vecForLive resolves a vector body for lock-free scans (storeSearch,
// searchResolved). A rebuild swap publishes the new index before dropping
// storage bodies, both under the write lock, so a scan that observes a nil
// body must resolve it through the live published index: taking the read
// lock after seeing nil observes either the pre-swap state or the
// already-published replacement.
func (m *Manager) vecForLive(key string) ([]float32, bool) {
	vec, ok := m.store.Get(key)
	if !ok {
		return nil, false
	}
	if vec != nil {
		return vec, true
	}
	m.mu.RLock()
	idx := m.index
	m.mu.RUnlock()
	if idx != nil {
		if v, ok := idx.Get(key); ok {
			return v, true
		}
	}
	return nil, false
}

func (m *Manager) searchResolved(query []float32, k int, allow func(string) bool) ([]vector.SearchResult, error) {
	normalizedQuery, err := vector.Normalize(query)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize query: %w", err)
	}
	h := &vector.TopKHeap{}
	heap.Init(h)
	var scanErr error
	m.store.Scan(func(key string) {
		if scanErr != nil || (allow != nil && !allow(key)) {
			return
		}
		vec, ok := m.vecForLive(key)
		if !ok {
			return
		}
		similarity, err := vector.DotProduct(normalizedQuery, vec)
		if err != nil {
			scanErr = err
			return
		}
		if h.Len() < k {
			heap.Push(h, vector.SearchResult{Key: key, Similarity: similarity})
		} else if k > 0 && similarity > (*h)[0].Similarity {
			heap.Pop(h)
			heap.Push(h, vector.SearchResult{Key: key, Similarity: similarity})
		}
	})
	if scanErr != nil {
		return nil, scanErr
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
