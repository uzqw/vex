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

package asm

//go:generate goat ../c/dot_avx256_amd64.c -O3 -mavx2 -mfma -mavx512f -mavx512dq -e="-mfloat-abi=hard" -e="-Rpass-analysis=loop-vectorize" -e="-Rpass=loop-vectorize" -e="-Rpass-missed=loop-vectorize"
//go:generate goat ../c/dot_avx512_amd64.c -O3 -mavx2 -mfma -mavx512f -mavx512dq -e="-mfloat-abi=hard" -e="-Rpass-analysis=loop-vectorize" -e="-Rpass=loop-vectorize" -e="-Rpass-missed=loop-vectorize"

import (
	"unsafe"
)

// DotAVX256 computes dot product using AVX2 SIMD instructions
func DotAVX256(x []float32, y []float32) float32 {
	var res float32
	l := len(x)
	dot_256(
		unsafe.Pointer(unsafe.SliceData(x)),
		unsafe.Pointer(unsafe.SliceData(y)),
		unsafe.Pointer(&res),
		unsafe.Pointer(&l))
	return res
}

// DotAVX512 computes dot product using AVX512 SIMD instructions
func DotAVX512(x []float32, y []float32) float32 {
	var res float32
	l := len(x)
	dot_512(
		unsafe.Pointer(unsafe.SliceData(x)),
		unsafe.Pointer(unsafe.SliceData(y)),
		unsafe.Pointer(&res),
		unsafe.Pointer(&l))
	return res
}
