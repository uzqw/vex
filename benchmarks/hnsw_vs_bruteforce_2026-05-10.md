# HNSW vs Brute Force Benchmark - 2026-05-10

This report compares HNSW and brute-force search on production-like dimensions.
The key metric is not just latency: HNSW must also preserve recall@10 against the
brute-force ground truth.

## Methodology

- HNSW and brute force use the exact same prepared vectors.
- Query vectors use a separate deterministic seed (`DefaultSeed + QuerySeedOffset`).
  They are not reused inserted vectors.
- recall@10 is calculated by key/id intersection between HNSW top-10 and
  brute-force top-10, not by approximate score matching.
- Recall validation uses 100 query vectors per dimension.
- Results are workload-specific; re-run the commands on target hardware before
  quoting numbers.

## Environment

- Date: 2026-05-10 / 2026-05-11 follow-up
- OS/Arch: linux/amd64
- CPU: Intel(R) Core(TM) Ultra 9 285H
- Go cache: `/tmp/vex-gocache`
- Top-K: 10
- HNSW defaults: `M=16`, `EfConstruct=600`, `Ef=600`

## High-Dimension recall@10 - 10K Vectors

Command:

```bash
GOCACHE=/tmp/vex-gocache go test -v -count=1 \
  -run='TestHNSWRecallAt10HighDimension' \
  -timeout=20m ./benchmarks/storage/
```

| Dimension | Dataset | Queries | recall@10 | Result |
|----------:|---------|--------:|----------:|--------|
| 1024 | 10K x 1024D | 100 | 96.40% | PASS |
| 1536 | 10K x 1536D | 100 | 95.80% | PASS |

## High-Dimension Standalone Index Search - 10K Vectors

Command:

```bash
GOCACHE=/tmp/vex-gocache go test -run='^$' \
  -bench='BenchmarkIndexSearch_Comparison_(1024D_10K|1536D_10K)/(BruteForce|HNSW)$' \
  -benchtime=200ms -count=1 -benchmem -timeout=15m ./benchmarks/storage/
```

| Dimension | Index | Dataset | ns/op | Search QPS/op-derived | B/op | allocs/op |
|----------:|-------|---------|------:|----------------------:|-----:|----------:|
| 1024 | BruteForce | 10K x 1024D | 11,239,639 | 89 | 4,795 | 164 |
| 1024 | HNSW | 10K x 1024D | 11,183,228 | 89 | 722,939 | 159 |
| 1536 | BruteForce | 10K x 1536D | 14,580,774 | 69 | 4,777 | 164 |
| 1536 | HNSW | 10K x 1536D | 11,444,413 | 87 | 723,312 | 163 |

## Interpretation

- With recall-safe defaults (`EfConstruct=600`, `Ef=600`), HNSW recall@10 is above
  95% for both 1024D and 1536D on 10K vectors with 100 independent queries.
- HNSW is only roughly tied with brute force at 1024D/10K in this standalone run,
  and is moderately faster at 1536D/10K.
- The previous lower-ef configuration was faster but failed high-dimensional
  recall badly, so it is not acceptable for production-like embeddings.
- HNSW still allocates much more memory per query than brute force. The next
  optimization target is reducing traversal memory (`visited`, candidate heaps,
  and result materialization) without reducing recall.
