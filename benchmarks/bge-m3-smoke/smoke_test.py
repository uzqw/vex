#!/usr/bin/env python3
"""bge-m3 semantic search smoke test for Vex.

Embeds real text sentences (SQuAD dev set) with the local Ollama bge-m3 model
(1024-dim), inserts them into a running vex-server with VSET, and verifies that
VSEARCH returns semantically relevant results: a query sentence (held out of
the index) returns sentences from the same article in the top-k.

Usage:
    python3 smoke_test.py [--host localhost] [--port 6379]
                          [--min-sentences 1000] [--batch 32] [--k 5]
                          [--min-top1-rate 0.8]

Requires:
    - a running vex-server:  go run ./cmd/vex-server/main.go
    - Ollama serving bge-m3 at http://localhost:11434
    - network access to download SQuAD dev-v1.1.json on first run
      (cached in ./data/, which is gitignored)

Writes a summary to results.json (committed artifact).
"""

import argparse
import json
import re
import socket
import sys
import time
import urllib.request
from pathlib import Path

SQUAD_URL = "https://rajpurkar.github.io/SQuAD-explorer/dataset/dev-v1.1.json"
OLLAMA_URL = "http://localhost:11434/api/embed"
MODEL = "bge-m3"
EXPECTED_DIM = 1024
DATA_DIR = Path(__file__).parent / "data"


def download_squad() -> str:
    """Return the path to the SQuAD dev JSON, downloading it if needed."""
    DATA_DIR.mkdir(exist_ok=True)
    path = DATA_DIR / "dev-v1.1.json"
    if not path.exists():
        print(f"downloading SQuAD dev set from {SQUAD_URL} ...")
        urllib.request.urlretrieve(SQUAD_URL, path)
    return str(path)


def split_sentences(text: str) -> list[str]:
    return [s.strip() for s in re.split(r"(?<=[.!?])\s+", text) if s.strip()]


def load_sentences(path: str) -> list[tuple[str, str]]:
    """Return [(article_label, sentence)] for every sentence in the dev set."""
    data = json.load(open(path))
    out = []
    for article in data["data"]:
        title = article["title"]
        for para in article["paragraphs"]:
            for s in split_sentences(para["context"]):
                out.append((title, s))
    return out


def embed_batch(inputs: list[str]) -> list[list[float]]:
    body = json.dumps({"model": MODEL, "input": inputs}).encode()
    req = urllib.request.Request(
        OLLAMA_URL, data=body, headers={"Content-Type": "application/json"}
    )
    with urllib.request.urlopen(req, timeout=120) as r:
        return json.load(r)["embeddings"]


def embed_all(texts: list[str], batch: int) -> tuple[list[list[float] | None], set[int]]:
    """Embed texts in batches; return (embeddings, failed_indices).

    Some inputs make bge-m3 emit NaN embeddings, which Ollama rejects with
    HTTP 500 ("unsupported value: NaN"). On a batch failure, retry each item
    individually and skip the ones that still fail.
    """
    embs: list[list[float] | None] = []
    failed: set[int] = set()
    for i in range(0, len(texts), batch):
        chunk = texts[i : i + batch]
        try:
            embs.extend(embed_batch(chunk))
        except urllib.error.HTTPError:
            for j, t in enumerate(chunk):
                try:
                    embs.append(embed_batch([t])[0])
                except urllib.error.HTTPError:
                    failed.add(i + j)
                    embs.append(None)
        print(f"  embedded {min(i + batch, len(texts))}/{len(texts)}")
    return embs, failed


