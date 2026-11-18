#!/usr/bin/env python3
"""Index cmd/**/*.go and internal/**/*.go for one-step lookup.

Tracks exported Go types, functions, methods, and ui.Probe / ui.RecordBounds calls.
Queries compare file stamps and reread only added, removed, or modified sources.
The readable map is .ai-map.md; stamps live in tmp/ai-map.cache.json.
"""

import argparse
import json
import os
import re
import sys
import tempfile
import time
from pathlib import Path

CACHE_VERSION = 1
TRACKED_TYPES = ("Workspace", "Session", "Manager", "Store")
MAX_BLOCKS = 80
MAX_NAMES = 12
MAX_PROBES = 12
MAX_SIGS = 16

TYPE_PATTERN = re.compile(r"^type\s+([A-Z][A-Za-z0-9_]*)\b(.*)$")
FUNC_PATTERN = re.compile(r"^func\s+([A-Z][A-Za-z0-9_]*)\b(.*)$")
METHOD_PATTERN = re.compile(r"^func\s+\(([^)]+)\)\s+([A-Za-z0-9_]+)\b(.*)$")
PROBE_PATTERN = re.compile(r"(?:blockui\.|ui\.)?(Probe|RecordBounds)\s*\(")


def find_repo_root() -> Path:
    current = Path(__file__).resolve().parent
    while current != current.parent:
        if (current / "go.mod").exists() and (current / "internal").exists():
            return current
        current = current.parent
    return Path.cwd().resolve()


def _extract_balanced_args(line: str, start_pos: int) -> str:
    depth = 1
    cur = start_pos
    in_quote = ""
    while cur < len(line) and depth > 0:
        ch = line[cur]
        if in_quote:
            if ch == "\\" and in_quote == '"':
                cur += 2
                continue
            if ch == in_quote:
                in_quote = ""
        else:
            if ch in ('"', "`"):
                in_quote = ch
            elif ch == "(":
                depth += 1
            elif ch == ")":
                depth -= 1
        cur += 1
    if depth == 0:
        return line[start_pos : cur - 1].strip()
    return line[start_pos:].strip()


def _split_top_level_comma(expr: str) -> list[str]:
    parts: list[str] = []
    depth = 0
    in_quote = ""
    start = 0
    i = 0
    while i < len(expr):
        ch = expr[i]
        if in_quote:
            if ch == "\\" and in_quote == '"':
                i += 2
                continue
            if ch == in_quote:
                in_quote = ""
        else:
            if ch in ('"', "`"):
                in_quote = ch
            elif ch in "([{":
                depth += 1
            elif ch in ")]}":
                depth = max(0, depth - 1)
            elif ch == "," and depth == 0:
                parts.append(expr[start:i].strip())
                start = i + 1
        i += 1
    tail = expr[start:].strip()
    if tail:
        parts.append(tail)
    return parts


def _clean_probe_id(arg: str) -> str:
    arg = arg.strip()
    if len(arg) >= 2 and ((arg[0] == '"' and arg[-1] == '"') or (arg[0] == "`" and arg[-1] == "`")):
        return arg[1:-1]
    return arg


def _receiver_type(recv: str) -> str:
    tokens = recv.strip().split()
    if not tokens:
        return ""
    ty = tokens[-1].lstrip("*")
    if "[" in ty:
        ty = ty.split("[", 1)[0]
    return ty


