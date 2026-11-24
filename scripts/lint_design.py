#!/usr/bin/env python3
"""Design-token guard: UI code must read colours, shadows, radii, font sizes and
weights from internal/theme instead of raw literals. Data colours (user content,
chart series) may opt out with a trailing `// design:allow` comment."""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
SKIP_DESIGN_DIRS = ("internal/theme/", "internal/markdown/")
RULES = [
    (re.compile(r"\bui\.(Hex|RGBA|RGB)\("), "raw colour -> theme.Ocean() / theme colour group"),
    (re.compile(r"\.Shadow\("), "raw shadow -> blockui.Elevate(el, theme.Elevation*)"),
    (re.compile(r"\bRadius\(\s*[1-9]|\bRadius:\s+[1-9]"), "raw radius -> theme.Radius*"),
    (re.compile(r"\bFontSize\(\s*[0-9]|\bFontSize:\s+[0-9]"), "raw font size -> theme.Font*"),
    (re.compile(r"\bFontWeight\(\s*[0-9]|\bFontWeight:\s+[0-9]"), "raw font weight -> theme.Weight*"),
]
NON_UI_RULES = [
    (re.compile(r"\bui\.Switch(?:Base)?\("), "raw switch -> internal/ui.Switch / SwitchWithOptions / TogglePill"),
]

ICON_CONST_RE = re.compile(
    r'^\s*[A-Z]\w*\s+(Icon|MascotState)\s*=\s*"([^"]+)"'
)
ICON_LIT_PATTERNS = [
    (
        re.compile(
            r'\bicons\.(?:Draw|DrawAnimated|Duo)\(\s*[^,\n]+,\s*(?:"([^"]*)"|`([^`]*)`)'
        ),
        "icon",
    ),
    (
        re.compile(
            r'\bicons\.(?:LineSVG|Icon)\(\s*(?:"([^"]*)"|`([^`]*)`)\s*[,\)]'
        ),
        "icon",
    ),
    (
        re.compile(
            r'\b(?:ui|bui|appUi|blockui|internalui)\.IconButton\(\s*[^,\n]+,\s*(?:"([^"]*)"|`([^`]*)`)'
        ),
        "icon",
    ),
    (
        re.compile(
            r'\bicons\.Mascot\(\s*[^,\n]+,\s*(?:"([^"]*)"|`([^`]*)`)'
        ),
        "mascot",
    ),
    (
        re.compile(
            r'\bicons\.MascotState\(\s*(?:"([^"]*)"|`([^`]*)`)\s*[,\)]'
        ),
        "mascot",
    ),
]

TR_CALL_RE = re.compile(
    r'\b(?:[a-zA-Z_]\w*\.)?[Tt]r\(\s*("[^"\n]*"|`[^`]*`)\s*,\s*("[^"\n]*"|`[^`]*`)\s*\)'
)
TR_ONE_EMPTY_RE = re.compile(
    r'\b(?:[a-zA-Z_]\w*\.)?[Tt]r\(\s*(?:""|``)\s*,\s*([^,\n)]+)\s*\)'
    r'|\b(?:[a-zA-Z_]\w*\.)?[Tt]r\(\s*([^,\n(]+?)\s*,\s*(?:""|``)\s*\)'
)
BILINGUAL_FIELD_RE = re.compile(r'\b(Zh|En|zh|en)\s*:\s*("([^"\n]*)"|`([^`]*)`)')
LOCALE_MAP_HDR_RE = re.compile(
    r'\bmap\[(?:string|model\.LanguageMode)\](?:map\[string\]string|[A-Za-z_]\w*)\s*\{'
)
STR_KEY_VAL_RE = re.compile(r'\b([A-Za-z_]\w*)\s*:\s*("([^"\n]*)"|`([^`]*)`)')
MAP_STR_KEY_VAL_RE = re.compile(r'"([^"\n]+)"\s*:\s*("([^"\n]*)"|`([^`]*)`)')
LOCALE_ENTRY_KEY_RE = re.compile(r'(?:"(en|zh|zh-Hans|zh-Hant|[a-z]{2}(?:-[A-Za-z]+)?)"|(model\.Lang(?:Zh|En)))\s*:\s*\{')
UI_CTOR_RE = re.compile(
    r"\b(?:ui|bui|appUi|blockui|internalui)\."
    r"(Row|Column|Box|Text|RichText|ButtonBase|TextInput|TextInputBase|TextArea|TextAreaBase|Scroll|ScrollHorizontal|TableGrid|TableGridRow)\("
)
FLEX_GROW_SHRINK_RE = re.compile(r"\.(?:Grow\([^0)]|Shrink\(1\))")
ROW_FLEX_SAFE_RE = re.compile(
    r"\b(?:MinWidth|SingleLine|Clip|Scroll|ScrollHorizontal|TextInput|TextInputBase|TextArea|TextAreaBase)\("
)
LEAF_BOX_RE = re.compile(r"\b(?:ui|bui|appUi|blockui|internalui)\.Box\(")
FUNC_LITERAL_RE = re.compile(r"\bfunc\s*\([^)]*\)\s*(?:[^{]*)\{")


