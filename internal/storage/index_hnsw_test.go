package storage

import (
	"fmt"
	"math"
	"testing"
)

// ---- helpers ----------------------------------------------------------------

func makeNormVec(vals ...float32) []float32 {
	var sum float32
	for _, v := range vals {
		sum += v * v
	}
	mag := float32(math.Sqrt(float64(sum)))
	out := make([]float32, len(vals))
	for i, v := range vals {
		out[i] = v / mag
	}
	return out
}

// ---- NewHNSWIndex -----------------------------------------------------------

func TestNewHNSWIndex(t *testing.T) {
	h := NewHNSWIndex()
	if h == nil {
		t.Fatal("NewHNSWIndex returned nil")
	}
	if h.M <= 0 {
		t.Errorf("M = %d, want > 0", h.M)
	}
	if h.EfConstruct <= 0 {
		t.Errorf("EfConstruct = %d, want > 0", h.EfConstruct)
	}
	if h.Ef <= 0 {
		t.Errorf("Ef = %d, want > 0", h.Ef)
	}
	if h.rng == nil {
		t.Error("rng is nil")
	}
	if h.Count() != 0 {
		t.Errorf("Count() = %d on empty index, want 0", h.Count())
	}
	if h.entry != noNode {
		t.Error("entry should be noNode on empty index")
	}
}

func TestNewHNSWIndexWithConfig(t *testing.T) {
	h := NewHNSWIndexWithConfig(HNSWConfig{
		M:           12,
		EfConstruct: 80,
		Ef:          40,
		Seed:        123,
	})

	if h.M != 12 {
		t.Errorf("M = %d, want 12", h.M)
	}
	if h.EfConstruct != 80 {
		t.Errorf("EfConstruct = %d, want 80", h.EfConstruct)
	}
	if h.Ef != 40 {
		t.Errorf("Ef = %d, want 40", h.Ef)
	}
	if h.rng == nil {
		t.Fatal("rng is nil")
	}
}

func TestVisitedPoolClears(t *testing.T) {
	m := acquireVisited()
	m[1] = true
	releaseVisited(m)
	m2 := acquireVisited()
	if len(m2) != 0 {
		t.Fatalf("pooled visited map not cleared: %d", len(m2))
	}
	releaseVisited(m2)
}

// ---- adaptiveEf -------------------------------------------------------------

func TestAdaptiveEf(t *testing.T) {
	h := NewHNSWIndex()
	base := h.EfConstruct

	for _, dim := range []int{64, 128, 256, 512, 1024, 1536} {
		if got := h.adaptiveEf(base, dim); got != base {
			t.Errorf("adaptiveEf(dim=%d) = %d, want %d", dim, got, base)
		}
	}
}

// ---- Insert -----------------------------------------------------------------

func TestHNSWInsert(t *testing.T) {
	t.Run("first node becomes entry point", func(t *testing.T) {
		h := NewHNSWIndex()
		v := makeNormVec(1, 0, 0)
		if err := h.Insert("a", v); err != nil {
			t.Fatalf("Insert error: %v", err)
		}
		if h.Count() != 1 {
			t.Errorf("Count = %d, want 1", h.Count())
		}
		if h.entry == noNode {
			t.Error("entry is noNode after first insert")
		}
	})

	t.Run("duplicate key returns error", func(t *testing.T) {
		h := NewHNSWIndex()
		v := makeNormVec(1, 0, 0)
		_ = h.Insert("a", v)
		if err := h.Insert("a", v); err == nil {
			t.Error("expected error on duplicate key, got nil")
		}
	})

	t.Run("multiple inserts increase Count", func(t *testing.T) {
		h := NewHNSWIndex()
		for i := 0; i < 10; i++ {
			v := makeNormVec(float32(i+1), 0, 0)
			if err := h.Insert(fmt.Sprintf("k%d", i), v); err != nil {
				t.Fatalf("Insert %d error: %v", i, err)
			}
		}
		if h.Count() != 10 {
			t.Errorf("Count = %d, want 10", h.Count())
		}
	})
}

// ---- Search -----------------------------------------------------------------

