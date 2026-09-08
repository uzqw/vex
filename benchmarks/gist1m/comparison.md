# GIST1M comparison: Vex vs ann-benchmarks

Vex numbers from [`results.json`](results.json) (this machine, Intel Core Ultra 9 285H).
Public numbers from the published
[gist-960-euclidean (k=10)](https://ann-benchmarks.com/gist-960-euclidean_10_euclidean.html)
Recall vs QPS chart (Chart.js data on that page, retrieved 2026-09-08).

Recall@10 is hardware-independent. QPS is not: ann-benchmarks uses its own
reference hardware, so QPS is indicative only.

## Table

| Library | Config | recall@10 | QPS |
|---|---|---:|---:|
| **Vex** | HNSW M=16, ef=600, efC=600, seed=1 | 0.3479 | 162.68 |
| hnswlib | M=8, efC=500 (nearest published recall) | 0.2940 | 6164.27 |
| hnswlib | M=8, efC=500 | 0.4122 | 4017.66 |
| hnsw(faiss) | M=8, efC=500, ef=20 | 0.3777 | 3317.49 |
| hnswlib | M=24, efC=500 (nearest published QPS) | 0.9899 | 194.78 |
| hnsw(faiss) | M=8, efC=500, ef=800 | 0.9534 | 149.58 |
| bruteforce-blas | exact (public) | 1.0000 | 2.63 |

hnswlib chart labels omit the search `ef`; values are copied as published.

## Reading

At Vex's recall (~0.35), published HNSW is thousands of QPS. At Vex's QPS
(~163), published HNSW is recall@10 ≈ 0.95–0.99. Vex did not run a GIST1M
brute-force baseline; the public `bruteforce-blas` row is the exact check.

Vex inserted 999,990 / 1,000,000 base vectors (10 all-zero vectors skipped;
Vex cannot normalize them). 1,000 official queries, k=10.
