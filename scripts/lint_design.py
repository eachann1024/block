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


def main() -> int:
    problems = []
    for path in sorted((ROOT / "internal").rglob("*.go")):
        rel = path.relative_to(ROOT).as_posix()
        if rel.endswith("_test.go") or rel.startswith(SKIP_DIRS) or rel == "internal/ui/surface.go":
            continue
        is_ui_pkg = rel.startswith("internal/ui/")
        for no, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            if "design:allow" in line or line.lstrip().startswith("//"):
                continue
            for pat, hint in RULES:
                if pat.search(line):
                    problems.append(f"{rel}:{no}: {hint}")
            if not is_ui_pkg:
                for pat, hint in NON_UI_RULES:
                    if pat.search(line):
                        problems.append(f"{rel}:{no}: {hint}")
    for p in problems:
        print(p)
    if problems:
        print(f"design-lint: {len(problems)} raw design value(s); use internal/theme tokens", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
