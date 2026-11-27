#!/usr/bin/env python3
"""Reject store plugin IDs in the host's Go source."""

import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SKIP_DIRS = {"testdata", "store", "docs"}
ALLOWED_LINES = {
    # Hide historical sync recovery directories in existing notebooks. Their
    # names are already on disk and may survive an upgrade or uninstall.
    ("internal/files/scan.go", '".block-sync-replaced": true,'),
    ("internal/files/scan.go", '".block-sync-recovery": true,'),
}


def main():
    index = json.loads((ROOT / "store/store-index.json").read_text(encoding="utf-8"))
    verified = json.loads((ROOT / "store/verified.json").read_text(encoding="utf-8"))
    plugin_ids = set(verified)
    for field in ("apps", "system_apps"):
        plugin_ids.update(app["id"] for app in index.get(field, []))
    if not plugin_ids:
        return 0
    ordered_ids = sorted(plugin_ids, key=lambda plugin_id: (-len(plugin_id), plugin_id))
    pattern = re.compile("|".join(map(re.escape, ordered_ids)))
    paths = subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "*.go"],
        cwd=ROOT,
    )
    failed = False
    for name in sorted(set(paths.decode().split("\0")) - {""}):
        rel = Path(name)
        if name.endswith("_test.go") or SKIP_DIRS.intersection(rel.parts[:-1]):
            continue
        path = ROOT / rel
        if not path.is_file():
            continue
        with path.open(encoding="utf-8") as source:
            for line_no, line in enumerate(source, 1):
                if (name, line.strip()) in ALLOWED_LINES:
                    continue
                for plugin_id in sorted(set(pattern.findall(line))):
                    print(f"{name}:{line_no}: store plugin ID {plugin_id!r} in host Go source")
                    failed = True
    return int(failed)


if __name__ == "__main__":
    raise SystemExit(main())
