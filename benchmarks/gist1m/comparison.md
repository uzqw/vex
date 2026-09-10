# GIST1M comparison: Vex vs ann-benchmarks

Vex numbers from [`results.json`](results.json) (this machine, Intel Core Ultra 9 285H).
Public numbers from the published
[gist-960-euclidean (k=10)](https://ann-benchmarks.com/gist-960-euclidean_10_euclidean.html)
Recall vs QPS chart (Chart.js data on that page, retrieved 2026-09-08).

Vex recall@10 is exact cosine on L2-normalized vectors (recomputed GT).
Published hnswlib/faiss rows are L2 / euclidean as on that page; comparison-only,
not the success ruler.

Recall@10 is hardware-independent. QPS is not: ann-benchmarks uses its own
reference hardware, so QPS is indicative only.

## Table

| Library | Metric | Config | recall@10 | QPS |
|---|---|---|---:|---:|
| **Vex** | cosine, L2-normalized | HNSW M=16, ef=600, efC=64, seed=1 | 0.8599 | 300.29 |
| hnswlib | L2 | M=8, efC=500 (nearest published recall) | 0.2940 | 6164.27 |
| hnswlib | L2 | M=8, efC=500 | 0.4122 | 4017.66 |
| hnsw(faiss) | L2 | M=8, efC=500, ef=20 | 0.3777 | 3317.49 |
| hnswlib | L2 | M=24, efC=500 (nearest published QPS) | 0.9899 | 194.78 |
| hnsw(faiss) | L2 | M=8, efC=500, ef=800 | 0.9534 | 149.58 |
| bruteforce-blas | L2 | exact (public) | 1.0000 | 2.63 |

hnswlib chart labels omit the search `ef`; values are copied as published.

## Reading

Vex inserted 999,990 / 1,000,000 base vectors in 860.9s (**1161.6 vec/s**;
10 all-zero vectors skipped). 1,000 official queries, k=10, cosine GT.

Public L2 rows are not a like-for-like recall comparison. QPS is not
comparable across machines.
