#!/usr/bin/env python3

"""Emit a deterministic content/type manifest for every non-ignored worktree path."""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys


def git_paths(root: Path) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        cwd=root,
        check=True,
        stdout=subprocess.PIPE,
    )
    return sorted(set(os.fsdecode(item) for item in result.stdout.split(b"\0") if item))


def hash_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def entry(root: Path, relative: str) -> dict[str, object]:
    path = root / relative
    try:
        metadata = path.lstat()
    except FileNotFoundError:
        return {"path": relative, "type": "missing"}

    mode = stat.S_IMODE(metadata.st_mode)
    base: dict[str, object] = {"path": relative, "mode": f"{mode:04o}"}
    if stat.S_ISREG(metadata.st_mode):
        base.update(type="file", size=metadata.st_size, sha256=hash_file(path))
    elif stat.S_ISLNK(metadata.st_mode):
        target = os.readlink(path)
        base.update(
            type="symlink",
            target=target,
            sha256=hashlib.sha256(os.fsencode(target)).hexdigest(),
        )
    elif stat.S_ISDIR(metadata.st_mode):
        base.update(type="directory")
    else:
        base.update(type="unsupported")
    return base


def main() -> int:
    root = Path(
        subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            check=True,
            stdout=subprocess.PIPE,
            text=True,
        ).stdout.strip()
    )
    print(json.dumps({"format": "simplus-worktree-manifest-v1"}, sort_keys=True))
    for relative in git_paths(root):
        print(json.dumps(entry(root, relative), ensure_ascii=True, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, subprocess.CalledProcessError, UnicodeError) as error:
        print(f"worktree manifest failed: {error}", file=sys.stderr)
        raise SystemExit(1)