def process_go_file(file_path: Path, repo_root: Path) -> dict:
    rel_path = file_path.relative_to(repo_root).as_posix()
    try:
        content = file_path.read_text(encoding="utf-8", errors="ignore")
    except OSError as exc:
        return {
            "path": rel_path,
            "package": "",
            "lines": 0,
            "sigs": [],
            "methods": [],
            "probes": [],
            "error": str(exc),
        }

    lines = content.splitlines()
    pkg = ""
    sigs = []
    methods = []
    probes = []

    for idx, raw_line in enumerate(lines, 1):
        sline = raw_line.strip()
        if not pkg and sline.startswith("package "):
            pkg = sline.split()[1] if len(sline.split()) > 1 else ""
        if sline.startswith("//") or sline.startswith("/*") or sline.startswith("*"):
            continue

        # Top-level declarations (no leading whitespace)
        if raw_line and not raw_line[0].isspace():
            if tm := TYPE_PATTERN.match(raw_line):
                sig_text = raw_line.split("{")[0].strip()
                sigs.append({"line": idx, "kind": "type", "name": tm.group(1), "sig": sig_text})
            elif fm := FUNC_PATTERN.match(raw_line):
                sig_text = raw_line.split("{")[0].strip()
                sigs.append({"line": idx, "kind": "func", "name": fm.group(1), "sig": sig_text})
            elif mm := METHOD_PATTERN.match(raw_line):
                recv_type = _receiver_type(mm.group(1))
                mname = mm.group(2)
                sig_text = raw_line.split("{")[0].strip()
                is_exported = mname[:1].isupper()
                if is_exported:
                    sigs.append({"line": idx, "kind": "method", "name": f"{recv_type}.{mname}", "sig": sig_text})
                if recv_type in TRACKED_TYPES or is_exported:
                    methods.append({
                        "line": idx,
                        "type": recv_type,
                        "name": mname,
                        "vis": "exported" if is_exported else "private",
                        "sig": sig_text,
                    })

        for match in PROBE_PATTERN.finditer(raw_line):
            fn_name = match.group(1)
            prefix = raw_line[: match.start()].strip()
            if prefix.startswith("func "):
                continue
            args_raw = _extract_balanced_args(raw_line, match.end())
            args = _split_top_level_comma(args_raw)
            if fn_name == "Probe":
                id_arg = args[1] if len(args) >= 2 else (args[0] if args else "")
            else:
                id_arg = args[0] if args else ""
            if not id_arg:
                continue
            probes.append({
                "line": idx,
                "fn": fn_name,
                "arg": id_arg,
                "id_hint": _clean_probe_id(id_arg),
            })

    return {
        "path": rel_path,
        "package": pkg,
        "lines": len(lines),
        "sigs": sigs,
        "methods": methods,
        "probes": probes,
    }


def generate_markdown(all_data: list[dict]) -> str:
    total_files = len(all_data)
    total_lines = sum(d["lines"] for d in all_data)
    total_sigs = sum(len(d["sigs"]) for d in all_data)
    total_probes = sum(len(d["probes"]) for d in all_data)

    out = [
        "# Block Go - AI Codebase Index & Map",
        "",
        "> **Auto-generated by `scripts/gen_ai_map.py`**. Do not edit manually.",
        "> Tracks `cmd/**/*.go` and `internal/**/*.go`: exported types/functions/methods and `Probe` / `RecordBounds` IDs.",
        "",
        "## Index Summary",
        f"- **Total Go Files**: {total_files}",
        f"- **Total Lines of Code**: {total_lines}",
        f"- **Total Exported Signatures**: {total_sigs}",
        f"- **Total Smoke Probes**: {total_probes}",
        "",
        "## Master Smoke Probe Index",
        "Locate any UI element or test probe target instantly by ID or pattern:",
        "",
        "| Probe ID / Expression | File | Line | Call Pattern |",
        "|---|---|---|---|",
    ]

    flat_probes = []
    for data in all_data:
        for probe in data["probes"]:
            flat_probes.append((probe["id_hint"], data["path"], probe["line"], f"{probe['fn']}({probe['arg']})"))
    flat_probes.sort(key=lambda item: (item[0], item[1], item[2]))
    for hint, fpath, line, call in flat_probes:
        out.append(f"| `{hint.replace('|', '\\|')}` | [`{fpath}`]({fpath}) | {line} | `{call.replace('|', '\\|')}` |")
    out.append("")

    out.append("## Tracked Type Methods")
    out.append("Methods on `Workspace`, `Session`, `Manager`, and `Store`.")
    out.append("")
    for type_name in TRACKED_TYPES:
        rows = []
        for data in all_data:
            for m in data["methods"]:
                if m["type"] == type_name:
                    rows.append((data["path"], m))
        if not rows:
            continue
        out.append(f"### `{type_name}` ({len(rows)} methods)")
        out.append("")
        for path, m in rows:
            out.append(f"- `{path}:{m['line']}` {m['vis']} `{m['name']}`")
        out.append("")

    out.append("## Detailed File Index")
    out.append("")
    categories: dict[str, list[dict]] = {}
    for data in all_data:
        parts = data["path"].split("/")
        cat = "/".join(parts[:2]) if len(parts) >= 2 else parts[0]
        categories.setdefault(cat, []).append(data)

    for cat in sorted(categories):
        out.append(f"### Package Group: `{cat}`")
        out.append("")
        for data in categories[cat]:
            out.append(f"#### [`{data['path']}`]({data['path']}) ({data['lines']} lines, package `{data['package'] or '?'}`)")
            if not data["sigs"] and not data["probes"]:
                out.append("- *No exported declarations or probes*")
                out.append("")
                continue
            if data["probes"]:
                out.append("- **Smoke Probes**:")
                for probe in data["probes"]:
                    out.append(f"  - L{probe['line']}: `{probe['fn']}({probe['arg']})`")
            if data["sigs"]:
                out.append("- **Exported Signatures**:")
                for sig in data["sigs"]:
                    out.append(f"  - L{sig['line']}: `{sig['sig']}`")
            out.append("")

    return "\n".join(out) + "\n"