def check_row_flex_min_width(rel: str, lines: list[str]) -> list[str]:
    problems = []
    stack: list[tuple[int, str]] = []
    depth = 0
    pending_kind: str | None = None
    for no, raw in enumerate(lines, 1):
        code = raw.split("//", 1)[0]
        m = UI_CTOR_RE.search(code)
        if m:
            pending_kind = m.group(1)
        parent_kind = stack[-1][1] if stack else None
        if parent_kind in ("Row", "ButtonBase") and FLEX_GROW_SHRINK_RE.search(code):
            # Collect the full fluent statement around this line.
            lo = no - 1
            while lo > 0 and lines[lo - 1].split("//", 1)[0].rstrip().endswith("."):
                lo -= 1
            hi = no
            while hi < len(lines) and lines[hi - 1].split("//", 1)[0].rstrip().endswith("."):
                hi += 1
            stmt_raw = "\n".join(lines[lo:hi])
            stmt_code = " ".join(l.split("//", 1)[0].strip() for l in lines[lo:hi])
            is_leaf_box = bool(LEAF_BOX_RE.search(stmt_code)) and "Children(" not in stmt_code
            if (
                "design:allow" not in stmt_raw
                and not ROW_FLEX_SAFE_RE.search(stmt_code)
                and not is_leaf_box
            ):
                problems.append(
                    f"{rel}:{no}: flexible Row child with Grow/Shrink(1) must set .MinWidth(0) to prevent long unbroken text overflow"
                )
        delta = code.count("{") - code.count("}")
        if "Children(func()" in code and "{" in code:
            stack.append((depth + delta, pending_kind or "Other"))
            depth += delta
            pending_kind = None
            continue
        if FUNC_LITERAL_RE.search(code):
            stack.append((depth + delta, "Other"))
            depth += delta
            pending_kind = None
            continue
        depth += delta
        while stack and depth < stack[-1][0]:
            stack.pop()
    return problems


