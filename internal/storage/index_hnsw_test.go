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
	if h.entryPoint != nil {
		t.Error("entryPoint should be nil on empty index")
	}
}

// ---- adaptiveEf -------------------------------------------------------------

func TestAdaptiveEf(t *testing.T) {
	h := NewHNSWIndex()
	base := h.EfConstruct

	cases := []struct {
		dim     int
		wantMax int // result must be <= wantMax
		wantMin int // result must be >= wantMin
	}{
		{64, base, h.M * 2},  // dim < 128: no scaling
		{128, base, base},    // dim == 128: no scaling
		{256, base, h.M * 2}, // dim > 128: scaled down
		{512, base, h.M * 2},
	}

	for _, tc := range cases {
		got := h.adaptiveEf(base, tc.dim)
		if got > tc.wantMax {
			t.Errorf("adaptiveEf(dim=%d) = %d > max %d", tc.dim, got, tc.wantMax)
		}
		if got < tc.wantMin {
			t.Errorf("adaptiveEf(dim=%d) = %d < min %d", tc.dim, got, tc.wantMin)
		}
	}

	// 128-D should return baseEf unchanged
	if h.adaptiveEf(base, 128) != base {
		t.Errorf("adaptiveEf(128) = %d, want %d", h.adaptiveEf(base, 128), base)
	}

	// 512-D should be strictly less than 256-D result
	ef512 := h.adaptiveEf(base, 512)
	ef256 := h.adaptiveEf(base, 256)
	if ef512 >= ef256 {
		t.Errorf("adaptiveEf(512)=%d should be < adaptiveEf(256)=%d", ef512, ef256)
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
		if h.entryPoint == nil {
			t.Error("entryPoint is nil after first insert")
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
		ep := h.entryPoint
		if err := h.Delete(ep.ID); err != nil {
			t.Fatalf("Delete entry point error: %v", err)
		}
		if h.entryPoint == ep {
			t.Error("entryPoint was not updated after deleting it")
		}
		if h.Count() > 0 && h.entryPoint == nil {
			t.Error("entryPoint is nil but index is non-empty")
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
		if h.entryPoint != nil {
			t.Error("entryPoint should be nil after deleting last node")
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
	if h.entryPoint != nil {
		t.Error("entryPoint should be nil after Clear")
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
	h := NewHNSWIndex()

	makeNode := func(v ...float32) *HNSWNode {
		return &HNSWNode{Vector: makeNormVec(v...)}
	}

	t.Run("fewer candidates than m returns all", func(t *testing.T) {
		candidates := []*HNSWNode{makeNode(1, 0), makeNode(0, 1)}
		got := h.selectNeighborsHeuristic(makeNormVec(1, 0), candidates, 5)
		if len(got) != 2 {
			t.Errorf("len = %d, want 2", len(got))
		}
	})

	t.Run("returns at most m neighbors", func(t *testing.T) {
		candidates := make([]*HNSWNode, 20)
		for i := range candidates {
			candidates[i] = makeNode(float32(i+1), 0)
		}
		got := h.selectNeighborsHeuristic(makeNormVec(1, 0), candidates, 4)
		if len(got) > 4 {
			t.Errorf("len = %d, want <= 4", len(got))
		}
	})

	t.Run("backfills dominated candidates when not enough undominated", func(t *testing.T) {
		// All candidates point in same direction → all dominated after first
		query := makeNormVec(1, 0, 0)
		candidates := []*HNSWNode{
			makeNode(1, 0, 0),
			makeNode(0.99, 0.01, 0),
			makeNode(0.98, 0.02, 0),
		}
		got := h.selectNeighborsHeuristic(query, candidates, 3)
		if len(got) == 0 {
			t.Error("expected non-empty result with backfill")
		}
	})
}

// ---- pruneNeighbors ---------------------------------------------------------

func TestPruneNeighbors(t *testing.T) {
	h := NewHNSWIndex()

	makeNeighbor := func(dist float32) *HNSWNeighbor {
		return &HNSWNeighbor{Node: &HNSWNode{}, Distance: dist}
	}

	t.Run("no prune when within limit", func(t *testing.T) {
		node := &HNSWNode{
			Neighbors: [][]*HNSWNeighbor{
				{makeNeighbor(0.1), makeNeighbor(0.2)},
			},
		}
		h.pruneNeighbors(node, 0, 5)
		if len(node.Neighbors[0]) != 2 {
			t.Errorf("expected 2 neighbors, got %d", len(node.Neighbors[0]))
		}
	})

	t.Run("prune sorts and truncates", func(t *testing.T) {
		node := &HNSWNode{
			Neighbors: [][]*HNSWNeighbor{
				{makeNeighbor(0.5), makeNeighbor(0.1), makeNeighbor(0.3), makeNeighbor(0.9)},
			},
		}
		h.pruneNeighbors(node, 0, 2)
		nb := node.Neighbors[0]
		if len(nb) != 2 {
			t.Fatalf("expected 2 neighbors after prune, got %d", len(nb))
		}
		if nb[0].Distance != 0.1 || nb[1].Distance != 0.3 {
			t.Errorf("wrong neighbors kept: %.1f, %.1f", nb[0].Distance, nb[1].Distance)
		}
	})
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
