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
	"sync"
	"testing"
)

func TestStorageBasicOperations(t *testing.T) {
	s := New()

	t.Run("Set and Get", func(t *testing.T) {
		err := s.Set("key1", []float32{0.1, 0.2, 0.3})
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		values, ok := s.Get("key1")
		if !ok {
			t.Fatal("Get() returned ok = false, want true")
		}
		if len(values) != 3 {
			t.Errorf("Get() returned %d values, want 3", len(values))
		}
	})

	t.Run("Get non-existent key", func(t *testing.T) {
		_, ok := s.Get("nonexistent")
		if ok {
			t.Error("Get(nonexistent) returned ok = true, want false")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		_ = s.Set("to-delete", []float32{0.1, 0.2, 0.3})
		deleted := s.Delete("to-delete")
		if !deleted {
			t.Error("Delete() returned false, want true")
		}

		_, ok := s.Get("to-delete")
		if ok {
			t.Error("Get() after Delete() returned ok = true, want false")
		}
	})

	t.Run("Delete non-existent key", func(t *testing.T) {
		deleted := s.Delete("never-existed")
		if deleted {
			t.Error("Delete(non-existent) returned true, want false")
		}
	})
}

func TestStorageDimensionConsistency(t *testing.T) {
	s := New()

	// First vector sets the dimension
	err := s.Set("key1", []float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Fatalf("First Set() error = %v", err)
	}

	// Same dimension should work
	err = s.Set("key2", []float32{0.4, 0.5, 0.6})
	if err != nil {
		t.Fatalf("Second Set() with same dim error = %v", err)
	}

	// Different dimension should fail
	err = s.Set("key3", []float32{0.1, 0.2})
	if err == nil {
		t.Error("Set() with different dimension should return error")
	}
}

func TestStorageCount(t *testing.T) {
	s := New()

	if s.Count() != 0 {
		t.Errorf("Empty storage Count() = %d, want 0", s.Count())
	}

	_ = s.Set("key1", []float32{0.1, 0.2, 0.3})
	_ = s.Set("key2", []float32{0.4, 0.5, 0.6})

	if s.Count() != 2 {
		t.Errorf("Count() = %d, want 2", s.Count())
	}

	s.Delete("key1")
	if s.Count() != 1 {
		t.Errorf("Count() after delete = %d, want 1", s.Count())
	}
}

func TestStorageClear(t *testing.T) {
	s := New()

	_ = s.Set("key1", []float32{0.1, 0.2, 0.3})
	_ = s.Set("key2", []float32{0.4, 0.5, 0.6})

	s.Clear()

	if s.Count() != 0 {
		t.Errorf("Count() after Clear() = %d, want 0", s.Count())
	}

	if s.Dimension() != 0 {
		t.Errorf("Dimension() after Clear() = %d, want 0", s.Dimension())
	}
}

func TestStorageSearch(t *testing.T) {
	s := New()

	// Insert some vectors
	_ = s.Set("vec1", []float32{1.0, 0.0, 0.0})
	_ = s.Set("vec2", []float32{0.9, 0.1, 0.0})
	_ = s.Set("vec3", []float32{0.0, 1.0, 0.0})
	_ = s.Set("vec4", []float32{0.0, 0.0, 1.0})

	t.Run("search similar vectors", func(t *testing.T) {
		query := []float32{1.0, 0.0, 0.0}
		results, err := s.Search(query, 2)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Search() returned %d results, want 2", len(results))
		}

		// vec1 should be most similar to query
		if results[0].Key != "vec1" {
			t.Errorf("Most similar key = %s, want vec1", results[0].Key)
		}
	})

	t.Run("search with k larger than data", func(t *testing.T) {
		query := []float32{1.0, 0.0, 0.0}
		results, err := s.Search(query, 10)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		if len(results) != 4 {
			t.Errorf("Search() returned %d results, want 4 (all vectors)", len(results))
		}
	})
}

func TestStorageConcurrency(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	numGoroutines := 100
	opsPerGoroutine := 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				_ = s.Set(key, []float32{float32(id), float32(j), 0.5})
			}
		}(i)
	}

	wg.Wait()

	expectedCount := numGoroutines * opsPerGoroutine
	if s.Count() != expectedCount {
		t.Errorf("Count() after concurrent writes = %d, want %d", s.Count(), expectedCount)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				_, ok := s.Get(key)
				if !ok {
					t.Errorf("Get(%s) returned false during concurrent read", key)
				}
			}
		}(i)
	}

	wg.Wait()
}

func BenchmarkStorageSet(b *testing.B) {
	s := New()
	vec := make([]float32, 128)
	for i := range vec {
		vec[i] = float32(i) / 128.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Set(fmt.Sprintf("key-%d", i), vec)
	}
}

func BenchmarkStorageGet(b *testing.B) {
	s := New()
	vec := make([]float32, 128)
	for i := range vec {
		vec[i] = float32(i) / 128.0
	}

	// Populate storage
	for i := 0; i < 10000; i++ {
		_ = s.Set(fmt.Sprintf("key-%d", i), vec)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Get(fmt.Sprintf("key-%d", i%10000))
	}
}

func BenchmarkStorageSearch(b *testing.B) {
	s := New()
	vec := make([]float32, 128)
	for i := range vec {
		vec[i] = float32(i) / 128.0
	}

	// Populate storage
	for i := 0; i < 1000; i++ {
		_ = s.Set(fmt.Sprintf("key-%d", i), vec)
	}

	query := make([]float32, 128)
	for i := range query {
		query[i] = float32(128-i) / 128.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Search(query, 10)
	}
}