func TestHNSWSearch(t *testing.T) {
	t.Run("empty index returns empty slice", func(t *testing.T) {
		h := NewHNSWIndex()
		res, err := h.Search(makeNormVec(1, 0, 0), 5)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if len(res) != 0 {
			t.Errorf("len(results) = %d, want 0", len(res))
		}
	})

	t.Run("k larger than index size", func(t *testing.T) {
		h := NewHNSWIndex()
		for i := 0; i < 3; i++ {
			_ = h.Insert(fmt.Sprintf("v%d", i), makeNormVec(float32(i+1), 0, 0))
		}
		res, err := h.Search(makeNormVec(1, 0, 0), 10)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if len(res) > 3 {
			t.Errorf("len(results) = %d, want <= 3", len(res))
		}
	})

	t.Run("nearest neighbor is correct", func(t *testing.T) {
		h := NewHNSWIndex()
		_ = h.Insert("x", makeNormVec(1, 0, 0))
		_ = h.Insert("y", makeNormVec(0, 1, 0))
		_ = h.Insert("z", makeNormVec(0, 0, 1))

		// query closest to "x"
		res, err := h.Search(makeNormVec(0.99, 0.01, 0), 1)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if len(res) == 0 {
			t.Fatal("no results returned")
		}
		if res[0].Key != "x" {
			t.Errorf("top result = %q, want \"x\"", res[0].Key)
		}
	})

	t.Run("results ordered by similarity descending", func(t *testing.T) {
		h := NewHNSWIndex()
		_ = h.Insert("a", makeNormVec(1, 0, 0))
		_ = h.Insert("b", makeNormVec(0.9, 0.1, 0))
		_ = h.Insert("c", makeNormVec(0, 1, 0))

		res, err := h.Search(makeNormVec(1, 0, 0), 3)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		for i := 1; i < len(res); i++ {
			if res[i].Similarity > res[i-1].Similarity {
				t.Errorf("results not sorted: res[%d].Similarity=%f > res[%d].Similarity=%f",
					i, res[i].Similarity, i-1, res[i-1].Similarity)
			}
		}
	})
}

// ---- Delete -----------------------------------------------------------------

func TestHNSWDelete(t *testing.T) {
	t.Run("delete non-existent key returns error", func(t *testing.T) {
		h := NewHNSWIndex()
		if err := h.Delete("nope"); err == nil {
			t.Error("expected error deleting non-existent key")
		}
	})

	t.Run("delete reduces Count", func(t *testing.T) {
		h := NewHNSWIndex()
		_ = h.Insert("a", makeNormVec(1, 0, 0))
		_ = h.Insert("b", makeNormVec(0, 1, 0))
		if err := h.Delete("a"); err != nil {
			t.Fatalf("Delete error: %v", err)
		}
		if h.Count() != 1 {
			t.Errorf("Count = %d after delete, want 1", h.Count())
		}
	})

	t.Run("delete entry point re-elects new one", func(t *testing.T) {
		h := NewHNSWIndex()
		_ = h.Insert("a", makeNormVec(1, 0, 0))
		_ = h.Insert("b", makeNormVec(0, 1, 0))
		ep := h.entry
		if err := h.Delete(h.nodes[ep].id); err != nil {
			t.Fatalf("Delete entry point error: %v", err)
		}
		if h.entry == ep {
			t.Error("entry was not updated after deleting it")
		}
		if h.Count() > 0 && h.entry == noNode {
			t.Error("entry is noNode but index is non-empty")
		}
	})

	t.Run("delete only node leaves empty index", func(t *testing.T) {
		h := NewHNSWIndex()
		_ = h.Insert("solo", makeNormVec(1, 0, 0))
		if err := h.Delete("solo"); err != nil {
			t.Fatalf("Delete error: %v", err)
		}
		if h.Count() != 0 {
			t.Errorf("Count = %d after deleting last node, want 0", h.Count())
		}
		if h.entry != noNode {
			t.Error("entry should be noNode after deleting last node")
		}
		if err := h.Insert("b", makeNormVec(1, 0)); err != nil {
			t.Fatalf("empty index should accept a new dimension: %v", err)
		}
	})

	t.Run("deleted node not reachable via search", func(t *testing.T) {
		h := NewHNSWIndex()
		for i := 0; i < 20; i++ {
			_ = h.Insert(fmt.Sprintf("v%d", i), makeNormVec(float32(i+1), float32(i), 0))
		}
		_ = h.Delete("v0")
		res, err := h.Search(makeNormVec(1, 0, 0), 20)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		for _, r := range res {
			if r.Key == "v0" {
				t.Error("deleted key v0 appeared in search results")
			}
		}
	})
}

