#!/usr/bin/env python3
"""
dump_ai_state.py - 1-Second Runtime & AI Plan Inspector for Block (thinkly-fast)

Reads ~/Library/Application Support/Block/ (or $XDG_DATA_HOME/Block on Linux):
- state.json: active notebook folder, loaded notebooks, open_tabs, active_tab
- ai-active-draft.json: current active conversation ID, draft text, @refs, /skills
- ai-chats.json: conversations, latest messages, and full executeBatchPlan details
  (including resolved absolute file paths, operation types, and expected note snapshots)

Usage:
  python3 scripts/dump_ai_state.py                 # Summary of loaded notes + latest AI plans
  python3 scripts/dump_ai_state.py -q "健身计划"    # Search conversations/plans/notes by keyword
  python3 scripts/dump_ai_state.py --plan          # Dump full oldString/newString/expected of latest plan
  python3 scripts/dump_ai_state.py --conv <ID>     # Inspect a specific conversation ID
"""

import argparse
import json
import os
import platform
import sys
from pathlib import Path


def app_data_dir() -> Path:
    if platform.system() == "Darwin":
        return Path.home() / "Library" / "Application Support" / "Block"
    if platform.system() == "Windows":
        appdata = os.environ.get("APPDATA")
        return (Path(appdata) if appdata else Path.home() / "AppData" / "Roaming") / "Block"
    xdg = os.environ.get("XDG_DATA_HOME")
    return (Path(xdg) if xdg else Path.home() / ".local" / "share") / "Block"


def load_json(path: Path, default):
    try:
        if path.exists():
            return json.loads(path.read_text(encoding="utf-8", errors="replace"))
    except Exception as e:
        print(f"[WARN] Failed to read {path}: {e}", file=sys.stderr)
    return default


def resolve_note_path(root: str | None, page: str) -> str:
    if not root:
        return f"{page}.md"
    p = Path(root) / (page if page.endswith(".md") else f"{page}.md")
    return str(p)


def print_state_summary(base: Path, state: dict, draft: dict):
    print("# 1. Block Runtime & Loaded Notes (`state.json`)")
    print(f"- **App Data Dir**: `{base}`")
    print(f"- **State File**: `{base / 'state.json'}`")
    print(f"- **Active Notebook (`folder`)**: `{state.get('folder') or '(none)'}`")
    print(f"- **Active Note (`active_tab`)**: `{state.get('active_tab') or '(none)'}`")

    open_tabs = state.get("open_tabs") or []
    print(f"- **Open Note Tabs ({len(open_tabs)})**:")
    for tab in open_tabs:
        exists = "OK" if Path(tab).exists() else "MISSING"
        print(f"  - [{exists}] `{tab}`")

    notebooks = state.get("notebooks") or []
    ignored = set(state.get("ignored_search_folders") or [])
    print(f"- **Loaded Notebooks ({len(notebooks)})**:")
    for nb in notebooks:
        tag = " (ignored in search)" if nb in ignored else ""
        print(f"  - `{nb}`{tag}")

    if draft and any(draft.get(k) for k in ("conversation", "text", "refs", "skills")):
        print("\n# 2. Active AI Composer Draft (`ai-active-draft.json`)")
        print(f"- **Active Conversation ID**: `{draft.get('conversation')}`")
        if draft.get("refs"):
            print(f"- **Referenced Notes (`@refs`)**: `{draft.get('refs')}`")
        if draft.get("skills"):
            print(f"- **Selected Skills (`/skills`)**: `{draft.get('skills')}`")
        if draft.get("text"):
            print(f"- **Draft Text**: {draft.get('text')!r}")
    print("")


