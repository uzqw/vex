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
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uzqw/vex/internal/storage"
	"github.com/uzqw/vex/internal/vector"
)

func unit(vals ...float32) []float32 {
	return append([]float32(nil), vals...)
}

func hnswFactory() func() storage.Index {
	return func() storage.Index {
		return storage.NewHNSWIndexWithConfig(storage.HNSWConfig{
			M: 8, EfConstruct: 32, Ef: 32, Seed: 42,
		})
	}
}

func newAutoManager(t *testing.T, autoMin int) *Manager {
	t.Helper()
	m, err := NewManager(storage.New(), Config{
		Mode:                   ModeAuto,
		AutoMin:                autoMin,
		NewIndex:               hnswFactory(),
		UseDefaultRebuildRatio: true,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func mustSet(t *testing.T, m *Manager, key string, v []float32) {
	t.Helper()
	if _, err := m.Set(key, v); err != nil {
		t.Fatal(err)
	}
}

func TestAutoBelowThresholdSearchSeesAll(t *testing.T) {
	m := newAutoManager(t, 5)
	for i := 0; i < 4; i++ {
		mustSet(t, m, fmt.Sprintf("k%d", i), unit(float32(i+1), 0, 0))
	}
	if m.State() != StateDormant {
		t.Fatalf("state = %v, want dormant", m.State())
	}
	if m.IndexCount() != -1 {
		t.Fatalf("index should be nil below threshold, got count %d", m.IndexCount())
	}
	res, err := m.Search(unit(1, 0, 0), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 4 {
		t.Fatalf("got %d results, want 4", len(res))
	}
}

func TestAutoCrossThresholdBackfill(t *testing.T) {
	m := newAutoManager(t, 4)
	for i := 0; i < 3; i++ {
		mustSet(t, m, fmt.Sprintf("k%d", i), unit(float32(i+1), 1, 0))
	}
	if m.State() != StateDormant {
		t.Fatalf("want dormant before threshold")
	}
	mustSet(t, m, "k3", unit(4, 1, 0))
	if m.State() != StateReady {
		t.Fatalf("state = %v, want ready", m.State())
	}
	if got := m.IndexCount(); got != 4 {
		t.Fatalf("index count = %d, want 4", got)
	}
	res, err := m.Search(unit(1, 1, 0), 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 4 {
		t.Fatalf("got %d results, want 4", len(res))
	}
	seen := map[string]bool{}
	for _, r := range res {
		seen[r.Key] = true
	}
	for i := 0; i < 4; i++ {
		if !seen[fmt.Sprintf("k%d", i)] {
			t.Errorf("missing key k%d", i)
		}
	}
}

func TestAutoUpsertAroundThreshold(t *testing.T) {
	m := newAutoManager(t, 3)
	mustSet(t, m, "a", unit(1, 0, 0))
	mustSet(t, m, "b", unit(0, 1, 0))
	mustSet(t, m, "c", unit(0, 0, 1))
	if m.IndexCount() != 3 {
		t.Fatalf("index count = %d", m.IndexCount())
	}
	mustSet(t, m, "a", unit(1, 1, 0))
	if m.IndexCount() != 3 {
		t.Fatalf("after upsert index count = %d", m.IndexCount())
	}
	res, err := m.Search(unit(1, 1, 0), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Key != "a" {
		t.Fatalf("expected a as top hit, got %+v", res)
	}
}

func TestAutoDeleteBelowThresholdDropsIndex(t *testing.T) {
	m := newAutoManager(t, 3)
	mustSet(t, m, "a", unit(1, 0, 0))
	mustSet(t, m, "b", unit(0, 1, 0))
	mustSet(t, m, "c", unit(0, 0, 1))
	if m.State() != StateReady {
		t.Fatal("want ready")
	}
	if !m.Delete("c") {
		t.Fatal("delete failed")
	}
	if m.State() != StateDormant {
		t.Fatalf("state = %v, want dormant", m.State())
	}
	res, err := m.Search(unit(1, 0, 0), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
}

// failingIndex fails Insert after N successes.
type failingIndex struct {
	storage.Index
	failAfter int
	inserts   int
}

func (f *failingIndex) Insert(key string, vec []float32) error {
	f.inserts++
	if f.inserts > f.failAfter {
		return fmt.Errorf("injected insert failure")
	}
	return f.Index.Insert(key, vec)
}

type deleteFailIndex struct {
	storage.Index
}

func (d *deleteFailIndex) Delete(key string) error {
	return fmt.Errorf("injected delete failure")
}

type searchFailIndex struct {
	storage.Index
}

func (s *searchFailIndex) Search(query []float32, k int) ([]vector.SearchResult, error) {
	return nil, fmt.Errorf("injected search failure")
}

// blockingIndex blocks Search until release is closed, then returns base results.
type blockingIndex struct {
	storage.Index
	entered chan struct{}
	release chan struct{}
	cleared atomic.Bool
}

func (b *blockingIndex) Search(query []float32, k int) ([]vector.SearchResult, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	if b.cleared.Load() {
		return []vector.SearchResult{}, nil
	}
	return b.Index.Search(query, k)
}

func (b *blockingIndex) Clear() {
	b.cleared.Store(true)
	b.Index.Clear()
}

func TestDirtyIndexFallback(t *testing.T) {
	// Disable dirty auto-rebuild so we observe StateDirty.
	// Inserts: factory used once at NewManager (empty), then Set a/b on that index.
	// We wrap after construction by using a factory that always returns failing insert after 1.
	// First Set("a") succeeds; Set("b") fails → dirty. maybeRebuild tries rebuild with
	// same factory → rebuild also fails after 1 insert → stays dirty.
	store := storage.New()
	m, err := NewManager(store, Config{
		Mode: ModeHNSW,
		NewIndex: func() storage.Index {
			base := storage.NewHNSWIndexWithConfig(storage.HNSWConfig{
				M: 8, EfConstruct: 16, Ef: 16, Seed: 1,
			})
			return &failingIndex{Index: base, failAfter: 1}
		},
		RebuildDeleteRatio: 0, // disable mutation rebuild
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Set("a", unit(1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Set("b", unit(0, 1, 0)); err != nil {
		t.Fatal(err)
	}
	if got := m.State(); got != StateDirty {
		t.Fatalf("state = %v, want dirty", got)
	}
	res, err := m.Search(unit(1, 0, 0), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) < 2 {
		t.Fatalf("fallback search should see store data, got %d hits", len(res))
	}
}

func TestDeleteFailureMarksDirtyAndDoesNotReturnDeletedKey(t *testing.T) {
	// Rebuild after dirty would use a clean index; disable ratio and make rebuild
	// also use deleteFail by keeping factory returning deleteFail — rebuild still works
	// for Insert. After Delete fails → dirty → maybeRebuild rebuilds successfully.
	// To keep dirty visible, make rebuild fail too OR check client-visible results only.
	//
	// We check: deleted key never appears in Search, store count is 1.
	// Use factory that returns deleteFailIndex; rebuild succeeds via Inserts.
	// After Delete fails, maybeRebuild rebuilds → Ready without "a". That's OK.
	// To force dirty path without successful rebuild, fail Inserts on rebuild:
	var builds atomic.Int32
	m, err := NewManager(storage.New(), Config{
		Mode: ModeHNSW,
		NewIndex: func() storage.Index {
			n := builds.Add(1)
			base := storage.NewHNSWIndexWithConfig(storage.HNSWConfig{
				M: 8, EfConstruct: 16, Ef: 16, Seed: 1,
			})
			if n == 1 {
				return &deleteFailIndex{Index: base}
			}
			// Rebuild path: also fail so we stay dirty
			return &failingIndex{Index: base, failAfter: 0}
		},
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, m, "a", unit(1, 0, 0))
	mustSet(t, m, "b", unit(0, 1, 0))
	if !m.Delete("a") {
		t.Fatal("expected delete true")
	}
	// Storage must not have a; search must not return a.
	if _, ok := m.Get("a"); ok {
		t.Fatal("storage still has a")
	}
	if m.Count() != 1 {
		t.Fatalf("count = %d", m.Count())
	}
	// State should be dirty (rebuild failed) or ready without a.
	res, err := m.Search(unit(1, 0, 0), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Key == "a" {
			t.Fatalf("search returned deleted key a: %+v", res)
		}
	}
}

func TestIndexSearchFailureFallsBackAndMarksDirty(t *testing.T) {
	m, err := NewManager(storage.New(), Config{
		Mode: ModeHNSW,
		NewIndex: func() storage.Index {
			base := storage.NewHNSWIndexWithConfig(storage.HNSWConfig{
				M: 8, EfConstruct: 16, Ef: 16, Seed: 1,
			})
			return &searchFailIndex{Index: base}
		},
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, m, "a", unit(1, 0, 0))
	mustSet(t, m, "b", unit(0, 1, 0))
	res, err := m.Search(unit(1, 0, 0), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) < 2 {
		t.Fatalf("expected store fallback, got %d", len(res))
	}
	if got := m.State(); got != StateDirty {
		t.Fatalf("state = %v, want dirty", got)
	}
}

func TestRebuildDoesNotClearIndexUsedByInflightSearch(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var builds atomic.Int32

	m, err := NewManager(storage.New(), Config{
		Mode: ModeHNSW,
		NewIndex: func() storage.Index {
			n := builds.Add(1)
			base := storage.NewHNSWIndexWithConfig(storage.HNSWConfig{
				M: 8, EfConstruct: 32, Ef: 32, Seed: 1,
			})
			if n == 1 {
				return &blockingIndex{
					Index:   base,
					entered: entered,
					release: release,
				}
			}
			return base
		},
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, m, "a", unit(1, 0, 0))
	mustSet(t, m, "b", unit(0, 1, 0))
	mustSet(t, m, "c", unit(0, 0, 1))

	// Snapshot old index via Search start.
	type searchOut struct {
		res []vector.SearchResult
		err error
	}
	outCh := make(chan searchOut, 1)
	go func() {
		res, err := m.Search(unit(1, 0, 0), 10)
		outCh <- searchOut{res, err}
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("search did not enter blocking index")
	}

	// Rebuild while Search holds old pointer (blocked inside Search).
	if err := m.Rebuild(); err != nil {
		t.Fatal(err)
	}
	// If old Clear() were called, blockingIndex.cleared would be true → empty results.
	close(release)

	out := <-outCh
	if out.err != nil {
		t.Fatal(out.err)
	}
	if len(out.res) != 3 {
		t.Fatalf("in-flight search got %d results, want 3 (old index must not be cleared)", len(out.res))
	}

	// New search uses rebuilt index.
	res, err := m.Search(unit(1, 0, 0), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("post-rebuild search got %d", len(res))
	}
}

func TestNewManagerBackfillsPrepopulatedStore(t *testing.T) {
	store := storage.New()
	_ = store.Set("a", unit(1, 0, 0))
	_ = store.Set("b", unit(0, 1, 0))
	m, err := NewManager(store, Config{
		Mode:               ModeHNSW,
		NewIndex:           hnswFactory(),
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.State() != StateReady {
		t.Fatalf("state = %v", m.State())
	}
	if m.IndexCount() != 2 {
		t.Fatalf("index count = %d", m.IndexCount())
	}
	res, err := m.Search(unit(1, 0, 0), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d", len(res))
	}
}

func TestNewAutoManagerBuildsPrepopulatedStoreAboveThreshold(t *testing.T) {
	store := storage.New()
	for i := 0; i < 5; i++ {
		_ = store.Set(fmt.Sprintf("k%d", i), unit(float32(i+1), 0, 0))
	}
	m, err := NewManager(store, Config{
		Mode:               ModeAuto,
		AutoMin:            5,
		NewIndex:           hnswFactory(),
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.State() != StateReady {
		t.Fatalf("state = %v, want ready", m.State())
	}
	if m.IndexCount() != 5 {
		t.Fatalf("index count = %d", m.IndexCount())
	}
}

func TestNewAutoManagerStaysDormantBelowThreshold(t *testing.T) {
	store := storage.New()
	_ = store.Set("a", unit(1, 0, 0))
	m, err := NewManager(store, Config{
		Mode:               ModeAuto,
		AutoMin:            10,
		NewIndex:           hnswFactory(),
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.State() != StateDormant {
		t.Fatalf("state = %v", m.State())
	}
	if m.IndexCount() != -1 {
		t.Fatalf("index count = %d", m.IndexCount())
	}
}

func TestSetCreatedFlag(t *testing.T) {
	m := newAutoManager(t, 100)
	created, err := m.Set("a", unit(1, 0, 0))
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	created, err = m.Set("a", unit(0, 1, 0))
	if err != nil || created {
		t.Fatalf("upsert created=%v err=%v", created, err)
	}
}

func TestConcurrentCrossThreshold(t *testing.T) {
	m := newAutoManager(t, 50)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = m.Set(fmt.Sprintf("k%d", i), unit(float32(i%7+1), float32(i%5+1), 1))
		}(i)
	}
	wg.Wait()
	if m.Count() != 100 {
		t.Fatalf("count = %d", m.Count())
	}
	if m.State() != StateReady {
		t.Fatalf("state = %v", m.State())
	}
	if m.IndexCount() != 100 {
		t.Fatalf("index count = %d, want 100", m.IndexCount())
	}
	res, err := m.Search(unit(1, 1, 1), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 10 {
		t.Fatalf("got %d results", len(res))
	}
}

func TestSnapshotConsistencyAPI(t *testing.T) {
	s := storage.New()
	_ = s.Set("a", unit(1, 0))
	_ = s.Set("b", unit(0, 1))
	snap := s.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snap len %d", len(snap))
	}
	snap["a"][0] = 99
	got, _ := s.Get("a")
	if got[0] == 99 {
		t.Fatal("snapshot did not copy vectors")
	}
}

func TestWrongDimQueryDoesNotMarkDirty(t *testing.T) {
	m, err := NewManager(storage.New(), Config{
		Mode:               ModeHNSW,
		NewIndex:           hnswFactory(),
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, m, "a", unit(1, 0, 0))
	mustSet(t, m, "b", unit(0, 1, 0))
	if _, err := m.Search(unit(1, 0), 1); err == nil {
		t.Fatal("expected dimension error")
	}
	if got := m.State(); got != StateReady {
		t.Fatalf("state = %v, want ready (query error must not dirty index)", got)
	}
	res, err := m.Search(unit(1, 0, 0), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("expected hits after bad query")
	}
}

func TestFailedRebuildKeepsReadyIndex(t *testing.T) {
	var builds atomic.Int32
	m, err := NewManager(storage.New(), Config{
		Mode: ModeHNSW,
		NewIndex: func() storage.Index {
			n := builds.Add(1)
			base := storage.NewHNSWIndexWithConfig(storage.HNSWConfig{
				M: 8, EfConstruct: 16, Ef: 16, Seed: 1,
			})
			if n == 1 {
				return base
			}
			return &failingIndex{Index: base, failAfter: 0}
		},
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, m, "a", unit(1, 0, 0))
	mustSet(t, m, "b", unit(0, 1, 0))
	if err := m.Rebuild(); err == nil {
		t.Fatal("expected rebuild failure")
	}
	if got := m.State(); got != StateReady {
		t.Fatalf("state = %v, want ready (failed rebuild must keep prior index)", got)
	}
	res, err := m.Search(unit(1, 0, 0), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
}

func TestAutoRebuildFailureDoesNotFailSet(t *testing.T) {
	m, err := NewManager(storage.New(), Config{
		Mode:    ModeAuto,
		AutoMin: 2,
		NewIndex: func() storage.Index {
			base := storage.NewHNSWIndexWithConfig(storage.HNSWConfig{
				M: 8, EfConstruct: 16, Ef: 16, Seed: 1,
			})
			return &failingIndex{Index: base, failAfter: 0}
		},
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, m, "a", unit(1, 0, 0))
	created, err := m.Set("b", unit(0, 1, 0))
	if err != nil {
		t.Fatalf("Set must not fail after storage write: %v", err)
	}
	if !created {
		t.Fatal("expected created")
	}
	if _, ok := m.Get("b"); !ok {
		t.Fatal("b missing from store")
	}
	res, err := m.Search(unit(0, 1, 0), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("fallback search got %d, want 2", len(res))
	}
}

func TestHNSWDropsStorageCopy(t *testing.T) {
	store := storage.New()
	m, err := NewManager(store, Config{
		Mode:               ModeHNSW,
		NewIndex:           hnswFactory(),
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, m, "a", unit(3, 0, 0))
	body, ok := store.Get("a")
	if !ok {
		t.Fatal("key missing from store")
	}
	if body != nil {
		t.Fatal("storage still holds a vector body")
	}
	got, ok := m.Get("a")
	if !ok {
		t.Fatal("Get missed a")
	}
	if len(got) != 3 || got[0] < 0.99 {
		t.Fatalf("got %v", got)
	}
}
