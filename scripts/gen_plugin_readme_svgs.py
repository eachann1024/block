#!/usr/bin/env python3
"""Generate bilingual (zh/en) x dual-theme (light/dark) Ocean Paper SVG hero plates for a Block plugin.

Usage:
    python3 scripts/gen_plugin_readme_svgs.py --plugin-dir /path/to/plugin [--out-dir /path/to/out] [--spec /path/to/spec.json]
"""
import argparse
import json
import sys
import textwrap
import xml.etree.ElementTree as ET
from pathlib import Path
from xml.sax.saxutils import escape

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from build_readme_svgs import FONTS, THEMES  # noqa: E402

CATEGORY_LABELS = {
    "zh": {
        "sync": "同步与备份",
        "media": "图床与媒体",
        "editor": "编辑器增强",
        "tools": "效率工具",
        "appearance": "外观与主题",
        "languages": "语言扩展",
    },
    "en": {
        "sync": "Sync & Backup",
        "media": "Media & Host",
        "editor": "Editor",
        "tools": "Tools",
        "appearance": "Appearance",
        "languages": "Languages",
    },
}

SURFACE_LABELS = {
    "zh": {
        "settings_page": "设置页",
        "sidebar": "侧栏面板",
        "toolbar": "顶栏入口",
        "statusbar": "状态栏",
        "menubar": "菜单栏",
    },
    "en": {
        "settings_page": "Settings",
        "sidebar": "Sidebar",
        "toolbar": "Toolbar",
        "statusbar": "Status Bar",
        "menubar": "Menu Bar",
    },
}


def xml_escape(value: object) -> str:
    return escape(str(value), {'"': "&quot;", "'": "&apos;"})


def pick_lang(val, lang: str, fallback: str = "") -> str:
    if isinstance(val, dict):
        return str(val.get(lang) or val.get("en") or val.get("zh") or fallback)
    if isinstance(val, str):
        return val
    return fallback


def format_shortcut(raw: str) -> str:
    if not raw:
        return ""
    mapping = {"cmd": "Cmd", "ctrl": "Ctrl", "shift": "Shift", "alt": "Alt", "opt": "Opt"}
    parts = [mapping.get(p.lower(), p.upper() if len(p) == 1 else p.capitalize()) for p in raw.split("-") if p]
    return "+".join(parts)


def wrap_summary(text: str, lang: str, max_lines: int = 2) -> list[str]:
    width = 28 if lang == "zh" else 48
    lines = textwrap.wrap(text.strip(), width=width)
    if len(lines) > max_lines:
        lines = lines[:max_lines]
        lines[-1] = lines[-1].rstrip(" .，,") + "..."
    return lines or [""]


def render_plugin_icon(icon: dict, P: dict, x: int, y: int, size: int) -> str:
    if not icon:
        return (
            f'<rect x="{x}" y="{y}" width="{size}" height="{size}" rx="14" fill="{P["accentSoft"]}" stroke="{P["border"]}" stroke-width="1.2"/>'
            f'<circle cx="{x + size / 2}" cy="{y + size / 2}" r="12" fill="none" stroke="{P["accent"]}" stroke-width="2"/>'
        )
    vb = icon.get("view_box", "0 0 24 24").split()
    vw = float(vb[2]) if len(vb) == 4 else 24.0
    vh = float(vb[3]) if len(vb) == 4 else 24.0
    pad = 12
    inner = size - pad * 2
    scale = min(inner / vw, inner / vh)
    tx = x + pad + (inner - vw * scale) / 2
    ty = y + pad + (inner - vh * scale) / 2

    elems = []
    for d in icon.get("fill_paths", []) or []:
        elems.append(f'<path d="{xml_escape(d)}" fill="{P["duoFill"]}" stroke="none"/>')
    for d in icon.get("stroke_paths", []) or []:
        elems.append(
            f'<path d="{xml_escape(d)}" fill="none" stroke="{P["ink"]}" stroke-width="1.7" '
            f'stroke-linecap="round" stroke-linejoin="round"/>'
        )
    for d in icon.get("accent_paths", []) or []:
        elems.append(
            f'<path d="{xml_escape(d)}" fill="none" stroke="{P["accent"]}" stroke-width="1.9" '
            f'stroke-linecap="round" stroke-linejoin="round"/>'
        )
    return (
        f'<rect x="{x}" y="{y}" width="{size}" height="{size}" rx="14" fill="{P["paper"]}" stroke="{P["border"]}" stroke-width="1.2"/>'
        f'<g transform="translate({tx:.2f} {ty:.2f}) scale({scale:.4f})">{"".join(elems)}</g>'
    )


