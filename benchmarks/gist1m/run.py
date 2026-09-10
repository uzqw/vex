#!/usr/bin/env python3
"""GIST1M insert + recall@10 + QPS against a running vex-server.

Inserts the official 1M base vectors with VSET (key = 0-based index), runs the
1,000 official queries with VSEARCH k=10, and reports mean recall@10 against
exact cosine on L2-normalized vectors (recomputed; official L2 .ivecs are not
the ruler) plus QPS after a warmup pass.

Usage:
    go run ./cmd/vex-server -index hnsw -hnsw-seed 1 -log-level warn
    python3 run.py

Writes results.json (committed artifact). Dataset files live in ./data/ (gitignored).
"""

from __future__ import annotations

import argparse
import array
import ctypes
import ctypes.util
import heapq
import itertools
import json
import math
import socket
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
from prepare import DATA, FILES, find_file, iter_fvecs  # noqa: E402

REPORT = HERE / "results.json"
K = 10
DIM = 960
N_BASE = 1_000_000
N_QUERY = 1_000


def fmt_vec(vec: list[float]) -> str:
    return "[" + ",".join(f"{x:.8g}" for x in vec) + "]"


def encode(parts: list[str]) -> bytes:
    buf = f"*{len(parts)}\r\n".encode()
    for p in parts:
        b = p.encode()
        buf += f"${len(b)}\r\n".encode() + b + b"\r\n"
    return buf


def recall_at_k(pred: list[int], truth: list[int], k: int) -> float:
    return len(set(pred[:k]).intersection(truth[:k])) / k


def _unit(v: list[float]) -> list[float] | None:
    s = math.sqrt(sum(x * x for x in v))
    if s == 0.0:
        return None
    return [x / s for x in v]


def _dot_matrix(qpack: array.array, bpack: array.array, nq: int, nb: int, d: int) -> array.array:
    libname = ctypes.util.find_library("openblaso") or "libopenblaso.so.0"
    fn = ctypes.CDLL(libname).cblas_sgemm
    fn.restype = None
    fn.argtypes = [
        ctypes.c_int,
        ctypes.c_int,
        ctypes.c_int,
        ctypes.c_int,
        ctypes.c_int,
        ctypes.c_int,
        ctypes.c_float,
        ctypes.c_void_p,
        ctypes.c_int,
        ctypes.c_void_p,
        ctypes.c_int,
        ctypes.c_float,
        ctypes.c_void_p,
        ctypes.c_int,
    ]
    out = array.array("f", [0.0]) * (nq * nb)
    fn(
        101,
        111,
        112,  # RowMajor, NoTrans, Trans: scores = Q @ B.T
        nq,
        nb,
        d,
        ctypes.c_float(1.0),
        qpack.buffer_info()[0],
        d,
        bpack.buffer_info()[0],
        d,
        ctypes.c_float(0.0),
        out.buffer_info()[0],
        nb,
    )
    return out


def cosine_gt(queries: list[list[float]], base, k: int) -> list[list[int]]:
    """Top-k ids by cosine on L2-normalized vectors. Skips zero base vectors."""
    bpack = array.array("f")
    ids: list[int] = []
    d = len(queries[0])
    for nseen, (i, vec) in enumerate(base, 1):
        if len(vec) != d:
            raise ValueError(f"base dim {len(vec)} != {d} at {i}")
        u = _unit(vec)
        if u is None:
            continue
        bpack.extend(u)
        ids.append(i)
        if nseen % 100_000 == 0:
            print(f"  normalized {nseen} base vectors")
    qpack = array.array("f")
    for q in queries:
        u = _unit(q)
        if u is None:
            raise ValueError("zero query")
        qpack.extend(u)
    nq, nb = len(queries), len(ids)
    if nb == 0:
        raise ValueError("no non-zero base vectors")
    scores = _dot_matrix(qpack, bpack, nq, nb, d)
    kk = min(k, nb)
    out: list[list[int]] = []
    for qi in range(nq):
        best = heapq.nlargest(kk, range(nb), key=lambda j, qi=qi: (scores[qi * nb + j], -ids[j]))
        out.append([ids[j] for j in best])
    return out


def _self_check() -> None:
    assert recall_at_k([1, 2, 3], [1, 2, 3], 3) == 1.0
    assert recall_at_k([9, 8, 7], [1, 2, 3], 3) == 0.0
    assert recall_at_k([1, 9], [1, 2], 2) == 0.5
    # Raw-L2 nearest is id 0; cosine-on-normalized nearest is id 1.
    gt = cosine_gt([[1.0, 0.0]], [(0, [1.0, 0.1]), (1, [100.0, 0.0])], k=1)
    assert gt == [[1]], gt


