# Vex Documentation

Welcome to the Vex documentation! This directory contains detailed guides for building, testing, and optimizing the Vex vector database.

## Documentation Index

### Performance & Optimization
- **[PERFORMANCE.md](PERFORMANCE.md)** - Comprehensive guide to SIMD optimizations, benchmarking, and performance analysis
  - Performance benchmark results
  - How to run performance tests
  - Detailed analysis of speedups
  - Memory efficiency analysis

### Build & Development
- **[BUILD.md](BUILD.md)** - Instructions for compiling SIMD assembly from C code
  - Setting up the build environment
  - Using Docker with goat
  - Generating assembly code
  - Troubleshooting build issues

## Quick Links

### Running Performance Tests

```bash
# Quick dot product benchmark
cd internal/vector
go test -bench=BenchmarkDotProductComparison -benchtime=3s -benchmem

# HNSW index benchmarks
cd benchmarks/storage
go test -bench=BenchmarkIndexSearch_Comparison_128D_10K -benchtime=2s
```

### Building Assembly

```bash
# First, build the Docker image if not already built
docker build -f Dockerfile.goat -t goat-builder:latest .

# Using Docker (recommended)
docker run -it --rm -v ~/wp/vex:/app -w /app/internal/vector/asm goat-builder:latest bash
goat ../c/dot_avx256_amd64.c -O3 -mavx2 -mfma
```

## Performance Summary

Vex achieves **7.8x-9.1x performance improvement** for production-scale vector operations using SIMD assembly:

| Dimension | Speedup | Use Case |
|-----------|---------|----------|
| 768D      | 7.8x    | BERT, sentence transformers |
| 1024D     | 8.5x    | CLIP, ResNet image embeddings |
| 1536D     | 9.1x    | OpenAI text-embedding-ada-002 |
| 2048D     | 9.0x    | Large multi-modal models |

*Tested on Intel Core Ultra 9 285H with AVX2 support*

**Note**: Performance optimizations target production embedding dimensions (768D+). Lower dimensions are supported but rarely used in real-world applications.

## Project Structure

```
vex/
├── docs/                      # Documentation (you are here)
│   ├── README.md             # This file
│   ├── PERFORMANCE.md        # Performance guide
│   └── BUILD.md              # Build instructions
├── internal/
│   ├── vector/               # Vector operations
│   │   ├── c/               # C source for SIMD
│   │   ├── asm/             # Generated assembly
│   │   └── *.go             # Go implementation
│   └── storage/             # Storage layer
│       ├── index_hnsw.go    # HNSW index
│       └── index_bruteforce.go
├── benchmarks/              # Performance benchmarks
│   └── storage/
└── cmd/                     # Command-line tools
```

## Contributing

When contributing performance improvements:

1. **Modify C code** in `internal/vector/c/`
2. **Regenerate assembly** using goat (see BUILD.md)
3. **Run tests**: `go test ./...`
4. **Run benchmarks**: Document performance changes
5. **Update docs**: If changing APIs or performance characteristics

## License Information

Vex is licensed under **Apache License 2.0**.

This project includes SIMD optimization code derived from [Weaviate](https://github.com/weaviate/weaviate) (BSD-3-Clause), which is fully compatible with Apache 2.0.

📄 **License Documentation:**
- [LICENSES.md](LICENSES.md) - Complete license information
- [THIRD_PARTY_LICENSES.md](../THIRD_PARTY_LICENSES.md) - Third-party license texts
- [NOTICE](../NOTICE) - Attribution notices

## Additional Resources

### External Links
- [Weaviate Vector Database](https://github.com/weaviate/weaviate) - Reference implementation for SIMD optimizations
- [Intel Intrinsics Guide](https://www.intel.com/content/www/us/en/docs/intrinsics-guide/index.html) - SIMD instruction reference
- [Go Assembly Documentation](https://go.dev/doc/asm) - Go assembly language reference

### Related Documentation
- Main README: `../README.md` - Project overview and getting started
- API Documentation: Generate with `godoc -http=:6060`

## Getting Help

If you encounter issues:

1. Check the troubleshooting sections in BUILD.md and PERFORMANCE.md
2. Verify your environment matches the prerequisites
3. Run tests with `-v` flag for detailed output
4. Check CPU features: `grep avx2 /proc/cpuinfo`

## License

Apache License 2.0 - See LICENSE file in the project root
