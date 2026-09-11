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

//go:build !noasm && amd64

package storage

import "unsafe"

//go:noescape
func prefetchT0(addr unsafe.Pointer)

// prefetch pulls the first cache line of p toward L1. Best-effort hint; safe
// (and a no-op) to call on any address the process may legally touch.
func prefetch(p []float32) {
	prefetchT0(unsafe.Pointer(unsafe.SliceData(p)))
}