class VexClient:
    """Minimal RESP client for the subset of commands the smoke test needs."""

    def __init__(self, host: str, port: int):
        self.sock = socket.create_connection((host, port), timeout=60)
        self.f = self.sock.makefile("rb")

    def _send(self, parts: list[str]) -> None:
        buf = f"*{len(parts)}\r\n".encode()
        for p in parts:
            b = p.encode()
            buf += f"${len(b)}\r\n".encode() + b + b"\r\n"
        self.sock.sendall(buf)

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

    def clear(self) -> None:
        self._send(["CLEAR"])
        self._read()

    def vset(self, key: str, vec: list[float]) -> None:
        vec_str = "[" + ", ".join(f"{v:.6f}" for v in vec) + "]"
        self._send(["VSET", key, vec_str])
        self._read()

    def vsearch(self, vec: list[float], k: int) -> list[str]:
        vec_str = "[" + ", ".join(f"{v:.6f}" for v in vec) + "]"
        self._send(["VSEARCH", vec_str, str(k)])
        res = self._read()
        return res if isinstance(res, list) else [res]


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--host", default="localhost")
    ap.add_argument("--port", type=int, default=6379)
    ap.add_argument("--min-sentences", type=int, default=1000)
    ap.add_argument("--batch", type=int, default=32)
    ap.add_argument("--k", type=int, default=5)
    ap.add_argument("--min-top1-rate", type=float, default=0.8)
    args = ap.parse_args()

    # Group sentences by article; hold out the last sentence of each article
    # as a query so top results must be *other* sentences from the same
    # article (genuine semantic relevance, not exact match).
    by_article: dict[str, list[str]] = {}
    for label, s in load_sentences(download_squad()):
        by_article.setdefault(label, []).append(s)
    articles = [(label, sents) for label, sents in by_article.items() if len(sents) >= 2]

    inserts: list[tuple[str, str]] = []  # (key, text)
    queries: list[tuple[int, str]] = []  # (article_id, text)
    # Cap per article so the 1000 sentences span many Wikipedia titles
    # (a query for one article should return that article's sentences).
    # Over-collect: some sentences make bge-m3 emit NaN and get skipped.
    per_article = 50
    for aid, (label, sents) in enumerate(articles):
        if len(inserts) >= args.min_sentences + 200:
            break
        sents = sents[: per_article + 1]
        for j, s in enumerate(sents[:-1]):
            inserts.append((f"s:{aid}:{j}", s))
        queries.append((aid, sents[-1]))

    if len(inserts) < args.min_sentences:
        print(f"only {len(inserts)} insertable sentences (< {args.min_sentences}); "
              "need a bigger corpus", file=sys.stderr)
        return 1
    print(f"inserting {len(inserts)} sentences, querying {len(queries)} held-out sentences")

    # Embed everything (inserts + queries) in batches, skipping any text that
    # makes bge-m3 emit a NaN embedding.
    t0 = time.time()
    all_texts = [t for _, t in inserts] + [t for _, t in queries]
    embs, failed = embed_all(all_texts, args.batch)
    embed_secs = time.time() - t0
    if failed:
        print(f"  skipped {len(failed)} texts that produced NaN embeddings")
        ins_failed = {i for i in failed if i < len(inserts)}
        qry_failed = {i - len(inserts) for i in failed if i >= len(inserts)}
        inserts = [x for i, x in enumerate(inserts) if i not in ins_failed]
        queries = [x for i, x in enumerate(queries) if i not in qry_failed]
    embeddings = [e for e in embs if e is not None]
    if len(inserts) < args.min_sentences:
        print(f"only {len(inserts)} insertable sentences after filtering "
              f"(< {args.min_sentences})", file=sys.stderr)
        return 1
    dims = {len(e) for e in embeddings}
    if dims != {EXPECTED_DIM}:
        print(f"expected {EXPECTED_DIM}-dim embeddings, got {dims}", file=sys.stderr)
        return 1
    print(f"embedded {len(embeddings)} texts in {embed_secs:.1f}s, dim={EXPECTED_DIM}")

    ins_emb = embeddings[: len(inserts)]
    qry_emb = embeddings[len(inserts) :]

    # Insert into a fresh server state.
    client = VexClient(args.host, args.port)
    client.clear()
    t0 = time.time()
    for (key, _), emb in zip(inserts, ins_emb):
        client.vset(key, emb)
    insert_secs = time.time() - t0
    print(f"inserted {len(inserts)} vectors in {insert_secs:.1f}s")

    # Search and verify same-article relevance.
    key_to_text = {key: text for key, text in inserts}
    t0 = time.time()
    top1_hits = 0
    topk_hits = 0
    example = None
    for (aid, qtext), emb in zip(queries, qry_emb):
        keys = client.vsearch(emb, args.k)
        prefix = f"s:{aid}:"
        top1_hits += keys[0].startswith(prefix)
        topk_hits += sum(1 for k in keys if k.startswith(prefix))
        if example is None and keys[0].startswith(prefix):
            example = {"query": qtext, "top_results": [key_to_text[k] for k in keys]}
    search_secs = time.time() - t0

    top1_rate = top1_hits / len(queries)
    topk_rate = topk_hits / (len(queries) * args.k)
    passed = top1_rate >= args.min_top1_rate

    summary = {
        "model": MODEL,
        "corpus": "SQuAD dev-v1.1",
        "inserted": len(inserts),
        "queries": len(queries),
        "k": args.k,
        "top1_same_article_rate": round(top1_rate, 4),
        "topk_same_article_rate": round(topk_rate, 4),
        "embed_seconds": round(embed_secs, 1),
        "insert_seconds": round(insert_secs, 1),
        "search_seconds": round(search_secs, 1),
        "passed": passed,
        "example": example,
    }
    (Path(__file__).parent / "results.json").write_text(
        json.dumps(summary, indent=2) + "\n"
    )
    print(json.dumps(summary, indent=2))
    print("PASS" if passed else "FAIL")
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
