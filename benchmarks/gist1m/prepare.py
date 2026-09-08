#!/usr/bin/env python3
"""Download GIST1M and parse its .fvecs / .ivecs files.

Yields float32 (fvecs) and int32 (ivecs) arrays that later legs VSET / score
against. Does not insert into Vex.

Usage:
    python3 prepare.py

Requires network on first run (FTP to ftp.irisa.fr). Cache lives in ./data/
(gitignored). Writes dataset.json (committed) with count/dim verification.
"""

from __future__ import annotations

import hashlib
import json
import struct
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path
from typing import Iterator

URL = "ftp://ftp.irisa.fr/local/texmex/corpus/gist.tar.gz"
MD5 = "31185e0f00854f74d27e8ad8d52628a9"
ARCHIVE_BYTES = 2_740_172_684
DATA = Path(__file__).parent / "data"
REPORT = Path(__file__).parent / "dataset.json"

# role -> (filename, count, dim, struct format)
FILES = {
    "base": ("gist_base.fvecs", 1_000_000, 960, "f"),
    "query": ("gist_query.fvecs", 1_000, 960, "f"),
    "groundtruth": ("gist_groundtruth.ivecs", 1_000, 100, "i"),
}


def iter_records(path: Path, fmt: str) -> Iterator[list[float] | list[int]]:
    """Yield one vector per fvecs/ivecs record (little-endian, dim prefix)."""
    width = struct.calcsize(fmt)
    with open(path, "rb") as f:
        while True:
            hdr = f.read(4)
            if not hdr:
                return
            if len(hdr) != 4:
                raise ValueError(f"truncated header in {path}")
            (dim,) = struct.unpack("<i", hdr)
            payload = f.read(dim * width)
            if len(payload) != dim * width:
                raise ValueError(f"truncated vector in {path}")
            yield list(struct.unpack(f"<{dim}{fmt}", payload))


def iter_fvecs(path: Path) -> Iterator[list[float]]:
    return iter_records(path, "f")  # type: ignore[return-value]


def iter_ivecs(path: Path) -> Iterator[list[int]]:
    return iter_records(path, "i")  # type: ignore[return-value]


def find_file(root: Path, name: str) -> Path:
    matches = list(root.rglob(name))
    if not matches:
        raise FileNotFoundError(name)
    return matches[0]


def _self_check() -> None:
    raw = struct.pack("<i3f", 3, 1.0, 0.0, 0.0) + struct.pack("<i3f", 3, 0.0, 1.0, 0.0)
    with tempfile.NamedTemporaryFile(delete=False) as tmp:
        tmp.write(raw)
        path = Path(tmp.name)
    try:
        vecs = list(iter_fvecs(path))
    finally:
        path.unlink(missing_ok=True)
    assert vecs == [[1.0, 0.0, 0.0], [0.0, 1.0, 0.0]], vecs


def download(dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    if dest.exists() and dest.stat().st_size == ARCHIVE_BYTES:
        print(f"archive already present ({dest.stat().st_size} bytes)")
        return
    print(f"downloading {URL}")
    subprocess.check_call(
        ["curl", "-C", "-", "--retry", "5", "--retry-delay", "5", "-o", str(dest), URL]
    )
    if dest.stat().st_size != ARCHIVE_BYTES:
        raise RuntimeError(f"size {dest.stat().st_size} != {ARCHIVE_BYTES}")


def md5_file(path: Path) -> str:
    h = hashlib.md5()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def extract(archive: Path, dest: Path) -> None:
    dest.mkdir(parents=True, exist_ok=True)
    wanted = {spec[0] for spec in FILES.values()}
    present = {p.name for p in dest.rglob("*") if p.is_file()}
    if wanted <= present:
        print("extracted files already present")
        return
    print(f"extracting needed members from {archive.name}")
    kwargs = {"filter": "data"} if sys.version_info >= (3, 12) else {}
    extracted: set[str] = set()
    with tarfile.open(archive, "r:gz") as t:
        while True:
            member = t.next()
            if member is None:
                break
            name = Path(member.name).name
            if name not in wanted:
                continue
            t.extract(member, dest, **kwargs)
            extracted.add(name)
            if extracted >= wanted:
                break
    missing = wanted - extracted
    if missing:
        raise FileNotFoundError(f"archive missing {sorted(missing)}")


def verify(path: Path, n: int, dim: int, fmt: str) -> dict:
    """Parse every record: count, dim, and a 3-component sample of the first vector."""
    width = struct.calcsize(fmt)
    count = 0
    sample = None
    with open(path, "rb") as f:
        while True:
            hdr = f.read(4)
            if not hdr:
                break
            if len(hdr) != 4:
                raise ValueError(f"truncated header in {path.name} at {count}")
            (d,) = struct.unpack("<i", hdr)
            if d != dim:
                raise ValueError(f"{path.name}: dim {d} != {dim} at vector {count}")
            payload = f.read(dim * width)
            if len(payload) != dim * width:
                raise ValueError(f"{path.name}: truncated vector {count}")
            if sample is None:
                sample = list(struct.unpack(f"<{min(3, dim)}{fmt}", payload[: 3 * width]))
            count += 1
    if count != n:
        raise ValueError(f"{path.name}: count {count} != {n}")
    return {"file": path.name, "count": count, "dim": dim, "sample": sample}


def main() -> int:
    _self_check()
    archive = DATA / "gist.tar.gz"
    download(archive)
    digest = md5_file(archive)
    if digest != MD5:
        print(f"md5 mismatch: {digest} != {MD5}", file=sys.stderr)
        return 1
    print(f"md5 ok {digest}")
    extract(archive, DATA)
    report: dict = {
        "source": URL,
        "archive": {"bytes": archive.stat().st_size, "md5": digest},
        "files": {},
    }
    for role, (name, n, dim, fmt) in FILES.items():
        path = find_file(DATA, name)
        print(f"verifying {name} ...")
        report["files"][role] = verify(path, n, dim, fmt)
        print(f"  {report['files'][role]}")
    REPORT.write_text(json.dumps(report, indent=2) + "\n")
    print(f"wrote {REPORT}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
