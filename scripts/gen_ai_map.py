#!/usr/bin/env python3
"""Index src/**/*.rs for one-step lookup.

Tracks public items, smoke probes, #[path] modules resolved to source files,
and impl Editor / AiChat / Workspace blocks including private methods.

Queries compare file stamps and reread only added, removed, or modified
sources. The readable map is .ai-map.md; stamps live in tmp/ai-map.cache.json.
"""

import argparse
import json
import os
import posixpath
import re
import sys
import tempfile
import time
from pathlib import Path


CACHE_VERSION = 1
TRACKED_TYPES = ("Editor", "AiChat", "Workspace")
MAX_BLOCKS = 80
MAX_NAMES = 10
MAX_PROBES = 8
MAX_SIGS = 8
MAX_PATHS = 12


def find_repo_root() -> Path:
    current = Path(__file__).resolve().parent
    while current != current.parent:
        if (current / "Cargo.toml").exists() and (current / "src").exists():
            return current
        current = current.parent
    return Path.cwd().resolve()


VIS_PATTERN = r"(pub(?:\([^)]+\))?)"
ITEM_PATTERN = re.compile(
    r"^(?:#\[[^\]]+\]\s*)*"
    + VIS_PATTERN
    + r'\s+(?:(async|unsafe|const|extern\s+"[^"]+")\s+)*(struct|enum|fn|trait)\b'
)
PATH_MOD_PATTERN = re.compile(
    r'#\[path\s*=\s*"([^"]+)"\](?:\s*#\[[^\]]+\])*\s*(pub(?:\([^)]+\))?\s+)?mod\s+([A-Za-z0-9_]+)\s*;'
)
STD_MOD_PATTERN = re.compile(r"^\s*(pub(?:\([^)]+\))?\s+)?mod\s+([A-Za-z0-9_]+)\s*;")
PROBE_PATTERN = re.compile(r"(?:smoke_overlay::)?\b(probe(?:_for)?)\s*\(")


def extract_probe_arg(line: str, start_pos: int) -> str:
    depth = 1
    cur = start_pos
    while cur < len(line) and depth > 0:
        ch = line[cur]
        if ch == '"':
            cur += 1
            while cur < len(line) and line[cur] != '"':
                if line[cur] == "\\":
                    cur += 1
                cur += 1
        elif ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
        cur += 1
    if depth == 0:
        return line[start_pos : cur - 1].strip()
    return line[start_pos:].strip()


def extract_signature(lines: list[str], start_idx: int, kind: str) -> tuple[int, str]:
    start_line = start_idx + 1
    parts = []
    cur = start_idx
    while cur < len(lines):
        line = lines[cur]
        if "//" in line:
            line = line.split("//")[0]
        line = line.strip()
        if cur == start_idx:
            line = re.sub(r"^(?:#\[[^\]]+\]\s*)+", "", line)
        parts.append(line)
        if "{" in line or ";" in line or (kind == "fn" and ")" in line and "->" not in line and not line.endswith(",")):
            break
        cur += 1
        if cur - start_idx > 15:
            break
    raw = " ".join(parts).split("{")[0].split(";")[0].strip()
    raw = re.sub(r"\s+", " ", raw)
    return start_line, raw


def _ident_char(ch: str) -> bool:
    return ch.isascii() and (ch.isalnum() or ch == "_")


def _skip_cooked(text: str, i: int, line: int) -> tuple[int, int]:
    n = len(text)
    while i < n:
        ch = text[i]
        if ch == "\n":
            line += 1
            i += 1
            continue
        if ch == "\\":
            i += 1
            if i < n:
                if text[i] == "\n":
                    line += 1
                i += 1
            continue
        if ch == '"':
            return i + 1, line
        i += 1
    return i, line


def _try_string(text: str, i: int, line: int) -> tuple[int, int] | None:
    n = len(text)
    if i > 0 and _ident_char(text[i - 1]):
        return None
    prefix = ""
    for cand in ("br", "cr", "b", "c", "r"):
        if not text.startswith(cand, i):
            continue
        nxt = text[i + len(cand)] if i + len(cand) < n else ""
        if cand.endswith("r") and nxt in ('"', "#"):
            prefix = cand
            break
        if cand in ("b", "c") and nxt == '"':
            prefix = cand
            break
    if not prefix and i < n and text[i] == '"':
        return _skip_cooked(text, i + 1, line)
    if not prefix:
        return None
    j = i + len(prefix)
    if prefix.endswith("r"):
        hashes = 0
        while j < n and text[j] == "#":
            hashes += 1
            j += 1
        if j >= n or text[j] != '"':
            return None
        j += 1
        closer = '"' + ("#" * hashes)
        while j < n:
            if text[j] == "\n":
                line += 1
            if text.startswith(closer, j):
                return j + len(closer), line
            j += 1
        return j, line
    return _skip_cooked(text, j + 1, line)


