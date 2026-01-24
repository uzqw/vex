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
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// Helper to compare floats with tolerance
func floatEquals(a, b float32, tolerance float32) bool {
	return math.Abs(float64(a-b)) < float64(tolerance)
}

// Test dot product with fixed values
func TestDotProductFixedValue(t *testing.T) {
	dims := []int{1, 2, 3, 4, 5, 6, 7, 8, 16, 32, 64, 128, 256, 512, 1024}

	for _, dim := range dims {
		t.Run(fmt.Sprintf("dim_%d", dim), func(t *testing.T) {
			a := make([]float32, dim)
			b := make([]float32, dim)

			// All ones
			for i := 0; i < dim; i++ {
				a[i] = 1.0
				b[i] = 1.0
			}

			expected := float32(dim)
			result := DotProductASM(a, b)
			goResult := dotProductGo(a, b)

			if !floatEquals(result, expected, 0.001) {
				t.Errorf("ASM: expected %f, got %f", expected, result)
			}
			if !floatEquals(goResult, expected, 0.001) {
				t.Errorf("Go: expected %f, got %f", expected, goResult)
			}
			if !floatEquals(result, goResult, 0.001) {
				t.Errorf("ASM vs Go mismatch: %f vs %f", result, goResult)
			}
		})
	}
}

// Test dot product with random values
func TestDotProductRandomValue(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	dims := []int{1, 2, 3, 4, 5, 6, 7, 8, 16, 32, 64, 128, 256, 512, 1024}
	iterations := 100

	for _, dim := range dims {
		t.Run(fmt.Sprintf("dim_%d", dim), func(t *testing.T) {
			for i := 0; i < iterations; i++ {
				a := make([]float32, dim)
				b := make([]float32, dim)

				for j := 0; j < dim; j++ {
					a[j] = r.Float32()*2 - 1
					b[j] = r.Float32()*2 - 1
				}

				goResult := dotProductGo(a, b)
				asmResult := DotProductASM(a, b)

				tolerance := float32(math.Abs(float64(goResult))) * 0.01
				if tolerance < 0.001 {
					tolerance = 0.001
				}

				if !floatEquals(goResult, asmResult, tolerance) {
					t.Errorf("dim=%d: Go %f != ASM %f (diff: %f)", dim, goResult, asmResult, goResult-asmResult)
				}
			}
		})
	}
}

// Test dot product with normalized vectors (common in practice)
func TestDotProductNormalized(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	dims := []int{8, 16, 32, 64, 128, 256, 512, 1024}
	iterations := 50

	for _, dim := range dims {
		t.Run(fmt.Sprintf("normalized_dim_%d", dim), func(t *testing.T) {
			for i := 0; i < iterations; i++ {
				a := make([]float32, dim)
				b := make([]float32, dim)

				// Generate random vectors
				for j := 0; j < dim; j++ {
					a[j] = r.Float32()*2 - 1
					b[j] = r.Float32()*2 - 1
				}

				// Normalize
				normA, _ := Normalize(a)
				normB, _ := Normalize(b)

				goResult := dotProductGo(normA, normB)
				asmResult := DotProductASM(normA, normB)

				// For normalized vectors, result should be in [-1, 1]
				if goResult < -1.0 || goResult > 1.0 {
					t.Errorf("Go result out of bounds: %f", goResult)
				}
				if asmResult < -1.0 || asmResult > 1.0 {
					t.Errorf("ASM result out of bounds: %f", asmResult)
				}

				if !floatEquals(goResult, asmResult, 0.001) {
					t.Errorf("normalized dim=%d: Go %f != ASM %f", dim, goResult, asmResult)
				}
			}
		})
	}
}

// Benchmark: Go implementation
func BenchmarkDotProductGo(b *testing.B) {
	dims := []int{8, 16, 32, 64, 128, 256, 512, 1024}

	for _, dim := range dims {
		b.Run(fmt.Sprintf("dim_%d", dim), func(b *testing.B) {
			r := rand.New(rand.NewSource(42))
			a := make([]float32, dim)
			v := make([]float32, dim)

			for i := 0; i < dim; i++ {
				a[i] = r.Float32()
				v[i] = r.Float32()
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = dotProductGo(a, v)
			}
		})
	}
}

// Benchmark: ASM implementation
func BenchmarkDotProductASM(b *testing.B) {
	dims := []int{8, 16, 32, 64, 128, 256, 512, 1024}

	for _, dim := range dims {
		b.Run(fmt.Sprintf("dim_%d", dim), func(b *testing.B) {
			r := rand.New(rand.NewSource(42))
			a := make([]float32, dim)
			v := make([]float32, dim)

			for i := 0; i < dim; i++ {
				a[i] = r.Float32()
				v[i] = r.Float32()
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = DotProductASM(a, v)
			}
		})
	}
}

// Benchmark comparison
func BenchmarkDotProductComparison(b *testing.B) {
	dims := []int{128, 256, 512, 1024}

	for _, dim := range dims {
		b.Run(fmt.Sprintf("dim_%d", dim), func(b *testing.B) {
			r := rand.New(rand.NewSource(42))
			a := make([]float32, dim)
			v := make([]float32, dim)

			for i := 0; i < dim; i++ {
				a[i] = r.Float32()
				v[i] = r.Float32()
			}

			b.Run("Go", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = dotProductGo(a, v)
				}
			})

			b.Run("ASM", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = DotProductASM(a, v)
				}
			})
		})
	}
}

// Test edge cases
func TestDotProductEdgeCases(t *testing.T) {
	// Zero vectors
	t.Run("zero_vectors", func(t *testing.T) {
		a := make([]float32, 128)
		b := make([]float32, 128)
		expected := float32(0)

		result := DotProductASM(a, b)
		if result != expected {
			t.Errorf("zero vectors: expected %f, got %f", expected, result)
		}
	})

	// Single element
	t.Run("single_element", func(t *testing.T) {
		a := []float32{3.5}
		b := []float32{2.0}
		expected := float32(7.0)

		result := DotProductASM(a, b)
		if !floatEquals(result, expected, 0.001) {
			t.Errorf("single element: expected %f, got %f", expected, result)
		}
	})

	// Very large values
	t.Run("large_values", func(t *testing.T) {
		a := make([]float32, 10)
		b := make([]float32, 10)
		for i := 0; i < 10; i++ {
			a[i] = 1e6
			b[i] = 1e6
		}

		goResult := dotProductGo(a, b)
		asmResult := DotProductASM(a, b)

		if !floatEquals(goResult, asmResult, 1e6*0.001) {
			t.Errorf("large values: Go %e != ASM %e", goResult, asmResult)
		}
	})

	// Very small values
	t.Run("small_values", func(t *testing.T) {
		a := make([]float32, 10)
		b := make([]float32, 10)
		for i := 0; i < 10; i++ {
			a[i] = 1e-6
			b[i] = 1e-6
		}

		goResult := dotProductGo(a, b)
		asmResult := DotProductASM(a, b)

		if !floatEquals(goResult, asmResult, 1e-12) {
			t.Errorf("small values: Go %e != ASM %e", goResult, asmResult)
		}
	})
}
