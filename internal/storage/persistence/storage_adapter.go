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

package persistence

import "fmt"

// StorageAdapter adapts storage.Storage to VectorDataSource interface
type StorageAdapter struct {
	storage StorageInterface
}

// StorageInterface defines the methods needed from storage.Storage
type StorageInterface interface {
	// Get retrieves a vector by key
	Get(key string) ([]float32, bool)

	// Set stores a vector with the given key
	Set(key string, values []float32) error

	// Count returns the total number of vectors stored
	Count() int

	// Dimension returns the expected vector dimension
	Dimension() int

	// GetAllKeys returns all keys (need to add this method to storage.Storage)
	GetAllKeys() []string
}

// NewStorageAdapter creates a new storage adapter
func NewStorageAdapter(storage StorageInterface) *StorageAdapter {
	return &StorageAdapter{
		storage: storage,
	}
}

// GetAllVectors implements VectorDataSource.GetAllVectors
func (a *StorageAdapter) GetAllVectors() (map[string][]float32, error) {
	keys := a.storage.GetAllKeys()
	vectors := make(map[string][]float32, len(keys))

	for _, key := range keys {
		vec, ok := a.storage.Get(key)
		if !ok {
			// Key was deleted between GetAllKeys and Get, skip it
			continue
		}
		// Make a copy to avoid holding references to internal storage
		vecCopy := make([]float32, len(vec))
		copy(vecCopy, vec)
		vectors[key] = vecCopy
	}

	return vectors, nil
}

// SetAllVectors implements VectorDataSource.SetAllVectors
func (a *StorageAdapter) SetAllVectors(vectors map[string][]float32) error {
	for key, vec := range vectors {
		if err := a.storage.Set(key, vec); err != nil {
			return fmt.Errorf("failed to set vector %s: %w", key, err)
		}
	}
	return nil
}

// GetDimension implements VectorDataSource.GetDimension
func (a *StorageAdapter) GetDimension() int {
	return a.storage.Dimension()
}
