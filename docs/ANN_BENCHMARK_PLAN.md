# Vex ANN Benchmark Plan

This document is the plan for benchmarking Vex's approximate nearest neighbor
(ANN) search against a real, public dataset and comparing the results with
published numbers from [ann-benchmarks](https://ann-benchmarks.com/).

## 1. Goal

Measure Vex's search quality (recall) and throughput (QPS) on a large,
real-world vector dataset with an official ground truth, and position the
results against public ANN benchmark numbers for established libraries
(e.g., hnswlib, faiss).

## 2. Dataset choice and rationale

### Primary: GIST1M

- **Source:** [corpus-texmex.irisa.fr](http://corpus-texmex.irisa.fr/) (public,
  no license restrictions for research use).
- **Contents:** 1,000,000 base vectors of 960 dimensions (float32), 1,000 query
  vectors, and an official ground truth of the 100 nearest neighbors per query
  (computed with exact L2 distance).
- **Why GIST1M:**
  - **Dimension fit:** 960-dim is the closest widely used public dataset to
    bge-m3's 1024-dim embeddings, so results are representative of the
    embedding workloads Vex is designed for.
  - **Public ground truth:** recall can be measured against the official
    nearest-neighbor lists without recomputing exact search.
  - **ann-benchmarks comparison:** ann-benchmarks publishes recall@10 vs QPS
    results for GIST1M across many libraries, giving a direct comparison
    target.
  - **Size:** ~3.5 GB download, acceptable for this machine (~997 GB free).

### Fallbacks (if the GIST1M download is permanently unavailable)

| Dataset | Dim | Size | Notes |
|---------|-----|------|-------|
| SIFT1M | 128 | ~500 MB | Same corpus-texmex source, same ground-truth format; smaller and faster to run, but dimension is far from bge-m3. |
| NYTimes-256 | 256 | ~1.5 GB | 300k vectors, 256-dim; available from ann-benchmarks' dataset repository. |

The methodology below is dataset-agnostic; only the dimension and file format
change.

## 3. Benchmark methodology

### 3.1 Data preparation

1. Download the GIST1M archive (`gist.tar.gz`) and extract the `.fvecs` files
   (base, query, ground truth).
2. Convert the base and query `.fvecs` files into the vector format Vex
   accepts (float32 arrays). The conversion tooling lives in
   `benchmarks/gist1m/` and is committed with the benchmark scripts.

### 3.2 Insert

- Insert all 1,000,000 base vectors with `VSET` into a running `vex-server`.
- Record total insert time and insert throughput (vectors/sec).
- Vex normalizes every vector to unit length at insert time
   (`internal/storage/storage.go`), which is required for its cosine search.

### 3.3 Query and metrics

- Run all 1,000 official query vectors with `VSEARCH ... k=10` (the official
  query set satisfies the "at least 1,000 queries" requirement).
- **Recall@10:** for each query, the fraction of the 10 true nearest neighbors
  (exact cosine on L2-normalized vectors, recomputed) that appear in Vex's
  top-10 results. Reported as the mean over all queries. Official L2 `.ivecs`
  are not the ruler.
- **QPS:** number of queries completed per second over the full query set,
  measured after a warmup pass.

### 3.4 Cosine search vs L2 ground truth

Vex searches by cosine on L2-normalized vectors. GIST1M base vectors are not
unit length, so cosine ranking is not the same as raw L2. The success ruler
is exact cosine on L2-normalized vectors (recomputed GT). Official L2 `.ivecs`
and published hnswlib/faiss L2 numbers are comparison-only.

### 3.5 Reproducibility

- Deterministic HNSW seed (`-hnsw-seed`) and fixed index parameters (M, ef,
  efConstruction) are recorded with the results.
- The benchmark script, dataset checksums, and result artifacts are committed
  under `benchmarks/gist1m/`.

## 4. Comparison approach against public ann-benchmarks numbers

1. **Collect public numbers:** ann-benchmarks publishes recall@10 vs QPS
   scatter plots for GIST1M for hnswlib, faiss, and other libraries. The
   published per-library best points (or the curve) are the comparison target.
2. **Report Vex's point(s):** run Vex with a few index configurations
   (e.g., different `ef` values) and report each (recall@10, QPS) pair.
3. **Produce a comparison table:** one row per library/configuration with
   recall@10, QPS, and the index parameters used, plus Vex's brute-force
   baseline (recall@10 = 1.0) as an internal consistency check.
4. **State the caveats:**
   - ann-benchmarks runs on specific reference hardware; Vex runs on this
     machine (Intel Core Ultra 9 285H). QPS is indicative, not directly
     comparable across machines; recall is hardware-independent.
   - Index parameters differ per library; the table records each
     configuration so comparisons are apples-to-apples where possible.

## 5. Related work in this repository

- `docs/HNSW_vs_BRUTEFORCE.md` — existing internal HNSW vs brute-force
  comparison on synthetic data (10K–100K vectors, 128–1024 dims).
- `benchmarks/README.md` — existing unit and integration benchmark harness.
- This plan extends that work to a real public dataset with official ground
  truth and external comparison.

## 6. Deliverables

- `benchmarks/gist1m/` — download/convert/benchmark scripts.
- `benchmarks/gist1m/results.json` — Vex GIST1M insert/recall@10/QPS run.
- `benchmarks/gist1m/comparison.md` — comparison table vs public ann-benchmarks.
- This plan document, including the results section below.

## 7. Results

Vex GIST1M (HNSW M=16, ef=600, efC=64, seed=1): **recall@10 = 0.8599** (cosine
GT), **QPS = 300.29**, insert **1161.6 vec/s** on Intel Core Ultra 9 285H.
Full table and source citations:
[`benchmarks/gist1m/comparison.md`](../benchmarks/gist1m/comparison.md).

| Library | Metric | Config | recall@10 | QPS |
|---|---|---|---:|---:|
| **Vex** | cosine, L2-normalized | HNSW M=16, ef=600, efC=64, seed=1 | 0.8599 | 300.29 |
| hnswlib | L2 | M=8, efC=500 | 0.4122 | 4017.66 |
| hnsw(faiss) | L2 | M=8, efC=500, ef=20 | 0.3777 | 3317.49 |
| hnswlib | L2 | M=24, efC=500 | 0.9899 | 194.78 |
| hnsw(faiss) | L2 | M=8, efC=500, ef=800 | 0.9534 | 149.58 |

Public rows are L2 from
[gist-960-euclidean (k=10)](https://ann-benchmarks.com/gist-960-euclidean_10_euclidean.html).
QPS is not comparable across machines; recall is not the same metric.
