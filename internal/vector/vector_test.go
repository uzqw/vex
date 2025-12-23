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

package vector

import (
	"math"
	"testing"
)

func TestMagnitude(t *testing.T) {
	tests := []struct {
		name     string
		v        []float32
		expected float32
	}{
		{"unit vector x", []float32{1, 0, 0}, 1.0},
		{"unit vector y", []float32{0, 1, 0}, 1.0},
		{"3-4-5 triangle", []float32{3, 4}, 5.0},
		{"zero vector", []float32{0, 0, 0}, 0.0},
		{"negative values", []float32{-3, -4}, 5.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Magnitude(tt.v)
			if math.Abs(float64(got-tt.expected)) > 0.0001 {
				t.Errorf("Magnitude(%v) = %v, want %v", tt.v, got, tt.expected)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	t.Run("normal vector", func(t *testing.T) {
		v := []float32{3, 4}
		normalized, err := Normalize(v)
		if err != nil {
			t.Fatalf("Normalize() error = %v", err)
		}

		// Check magnitude is 1
		mag := Magnitude(normalized)
		if math.Abs(float64(mag-1.0)) > 0.0001 {
			t.Errorf("Normalized vector magnitude = %v, want 1.0", mag)
		}

		// Check direction is preserved
		expectedX := float32(3.0 / 5.0)
		expectedY := float32(4.0 / 5.0)
		if math.Abs(float64(normalized[0]-expectedX)) > 0.0001 {
			t.Errorf("Normalized[0] = %v, want %v", normalized[0], expectedX)
		}
		if math.Abs(float64(normalized[1]-expectedY)) > 0.0001 {
			t.Errorf("Normalized[1] = %v, want %v", normalized[1], expectedY)
		}
	})

	t.Run("zero vector returns error", func(t *testing.T) {
		v := []float32{0, 0, 0}
		_, err := Normalize(v)
		if err != ErrZeroVector {
			t.Errorf("Normalize(zero vector) error = %v, want ErrZeroVector", err)
		}
	})
}

func TestDotProduct(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []float32
		expected float32
		wantErr  bool
	}{
		{"orthogonal vectors", []float32{1, 0}, []float32{0, 1}, 0, false},
		{"same direction", []float32{1, 0}, []float32{1, 0}, 1, false},
		{"opposite direction", []float32{1, 0}, []float32{-1, 0}, -1, false},
		{"general case", []float32{1, 2, 3}, []float32{4, 5, 6}, 32, false},
		{"dimension mismatch", []float32{1, 2}, []float32{1, 2, 3}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DotProduct(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("DotProduct() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && math.Abs(float64(got-tt.expected)) > 0.0001 {
				t.Errorf("DotProduct(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []float32
		expected float32
		wantErr  bool
	}{
		{"identical vectors", []float32{1, 2, 3}, []float32{1, 2, 3}, 1.0, false},
		{"opposite vectors", []float32{1, 0}, []float32{-1, 0}, -1.0, false},
		{"orthogonal vectors", []float32{1, 0}, []float32{0, 1}, 0.0, false},
		{"dimension mismatch", []float32{1, 2}, []float32{1, 2, 3}, 0, true},
		{"zero vector a", []float32{0, 0}, []float32{1, 1}, 0, true},
		{"zero vector b", []float32{1, 1}, []float32{0, 0}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CosineSimilarity(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("CosineSimilarity() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && math.Abs(float64(got-tt.expected)) > 0.0001 {
				t.Errorf("CosineSimilarity(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestEuclideanDistance(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []float32
		expected float32
		wantErr  bool
	}{
		{"same point", []float32{1, 2, 3}, []float32{1, 2, 3}, 0.0, false},
		{"unit distance x", []float32{0, 0}, []float32{1, 0}, 1.0, false},
		{"3-4-5 triangle", []float32{0, 0}, []float32{3, 4}, 5.0, false},
		{"dimension mismatch", []float32{1, 2}, []float32{1, 2, 3}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EuclideanDistance(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("EuclideanDistance() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && math.Abs(float64(got-tt.expected)) > 0.0001 {
				t.Errorf("EuclideanDistance(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestTopKHeap(t *testing.T) {
	t.Run("heap operations", func(t *testing.T) {
		h := &TopKHeap{}
		if h.Len() != 0 {
			t.Errorf("Empty heap Len() = %d, want 0", h.Len())
		}

		// Test Push
		h.Push(SearchResult{Key: "a", Similarity: 0.5})
		h.Push(SearchResult{Key: "b", Similarity: 0.3})
		h.Push(SearchResult{Key: "c", Similarity: 0.8})

		if h.Len() != 3 {
			t.Errorf("Heap Len() = %d, want 3", h.Len())
		}

		// Test Pop (should return lowest similarity first - min heap)
		popped := h.Pop().(SearchResult)
		if popped.Similarity != 0.8 {
			t.Errorf("Pop() returned similarity %v, expected 0.8 (last pushed)", popped.Similarity)
		}
	})

	t.Run("Less comparison", func(t *testing.T) {
		h := TopKHeap{
			{Key: "low", Similarity: 0.3},
			{Key: "high", Similarity: 0.9},
		}

		// Less returns true if i has lower similarity than j (min-heap behavior)
		if !h.Less(0, 1) {
			t.Error("Less(0, 1) should be true since 0.3 < 0.9")
		}
		if h.Less(1, 0) {
			t.Error("Less(1, 0) should be false since 0.9 > 0.3")
		}
	})

	t.Run("Swap operation", func(t *testing.T) {
		h := TopKHeap{
			{Key: "first", Similarity: 0.1},
			{Key: "second", Similarity: 0.9},
		}

		h.Swap(0, 1)

		if h[0].Key != "second" || h[1].Key != "first" {
			t.Errorf("Swap failed: got [%s, %s], want [second, first]", h[0].Key, h[1].Key)
		}
	})
}

func BenchmarkDotProduct(b *testing.B) {
	v1 := make([]float32, 128)
	v2 := make([]float32, 128)
	for i := range v1 {
		v1[i] = float32(i) / 128.0
		v2[i] = float32(128-i) / 128.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DotProduct(v1, v2)
	}
}

func BenchmarkNormalize(b *testing.B) {
	v := make([]float32, 128)
	for i := range v {
		v[i] = float32(i) / 128.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Normalize(v)
	}
}
