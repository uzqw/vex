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

// dotProductImplementation is a function pointer that can be overridden
// based on CPU architecture and capabilities. It defaults to pure Go
// but can be optimized to use AVX2, AVX512, or NEON assembly.
var dotProductImplementation func(a, b []float32) float32 = dotProductGo

// dotProductGo is the pure Go implementation of dot product
// This serves as the fallback when no CPU optimizations are available
func dotProductGo(a, b []float32) float32 {
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// DotProductASM returns the assembly-optimized dot product if available,
// or the pure Go fallback otherwise. The implementation is chosen
// at init() time based on CPU capabilities.
func DotProductASM(a, b []float32) float32 {
	return dotProductImplementation(a, b)
}