def _cache_path(repo_root: Path) -> Path:
    return repo_root / "tmp" / "ai-map.cache.json"


def _load_cache(repo_root: Path) -> dict:
    path = _cache_path(repo_root)
    if not path.is_file():
        return {}
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    if payload.get("version") != CACHE_VERSION or not isinstance(payload.get("files"), dict):
        return {}
    return payload["files"]


def _atomic_write(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent, prefix=path.name + ".", delete=False) as handle:
        tmp = Path(handle.name)
        try:
            handle.write(text)
        except BaseException:
            tmp.unlink(missing_ok=True)
            raise
    try:
        os.replace(tmp, path)
    finally:
        tmp.unlink(missing_ok=True)


def _source_files(repo_root: Path) -> list[Path]:
    files: list[Path] = []
    for root_name in ("cmd", "internal"):
        d = repo_root / root_name
        if d.is_dir():
            files.extend(d.glob("**/*.go"))
    return sorted(files)


def sync_index(repo_root: Path, force: bool = False) -> dict:
    files = _source_files(repo_root)
    stamps = {}
    for file_path in files:
        st = file_path.stat()
        stamps[file_path.relative_to(repo_root).as_posix()] = (st.st_mtime_ns, st.st_size)
    old = {} if force else _load_cache(repo_root)
    records = {}
    added, modified, read = [], [], []
    for rel, stamp in stamps.items():
        prev = old.get(rel)
        cached = None if prev is None else prev.get("record")
        fresh = (
            not force
            and prev
            and prev.get("mtime_ns") == stamp[0]
            and prev.get("size") == stamp[1]
            and isinstance(cached, dict)
            and "sigs" in cached
            and "methods" in cached
        )
        if fresh:
            records[rel] = cached
            continue
        if prev is None:
            added.append(rel)
        else:
            modified.append(rel)
        records[rel] = process_go_file(repo_root / rel, repo_root)
        read.append(rel)
    removed = sorted(rel for rel in old if rel not in stamps)
    ordered = [records[rel] for rel in sorted(records)]
    map_path = repo_root / ".ai-map.md"
    rewritten = bool(read or removed or not map_path.is_file())
    if rewritten:
        _atomic_write(map_path, generate_markdown(ordered))
        payload = {
            "version": CACHE_VERSION,
            "files": {
                rel: {"mtime_ns": stamps[rel][0], "size": stamps[rel][1], "record": records[rel]}
                for rel in stamps
            },
        }
        _atomic_write(_cache_path(repo_root), json.dumps(payload, ensure_ascii=False))
    return {
        "added": added,
        "modified": modified,
        "removed": removed,
        "read": read,
        "unchanged": len(stamps) - len(read),
        "rewritten": rewritten,
        "records": ordered,
    }