def build_hero_svg(manifest: dict, spec: dict, lang: str, theme: str) -> str:
    P = THEMES[theme]
    tag = f"{lang[0].upper()}{'L' if theme == 'light' else 'D'}"
    w, h = 1200, 380

    plugin_id = str(manifest.get("id") or "block-plugin")
    version = str(manifest.get("version") or "1.0.0")
    category = str(manifest.get("category") or "tools")
    cat_label = CATEGORY_LABELS[lang].get(category, category)

    title = pick_lang(spec.get("title") if spec else None, lang) or pick_lang(manifest.get("name"), lang, plugin_id)
    summary = pick_lang(spec.get("summary") if spec else None, lang) or pick_lang(manifest.get("summary"), lang, "")
    summary_lines = wrap_summary(summary, lang, max_lines=2)

    commands = ((manifest.get("contributes") or {}).get("commands")) or []
    shortcuts = [format_shortcut(c.get("shortcut", "")) for c in commands if c.get("shortcut")]
    surfaces = manifest.get("surfaces") or []
    permissions = manifest.get("permissions") or []

    if spec and isinstance(spec.get("highlights"), dict):
        highlights = spec["highlights"].get(lang) or []
    elif spec and isinstance(spec.get("highlights"), list):
        highlights = spec["highlights"]
    else:
        highlights = []

    badge_text = f"{cat_label}  ·  v{version}"
    badge_w = max(136, len(badge_text) * (10 if lang == "zh" else 8.5) + 32)

    shortcut_svg = ""
    if shortcuts:
        sc = shortcuts[0]
        sc_w = max(84, len(sc) * 9 + 28)
        shortcut_svg = (
            f'<rect x="{56 + badge_w + 12:.1f}" y="294" width="{sc_w}" height="34" rx="8" '
            f'fill="{P["keyTop"]}" stroke="{P["keyEdge"]}" stroke-width="1.2"/>'
            f'<text class="mono" x="{56 + badge_w + 12 + sc_w / 2:.1f}" y="316" text-anchor="middle" '
            f'font-size="13.5" font-weight="600" fill="{P["ink"]}">{xml_escape(sc)}</text>'
        )

    summary_svg = "\n".join(
        f'    <text class="sans" x="56" y="{214 + idx * 30}" font-size="18.5" fill="{P["secondary"]}">{xml_escape(line)}</text>'
        for idx, line in enumerate(summary_lines)
    )

    # Right panel: clean Ocean Paper inspector card
    panel_title = "插件能力" if lang == "zh" else "Capabilities"
    perm_title = "权限声明" if lang == "zh" else "Permissions"
    surf_title = "挂载位置" if lang == "zh" else "Surfaces"
    cmd_title = "快捷命令" if lang == "zh" else "Commands"

    surface_str = " · ".join(SURFACE_LABELS[lang].get(s, s) for s in surfaces[:3]) or ("默认后台" if lang == "zh" else "Background")
    cmd_names = [pick_lang(c.get("title"), lang) for c in commands[:2] if pick_lang(c.get("title"), lang)]
    cmd_str = " · ".join(cmd_names) if cmd_names else (highlights[0] if highlights else plugin_id)
    perm_items = permissions[:4] if permissions else ["minimal"]

    perm_chips = []
    cx = 648
    cy = 248
    for p_name in perm_items:
        pw = max(76, len(p_name) * 8.2 + 22)
        if cx + pw > 1116:
            cx = 648
            cy += 34
        perm_chips.append(
            f'<rect x="{cx}" y="{cy}" width="{pw:.1f}" height="26" rx="7" fill="{P["shell"]}" stroke="{P["border"]}" stroke-width="1"/>'
            f'<text class="mono" x="{cx + pw / 2:.1f}" y="{cy + 17}" text-anchor="middle" font-size="12" fill="{P["secondary"]}">{xml_escape(p_name)}</text>'
        )
        cx += pw + 8

    aria_label = xml_escape(f"{title} — {summary}")

    svg = f"""<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}" role="img" aria-label="{aria_label}" preserveAspectRatio="xMidYMid meet">
  <defs>
    <clipPath id="fr{tag}"><rect x="0" y="0" width="{w}" height="{h}" rx="18"/></clipPath>
    <clipPath id="card{tag}"><rect x="616" y="42" width="532" height="296" rx="14"/></clipPath>
  </defs>
  <style>
{FONTS}
  </style>
  <g clip-path="url(#fr{tag})">
    <rect width="{w}" height="{h}" fill="{P['bg']}"/>

    <!-- Left Brand & Copy -->
    {render_plugin_icon(manifest.get("icon"), P, 56, 48, 58)}
    <text class="sans" x="132" y="75" font-size="14" font-weight="600" fill="{P['accentStrong']}">Block Plugin</text>
    <text class="mono" x="132" y="98" font-size="13.5" fill="{P['muted']}">{xml_escape(plugin_id)}</text>

    <text class="sans" x="56" y="168" font-size="38" font-weight="700" letter-spacing="-0.6" fill="{P['ink']}">{xml_escape(title)}</text>
{summary_svg}

    <rect x="56" y="294" width="{badge_w:.1f}" height="34" rx="17" fill="{P['accentSoft']}"/>
    <text class="sans" x="{56 + badge_w / 2:.1f}" y="316" text-anchor="middle" font-size="14" font-weight="600" fill="{P['accentStrong']}">{xml_escape(badge_text)}</text>
    {shortcut_svg}

    <!-- Right Ocean Paper Card -->
    <g clip-path="url(#card{tag})">
      <rect x="616" y="42" width="532" height="296" fill="{P['paper']}"/>
      <rect x="616" y="42" width="532" height="42" fill="{P['shell']}"/>
      <line x1="616" y1="84" x2="1148" y2="84" stroke="{P['border']}" stroke-width="1"/>
      <circle cx="640" cy="63" r="4.5" fill="{P['lineMid']}"/>
      <circle cx="656" cy="63" r="4.5" fill="{P['lineMid']}"/>
      <circle cx="672" cy="63" r="4.5" fill="{P['lineMid']}"/>
      <text class="sans" x="694" y="68" font-size="13.5" font-weight="600" fill="{P['secondary']}">{xml_escape(panel_title)}</text>

      <text class="sans" x="648" y="120" font-size="12.5" font-weight="600" fill="{P['muted']}">{xml_escape(surf_title)}</text>
      <text class="sans" x="648" y="145" font-size="16" font-weight="600" fill="{P['ink']}">{xml_escape(surface_str)}</text>
      <line x1="648" y1="164" x2="1116" y2="164" stroke="{P['lineSoft']}" stroke-width="1"/>

      <text class="sans" x="648" y="190" font-size="12.5" font-weight="600" fill="{P['muted']}">{xml_escape(cmd_title)}</text>
      <text class="sans" x="648" y="214" font-size="15.5" fill="{P['text']}">{xml_escape(cmd_str)}</text>

      <text class="sans" x="648" y="236" font-size="12.5" font-weight="600" fill="{P['muted']}">{xml_escape(perm_title)}</text>
      {"".join(perm_chips)}
    </g>
    <rect x="616" y="42" width="532" height="296" rx="14" fill="none" stroke="{P['border']}" stroke-width="1.2"/>
  </g>
  <rect x="0.75" y="0.75" width="{w - 1.5}" height="{h - 1.5}" rx="17.25" fill="none" stroke="{P['border']}" stroke-width="1.5"/>
</svg>
"""
    ET.fromstring(svg)
    return svg