def load_valid_icons(root: pathlib.Path) -> tuple[set[str], set[str]]:
    valid_icons: set[str] = set()
    valid_mascots: set[str] = set()
    icons_dir = root / "internal" / "icons"
    if not icons_dir.is_dir():
        return valid_icons, valid_mascots
    for path in sorted(icons_dir.glob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        for line in path.read_text(encoding="utf-8").splitlines():
            m = ICON_CONST_RE.match(line)
            if not m:
                continue
            kind, val = m.group(1), m.group(2)
            if kind == "Icon":
                valid_icons.add(val)
            else:
                valid_mascots.add(val)
    return valid_icons, valid_mascots


def check_icon_literals(
    rel: str,
    text: str,
    lines: list[str],
    valid_icons: set[str],
    valid_mascots: set[str],
) -> list[str]:
    if rel.startswith("internal/icons/") or ("icons." not in text and "IconButton(" not in text):
        return []
    problems: list[str] = []
    for no, line in enumerate(lines, 1):
        if "design:allow" in line or line.lstrip().startswith("//"):
            continue
        code = line.split("//", 1)[0]
        if "icons." not in code and "IconButton(" not in code:
            continue
        for pat, kind in ICON_LIT_PATTERNS:
            for m in pat.finditer(code):
                lit = m.group(1) if m.group(1) is not None else (m.group(2) or "")
                valid = valid_icons if kind == "icon" else valid_mascots
                if lit not in valid:
                    problems.append(
                        f"{rel}:{no}: unknown icon literal {lit!r} (not defined in internal/icons)"
                    )
    return problems


def _strip_go_literal(raw_lit: str) -> str:
    if len(raw_lit) >= 2 and raw_lit[0] in ('"', "`") and raw_lit[-1] == raw_lit[0]:
        return raw_lit[1:-1]
    return raw_lit


def _find_enclosing_brace(text: str, pos: int) -> int:
    """Find the index of the '{' that immediately encloses `pos`, or -1."""
    depth = 0
    i = pos - 1
    while i >= 0:
        ch = text[i]
        if ch == "}":
            depth += 1
        elif ch == "{":
            if depth == 0:
                return i
            depth -= 1
        i -= 1
    return -1


def _find_matching_brace(text: str, open_idx: int) -> int:
    depth = 0
    i = open_idx
    n = len(text)
    in_str: str | None = None
    while i < n:
        ch = text[i]
        if in_str:
            if in_str == "`":
                if ch == "`":
                    in_str = None
            else:
                if ch == "\\":
                    i += 2
                    continue
                if ch == in_str:
                    in_str = None
            i += 1
            continue
        if ch == "/" and i + 1 < n:
            if text[i + 1] == "/":
                nl = text.find("\n", i)
                i = n if nl == -1 else nl + 1
                continue
            if text[i + 1] == "*":
                end = text.find("*/", i + 2)
                i = n if end == -1 else end + 2
                continue
        if ch in ('"', "'", "`"):
            in_str = ch
        elif ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return -1


def check_bilingual_text(rel: str, text: str, lines: list[str]) -> list[str]:
    problems: list[str] = []

    def line_allowed(start_idx: int, end_idx: int) -> bool:
        lo = text[:start_idx].count("\n")
        hi = text[:end_idx].count("\n")
        return any(
            "design:allow" in lines[idx] or lines[idx].lstrip().startswith("//")
            for idx in range(lo, min(hi + 1, len(lines)))
        )

    # 1. Tr(zh, en) / tr(zh, en) helper calls.
    if ("Tr(" in text or "tr(" in text) and ('""' in text or "``" in text):
        flagged_spans: set[tuple[int, int]] = set()
        for m in TR_CALL_RE.finditer(text):
            if line_allowed(m.start(), m.end()):
                continue
            zh = _strip_go_literal(m.group(1))
            en = _strip_go_literal(m.group(2))
            if (zh.strip() == "") != (en.strip() == ""):
                no = text[:m.start()].count("\n") + 1
                problems.append(
                    f"{rel}:{no}: bilingual Tr() has empty translation on one side (zh={zh!r}, en={en!r})"
                )
                flagged_spans.add((m.start(), m.end()))
        for m in TR_ONE_EMPTY_RE.finditer(text):
            if any(lo <= m.start() < hi for lo, hi in flagged_spans):
                continue
            if line_allowed(m.start(), m.end()):
                continue
            other = (m.group(1) or m.group(2) or "").strip()
            if other in ('""', "``"):
                continue
            no = text[:m.start()].count("\n") + 1
            problems.append(
                f"{rel}:{no}: bilingual Tr() has empty string literal on one side"
            )

    # 2. Bilingual struct / map literals with Zh/En or zh/en fields.
    if "Zh:" in text or "En:" in text or "zh:" in text or "en:" in text:
        seen_blocks: set[int] = set()
        for bfm in BILINGUAL_FIELD_RE.finditer(text):
            open_idx = _find_enclosing_brace(text, bfm.start())
            if open_idx == -1 or open_idx in seen_blocks:
                continue
            seen_blocks.add(open_idx)
            end = _find_matching_brace(text, open_idx)
            if end == -1:
                continue
            block = text[open_idx + 1 : end]
            # Extract only depth-0 fields inside this block to avoid double-counting nested structs.
            top_parts: list[str] = []
            d = 0
            seg_start = 0
            in_s: str | None = None
            idx = 0
            blen = len(block)
            while idx < blen:
                c = block[idx]
                if in_s:
                    if in_s == "`":
                        if c == "`":
                            in_s = None
                    elif c == "\\":
                        idx += 2
                        continue
                    elif c == in_s:
                        in_s = None
                else:
                    if c in ('"', "'", "`"):
                        in_s = c
                    elif c == "{":
                        if d == 0:
                            top_parts.append(block[seg_start:idx])
                        d += 1
                    elif c == "}":
                        d -= 1
                        if d == 0:
                            seg_start = idx + 1
                idx += 1
            if d == 0:
                top_parts.append(block[seg_start:])
            top_text = " ".join(top_parts)
            fields = {
                fm.group(1): (fm.group(3) if fm.group(3) is not None else (fm.group(4) or ""))
                for fm in BILINGUAL_FIELD_RE.finditer(top_text)
            }
            for zh_k, en_k in (("Zh", "En"), ("zh", "en")):
                if zh_k in fields and en_k in fields:
                    if (fields[zh_k].strip() == "") != (fields[en_k].strip() == ""):
                        if not line_allowed(open_idx, end):
                            no = text[:open_idx].count("\n") + 1
                            problems.append(
                                f"{rel}:{no}: bilingual {zh_k}/{en_k} pair has empty translation on one side"
                            )
                elif (zh_k in fields) != (en_k in fields):
                    other_k = en_k if zh_k in fields else zh_k
                    if not re.search(rf"\b{other_k}\s*:", top_text):
                        if not line_allowed(open_idx, end):
                            no = text[:open_idx].count("\n") + 1
                            problems.append(
                                f"{rel}:{no}: asymmetric bilingual struct literal (has {zh_k if zh_k in fields else en_k} without {other_k})"
                            )

    # 3. Bilingual locale maps: map[string]T{"en": {...}, "zh-Hans": {...}} or map[string]string{"zh": ..., "en": ...}.
    if "map[" in text:
        for hm in LOCALE_MAP_HDR_RE.finditer(text):
            open_idx = hm.end() - 1
            end = _find_matching_brace(text, open_idx)
            if end == -1 or line_allowed(hm.start(), end):
                continue
            body = text[open_idx + 1 : end]
            # Case A: nested locale -> struct/map entries.
            locales: dict[str, dict[str, str]] = {}
            pos = 0
            while True:
                em = LOCALE_ENTRY_KEY_RE.search(body, pos)
                if not em:
                    break
                loc_name = em.group(1) or em.group(2)
                inner_open = em.end() - 1
                inner_end = _find_matching_brace(body, inner_open)
                if inner_end == -1:
                    break
                inner = body[inner_open + 1 : inner_end]
                entries: dict[str, str] = {}
                for km in STR_KEY_VAL_RE.finditer(inner):
                    entries[km.group(1)] = km.group(3) if km.group(3) is not None else (km.group(4) or "")
                for km in MAP_STR_KEY_VAL_RE.finditer(inner):
                    entries[km.group(1)] = km.group(3) if km.group(3) is not None else (km.group(4) or "")
                if entries:
                    locales[loc_name] = entries
                pos = inner_end + 1
            if len(locales) >= 2:
                all_keys = set().union(*(d.keys() for d in locales.values()))
                no = text[:hm.start()].count("\n") + 1
                for loc_name, d in locales.items():
                    missing = sorted(all_keys - set(d.keys()))
                    if missing:
                        problems.append(
                            f"{rel}:{no}: bilingual map locale {loc_name!r} missing key(s): {', '.join(missing)}"
                        )
                for k in sorted(all_keys):
                    non_empty = [loc for loc, d in locales.items() if k in d and d[k].strip() != ""]
                    empty = [loc for loc, d in locales.items() if k in d and d[k].strip() == ""]
                    if non_empty and empty:
                        problems.append(
                            f"{rel}:{no}: bilingual map key {k!r} is empty in locale(s): {', '.join(empty)}"
                        )
                continue

            # Case B: flat map[string]string{"zh": "...", "en": "..."}.
            flat = {
                km.group(1): (km.group(3) if km.group(3) is not None else (km.group(4) or ""))
                for km in MAP_STR_KEY_VAL_RE.finditer(body)
            }
            zh_key = "zh" if "zh" in flat else ("zh-Hans" if "zh-Hans" in flat else None)
            en_key = "en" if "en" in flat else None
            if zh_key and en_key:
                if (flat[zh_key].strip() == "") != (flat[en_key].strip() == ""):
                    no = text[:hm.start()].count("\n") + 1
                    problems.append(
                        f"{rel}:{no}: bilingual map has empty translation for {zh_key!r}/{en_key!r}"
                    )
            elif bool(zh_key) != bool(en_key):
                no = text[:hm.start()].count("\n") + 1
                present = zh_key or en_key
                problems.append(
                    f"{rel}:{no}: asymmetric bilingual map (has {present!r} without counterpart)"
                )

    return problems


def check_text(rel: str, text: str, valid_icons: set[str], valid_mascots: set[str]) -> list[str]:
    problems: list[str] = []
    lines = text.splitlines()
    if not rel.startswith(SKIP_DESIGN_DIRS) and rel != "internal/ui/surface.go":
        is_ui_pkg = rel.startswith("internal/ui/")
        for no, line in enumerate(lines, 1):
            if "design:allow" in line or line.lstrip().startswith("//"):
                continue
            for pat, hint in RULES:
                if pat.search(line):
                    problems.append(f"{rel}:{no}: {hint}")
            if not is_ui_pkg:
                for pat, hint in NON_UI_RULES:
                    if pat.search(line):
                        problems.append(f"{rel}:{no}: {hint}")
        problems.extend(check_row_flex_min_width(rel, lines))
    problems.extend(check_icon_literals(rel, text, lines, valid_icons, valid_mascots))
    problems.extend(check_bilingual_text(rel, text, lines))
    return problems


def main() -> int:
    valid_icons, valid_mascots = load_valid_icons(ROOT)
    problems = []
    for path in sorted((ROOT / "internal").rglob("*.go")):
        rel = path.relative_to(ROOT).as_posix()
        if rel.endswith("_test.go"):
            continue
        text = path.read_text(encoding="utf-8")
        problems.extend(check_text(rel, text, valid_icons, valid_mascots))
    for p in problems:
        print(p)
    if problems:
        print(f"design-lint: {len(problems)} violation(s); fix or annotate intentional opt-out with // design:allow", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