def _skip_tick(text: str, i: int, line: int) -> tuple[int, int]:
    n = len(text)
    j = i + 1
    if j >= n:
        return j, line
    if text[j] == "\\":
        j += 2
        if j < n and text[j] == "'":
            j += 1
        return j, line
    if text[j].isalpha() or text[j] == "_":
        j += 1
        while j < n and _ident_char(text[j]):
            j += 1
        return j, line
    if j + 1 < n and text[j + 1] == "'":
        return j + 2, line
    return i + 1, line


def _strip_leading_generics(src: str) -> str:
    src = src.strip()
    if not src.startswith("<"):
        return src
    depth = 0
    for i, ch in enumerate(src):
        if ch == "<":
            depth += 1
        elif ch == ">":
            depth -= 1
            if depth == 0:
                return src[i + 1 :].strip()
    return src


def _classify_impl(header: str) -> tuple[str, str] | None:
    cleaned = re.sub(r"\s+", " ", header).strip()
    cleaned = re.sub(r"^impl\b", "", cleaned).strip()
    cleaned = _strip_leading_generics(cleaned)
    cleaned = re.split(r"\bwhere\b", cleaned, maxsplit=1)[0].strip()
    if " for " in cleaned:
        trait, ty = cleaned.rsplit(" for ", 1)
        kind = trait.strip()
    else:
        kind = "inherent"
        ty = cleaned
    ty = _strip_leading_generics(ty).split("<", 1)[0].strip().split("::")[-1].strip()
    if kind != "inherent":
        kind = kind.split("<", 1)[0].strip().split("::")[-1].strip() or "trait"
    if ty in TRACKED_TYPES:
        return ty, kind
    return None


def scan_tracked_impls(text: str) -> list[dict]:
    """Impl blocks for Editor, AiChat, and Workspace, with method visibility."""
    impls: list[dict] = []
    n = len(text)
    i = 0
    line = 1
    depth = 0
    paren = 0
    active = None
    header = None
    vis = ""
    expect_name = False
    fn_line = 1

    def close_active() -> None:
        nonlocal active, vis, expect_name, paren
        if active is not None:
            impls.append(
                {
                    "line": active["line"],
                    "type": active["type"],
                    "kind": active["kind"],
                    "methods": active["methods"],
                }
            )
        active = None
        vis = ""
        expect_name = False
        paren = 0

    while i < n:
        ch = text[i]
        if ch == "\n":
            line += 1
            i += 1
            if header is not None and line > header["line"] + 24:
                header = None
            continue
        if ch == "\r":
            i += 1
            continue
        if text.startswith("//", i):
            i += 2
            while i < n and text[i] != "\n":
                i += 1
            continue
        if text.startswith("/*", i):
            i += 2
            while i < n and not text.startswith("*/", i):
                if text[i] == "\n":
                    line += 1
                i += 1
            i = min(n, i + 2)
            continue
        skipped = _try_string(text, i, line)
        if skipped is not None:
            i, line = skipped
            continue
        if ch == "'" and header is None:
            i, line = _skip_tick(text, i, line)
            continue
        if ch == "{":
            if header is not None:
                info = _classify_impl("".join(header["parts"]))
                if info:
                    active = {"line": header["line"], "type": info[0], "kind": info[1], "methods": [], "open_at": depth}
                header = None
                vis = ""
                expect_name = False
                paren = 0
            elif active is not None and depth == active["open_at"] + 1 and paren == 0:
                vis = ""
                expect_name = False
            depth += 1
            i += 1
            continue
        if ch == "}":
            depth = max(0, depth - 1)
            i += 1
            if active is not None and depth == active["open_at"]:
                close_active()
            elif active is not None and depth == active["open_at"] + 1:
                vis = ""
                expect_name = False
            continue
        if header is None and ch == "(":
            paren += 1
            i += 1
            continue
        if header is None and ch == ")":
            paren = max(0, paren - 1)
            i += 1
            continue
        if header is None and ch == ";" and paren == 0:
            if active is not None and depth == active["open_at"] + 1:
                vis = ""
                expect_name = False
            i += 1
            continue
        if ch.isascii() and (ch.isalpha() or ch == "_"):
            start = i
            word_line = line
            i += 1
            while i < n and _ident_char(text[i]):
                i += 1
            word = text[start:i]
            if header is not None:
                header["parts"].append(word)
                header["parts"].append(" ")
                continue
            body = active is not None and depth == active["open_at"] + 1 and paren == 0
            if body:
                if expect_name:
                    active["methods"].append({"line": fn_line, "name": word, "vis": vis or "private"})
                    expect_name = False
                    vis = ""
                    continue
                if word == "fn":
                    expect_name = True
                    fn_line = word_line
                    continue
                if word == "pub":
                    j = i
                    ln = line
                    while j < n and text[j] in " \t":
                        j += 1
                    if j < n and text[j] == "(":
                        k = j
                        pd = 0
                        while k < n:
                            if text[k] == "\n":
                                ln += 1
                            if text[k] == "(":
                                pd += 1
                            elif text[k] == ")":
                                pd -= 1
                                if pd == 0:
                                    k += 1
                                    break
                            k += 1
                        vis = "pub" + re.sub(r"\s+", " ", text[j:k])
                        i, line = k, ln
                    else:
                        vis = "pub"
                    continue
                if word in {"async", "const", "unsafe", "extern"}:
                    continue
                vis = ""
                continue
            if depth == 0 and paren == 0 and word == "impl" and active is None and header is None:
                header = {"line": word_line, "parts": ["impl "]}
            continue
        if header is not None:
            header["parts"].append(ch)
        i += 1
    close_active()
    return impls


