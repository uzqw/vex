package storage

import (
	"fmt"
	"testing"
)

func TestBruteForceNewIndex(t *testing.T) {
	bf := NewBruteForceIndex()
	if bf == nil {
		t.Fatal("NewBruteForceIndex returned nil")
	}
	if bf.Count() != 0 {
		t.Errorf("Count = %d on new index, want 0", bf.Count())
	}
}

func TestBruteForceInsert(t *testing.T) {
	bf := NewBruteForceIndex()

	if err := bf.Insert("k1", []float32{1, 0, 0}); err != nil {
		t.Fatalf("Insert error: %v", err)
	}
	if bf.Count() != 1 {
		t.Errorf("Count = %d after insert, want 1", bf.Count())
	}

	// Overwrite same key (BruteForce allows it)
	if err := bf.Insert("k1", []float32{0, 1, 0}); err != nil {
		t.Fatalf("Overwrite Insert error: %v", err)
	}
	if bf.Count() != 1 {
		t.Errorf("Count = %d after overwrite, want 1", bf.Count())
	}
}

func TestBruteForceSearch(t *testing.T) {
	t.Run("empty index", func(t *testing.T) {
		bf := NewBruteForceIndex()
		res, err := bf.Search([]float32{1, 0, 0}, 5)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if len(res) != 0 {
			t.Errorf("len(results) = %d, want 0", len(res))
		}
	})

	t.Run("top-1 is nearest", func(t *testing.T) {
		bf := NewBruteForceIndex()
		_ = bf.Insert("x", makeNormVec(1, 0, 0))
		_ = bf.Insert("y", makeNormVec(0, 1, 0))
		_ = bf.Insert("z", makeNormVec(0, 0, 1))

		res, err := bf.Search(makeNormVec(0.99, 0.01, 0), 1)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("len(results) = %d, want 1", len(res))
		}
		if res[0].Key != "x" {
			t.Errorf("top result = %q, want \"x\"", res[0].Key)
		}
	})

	t.Run("k larger than data size", func(t *testing.T) {
		bf := NewBruteForceIndex()
		_ = bf.Insert("a", makeNormVec(1, 0, 0))
		_ = bf.Insert("b", makeNormVec(0, 1, 0))

		res, err := bf.Search(makeNormVec(1, 0, 0), 10)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if len(res) != 2 {
			t.Errorf("len(results) = %d, want 2", len(res))
		}
	})

	t.Run("results sorted descending by similarity", func(t *testing.T) {
		bf := NewBruteForceIndex()
		for i := 0; i < 5; i++ {
			_ = bf.Insert(fmt.Sprintf("v%d", i), makeNormVec(float32(i+1), 0, 0))
		}
		res, err := bf.Search(makeNormVec(1, 0, 0), 5)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}
		for i := 1; i < len(res); i++ {
			if res[i].Similarity > res[i-1].Similarity {
				t.Errorf("results not sorted at index %d", i)
			}
		}
	})
}

func TestBruteForceDelete(t *testing.T) {
	bf := NewBruteForceIndex()
	_ = bf.Insert("a", makeNormVec(1, 0, 0))
	_ = bf.Insert("b", makeNormVec(0, 1, 0))

	if err := bf.Delete("a"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if bf.Count() != 1 {
		t.Errorf("Count = %d after delete, want 1", bf.Count())
	}

	// Delete non-existent: BruteForce silently ignores (no error)
	if err := bf.Delete("nonexistent"); err != nil {
		t.Errorf("Delete non-existent returned error: %v", err)
	}
}

func TestBruteForceClear(t *testing.T) {
	bf := NewBruteForceIndex()
	for i := 0; i < 5; i++ {
		_ = bf.Insert(fmt.Sprintf("k%d", i), makeNormVec(float32(i+1), 0, 0))
	}
	bf.Clear()
	if bf.Count() != 0 {
		t.Errorf("Count = %d after Clear, want 0", bf.Count())
	}
}

func TestBruteForceCount(t *testing.T) {
	bf := NewBruteForceIndex()
	if bf.Count() != 0 {
		t.Errorf("initial Count = %d, want 0", bf.Count())
	}
	_ = bf.Insert("a", makeNormVec(1, 0, 0))
	_ = bf.Insert("b", makeNormVec(0, 1, 0))
	if bf.Count() != 2 {
		t.Errorf("Count = %d, want 2", bf.Count())
	}
}