// ---- Clear ------------------------------------------------------------------

func TestHNSWClear(t *testing.T) {
	h := NewHNSWIndex()
	for i := 0; i < 5; i++ {
		_ = h.Insert(fmt.Sprintf("k%d", i), makeNormVec(float32(i+1), 0, 0))
	}
	h.Clear()
	if h.Count() != 0 {
		t.Errorf("Count = %d after Clear, want 0", h.Count())
	}
	if h.entry != noNode {
		t.Error("entry should be noNode after Clear")
	}
	if h.maxLevel != 0 {
		t.Errorf("maxLevel = %d after Clear, want 0", h.maxLevel)
	}
}

// ---- GetStats ---------------------------------------------------------------

func TestHNSWGetStats(t *testing.T) {
	h := NewHNSWIndex()

	t.Run("empty index stats", func(t *testing.T) {
		stats := h.GetStats()
		if stats.TotalVectors != 0 {
			t.Errorf("TotalVectors = %d, want 0", stats.TotalVectors)
		}
	})

	t.Run("stats after inserts", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			_ = h.Insert(fmt.Sprintf("v%d", i), makeNormVec(float32(i+1), float32(i), 0))
		}
		stats := h.GetStats()
		if stats.TotalVectors != 50 {
			t.Errorf("TotalVectors = %d, want 50", stats.TotalVectors)
		}
		if stats.MaxLevel < 0 {
			t.Errorf("MaxLevel = %d, want >= 0", stats.MaxLevel)
		}
		if stats.AverageNeighbors <= 0 {
			t.Errorf("AverageNeighbors = %f, want > 0", stats.AverageNeighbors)
		}
		if len(stats.LayerDistribution) == 0 {
			t.Error("LayerDistribution is empty")
		}
	})
}

// ---- selectNeighborsHeuristic -----------------------------------------------

func TestSelectNeighborsHeuristic(t *testing.T) {
	insert := func(h *HNSWIndex, id string, v ...float32) int32 {
		t.Helper()
		if err := h.Insert(id, makeNormVec(v...)); err != nil {
			t.Fatal(err)
		}
		return h.keys[id]
	}

	t.Run("fewer candidates than m returns all", func(t *testing.T) {
		h := NewHNSWIndex()
		candidates := []int32{insert(h, "a", 1, 0), insert(h, "b", 0, 1)}
		got, err := h.selectNeighborsHeuristic(makeNormVec(1, 0), candidates, 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Errorf("len = %d, want 2", len(got))
		}
	})

	t.Run("returns at most m neighbors", func(t *testing.T) {
		h := NewHNSWIndex()
		candidates := make([]int32, 20)
		for i := range candidates {
			candidates[i] = insert(h, fmt.Sprintf("k%d", i), float32(i+1), 0)
		}
		got, err := h.selectNeighborsHeuristic(makeNormVec(1, 0), candidates, 4)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) > 4 {
			t.Errorf("len = %d, want <= 4", len(got))
		}
	})

	t.Run("backfills dominated candidates when not enough undominated", func(t *testing.T) {
		h := NewHNSWIndex()
		query := makeNormVec(1, 0, 0)
		candidates := []int32{
			insert(h, "a", 1, 0, 0),
			insert(h, "b", 0.99, 0.01, 0),
			insert(h, "c", 0.98, 0.02, 0),
		}
		got, err := h.selectNeighborsHeuristic(query, candidates, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 {
			t.Error("expected non-empty result with backfill")
		}
	})
}

// ---- pruneNeighbors ---------------------------------------------------------