def format_plan_summary(conv: dict, msg_idx: int, plan: dict, full: bool = False):
    title = plan.get("title", "")
    summary = plan.get("summary", "")
    pstate = plan.get("state", "Unknown")
    root = plan.get("root")
    ops = plan.get("ops") or []
    expected = dict(plan.get("expected") or [])

    print(f"### Plan in Conv `{conv.get('id')}` ({conv.get('title')!r}) @ msg[{msg_idx}]")
    print(f"- **Plan Title**: {title!r} | **State**: `{pstate}`")
    print(f"- **Summary**: {summary}")
    print(f"- **Notebook Root**: `{root}`")
    print(f"- **Operations ({len(ops)})**:")

    for idx, op in enumerate(ops):
        kind = op.get("kind") or op.get("type") or "op"
        page = op.get("page") or op.get("pageId") or op.get("title") or ""
        abs_path = resolve_note_path(root, page)
        disk_exists = Path(abs_path).exists()
        exp_content = expected.get(page)
        exp_info = f"{len(exp_content)} chars" if isinstance(exp_content, str) else "None (new note)"
        print(f"  - **op[{idx}]**: `{kind}` on page `{page}`")
        print(f"    - **Resolved File Path**: `{abs_path}` ({'exists on disk' if disk_exists else 'not on disk'})")
        print(f"    - **Expected Snapshot**: {exp_info}")

        if full:
            if op.get("old"):
                print("    - **old (`oldString`)**:")
                for line in op["old"].splitlines()[:40]:
                    print(f"      | {line}")
            if op.get("new"):
                print("    - **new (`newString` / `markdown`)**:")
                for line in op["new"].splitlines()[:40]:
                    print(f"      | {line}")
            if isinstance(exp_content, str):
                print("    - **expected note snapshot (first 40 lines)**:")
                for line in exp_content.splitlines()[:40]:
                    print(f"      | {line}")
    print("")


def main():
    parser = argparse.ArgumentParser(description="Inspect Block loaded notes, state.json, and AI chats/plans")
    parser.add_argument("-q", "--query", help="Filter conversations/plans/notes by keyword")
    parser.add_argument("--plan", action="store_true", help="Dump full old/new/expected text for matching plans")
    parser.add_argument("--conv", type=int, help="Inspect a specific conversation ID")
    parser.add_argument("--limit", type=int, default=5, help="Max conversations/plans to display (default: 5)")
    args = parser.parse_args()

    base = app_data_dir()
    state = load_json(base / "state.json", {})
    draft = load_json(base / "ai-active-draft.json", {})
    chats = load_json(base / "ai-chats.json", [])

    print_state_summary(base, state, draft)

    print(f"# 3. AI Conversations & Plans (`ai-chats.json`, total: {len(chats)})")
    if not chats:
        print("No conversations found.")
        return

    q = args.query.lower() if args.query else None
    shown_convs = 0
    shown_plans = 0

    for conv in chats:
        cid = conv.get("id")
        if args.conv and cid != args.conv:
            continue

        blob = json.dumps(conv, ensure_ascii=False).lower()
        if q and q not in blob:
            continue

        msgs = conv.get("messages") or []
        plans = [(i, m["plan"]) for i, m in enumerate(msgs) if m.get("plan")]

        if shown_convs < args.limit or args.conv:
            backend = conv.get("backend", "builtin")
            print(f"- **Conv `{cid}`**: {conv.get('title')!r} (backend: `{backend}`, messages: {len(msgs)}, plans: {len(plans)})")
            for i, m in enumerate(msgs[-4:]):
                real_idx = len(msgs) - len(msgs[-4:]) + i
                role = m.get("role", "?")
                text = (m.get("content") or "").strip().replace("\n", " ")
                if len(text) > 100:
                    text = text[:100] + "…"
                has_plan = " [HAS PLAN]" if m.get("plan") else ""
                print(f"  - msg[{real_idx}] `{role}`{has_plan}: {text}")
            shown_convs += 1

        for msg_idx, plan in reversed(plans):
            if q and q not in json.dumps(plan, ensure_ascii=False).lower() and q not in (conv.get("title") or "").lower():
                continue
            if shown_plans < args.limit or args.conv or args.plan:
                print("")
                format_plan_summary(conv, msg_idx, plan, full=args.plan or bool(args.conv) or bool(q))
                shown_plans += 1


if __name__ == "__main__":
    main()
