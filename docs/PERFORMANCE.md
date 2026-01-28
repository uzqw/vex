# Performance Optimization with SIMD

This document describes the SIMD (Single Instruction, Multiple Data) optimizations implemented in Vex for accelerated vector operations.

## Overview

Vex uses assembly-level SIMD instructions to accelerate dot product calculations, which are the core operation in vector similarity search. The implementation follows Weaviate's approach, using the `goat` tool to convert optimized C code into Go assembly.

## Architecture Support

- **AMD64**: AVX2 (256-bit) and AVX512 (512-bit) SIMD instructions
- **ARM64**: Planned (NEON support)
- **Fallback**: Pure Go implementation when SIMD unavailable

## Performance Results

### Dot Product Benchmarks

Performance comparison between pure Go and SIMD-optimized assembly implementations:

| Dimension | Pure Go (ns/op) | Assembly (ns/op) | Speedup | Memory |
|-----------|-----------------|------------------|---------|--------|
| 128D      | 42.19          | 9.53             | **4.4x** ⚡ | 0 B/op |
| 256D      | 85.58          | 15.28            | **5.6x** ⚡⚡ | 0 B/op |
| 512D      | 178.60         | 26.70            | **6.7x** ⚡⚡⚡ | 0 B/op |
| 1024D     | 431.70         | 51.03            | **8.5x** ⚡⚡⚡ | 0 B/op |

*Tests run on Intel Core Ultra 9 285H with AVX2 support*

### Key Findings

- **Best performance**: 8.5x speedup for 1024-dimensional vectors
- **Zero overhead**: No memory allocations, identical memory usage
- **Consistent gains**: 4-8x improvement across all common dimensions
- **Automatic selection**: Runtime CPU feature detection for optimal implementation

## Running Performance Tests

### Prerequisites

```bash
cd /home/uzqw/wp/vex
```

### Quick Tests

#### 1. Dot Product Core Performance
Compare pure Go vs SIMD assembly for dot product operations:

```bash
cd internal/vector
go test -bench=BenchmarkDotProductComparison -benchtime=3s -benchmem
```

**Expected output:**
```
BenchmarkDotProductComparison/dim_128/Go-16      	63327304	 42.19 ns/op	 0 B/op	 0 allocs/op
BenchmarkDotProductComparison/dim_128/ASM-16     	257979276	  9.53 ns/op	 0 B/op	 0 allocs/op
BenchmarkDotProductComparison/dim_256/Go-16      	24556746	 85.58 ns/op	 0 B/op	 0 allocs/op
BenchmarkDotProductComparison/dim_256/ASM-16     	179489601	 15.28 ns/op	 0 B/op	 0 allocs/op
...
```

#### 2. All Dot Product Benchmarks
Run comprehensive dot product benchmarks across all dimensions:

```bash
cd internal/vector
go test -bench=BenchmarkDotProduct -benchtime=2s -benchmem
```

### HNSW Index Performance

See the real-world impact on HNSW index search operations:

#### 3. Small Dataset (10K vectors, 128D)
```bash
cd benchmarks/storage
go test -bench=BenchmarkIndexSearch_Comparison_128D_10K -benchtime=2s -benchmem
```

#### 4. Medium Dataset (50K vectors, 256D)
```bash
go test -bench=BenchmarkIndexSearch_Comparison_256D_50K -benchtime=2s -benchmem
```

#### 5. Large Dataset (100K vectors, 512D)
```bash
go test -bench=BenchmarkIndexSearch_Comparison_512D_100K -benchtime=1s -benchmem
```

#### 6. Very Large Dataset (50K vectors, 1024D)
```bash
go test -bench=BenchmarkIndexSearch_Comparison_1024D_50K -benchtime=1s -benchmem
```

### All Benchmarks

Run the complete benchmark suite:

```bash
cd benchmarks/storage
go test -bench=. -benchtime=2s -benchmem
```

## Detailed Performance Analysis

### Dimension-Specific Behavior

**Small dimensions (< 32)**: SIMD has overhead for very small vectors. Pure Go may be competitive.

**Medium dimensions (128-256)**: SIMD shows 4-5x speedup. Sweet spot for most applications.

**Large dimensions (512-1024)**: SIMD achieves 6-8x speedup. Best performance for high-dimensional embeddings.

### CPU Feature Detection

The implementation automatically detects available CPU features at runtime:

```go
// Priority order:
1. AVX512 (if available) -> 512-bit SIMD
2. AVX2 (if available)   -> 256-bit SIMD
3. Pure Go (fallback)    -> No SIMD
```

Check your CPU features:
```bash
cd internal/vector
go run -tags debug . # If debug tools available
# Or check manually:
grep -E "avx2|avx512" /proc/cpuinfo
```

## Memory Efficiency

All SIMD implementations maintain the same memory characteristics as pure Go:
- **Zero allocations**: No heap allocations during computation
- **Stack-only**: All operations use stack or registers
- **No overhead**: Same memory footprint as Go implementation

## Integration

The optimized dot product is automatically used throughout Vex:

```go
import "github.com/uzqw/vex/internal/vector"

// Automatically uses best available implementation
result := vector.DotProduct(vec1, vec2)
```

No code changes needed - performance improvement is transparent!

## Benchmarking Tips

### Run with CPU pinning for stable results:
```bash
taskset -c 0 go test -bench=BenchmarkDotProduct -benchtime=5s
```

### Generate benchmark comparison:
```bash
go test -bench=BenchmarkDotProductComparison -benchtime=3s | tee old.txt
# After changes:
go test -bench=BenchmarkDotProductComparison -benchtime=3s | tee new.txt
benchcmp old.txt new.txt  # If benchcmp installed
```

### Profile memory allocations:
```bash
go test -bench=BenchmarkDotProduct -benchmem -memprofile=mem.out
go tool pprof mem.out
```

### Profile CPU usage:
```bash
go test -bench=BenchmarkDotProduct -cpuprofile=cpu.out
go tool pprof cpu.out
```

## Troubleshooting

### Tests fail with "undefined: dot_256"
- Ensure you're on amd64 architecture
- Check build tags are correct
- Try: `go clean -cache && go test`

### Performance not improving
- Verify SIMD is being used: check CPU features
- Ensure you're testing release build: `go test -bench=.`
- Check thermal throttling: `sensors` (if lm-sensors installed)

### CI/CD builds fail
- Tests support `noasm` tag for environments without SIMD
- Run: `go test -tags=noasm ./...`

## References

- [Intel Intrinsics Guide](https://www.intel.com/content/www/us/en/docs/intrinsics-guide/index.html)
- [Go Assembly Documentation](https://go.dev/doc/asm)
- [Weaviate SIMD Implementation](https://github.com/weaviate/weaviate)
- [GOAT Tool](https://github.com/mmcloughlin/goat)