func TestPruneNeighbors(t *testing.T) {
	h := NewHNSWIndex()

	t.Run("no prune when within limit", func(t *testing.T) {
		h.nodes = []hnswNode{{
			neighbors: [][]hnswEdge{
				{{idx: 0, dist: 0.1}, {idx: 1, dist: 0.2}},
			},
		}}
		if err := h.pruneNeighbors(0, 0, 5); err != nil {
			t.Fatal(err)
		}
		if len(h.nodes[0].neighbors[0]) != 2 {
			t.Errorf("expected 2 neighbors, got %d", len(h.nodes[0].neighbors[0]))
		}
	})

	t.Run("prune sorts and truncates", func(t *testing.T) {
		h.nodes = []hnswNode{{
			neighbors: [][]hnswEdge{
				{{idx: 0, dist: 0.5}, {idx: 1, dist: 0.1}, {idx: 2, dist: 0.3}, {idx: 3, dist: 0.9}},
			},
		}}
		if err := h.pruneNeighbors(0, 0, 2); err != nil {
			t.Fatal(err)
		}
		nb := h.nodes[0].neighbors[0]
		if len(nb) != 2 {
			t.Fatalf("expected 2 neighbors after prune, got %d", len(nb))
		}
		if nb[0].dist != 0.1 || nb[1].dist != 0.3 {
			t.Errorf("wrong neighbors kept: %.1f, %.1f", nb[0].dist, nb[1].dist)
		}
	})
}

func TestComputeLevel(t *testing.T) {
	mult := float32(1.0 / math.Log(2.0))
	cases := []struct {
		name string
		u    float64
		mult float32
		cap  int
		min  int
		max  int
	}{
		{"u=0", 0, mult, 64, 0, 0},
		{"u near 1", 0.999999, mult, 64, 1, 64},
		{"u=1 huge mult", 1, 1e9, 64, 64, 64},
		{"NaN", math.NaN(), mult, 64, 0, 0},
		{"cap 8", 1, 1e9, 8, 8, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeLevel(tc.u, tc.mult, tc.cap)
			if got < tc.min || got > tc.max {
				t.Fatalf("level = %d, want in [%d,%d]", got, tc.min, tc.max)
			}
		})
	}
}

func TestHNSWLayerInvariant(t *testing.T) {
	h := NewHNSWIndexWithConfig(HNSWConfig{M: 4, EfConstruct: 32, Ef: 16, Seed: 7})
	for i := 0; i < 80; i++ {
		v := makeNormVec(float32(i%5+1), float32(i%3+1), float32(i%7+1), 1)
		if err := h.Insert(fmt.Sprintf("n%d", i), v); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.CheckLayerInvariant(); err != nil {
		t.Fatal(err)
	}
}

func TestHNSWCopyAndDimCheck(t *testing.T) {
	h := NewHNSWIndex()
	v := makeNormVec(1, 0, 0)
	orig0 := v[0]
	if err := h.Insert("a", v); err != nil {
		t.Fatal(err)
	}
	v[0] = 0 // mutate caller slice
	if got := h.vecAt(h.keys["a"])[0]; got != orig0 {
		t.Fatalf("index retained caller slice: got %v want %v", got, orig0)
	}
	if err := h.Insert("b", makeNormVec(1, 0)); err == nil {
		t.Fatal("expected dimension mismatch")
	}
	if _, err := h.Search(makeNormVec(1, 0), 1); err == nil {
		t.Fatal("expected query dimension mismatch")
	}
}

// ---- distanceBetween --------------------------------------------------------

func TestDistanceBetween(t *testing.T) {
	a := makeNormVec(1, 0, 0)
	b := makeNormVec(1, 0, 0)

	d, err := distanceBetween(a, b)
	if err != nil {
		t.Fatalf("distanceBetween error: %v", err)
	}
	// identical vectors: dot=1, distance=-1
	if d > -0.99 {
		t.Errorf("distance for identical vectors = %f, want ~-1", d)
	}

	// orthogonal vectors: dot=0, distance=0
	c := makeNormVec(0, 1, 0)
	d2, _ := distanceBetween(a, c)
	if d2 < -0.01 || d2 > 0.01 {
		t.Errorf("distance for orthogonal vectors = %f, want ~0", d2)
	}
}