def process_rs_file(file_path: Path, repo_root: Path) -> dict:
    rel_path = file_path.relative_to(repo_root).as_posix()
    try:
        content = file_path.read_text(encoding="utf-8", errors="ignore")
    except OSError as exc:
        return {
            "path": rel_path,
            "lines": 0,
            "path_mods": [],
            "std_mods": [],
            "sigs": [],
            "probes": [],
            "impls": [],
            "error": str(exc),
        }

    lines = content.splitlines()
    path_mods = []
    for match in PATH_MOD_PATTERN.finditer(content):
        line_num = content[: match.start()].count("\n") + 1
        target = match.group(1)
        resolved = posixpath.normpath(posixpath.join(posixpath.dirname(rel_path), target))
        path_mods.append(
            {
                "line": line_num,
                "target": target,
                "mod": match.group(3),
                "decl": re.sub(r"\s+", " ", match.group(0).strip()),
                "resolved": resolved,
                "exists": (repo_root / resolved).is_file(),
            }
        )

    sigs = []
    std_mods = []
    i = 0
    while i < len(lines):
        line = lines[i]
        sline = line.strip()
        if sline.startswith("//") or sline.startswith("/*") or sline.startswith("*"):
            i += 1
            continue
        if not path_mods or not any(pm["line"] in (i + 1, i) for pm in path_mods):
            mod_match = STD_MOD_PATTERN.match(sline)
            if mod_match and not sline.startswith("#[path"):
                prev_line = lines[i - 1].strip() if i > 0 else ""
                if not prev_line.startswith("#[path"):
                    std_mods.append({"line": i + 1, "mod": mod_match.group(2), "decl": sline})
        item = ITEM_PATTERN.match(sline)
        if item:
            kind = item.group(3)
            start_line, sig_text = extract_signature(lines, i, kind)
            sigs.append({"line": start_line, "kind": kind, "sig": sig_text})
            while i < len(lines):
                cur = lines[i]
                if "{" in cur or ";" in cur:
                    break
                i += 1
            i += 1
            continue
        i += 1

    probes = []
    for idx, line in enumerate(lines, 1):
        sline = line.strip()
        if sline.startswith("//") or sline.startswith("/*") or sline.startswith("*"):
            continue
        for match in PROBE_PATTERN.finditer(line):
            prefix = line[: match.start()].strip()
            if prefix.endswith("fn") or prefix.endswith("def"):
                continue
            arg = extract_probe_arg(line, match.end())
            if "src/ui/smoke_overlay.rs" in rel_path and idx in (41, 49, 51):
                continue
            if "adapter.probe()" in line:
                continue
            clean_id = arg
            if arg.startswith('"') and arg.endswith('"'):
                clean_id = arg[1:-1]
            elif arg.startswith('&"') and arg.endswith('".into()'):
                clean_id = arg[2:-8]
            elif arg.startswith("&"):
                clean_id = arg[1:]
            probes.append({"line": idx, "fn": match.group(1), "arg": arg, "id_hint": clean_id})

    return {
        "path": rel_path,
        "lines": len(lines),
        "path_mods": path_mods,
        "std_mods": std_mods,
        "sigs": sigs,
        "probes": probes,
        "impls": scan_tracked_impls(content),
    }


