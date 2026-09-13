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

// gatedIndex signals on its first Insert and blocks every Insert until
// release is closed, holding a rebuild deterministically in progress.
type gatedIndex struct {
	storage.Index
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (g *gatedIndex) Insert(key string, vec []float32) error {
	g.once.Do(func() { close(g.entered) })
	<-g.release
	return g.Index.Insert(key, vec)
}

// waitEventually polls cond until it holds or the timeout passes.
// Rebuilds triggered by mutations run in the background, so tests that
// need the rebuilt index poll instead of assuming synchronous completion.
func waitEventually(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
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
	waitEventually(t, 5*time.Second, "index ready after threshold crossing", func() bool {
		return m.State() == StateReady && m.IndexCount() == 4
	})
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
	waitEventually(t, 5*time.Second, "index ready after threshold crossing", func() bool {
		return m.State() == StateReady && m.IndexCount() == 3
	})
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
	waitEventually(t, 5*time.Second, "index ready after threshold crossing", func() bool {
		return m.State() == StateReady
	})
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

func (s *searchFailIndex) SearchWhere(query []float32, k int, allow func(string) bool) ([]vector.SearchResult, error) {
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
	return b.SearchWhere(query, k, nil)
}

func (b *blockingIndex) SearchWhere(query []float32, k int, allow func(string) bool) ([]vector.SearchResult, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	if b.cleared.Load() {
		return []vector.SearchResult{}, nil
	}
	return b.Index.SearchWhere(query, k, allow)
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

// TestSearchAndWriteProgressDuringRebuild is the acceptance check for the
// rebuild lock-scope fix: a churn-triggered rebuild builds its new index
// outside the manager write lock, so searches and writes keep completing
// while the rebuild is in progress, and mutations issued during the build
// are folded into the new index before it is published.
func TestSearchAndWriteProgressDuringRebuild(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var builds atomic.Int32

	m, err := NewManager(storage.New(), Config{
		Mode: ModeHNSW,
		NewIndex: func() storage.Index {
			n := builds.Add(1)
			base := storage.NewHNSWIndexWithConfig(storage.HNSWConfig{
				M: 8, EfConstruct: 32, Ef: 32, Seed: 1,
			})
			if n == 2 {
				return &gatedIndex{Index: base, entered: entered, release: release}
			}
			return base
		},
		RebuildDeleteRatio: 0.1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 50 vectors on the x-axis.
	for i := 0; i < 50; i++ {
		mustSet(t, m, fmt.Sprintf("k%02d", i), unit(float32(i+1), 0, 0))
	}
	// Upsert churn: 6 upserts >= 0.1*50 triggers a rebuild on the last one.
	for i := 0; i < 6; i++ {
		mustSet(t, m, fmt.Sprintf("k%02d", i), unit(0, 1, 0))
	}

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("rebuild did not start")
	}

	// The rebuild is now blocked mid-build. Searches and writes must still
	// complete instead of stalling for the whole rebuild.
	type opResult struct {
		err error
	}
	done := make(chan opResult, 2)
	go func() {
		res, err := m.Search(unit(0, 1, 0), 6)
		if err == nil {
			got := map[string]bool{}
			for _, r := range res {
				got[r.Key] = true
			}
			for i := 0; i < 6; i++ {
				if !got[fmt.Sprintf("k%02d", i)] {
					err = fmt.Errorf("search missed upserted key k%02d: %v", i, res)
					break
				}
			}
		}
		done <- opResult{err}
	}()
	go func() {
		_, err := m.Set("during", unit(0, 0, 1))
		done <- opResult{err}
	}()

	for i := 0; i < 2; i++ {
		select {
		case r := <-done:
			if r.err != nil {
				t.Fatal(r.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("search/write stalled while rebuild in progress")
		}
	}

	// Let the rebuild finish and publish, then verify the mutation issued
	// during the build is visible through the new index.
	close(release)
	waitEventually(t, 5*time.Second, "rebuilt index with 51 entries", func() bool {
		return m.State() == StateReady && m.IndexCount() == 51
	})

	res, err := m.Search(unit(0, 0, 1), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Key != "during" {
		t.Fatalf("key set during rebuild missing from new index: %v", res)
	}

	res, err = m.Search(unit(0, 1, 0), 6)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range res {
		got[r.Key] = true
	}
	for i := 0; i < 6; i++ {
		if !got[fmt.Sprintf("k%02d", i)] {
			t.Fatalf("upserted key k%02d missing after rebuild: %v", i, res)
		}
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
	waitEventually(t, 10*time.Second, "index ready with all keys", func() bool {
		return m.State() == StateReady && m.IndexCount() == 100
	})
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

func TestGetAllVectorsResolvesDroppedBodies(t *testing.T) {
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
	mustSet(t, m, "b", unit(0, 4, 0))

	// HNSW mode drops store bodies; a naive store read would snapshot nils.
	if body, _ := store.Get("a"); body != nil {
		t.Fatal("expected dropped store body")
	}

	vecs, err := m.GetAllVectors()
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vecs))
	}
	if len(vecs["a"]) != 3 || vecs["a"][0] < 0.99 {
		t.Fatalf("a = %v, want ~[1 0 0]", vecs["a"])
	}
	if len(vecs["b"]) != 3 || vecs["b"][1] < 0.99 {
		t.Fatalf("b = %v, want ~[0 1 0]", vecs["b"])
	}

	// Snapshot copies must not alias index internals.
	vecs["a"][0] = 42
	got, _ := m.Get("a")
	if got[0] == 42 {
		t.Fatal("GetAllVectors aliased internal vector")
	}
}

func TestSetAllVectorsRestoresAndIndexes(t *testing.T) {
	m, err := NewManager(storage.New(), Config{
		Mode:               ModeHNSW,
		NewIndex:           hnswFactory(),
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = m.SetAllVectors(map[string][]float32{
		"a": unit(1, 0, 0),
		"b": unit(0, 1, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Count() != 2 {
		t.Fatalf("count = %d, want 2", m.Count())
	}
	res, err := m.Search(unit(0, 1, 0), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Key != "b" {
		t.Fatalf("search = %v, want b", res)
	}
	if err := m.SetAllVectors(map[string][]float32{"bad": unit(1, 2)}); err == nil {
		t.Fatal("expected dimension error for mismatched vector")
	}
}

func TestSearchFilteredEquality(t *testing.T) {
	for _, mode := range []Mode{ModeNone, ModeBruteForce, ModeHNSW, ModeAuto} {
		m, err := NewManager(storage.New(), Config{
			Mode:     mode,
			AutoMin:  2,
			NewIndex: hnswFactory(),
		})
		if err != nil {
			t.Fatalf("mode %v: %v", mode, err)
		}
		mustSet(t, m, "red1", unit(1, 0, 0))
		mustSet(t, m, "red2", unit(0.9, 0.1, 0))
		mustSet(t, m, "blue1", unit(0.8, 0.2, 0))
		for _, k := range []string{"red1", "red2"} {
			if err := m.SetMetadata(k, map[string]string{"color": "red"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.SetMetadata("blue1", map[string]string{"color": "blue"}); err != nil {
			t.Fatal(err)
		}

		res, err := m.SearchFiltered(unit(1, 0, 0), 3, "color", "red")
		if err != nil {
			t.Fatalf("mode %v: %v", mode, err)
		}
		if len(res) != 2 {
			t.Fatalf("mode %v: got %d results, want 2: %v", mode, len(res), res)
		}
		for _, r := range res {
			if r.Key == "blue1" {
				t.Fatalf("mode %v: filter leaked non-matching key: %v", mode, res)
			}
		}

		// No match at all → empty.
		res, err = m.SearchFiltered(unit(1, 0, 0), 3, "color", "green")
		if err != nil {
			t.Fatalf("mode %v: %v", mode, err)
		}
		if len(res) != 0 {
			t.Fatalf("mode %v: got %v, want empty", mode, res)
		}

		// Empty field disables filtering.
		res, err = m.SearchFiltered(unit(1, 0, 0), 3, "", "")
		if err != nil {
			t.Fatalf("mode %v: %v", mode, err)
		}
		if len(res) != 3 {
			t.Fatalf("mode %v: unfiltered got %d, want 3", mode, len(res))
		}
	}
}

func TestMetadataLifecycle(t *testing.T) {
	m, err := NewManager(storage.New(), Config{Mode: ModeNone})
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, m, "a", unit(1, 0, 0))

	if meta, ok := m.GetMetadata("a"); !ok || meta != nil {
		t.Fatalf("expected nil metadata, got %v ok=%v", meta, ok)
	}
	if err := m.SetMetadata("missing", map[string]string{"x": "y"}); err == nil {
		t.Fatal("expected error setting metadata on missing key")
	}
	if err := m.SetMetadata("a", map[string]string{"color": "red", "n": "3"}); err != nil {
		t.Fatal(err)
	}
	meta, ok := m.GetMetadata("a")
	if !ok || meta["color"] != "red" || meta["n"] != "3" {
		t.Fatalf("metadata = %v ok=%v", meta, ok)
	}

	// Clear by empty map.
	if err := m.SetMetadata("a", nil); err != nil {
		t.Fatal(err)
	}
	if meta, ok := m.GetMetadata("a"); !ok || meta != nil {
		t.Fatalf("expected cleared metadata, got %v", meta)
	}

	// Delete removes metadata.
	if err := m.SetMetadata("a", map[string]string{"color": "red"}); err != nil {
		t.Fatal(err)
	}
	m.Delete("a")
	mustSet(t, m, "a", unit(1, 0, 0))
	if meta, _ := m.GetMetadata("a"); meta != nil {
		t.Fatalf("metadata survived delete: %v", meta)
	}
}

func TestMetadataSnapshotRoundTrip(t *testing.T) {
	m, err := NewManager(storage.New(), Config{Mode: ModeNone})
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, m, "a", unit(1, 0, 0))
	mustSet(t, m, "b", unit(0, 1, 0))
	if err := m.SetMetadata("a", map[string]string{"color": "red"}); err != nil {
		t.Fatal(err)
	}

	meta, err := m.GetAllMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 1 || meta["a"]["color"] != "red" {
		t.Fatalf("GetAllMetadata = %v", meta)
	}
	meta["a"]["color"] = "mutated"
	if got, _ := m.GetMetadata("a"); got["color"] != "red" {
		t.Fatal("GetAllMetadata aliased internal metadata")
	}

	m2, err := NewManager(storage.New(), Config{Mode: ModeNone})
	if err != nil {
		t.Fatal(err)
	}
	vecs, _ := m.GetAllVectors()
	allMeta, _ := m.GetAllMetadata()
	if err := m2.SetAllVectorsWithMetadata(vecs, allMeta); err != nil {
		t.Fatal(err)
	}
	got, ok := m2.GetMetadata("a")
	if !ok || got["color"] != "red" {
		t.Fatalf("restored metadata = %v ok=%v", got, ok)
	}
	if got, _ := m2.GetMetadata("b"); got != nil {
		t.Fatalf("unexpected metadata on b: %v", got)
	}
}

// axis returns a unit vector along axis i of the given dimension.
func axis(dim, i int) []float32 {
	v := make([]float32, dim)
	v[i] = 1
	return v
}

func vecsEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		d := a[i] - b[i]
		if d > 1e-6 || d < -1e-6 {
			return false
		}
	}
	return true
}

// TestSnapshotStreamsWhileWritesProceed is the leg-2 acceptance test: a
// streaming snapshot must not block writes for the whole copy, and the
// emitted state must be internally consistent — no torn pre/post mix.
func TestSnapshotStreamsWhileWritesProceed(t *testing.T) {
	m, err := NewManager(storage.New(), Config{
		Mode:               ModeHNSW,
		NewIndex:           hnswFactory(),
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	const n = 12
	for i := 0; i < n; i++ {
		mustSet(t, m, fmt.Sprintf("k%02d", i), axis(n, i))
	}

	entered := make(chan string, 1)
	gate := make(chan struct{})
	type snapResult struct {
		count int
		err   error
	}
	done := make(chan snapResult, 1)
	assembled := make(map[string][]float32)
	removed := make(map[string]bool)
	go func() {
		var gated bool
		count, err := m.SnapshotVectors(
			func(key string, vec []float32) error {
				if !gated {
					gated = true
					entered <- key
					<-gate
				}
				assembled[key] = append([]float32(nil), vec...)
				return nil
			},
			func(key string) error {
				removed[key] = true
				return nil
			},
		)
		done <- snapResult{count, err}
	}()

	// The snapshot is now blocked mid-stream inside fn. All three mutation
	// kinds must complete while it stays blocked: the write stall is
	// bounded to the short end barrier, never the full-library copy.
	first := <-entered
	delKey := "k00"
	if first == delKey {
		delKey = "k01"
	}
	writeDone := make(chan error, 1)
	go func() {
		if _, err := m.Set(first, axis(n, 11)); err != nil {
			writeDone <- err
			return
		}
		m.Delete(delKey)
		_, err := m.Set("added", axis(n, 10))
		writeDone <- err
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writes stalled while the snapshot stream was blocked")
	}

	close(gate)
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatal(res.err)
		}
		if res.count != n {
			t.Fatalf("snapshot count = %d, want %d", res.count, n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot did not finish after the stream was released")
	}

	// Deletions supersede earlier emissions.
	for key := range removed {
		delete(assembled, key)
	}

	expected := make(map[string][]float32)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("k%02d", i)
		switch {
		case key == delKey:
			// removed mid-snapshot
		case key == first:
			expected[key] = axis(n, 11)
		default:
			expected[key] = axis(n, i)
		}
	}
	expected["added"] = axis(n, 10)

	if len(assembled) != len(expected) {
		t.Fatalf("snapshot has %d keys, want %d (removed=%v, extra=%v)",
			len(assembled), len(expected), removed, assembled)
	}
	for key, want := range expected {
		got, ok := assembled[key]
		if !ok {
			t.Fatalf("snapshot missing key %q", key)
		}
		if !vecsEqual(got, want) {
			t.Fatalf("key %q: got %v, want %v", key, got, want)
		}
	}
}

func TestSnapshotVectorsMatchesGetAllVectors(t *testing.T) {
	m, err := NewManager(storage.New(), Config{
		Mode:               ModeHNSW,
		NewIndex:           hnswFactory(),
		RebuildDeleteRatio: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, m, "a", axis(3, 0))
	mustSet(t, m, "b", axis(3, 1))
	mustSet(t, m, "c", axis(3, 2))

	// HNSW dropped the store bodies; the stream must resolve them through
	// the published index.
	if body, _ := m.store.Get("a"); body != nil {
		t.Fatal("expected dropped store body")
	}

	assembled := make(map[string][]float32)
	count, err := m.SnapshotVectors(
		func(key string, vec []float32) error {
			assembled[key] = append([]float32(nil), vec...)
			return nil
		},
		func(key string) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	want, err := m.GetAllVectors()
	if err != nil {
		t.Fatal(err)
	}
	if count != len(want) || len(assembled) != len(want) {
		t.Fatalf("snapshot count=%d assembled=%d want=%d", count, len(assembled), len(want))
	}
	for key, w := range want {
		if !vecsEqual(assembled[key], w) {
			t.Fatalf("key %q: got %v want %v", key, assembled[key], w)
		}
	}
}
