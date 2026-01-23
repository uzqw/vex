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

import "github.com/uzqw/vex/internal/vector"

// Index defines the interface for vector search indices.
// Different implementations (brute force, HNSW, etc.) can satisfy this interface.
type Index interface {
	// Insert adds a vector to the index
	Insert(key string, vec []float32) error

	// Search finds the top-k most similar vectors to the query
	Search(query []float32, k int) ([]vector.SearchResult, error)

	// Delete removes a vector from the index
	Delete(key string) error

	// Clear removes all vectors from the index
	Clear()

	// Count returns the total number of vectors in the index
	Count() int
}