def _impl_lines(impl: dict, path: str) -> list[str]:
    rows = [f"- `{path}:{impl['line']}` {impl['kind']}"]
    for method in impl["methods"]:
        rows.append(f"  - `{path}:{method['line']}` {method['vis']} `{method['name']}`")
    return rows


def generate_markdown(all_data: list[dict]) -> str:
    total_files = len(all_data)
    total_lines = sum(d["lines"] for d in all_data)
    total_sigs = sum(len(d["sigs"]) for d in all_data)
    total_probes = sum(len(d["probes"]) for d in all_data)
    total_path_mods = sum(len(d["path_mods"]) for d in all_data)
    total_impls = sum(len(d["impls"]) for d in all_data)

    out = [
        "# Thinkly Fast - AI Codebase Index & Map",
        "",
        "> **Auto-generated by `scripts/gen_ai_map.py`**. Do not edit manually.",
        "> Queries reread only added, removed, or modified sources. Private methods below are limited to `Editor`, `AiChat`, and `Workspace`.",
        "",
        "## Index Summary",
        f"- **Total Rust Files**: {total_files}",
        f"- **Total Lines of Code**: {total_lines}",
        f"- **Total Public Signatures**: {total_sigs}",
        f"- **Total Smoke Probes**: {total_probes}",
        f"- **Path-Injected Modules (`#[path = \"...\"]`)**: {total_path_mods}",
        f"- **Tracked Impl Blocks**: {total_impls}",
        "",
        "## Core Subsystem Routing Map",
        "| Subsystem / Feature | Primary Implementation File | Key Probes & Signatures |",
        "|---|---|---|",
        "| **AI Chat UI & Bottom Composer Dock** | `src/ai/chat_ui.rs` | `ai-composer-actions`, `conv-{id}`, `ai-pick-{i}` |",
        "| **AI Interactive Cards (Approval / Question / Odometer)** | `src/ui/ai_approval.rs` | `ai-approval-card`, `ai-approval-continue`, `ai-question-card`, `ai-question-continue` |",
        "| **AI Process Stream & Step Badges** | `src/ai/chat_process.rs` | `ai-user-message`, `ai-assistant-message`, `ai-reply-copy` |",
        "| **AI Tool Registry SSOT** | `src/ai/agent.rs` | `tools()`, `OpKind`, `Op`, `Ctx`, `apply_op` |",
        "| **AI Turn Loop & State Machine** | `src/ai/chat.rs`, `src/ai/chat_turn.rs` | `AiChat`, `chat()`, `new_conv()` |",
        "| **AI Provider & Backend Adapters** | `src/ai/chat_backend.rs`, `src/ai/backend/` | `probe_backend()`, `claude/`, `codex/`, `dsh/`, `pi/` |",
        "| **Smoke Tests (Interactive Verification)** | `src/smoke_ai_beautiful.rs`, `src/smoke_chat_selection.rs` | End-to-end AI UI verification |",
        "| **Editor Core & Layout** | `src/editor/view.rs`, `src/editor/model.rs` | `Editor`, `new()`, `reset_layouts()` |",
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
    flat_probes.sort(key=lambda item: (item[0].replace('"', ""), item[1], item[2]))
    for hint, fpath, line, call in flat_probes:
        out.append(f"| `{hint.replace('|', '\\|')}` | [`{fpath}`]({fpath}) | {line} | `{call.replace('|', '\\|')}` |")
    out.append("")
    out.append("## Module Inclusions Map (`#[path = \"...\"]`)")
    out.append("Resolved paths are relative to the declaring file.")
    out.append("")
    out.append("| Declaring File | Line | Module | Resolved source | Declaration |")
    out.append("|---|---|---|---|---|")
    for data in all_data:
        for mod in data["path_mods"]:
            status = mod["resolved"] if mod["exists"] else f"{mod['resolved']} (missing)"
            decl = mod["decl"].replace("|", "\\|")
            out.append(
                f"| [`{data['path']}`]({data['path']}) | {mod['line']} | `{mod['mod']}` | `{status}` | `{decl}` |"
            )
    out.append("")
    out.append("## Tracked Type Impls")
    out.append("`Editor`, `AiChat`, and `Workspace`, including private methods.")
    out.append("")
    for type_name in TRACKED_TYPES:
        blocks = [(data["path"], impl) for data in all_data for impl in data["impls"] if impl["type"] == type_name]
        if not blocks:
            continue
        private_count = sum(1 for _, impl in blocks for method in impl["methods"] if method["vis"] == "private" and impl["kind"] == "inherent")
        out.append(f"### `{type_name}` ({len(blocks)} blocks, {private_count} inherent private methods)")
        out.append("")
        for path, impl in blocks:
            out.extend(_impl_lines(impl, path))
        out.append("")

    out.append("## Detailed File Index")
    out.append("")
    categories: dict[str, list[dict]] = {}
    for data in all_data:
        parts = data["path"].split("/")
        categories.setdefault(parts[1] if len(parts) > 2 else "root", []).append(data)
    for cat in sorted(categories):
        out.append(f"### Subsystem: `src/{cat}`")
        out.append("")
        for data in categories[cat]:
            out.append(f"#### [`{data['path']}`]({data['path']}) ({data['lines']} lines)")
            if not data["sigs"] and not data["probes"] and not data["path_mods"] and not data["std_mods"] and not data["impls"]:
                out.append("- *Internal implementation file (no public items or probes)*")
                out.append("")
                continue
            if data["impls"]:
                out.append("- **Tracked impls**:")
                for impl in data["impls"]:
                    out.append(f"  - L{impl['line']}: `{impl['type']}` {impl['kind']} ({len(impl['methods'])} methods)")
            if data["path_mods"]:
                out.append("- **Path Inclusions**:")
                for mod in data["path_mods"]:
                    status = mod["resolved"] if mod["exists"] else f"{mod['resolved']} (missing)"
                    out.append(f"  - L{mod['line']}: `{mod['decl']}` → `{status}`")
            if data["std_mods"]:
                out.append("- **Submodules**:")
                for mod in data["std_mods"]:
                    out.append(f"  - L{mod['line']}: `{mod['decl']}`")
            if data["probes"]:
                out.append("- **Smoke Probes**:")
                for probe in data["probes"]:
                    out.append(f"  - L{probe['line']}: `{probe['fn']}({probe['arg']})`")
            if data["sigs"]:
                out.append("- **Public Signatures**:")
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
    src = repo_root / "src"
    if not src.is_dir():
        print(f"Error: src directory not found at {src}", file=sys.stderr)
        sys.exit(1)
    return sorted(src.glob("**/*.rs"))


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
            and "impls" in cached
            and "path_mods" in cached
        )
        if fresh:
            records[rel] = cached
            continue
        if prev is None:
            added.append(rel)
        else:
            modified.append(rel)
        records[rel] = process_rs_file(repo_root / rel, repo_root)
        read.append(rel)
    removed = sorted(rel for rel in old if rel not in stamps)
    ordered = [records[rel] for rel in sorted(records)]
    links_changed = False
    for record in ordered:
        for mod in record["path_mods"]:
            exists = (repo_root / mod["resolved"]).is_file()
            if mod["exists"] != exists:
                mod["exists"] = exists
                links_changed = True
    map_path = repo_root / ".ai-map.md"
    rewritten = bool(read or removed or links_changed or not map_path.is_file())
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


