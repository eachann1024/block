#!/usr/bin/env python3
"""Reject store plugin IDs in the host's Go source."""

import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SKIP_DIRS = {"testdata", "store", "docs", ".git", "node_modules", "dist", "build"}


def iter_go_files():
    if (ROOT / ".git").exists():
        paths = subprocess.check_output(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "*.go"],
            cwd=ROOT,
        )
        return sorted(set(paths.decode().split("\0")) - {""})
    return sorted(
        path.relative_to(ROOT).as_posix()
        for path in ROOT.rglob("*.go")
        if not SKIP_DIRS.intersection(path.relative_to(ROOT).parts[:-1])
    )


def main():
    index = json.loads((ROOT / "store/store-index.json").read_text(encoding="utf-8"))
    verified = json.loads((ROOT / "store/verified.json").read_text(encoding="utf-8"))
    plugin_ids = set(verified)
    blocklist = json.loads((ROOT / "store/blocklist.json").read_text(encoding="utf-8"))
    plugin_ids.update(entry for entry in blocklist if "/" not in entry)
    for field in ("apps", "system_apps"):
        plugin_ids.update(app["id"] for app in index.get(field, []))
    if not plugin_ids:
        return 0
    ordered_ids = sorted(plugin_ids, key=lambda plugin_id: (-len(plugin_id), plugin_id))
    pattern = re.compile("|".join(map(re.escape, ordered_ids)))
    failed = False
    for name in iter_go_files():
        rel = Path(name)
        if name.endswith("_test.go") or SKIP_DIRS.intersection(rel.parts[:-1]):
            continue
        path = ROOT / rel
        if not path.is_file():
            continue
        with path.open(encoding="utf-8") as source:
            for line_no, line in enumerate(source, 1):
                for plugin_id in sorted(set(pattern.findall(line))):
                    print(f"{name}:{line_no}: store plugin ID {plugin_id!r} in host Go source")
                    failed = True
    return int(failed)


if __name__ == "__main__":
    raise SystemExit(main())