class VexClient:
    """Minimal RESP client (same subset as bge-m3-smoke, plus pipelined VSET)."""

    def __init__(self, host: str, port: int):
        self.sock = socket.create_connection((host, port), timeout=600)
        self.sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        self.f = self.sock.makefile("rb")

    def _send(self, parts: list[str]) -> None:
        self.sock.sendall(encode(parts))

    def _read(self):
        line = self.f.readline()
        if not line:
            raise ConnectionError("connection closed by server")
        t, rest = line[:1], line[1:-2]
        if t == b"+":
            return rest.decode()
        if t == b"-":
            raise RuntimeError(rest.decode())
        if t == b":":
            return rest.decode()
        if t == b"$":
            n = int(rest)
            if n == -1:
                return ""
            return self.f.read(n + 2)[:n].decode()
        if t == b"*":
            return [self._read() for _ in range(int(rest))]
        raise RuntimeError(f"unexpected RESP type {t!r}")

    def close(self) -> None:
        try:
            self.sock.close()
        except OSError:
            pass

    def clear(self) -> None:
        self._send(["CLEAR"])
        self._read()

    def vsearch(self, vec: list[float], k: int) -> list[str]:
        self._send(["VSEARCH", fmt_vec(vec), str(k)])
        res = self._read()
        return res if isinstance(res, list) else [res]

    def vset_pipeline(self, items: list[tuple[str, list[float]]]) -> None:
        self.sock.sendall(b"".join(encode(["VSET", key, fmt_vec(vec)]) for key, vec in items))
        for _ in items:
            self._read()


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--host", default="localhost")
    ap.add_argument("--port", type=int, default=6379)
    ap.add_argument("--batch", type=int, default=64)
    ap.add_argument("--k", type=int, default=K)
    ap.add_argument("--n", type=int, default=N_BASE, help="insert first N base vectors (default 1M)")
    ap.add_argument("--start", type=int, default=0, help="skip base[0:start] (implies no CLEAR)")
    ap.add_argument("--prior-seconds", type=float, default=0, help="add to insert_seconds when resuming")
    ap.add_argument("--index", default="hnsw", help="recorded server -index (metadata only)")
    ap.add_argument("--hnsw-m", type=int, default=16)
    ap.add_argument("--hnsw-ef", type=int, default=600)
    ap.add_argument("--hnsw-ef-construction", type=int, default=600)
    ap.add_argument("--hnsw-seed", type=int, default=1)
    args = ap.parse_args()

    _self_check()
    base_path = find_file(DATA, FILES["base"][0])
    query_path = find_file(DATA, FILES["query"][0])

    print("loading queries ...")
    queries = list(iter_fvecs(query_path))
    if len(queries) != N_QUERY:
        print(f"expected {N_QUERY} queries, got {len(queries)}", file=sys.stderr)
        return 1
    if any(len(q) != DIM for q in queries):
        print("query dim mismatch", file=sys.stderr)
        return 1
    print(f"computing cosine ground truth on first {args.n} base vectors ...")
    groundtruth = cosine_gt(
        queries,
        enumerate(itertools.islice(iter_fvecs(base_path), args.n)),
        args.k,
    )

    client = VexClient(args.host, args.port)
    try:
        if args.start == 0:
            client.clear()
        print(f"inserting {args.n} vectors from {args.start} (batch={args.batch}) ...")
        t0 = time.time()
        batch: list[tuple[str, list[float]]] = []
        inserted = 0
        skipped: list[int] = []
        for i, vec in enumerate(iter_fvecs(base_path)):
            if i >= args.n:
                break
            if len(vec) != DIM:
                raise ValueError(f"base dim {len(vec)} != {DIM} at {i}")
            # Vex cosine search rejects zero vectors; GIST1M has a handful.
            if not any(vec):
                skipped.append(i)
                continue
            if i < args.start:
                continue
            batch.append((str(i), vec))
            if len(batch) >= args.batch:
                client.vset_pipeline(batch)
                inserted += len(batch)
                batch = []
                if inserted % 10_000 == 0:
                    elapsed = time.time() - t0
                    rate = inserted / elapsed if elapsed else 0
                    print(f"  inserted {inserted}/{args.n} ({rate:.0f} vec/s)")
        if batch:
            client.vset_pipeline(batch)
            inserted += len(batch)
        this_secs = time.time() - t0
        insert_secs = this_secs + args.prior_seconds
        zeros_before = sum(1 for s in skipped if s < args.start)
        inserted = args.start - zeros_before + inserted
        print(f"inserted {inserted} vectors in {insert_secs:.1f}s, skipped {len(skipped)} zero vectors")

        print(f"warmup {len(queries)} queries ...")
        t0 = time.time()
        for q in queries:
            client.vsearch(q, args.k)
        warmup_secs = time.time() - t0

        print(f"timed {len(queries)} queries k={args.k} ...")
        hits = 0.0
        t0 = time.time()
        for q, gt in zip(queries, groundtruth):
            keys = client.vsearch(q, args.k)
            pred = [int(k) for k in keys]
            hits += recall_at_k(pred, gt, args.k)
        search_secs = time.time() - t0
        recall = hits / len(queries)
        qps = len(queries) / search_secs if search_secs else 0.0
    finally:
        client.close()

    official = args.n == N_BASE
    summary = {
        "dataset": "GIST1M",
        "inserted": inserted,
        "resume_from": args.start,
        "dim": DIM,
        "queries": len(queries),
        "k": args.k,
        "index": args.index,
        "hnsw": {
            "m": args.hnsw_m,
            "ef": args.hnsw_ef,
            "ef_construction": args.hnsw_ef_construction,
            "seed": args.hnsw_seed,
        },
        "skipped_zero_vectors": skipped,
        "insert_seconds": round(insert_secs, 1),
        "insert_vecs_per_sec": round(inserted / insert_secs, 1) if insert_secs else 0,
        "warmup_seconds": round(warmup_secs, 1),
        "search_seconds": round(search_secs, 1),
        "qps": round(qps, 2),
        "recall_at_10": round(recall, 4),
        "groundtruth": "cosine_l2_normalized",
        "official_1m": official,
    }
    REPORT.write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps(summary, indent=2))
    if not official:
        print("NOTE: n != 1M; recall@10 is vs cosine GT on the subset", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