def _token_hit(text: str, terms: list[str]) -> bool:
    return any(re.search(rf"(?<![A-Za-z0-9_]){re.escape(term)}(?![A-Za-z0-9_])", text, re.I) for term in terms)


def _compact_methods(impl: dict, limit: int) -> str:
    private = [m for m in impl["methods"] if m["vis"] == "private"]
    public = [m for m in impl["methods"] if m["vis"] != "private"]
    ordered = private + public
    shown = ordered[:limit]
    label = ", ".join(f"{m['vis']} `{m['name']}`@{m['line']}" for m in shown)
    extra = len(ordered) - len(shown)
    if extra:
        label = f"{label}, +{extra}" if label else f"+{extra}"
    return label or "(no methods)"


def render_query(terms: list[str], records: list[dict]) -> str:
    terms_l = [term.strip().lower() for term in terms if term.strip()]
    if not terms_l:
        return "No search terms."
    lines = [f"# query: {' '.join(terms)}", ""]
    impl_rows = []
    matched_files = set()
    overflow = False
    for rec in records:
        stem = Path(rec["path"]).stem.lower()
        file_hit = stem in terms_l or rec["path"].lower() in terms_l
        for impl in rec["impls"]:
            matching = [
                method
                for method in impl["methods"]
                if _contains(method["name"], terms_l) or _contains(method["vis"], terms_l)
            ]
            type_hit = impl["type"].lower() in terms_l
            kind_hit = _contains(impl["kind"], terms_l) and impl["kind"] != "inherent"
            if matching and not type_hit and not file_hit:
                shown = matching
            elif type_hit or file_hit or kind_hit:
                shown = matching or impl["methods"]
            else:
                continue
            matched_files.add(rec["path"])
            if len(impl_rows) >= MAX_BLOCKS:
                overflow = True
                break
            names = _compact_methods({**impl, "methods": shown}, MAX_NAMES)
            impl_rows.append(f"- `{rec['path']}:{impl['line']}` `{impl['type']}` {impl['kind']}: {names}")
        if overflow:
            break
    if overflow:
        impl_rows.append("- +more impl blocks in .ai-map.md")
    if impl_rows:
        lines.append("### Impls")
        lines.extend(impl_rows)
        lines.append("")

    path_hits = []
    for rec in records:
        for mod in rec["path_mods"]:
            blob = f"{rec['path']} {mod['mod']} {mod['target']} {mod['resolved']} {mod['decl']}"
            text_hit = _contains(blob, terms_l)
            resolved_hit = mod["resolved"] in matched_files
            declare_hit = rec["path"] in matched_files
            if not (text_hit or resolved_hit or declare_hit):
                continue
            rank = 0 if resolved_hit or text_hit else 1
            status = mod["resolved"] if mod["exists"] else f"{mod['resolved']} (missing)"
            path_hits.append((rank, rec["path"], mod["line"], f"- `{rec['path']}:{mod['line']}` mod `{mod['mod']}` → `{status}`"))
    path_hits.sort(key=lambda item: (item[0], item[1], item[2]))
    path_rows = [item[3] for item in path_hits[:MAX_PATHS]]
    if len(path_hits) > MAX_PATHS:
        path_rows.append("- +more #[path] rows in .ai-map.md")
    if path_rows:
        lines.append("### #[path] sources")
        lines.extend(path_rows)
        lines.append("")

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

    sig_rows = []
    for rec in records:
        for sig in rec["sigs"]:
            if not _token_hit(sig["sig"], terms_l):
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


