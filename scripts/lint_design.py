#!/usr/bin/env python3
"""Design-token guard: UI code must read colours, shadows, radii, font sizes and
weights from internal/theme instead of raw literals. Data colours (user content,
chart series) may opt out with a trailing `// design:allow` comment."""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
SKIP_DIRS = ("internal/theme/", "internal/markdown/")
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


def main() -> int:
    problems = []
    for path in sorted((ROOT / "internal").rglob("*.go")):
        rel = path.relative_to(ROOT).as_posix()
        if rel.endswith("_test.go") or rel.startswith(SKIP_DIRS) or rel == "internal/ui/surface.go":
            continue
        is_ui_pkg = rel.startswith("internal/ui/")
        lines = path.read_text(encoding="utf-8").splitlines()
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
    for p in problems:
        print(p)
    if problems:
        print(f"design-lint: {len(problems)} violation(s); fix or annotate intentional opt-out with // design:allow", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