def _status_line(report: dict) -> str:
    if not report["read"] and not report["removed"]:
        return f"index: up to date ({report['unchanged']} files)"
    if report["unchanged"] == 0 and not report["modified"] and not report["removed"]:
        return f"index: cached {len(report['added'])} files"
    return (
        f"index: reread {len(report['read'])} "
        f"(added {len(report['added'])}, changed {len(report['modified'])}), "
        f"removed {len(report['removed'])}, skipped {report['unchanged']}"
    )


def _contains(text: str, terms: list[str]) -> bool:
    low = text.lower()
    return any(term in low for term in terms)


def render_query(terms: list[str], records: list[dict]) -> str:
    terms_l = [term.strip().lower() for term in terms if term.strip()]
    if not terms_l:
        return "No search terms."
    lines = [f"# query: {' '.join(terms)}", ""]

    probe_rows = []
    for rec in records:
        for probe in rec["probes"]:
            blob = f"{probe['id_hint']} {probe['fn']} {probe['arg']}"
            if not _contains(blob, terms_l):
                continue
            probe_rows.append(f"- `{rec['path']}:{probe['line']}` `{probe['fn']}({probe['arg']})`")
            if len(probe_rows) >= MAX_PROBES:
                break
        if len(probe_rows) >= MAX_PROBES:
            probe_rows.append("- +more probes in .ai-map.md")
            break
    if probe_rows:
        lines.append("### Probes")
        lines.extend(probe_rows)
        lines.append("")

    method_rows = []
    for rec in records:
        for m in rec["methods"]:
            blob = f"{rec['path']} {m['type']} {m['name']} {m['sig']}"
            if not _contains(blob, terms_l):
                continue
            method_rows.append(f"- `{rec['path']}:{m['line']}` `({m['type']}) {m['name']}` ({m['vis']})")
            if len(method_rows) >= MAX_BLOCKS:
                break
        if len(method_rows) >= MAX_BLOCKS:
            method_rows.append("- +more methods in .ai-map.md")
            break
    if method_rows:
        lines.append("### Methods")
        lines.extend(method_rows)
        lines.append("")

    sig_rows = []
    for rec in records:
        for sig in rec["sigs"]:
            blob = f"{rec['path']} {sig['name']} {sig['sig']}"
            if not _contains(blob, terms_l):
                continue
            sig_rows.append(f"- `{rec['path']}:{sig['line']}` `{sig['sig']}`")
            if len(sig_rows) >= MAX_SIGS:
                break
        if len(sig_rows) >= MAX_SIGS:
            sig_rows.append("- +more signatures in .ai-map.md")
            break
    if sig_rows:
        lines.append("### Signatures")
        lines.extend(sig_rows)
        lines.append("")

    if len(lines) == 2:
        return f"No matches for '{' '.join(terms)}'."
    return "\n".join(lines).rstrip() + "\n"


def main() -> None:
    repo_root = find_repo_root()
    parser = argparse.ArgumentParser(description="Code map for block-go")
    parser.add_argument("-q", "--query", nargs="+", metavar="TERM", help="Look up signatures, methods, or probes")
    parser.add_argument("--rebuild", action="store_true", help="Reread every Go source")
    args = parser.parse_args()
    if args.query:
        started = time.perf_counter()
        report = sync_index(repo_root, force=args.rebuild)
        print(_status_line(report))
        print(render_query(args.query, report["records"]).rstrip())
        print(f"({(time.perf_counter() - started) * 1000:.1f}ms)")
        return
    report = sync_index(repo_root, force=args.rebuild)
    data = report["records"]
    print(_status_line(report))
    print(f"map: {repo_root / '.ai-map.md'}")
    print(f"  files {len(data)}")
    print(f"  lines {sum(item['lines'] for item in data)}")
    print(f"  signatures {sum(len(item['sigs']) for item in data)}")
    print(f"  probes {sum(len(item['probes']) for item in data)}")


if __name__ == "__main__":
    main()