def query_ai_map(terms: list[str], repo_root: Path, force: bool = False) -> None:
    started = time.perf_counter()
    report = sync_index(repo_root, force=force)
    print(_status_line(report))
    print(render_query(terms, report["records"]).rstrip())
    elapsed_ms = (time.perf_counter() - started) * 1000
    print(f"({elapsed_ms:.1f}ms)")


def main() -> None:
    repo_root = find_repo_root()
    parser = argparse.ArgumentParser(description="Code map for thinkly-fast")
    parser.add_argument("-q", "--query", nargs="+", metavar="TERM", help="Look up signatures, impls, path modules, or probes")
    parser.add_argument("--rebuild", action="store_true", help="Reread every Rust source")
    args = parser.parse_args()
    if args.query:
        query_ai_map(args.query, repo_root, force=args.rebuild)
        return
    report = sync_index(repo_root, force=args.rebuild)
    data = report["records"]
    print(_status_line(report))
    print(f"map: {repo_root / '.ai-map.md'}")
    print(f"  files {len(data)}")
    print(f"  lines {sum(item['lines'] for item in data)}")
    print(f"  signatures {sum(len(item['sigs']) for item in data)}")
    print(f"  probes {sum(len(item['probes']) for item in data)}")
    print(f"  path modules {sum(len(item['path_mods']) for item in data)}")
    print(f"  tracked impls {sum(len(item['impls']) for item in data)}")


if __name__ == "__main__":
    main()
