# Third Party Licenses

This document contains the licenses for third-party software used in Vex.

## Summary

| Component | License | Usage |
|-----------|---------|-------|
| Weaviate SIMD Code | BSD-3-Clause | SIMD-optimized vector operations (C source files) |

---

## Weaviate SIMD Optimization Code

**License:** BSD-3-Clause
**Copyright:** (c) 2020-2025, Weaviate B.V.
**Source:** https://github.com/weaviate/weaviate
**Files:**
- `internal/vector/c/dot_avx256_amd64.c`
- `internal/vector/c/dot_avx512_amd64.c`

### BSD-3-Clause License

```
Copyright (c) 2020-2025, Weaviate B.V.
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its
   contributors may be used to endorse or promote products derived from
   this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### Usage

The C source files listed above are derived from Weaviate's SIMD-optimized
vector operations. These files are used to generate Go assembly code using
the `goat` tool for high-performance dot product calculations.

The generated assembly files (`internal/vector/asm/*.s`) are considered
derivative works and also fall under the BSD-3-Clause license for the portions
derived from Weaviate code.

### License Compatibility

The BSD-3-Clause license is compatible with Apache 2.0. The BSD-3-Clause is
a permissive license that allows:
- Use in commercial projects
- Modification and distribution
- Use in Apache 2.0 licensed projects

The main requirement is attribution (which is provided in this document and
in the source file headers).

---

## License Compatibility Matrix

| Original License | Vex License (Apache 2.0) | Compatible? | Notes |
|------------------|--------------------------|-------------|-------|
| BSD-3-Clause     | Apache 2.0              | ✓ Yes       | Must retain copyright notices |

---

## Additional Information

For questions about licensing, please contact the project maintainers or
refer to:
- Apache License 2.0: https://www.apache.org/licenses/LICENSE-2.0
- BSD-3-Clause License: https://opensource.org/licenses/BSD-3-Clause
- License Compatibility: https://www.apache.org/legal/resolved.html
