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
	"errors"
	"math"
)

var (
	ErrDimensionMismatch = errors.New("vector dimensions do not match")
	ErrZeroVector        = errors.New("cannot normalize zero vector")
)

// Vector represents a vector with its embedding
type Vector struct {
	Key    string
	Values []float32
}

// Normalize normalizes a vector to unit length (L2 normalization)
// After normalization, cosine similarity can be computed as a simple dot product
// This is a key optimization mentioned in the design doc
func Normalize(v []float32) ([]float32, error) {
	magnitude := Magnitude(v)
	if magnitude == 0 {
		return nil, ErrZeroVector
	}

	result := make([]float32, len(v))
	for i, val := range v {
		result[i] = val / magnitude
	}
	return result, nil
}

// Magnitude calculates the L2 norm (magnitude) of a vector
func Magnitude(v []float32) float32 {
	var sum float32
	for _, val := range v {
		sum += val * val
	}
	return float32(math.Sqrt(float64(sum)))
}

// DotProduct calculates the dot product of two vectors
// For normalized vectors, this equals the cosine similarity
func DotProduct(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, ErrDimensionMismatch
	}

	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum, nil
}

// CosineSimilarity calculates the cosine similarity between two vectors
// Returns a value between -1 and 1, where 1 means identical direction
func CosineSimilarity(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, ErrDimensionMismatch
	}

	dotProd, err := DotProduct(a, b)
	if err != nil {
		return 0, err
	}

	magA := Magnitude(a)
	magB := Magnitude(b)

	if magA == 0 || magB == 0 {
		return 0, ErrZeroVector
	}

	return dotProd / (magA * magB), nil
}

// EuclideanDistance calculates the Euclidean (L2) distance between two vectors
// Returns the straight-line distance in n-dimensional space
func EuclideanDistance(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, ErrDimensionMismatch
	}

	var sum float32
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}
	return float32(math.Sqrt(float64(sum))), nil
}

// SearchResult represents a single search result with key and similarity score
type SearchResult struct {
	Key        string
	Similarity float32 // Higher is better (for cosine similarity)
	Distance   float32 // Lower is better (for euclidean distance)
}

// TopKHeap is a min-heap for maintaining top-K results efficiently
// This is crucial for the VSEARCH command performance
type TopKHeap []SearchResult

func (h TopKHeap) Len() int { return len(h) }

func (h TopKHeap) Less(i, j int) bool {
	// Min heap based on similarity (lower similarity at root)
	return h[i].Similarity < h[j].Similarity
}

func (h TopKHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *TopKHeap) Push(x interface{}) {
	*h = append(*h, x.(SearchResult))
}

func (h *TopKHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
