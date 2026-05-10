# HNSW vs Brute Force Benchmark - 2026-05-10

This report compares the current HNSW search path with brute-force search after
HNSW parameter tuning and query-allocation cleanup. Results are workload-specific;
re-run the commands below on the target deployment hardware before quoting numbers.

## Environment

- Date: 2026-05-10
- OS/Arch: linux/amd64
- CPU: Intel(R) Core(TM) Ultra 9 285H
- Go cache: `/tmp/vex-gocache`
- Vector dimension: 128
- Top-K: 10
- HNSW defaults: `M=16`, `EfConstruct=128`, `Ef=64`

## Standalone Index Search Benchmark

Command:

```bash
GOCACHE=/tmp/vex-gocache go test -run='^$' \
  -bench='BenchmarkIndexSearch_Comparison_128D_10K/(BruteForce|HNSW)$' \
  -benchtime=200ms -count=1 -benchmem -timeout=5m ./benchmarks/storage/
```

| Index | Dataset | ns/op | Search QPS/op-derived | B/op | allocs/op |
|-------|---------|------:|----------------------:|-----:|----------:|
| BruteForce | 10K x 128D | 535,217 | 1,868 | 4,774 | 163 |
| HNSW | 10K x 128D | 516,407 | 1,936 | 169,503 | 107 |

HNSW is slightly faster on this run, but still allocates far more bytes per
query than brute force. More HNSW memory work remains.

## End-to-End Server Search Benchmark - 1K Vectors

Commands use `-concurrency=8 -n=2000 -dim=128 -prepare-n=1000 -warmup=200 -k=10 -seed=42`.

| Index | Prepared Vectors | QPS | Avg | P50 | P95 | P99 | Max | Errors |
|-------|-----------------:|----:|----:|----:|----:|----:|----:|-------:|
| BruteForce index | 1,000 | 24,668 | 270.134us | 214.277us | 602.486us | 1.1977ms | 2.248184ms | 0 |
| HNSW index | 1,000 | 9,858 | 764.793us | 661.592us | 1.582578ms | 2.146866ms | 2.634946ms | 0 |
| Auto (`threshold=10000`) | 1,000 | 47,561 | 117.065us | 96.092us | 267.952us | 448.137us | 783.414us | 0 |

At small sizes, sharded full scan (`auto` below threshold) wins because it avoids
HNSW traversal overhead and uses SIMD dot products across storage shards.

## End-to-End Server Search Benchmark - 10K Vectors

Commands use `-concurrency=8 -n=1000 -dim=128 -prepare-n=10000 -warmup=100 -k=10 -seed=42`.

| Index | Prepared Vectors | QPS | Avg | P50 | P95 | P99 | Max | Errors |
|-------|-----------------:|----:|----:|----:|----:|----:|----:|-------:|
| BruteForce index | 10,000 | 4,460 | 1.722033ms | 1.428653ms | 3.707043ms | 4.618198ms | 6.857669ms | 0 |
| HNSW index | 10,000 | 5,951 | 1.273183ms | 1.010226ms | 2.881913ms | 5.16373ms | 6.940113ms | 0 |

HNSW starts to pull ahead around this data size with the tuned defaults, though
P99 still needs attention.

## Recall Smoke Test

Command:

```bash
GOCACHE=/tmp/vex-gocache go test -v -count=1 \
  -run='TestHNSWRecallAccuracy/1K-128D' -timeout=5m ./benchmarks/storage/
```

Result: recall@1, @5, @10, @50, and @100 all passed; average recall was ~99.78%.

## Interpretation

- The old defaults (`M=32`, `EfConstruct=600`, `Ef=600`) were too expensive for
  small and mid-sized datasets.
- Tuned defaults make 10K/128D HNSW competitive, but brute force remains better
  for very small datasets.
- `-index=auto` uses sharded full-scan search below `-auto-index-min-vectors` and
  HNSW above it. This preserves small-dataset latency while allowing HNSW at
  larger sizes.
- HNSW still has high B/op, so query traversal memory optimization remains the
  next major target.