def main() -> None:
    parser = argparse.ArgumentParser(description="Generate bilingual dual-theme SVG hero plates for a Block plugin.")
    parser.add_argument("--plugin-dir", required=True, help="Path to plugin directory containing manifest.json")
    parser.add_argument("--out-dir", help="Output directory (defaults to <plugin-dir>/docs/readme)")
    parser.add_argument("--spec", help="Optional JSON spec file for custom copy/highlights")
    args = parser.parse_args()

    plugin_dir = Path(args.plugin_dir).resolve()
    manifest_path = plugin_dir / "manifest.json"
    if not manifest_path.is_file():
        raise SystemExit(f"manifest.json not found in {plugin_dir}")

    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    spec = json.loads(Path(args.spec).read_text(encoding="utf-8")) if args.spec else {}

    out_dir = Path(args.out_dir).resolve() if args.out_dir else plugin_dir / "docs" / "readme"
    out_dir.mkdir(parents=True, exist_ok=True)

    for lang in ("zh", "en"):
        for theme in ("light", "dark"):
            svg = build_hero_svg(manifest, spec, lang, theme)
            out_file = out_dir / f"hero-{lang}-{theme}.svg"
            out_file.write_text(svg, encoding="utf-8")
            print(f"{out_file} ({out_file.stat().st_size / 1024:.1f} KB)")


if __name__ == "__main__":
    main()
