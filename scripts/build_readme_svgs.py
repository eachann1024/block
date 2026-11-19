#!/usr/bin/env python3
"""Generate Light + Dark Ocean Paper animated SVG plates (zh-CN & en) for Block READMEs.

    python3 scripts/build_readme_svgs.py
"""
import base64
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = ROOT / "docs" / "readme"
APP_ICON = ROOT / "assets" / "app-icon" / "apple-touch-icon.png"

THEMES = {
    "light": {
        "bg": "#f7f9fc",
        "paper": "#ffffff",
        "sidebar": "#fafafa",
        "shell": "#f1f2f5",
        "border": "#e4e6ea",
        "btnBorder": "#d8dce3",
        "ink": "#161922",
        "text": "#1f1e1d",
        "secondary": "#525866",
        "muted": "#727a8a",
        "accent": "#547dd0",
        "accentStrong": "#2f57a6",
        "accentBtn": "#3d67bd",
        "accentSoft": "#e5edfc",
        "duoFill": "#c9d8f3",
        "blockBg": "#eef3fc",
        "blockBorder": "#547dd0",
        "lineStrong": "#8e97a8",
        "lineMid": "#c4cad6",
        "lineSoft": "#e2e6ee",
        "diffDelBg": "#fdecea",
        "diffDelFg": "#b42318",
        "diffAddBg": "#e9f7ef",
        "diffAddFg": "#166534",
        "keyTop": "#ffffff",
        "keyEdge": "#cfd5df",
    },
    "dark": {
        "bg": "#15171c",
        "paper": "#1e2128",
        "sidebar": "#191c22",
        "shell": "#252932",
        "border": "#2d323d",
        "btnBorder": "#3d4352",
        "ink": "#f0f2f5",
        "text": "#e4e6eb",
        "secondary": "#a9b1bf",
        "muted": "#838b9b",
        "accent": "#779de8",
        "accentStrong": "#9cbaf2",
        "accentBtn": "#4770c4",
        "accentSoft": "#1e2c47",
        "duoFill": "#31456a",
        "blockBg": "#202e47",
        "blockBorder": "#779de8",
        "lineStrong": "#7d8699",
        "lineMid": "#4c5364",
        "lineSoft": "#313744",
        "diffDelBg": "#381e22",
        "diffDelFg": "#f28b82",
        "diffAddBg": "#173125",
        "diffAddFg": "#6dd59c",
        "keyTop": "#282c36",
        "keyEdge": "#111318",
    },
}

COPY = {
    "zh": {
        "hero_label": "Block — 落笔成块，快若无物",
        "hero_slogan": "落笔成块，快若无物",
        "hero_slogan_size": 44,
        "hero_sub1": "不用注册登录，打开就能写，笔记全存在自己电脑里。",
        "hero_sub2": "通用 .md 文件 · macOS / Windows / Linux 永久免费",
        "hero_cta": "前往官网下载",
        "hero_cta_w": 176,
        "hero_cta_tx": 128,
        "hero_cta_ax": 196,
        "hero_domain_x": 250,
        "hero_crumb": "生活  /  潮汕之旅",
        "hero_doc": "潮汕之旅",
        "feat_label": "Block 核心体验：长文秒开、速记与版本恢复、AI 对比改稿、12 款纸面导出",
        "f1_title": "十万字长文，秒开不卡",
        "f1_sub": "直读本地 .md 文件夹，换软件照常打开",
        "f1_badge": "100% 本地",
        "f2_title": "随手记，删错也能找回",
        "f2_sub": "⌥N 呼出桌面小窗 · 改稿自动留底可恢复",
        "f3_title": "AI 改稿，先看对比再写入",
        "f3_sub": "行内、侧栏与全屏对话 · 接入本机已有 AI",
        "f3_btn": "接受修改",
        "f4_title": "12 款纸面，一键出图",
        "f4_sub": "浅深色各 6 款 · 整篇或选中段落导出",
        "eco_label": "Block 应用商店按需扩展：同步、图床上传、笔记迁移、性能与占用",
        "eco_title": "写作开箱即用，扩展按需安装",
        "eco_link": "扩展开发指南",
        "eco_cards": [
            ("同步", "目录镜像 · WebDAV · Git"),
            ("图床上传", "R2 · S3 · GitHub"),
            ("笔记迁移", "Obsidian · Notion · 语雀"),
            ("性能与占用", "CPU 内存 · 存储分布"),
        ],
        "cmp_label": "Block 核心优势与笔记软件对比",
        "cmp_cols": [
            ("100% 本地", "通用 .md 文件存自己电脑"),
            ("免注册登录", "打开即写，断网照常用"),
            ("接本机 AI", "不另付会员与额度费"),
            ("永久免费", "Apache 2.0 源码公开"),
        ],
        "cmp_cta": "一张表看差别",
        "cmp_cta_tx": 1073,
    },
    "en": {
        "hero_label": "Block — Write in blocks. Fast as air.",
        "hero_slogan": "Write in blocks. Fast as air.",
        "hero_slogan_size": 36,
        "hero_sub1": "No sign-up. Just write. Every note stays on your disk.",
        "hero_sub2": "Plain .md files · Free for macOS, Windows &amp; Linux",
        "hero_cta": "Download Free",
        "hero_cta_w": 190,
        "hero_cta_tx": 132,
        "hero_cta_ax": 212,
        "hero_domain_x": 264,
        "hero_crumb": "Life  /  Coastal Trip",
        "hero_doc": "Coastal Trip",
        "feat_label": "Block Highlights: Instant Long Docs, Quick Capture &amp; History, Visual AI Diff, 12 Export Themes",
        "f1_title": "Instant on 100k-word notes",
        "f1_sub": "Direct local .md folders, zero lock-in",
        "f1_badge": "100% Local",
        "f2_title": "Quick capture, safe history",
        "f2_sub": "⌥N floating scratchpad · restore any revision",
        "f3_title": "AI edits with visual diff",
        "f3_sub": "Inline, sidebar &amp; full-screen · bring your local AI",
        "f3_btn": "Accept",
        "f4_title": "12 themes, one-click export",
        "f4_sub": "6 light &amp; 6 dark styles · whole note or selection",
        "eco_label": "Block App Store Extensions: Sync, Image Host, Migrator, Performance",
        "eco_title": "Ready out of the box, extend on demand",
        "eco_link": "Plugin Guide",
        "eco_cards": [
            ("Sync", "Mirror · WebDAV · Git"),
            ("Image Host", "R2 · S3 · GitHub"),
            ("Migrator", "Obsidian · Notion · Yuque"),
            ("Performance", "CPU · Memory · Storage"),
        ],
        "cmp_label": "Block Advantages &amp; Comparison",
        "cmp_cols": [
            ("100% Local", "Plain .md on your disk"),
            ("No Sign-up", "Offline by default"),
            ("Local AI", "No extra subscription"),
            ("Free Forever", "Apache 2.0 open source"),
        ],
        "cmp_cta": "Compare All",
        "cmp_cta_tx": 1072,
    },
}

FONTS = """
  .sans{font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Hiragino Sans GB","Microsoft YaHei","Helvetica Neue",sans-serif}
  .mono{font-family:"JetBrains Mono",ui-monospace,"SF Mono",Menlo,Consolas,monospace}
"""


def app_icon_b64() -> str:
    return "data:image/png;base64," + base64.b64encode(APP_ICON.read_bytes()).decode("ascii")


def arrow_ne(cx: float, cy: float, color: str, scale: float = 1.0) -> str:
    return (
        f'<g transform="translate({cx} {cy}) scale({scale})" '
        f'fill="none" stroke="{color}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">'
        f'<path d="M-4.5 4.5 L4.5 -4.5 M-1.5 -4.5 H4.5 V1.5"/></g>'
    )


def frame(w: int, h: int, uid: str, label: str, P: dict, css: str, body: str) -> str:
    return f"""<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}" role="img" aria-label="{label}" preserveAspectRatio="xMidYMid meet">
  <defs>
    <clipPath id="fr{uid}"><rect x="0" y="0" width="{w}" height="{h}" rx="18"/></clipPath>
  </defs>
  <style>
{FONTS}{css}
  @media(prefers-reduced-motion:reduce){{*{{animation:none!important}}}}
  </style>
  <g clip-path="url(#fr{uid})">
    <rect width="{w}" height="{h}" fill="{P['bg']}"/>
{body}
  </g>
  <rect x="0.75" y="0.75" width="{w - 1.5}" height="{h - 1.5}" rx="17.25" fill="none" stroke="{P['border']}" stroke-width="1.5"/>
</svg>
"""


# ------------------------------------------------------------------ 1. Hero
def build_hero(P: dict, T: dict, tag: str) -> str:
    css = """
  .h-drag{animation:h-drag 8s cubic-bezier(.22,1,.36,1) infinite}
  .h-yield{animation:h-yield 8s cubic-bezier(.22,1,.36,1) infinite}
  .h-drop{transform-origin:748px 312px;animation:h-drop 8s cubic-bezier(.22,1,.36,1) infinite}
  .h-caret{animation:h-caret 1.15s steps(1,end) infinite}
  @keyframes h-drag{
    0%,14%,100%{transform:translateY(0)}
    38%,74%{transform:translateY(74px)}
  }
  @keyframes h-yield{
    0%,18%,100%{transform:translateY(0)}
    42%,74%{transform:translateY(-74px)}
  }
  @keyframes h-drop{
    0%,22%,78%,100%{transform:scaleX(0);opacity:0}
    34%,52%{transform:scaleX(1);opacity:1}
    64%{transform:scaleX(1);opacity:0}
  }
  @keyframes h-caret{0%,49%{opacity:1}50%,100%{opacity:0}}
"""
    icon_uri = app_icon_b64()
    body = f"""    <!-- Left Brand & Copy -->
    <defs>
      <clipPath id="heroIcon{tag}"><rect x="52" y="56" width="56" height="56" rx="14"/></clipPath>
      <clipPath id="winClip{tag}"><rect x="588" y="42" width="564" height="356" rx="14"/></clipPath>
    </defs>
    <image href="{icon_uri}" x="52" y="56" width="56" height="56" clip-path="url(#heroIcon{tag})" preserveAspectRatio="xMidYMid slice"/>
    <rect x="52" y="56" width="56" height="56" rx="14" fill="none" stroke="{P['border']}" stroke-width="1.2"/>
    <text class="sans" x="124" y="99" font-size="38" font-weight="700" letter-spacing="-0.8" fill="{P['ink']}">Block</text>

    <text class="sans" x="52" y="192" font-size="{T['hero_slogan_size']}" font-weight="700" letter-spacing="-0.6" fill="{P['ink']}">{T['hero_slogan']}</text>
    <text class="sans" x="52" y="242" font-size="18.5" fill="{P['secondary']}">{T['hero_sub1']}</text>
    <text class="sans" x="52" y="274" font-size="16" fill="{P['muted']}">{T['hero_sub2']}</text>

    <rect x="52" y="320" width="{T['hero_cta_w']}" height="46" rx="23" fill="{P['accentBtn']}"/>
    <text class="sans" x="{T['hero_cta_tx']}" y="349" text-anchor="middle" font-size="16.5" font-weight="600" fill="#ffffff">{T['hero_cta']}</text>
    {arrow_ne(T['hero_cta_ax'], 343, "#ffffff", 0.95)}

    <text class="mono" x="{T['hero_domain_x']}" y="349" font-size="16" font-weight="500" fill="{P['accentStrong']}">block.cooool.top</text>

    <!-- Right Animated Ocean Paper Editor Window -->
    <g clip-path="url(#winClip{tag})">
      <rect x="588" y="42" width="564" height="356" fill="{P['paper']}"/>
      <!-- Top Chrome -->
      <rect x="588" y="42" width="564" height="38" fill="{P['shell']}"/>
      <line x1="588" y1="80" x2="1152" y2="80" stroke="{P['border']}" stroke-width="1"/>
      <circle cx="612" cy="61" r="4.5" fill="#ff5f57"/>
      <circle cx="628" cy="61" r="4.5" fill="#febc2e"/>
      <circle cx="644" cy="61" r="4.5" fill="#28c840"/>
      <text class="sans" x="752" y="66" font-size="14" fill="{P['secondary']}">{T['hero_crumb']}</text>

      <!-- Mini Sidebar -->
      <rect x="588" y="80" width="132" height="318" fill="{P['sidebar']}"/>
      <line x1="720" y1="80" x2="720" y2="398" stroke="{P['border']}" stroke-width="1"/>
      <rect x="600" y="96" width="108" height="30" rx="7" fill="{P['accentSoft']}"/>
      <rect x="612" y="108" width="62" height="6" rx="3" fill="{P['accent']}"/>
      <rect x="612" y="142" width="76" height="6" rx="3" fill="{P['lineMid']}"/>
      <rect x="612" y="168" width="54" height="6" rx="3" fill="{P['lineSoft']}"/>
      <rect x="612" y="194" width="68" height="6" rx="3" fill="{P['lineSoft']}"/>
      <rect x="612" y="220" width="48" height="6" rx="3" fill="{P['lineSoft']}"/>

      <!-- Editor Canvas -->
      <text class="sans" x="752" y="122" font-size="22" font-weight="700" fill="{P['ink']}">{T['hero_doc']}</text>
      <rect x="752" y="140" width="320" height="7" rx="3.5" fill="{P['lineMid']}"/>
      <rect x="752" y="156" width="236" height="7" rx="3.5" fill="{P['lineSoft']}"/>

      <!-- Yielding Block B -->
      <g class="h-yield">
        <rect x="744" y="254" width="376" height="54" rx="10" fill="{P['shell']}"/>
        <rect x="762" y="271" width="12" height="12" rx="3" fill="none" stroke="{P['lineStrong']}" stroke-width="1.6"/>
        <rect x="784" y="274" width="208" height="7" rx="3.5" fill="{P['lineStrong']}"/>
        <rect x="784" y="290" width="146" height="6" rx="3" fill="{P['lineMid']}"/>
      </g>

      <!-- Active Draggable Block A -->
      <g class="h-drag">
        <rect x="744" y="180" width="376" height="58" rx="10" fill="{P['blockBg']}" stroke="{P['blockBorder']}" stroke-width="1.5"/>
        <g fill="{P['accent']}">
          <circle cx="760" cy="200" r="2"/><circle cx="767" cy="200" r="2"/>
          <circle cx="760" cy="208" r="2"/><circle cx="767" cy="208" r="2"/>
          <circle cx="760" cy="216" r="2"/><circle cx="767" cy="216" r="2"/>
        </g>
        <rect x="782" y="198" width="194" height="8" rx="4" fill="{P['ink']}" opacity=".88"/>
        <rect class="h-caret" x="984" y="193" width="2.4" height="18" rx="1.2" fill="{P['accent']}"/>
        <rect x="782" y="216" width="142" height="6" rx="3" fill="{P['accent']}" opacity=".55"/>
      </g>

      <!-- Drop Target Rule -->
      <g class="h-drop">
        <rect x="748" y="311" width="368" height="2.6" rx="1.3" fill="{P['accent']}"/>
        <circle cx="748" cy="312.3" r="3.6" fill="{P['accent']}"/>
      </g>

      <!-- Lower Static Lines -->
      <rect x="752" y="336" width="264" height="7" rx="3.5" fill="{P['lineSoft']}"/>
      <rect x="752" y="354" width="184" height="7" rx="3.5" fill="{P['lineSoft']}"/>
    </g>
    <rect x="588" y="42" width="564" height="356" rx="14" fill="none" stroke="{P['border']}" stroke-width="1.5"/>"""
    return frame(1200, 440, f"H{tag}", T["hero_label"], P, css, body)


# ------------------------------------------------------------------ 2. Features (2x2 Bento)
def build_features(P: dict, T: dict, tag: str) -> str:
    css = """
  .f-bar{transform-origin:132px 190px;animation:f-bar 5.5s cubic-bezier(.22,1,.36,1) infinite}
  .f-note{animation:f-note 6.5s cubic-bezier(.22,1,.36,1) infinite}
  .f-key{animation:f-key 6.5s cubic-bezier(.22,1,.36,1) infinite}
  .f-v1{animation:f-vp 6.5s ease-in-out infinite}
  .f-v2{animation:f-vp 6.5s ease-in-out 1.4s infinite}
  .f-acc{transform-origin:495px 416px;animation:f-acc 5s ease-in-out infinite}
  .f-c1{animation:f-fan 7s ease-in-out infinite}
  .f-c2{animation:f-fan 7s ease-in-out .35s infinite}
  .f-c3{animation:f-fan 7s ease-in-out .7s infinite}
  .f-c4{animation:f-fan 7s ease-in-out 1.05s infinite}
  .f-c5{animation:f-fan 7s ease-in-out 1.4s infinite}
  @keyframes f-bar{0%,12%{transform:scaleX(.18)}35%,80%{transform:scaleX(1)}95%,100%{transform:scaleX(.18)}}
  @keyframes f-note{0%,18%,100%{transform:translateY(0)}46%,74%{transform:translateY(-6px)}}
  @keyframes f-key{0%,16%,34%,100%{transform:translateY(0)}24%{transform:translateY(2.5px)}}
  @keyframes f-vp{0%,100%{opacity:.45}45%,60%{opacity:1}}
  @keyframes f-acc{0%,100%{opacity:.88;transform:scale(1)}50%{opacity:1;transform:scale(1.04)}}
  @keyframes f-fan{0%,100%{transform:translateY(0)}50%{transform:translateY(-5px)}}
"""
    body = f"""    <!-- Card 1 (TL): Fast Local .md -->
    <rect x="28" y="28" width="562" height="226" rx="14" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1.2"/>
    <text class="sans" x="56" y="68" font-size="22" font-weight="700" fill="{P['ink']}">{T['f1_title']}</text>
    <text class="sans" x="56" y="96" font-size="15.5" fill="{P['secondary']}">{T['f1_sub']}</text>

    <rect x="56" y="116" width="506" height="114" rx="10" fill="{P['bg']}" stroke="{P['border']}" stroke-width="1"/>
    <rect x="76" y="142" width="44" height="58" rx="7" fill="{P['duoFill']}"/>
    <rect x="76" y="142" width="44" height="58" rx="7" fill="none" stroke="{P['accent']}" stroke-width="1.6"/>
    <text class="mono" x="98" y="177" text-anchor="middle" font-size="14" font-weight="700" fill="{P['accentStrong']}">.md</text>

    <rect x="138" y="146" width="214" height="7" rx="3.5" fill="{P['lineStrong']}"/>
    <rect x="138" y="162" width="168" height="6" rx="3" fill="{P['lineMid']}"/>
    <rect x="138" y="177" width="244" height="6" rx="3" fill="{P['lineSoft']}"/>
    <rect x="138" y="194" width="244" height="6" rx="3" fill="{P['lineSoft']}"/>
    <rect class="f-bar" x="138" y="194" width="244" height="6" rx="3" fill="{P['accent']}"/>

    <rect x="404" y="156" width="138" height="34" rx="17" fill="{P['accentSoft']}"/>
    <text class="mono" x="473" y="178" text-anchor="middle" font-size="14" font-weight="600" fill="{P['accentStrong']}">{T['f1_badge']}</text>

    <!-- Card 2 (TR): Quick Note & History -->
    <rect x="610" y="28" width="562" height="226" rx="14" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1.2"/>
    <text class="sans" x="638" y="68" font-size="22" font-weight="700" fill="{P['ink']}">{T['f2_title']}</text>
    <text class="sans" x="638" y="96" font-size="15.5" fill="{P['secondary']}">{T['f2_sub']}</text>

    <rect x="638" y="116" width="506" height="114" rx="10" fill="{P['bg']}" stroke="{P['border']}" stroke-width="1"/>
    <g class="f-key">
      <rect x="660" y="152" width="72" height="44" rx="9" fill="{P['keyEdge']}"/>
      <rect x="660" y="148" width="72" height="42" rx="9" fill="{P['keyTop']}" stroke="{P['btnBorder']}" stroke-width="1.2"/>
      <text class="mono" x="696" y="175" text-anchor="middle" font-size="16" font-weight="600" fill="{P['ink']}">⌥ N</text>
    </g>

    <rect x="756" y="144" width="150" height="66" rx="8" fill="{P['shell']}" stroke="{P['border']}" stroke-width="1"/>
    <g class="f-note">
      <rect x="766" y="136" width="150" height="66" rx="8" fill="{P['paper']}" stroke="{P['accent']}" stroke-width="1.3"/>
      <rect x="782" y="152" width="54" height="7" rx="3.5" fill="{P['ink']}" opacity=".85"/>
      <rect x="782" y="168" width="114" height="6" rx="3" fill="{P['lineMid']}"/>
      <rect x="782" y="182" width="84" height="6" rx="3" fill="{P['lineSoft']}"/>
    </g>

    <line x1="956" y1="144" x2="956" y2="202" stroke="{P['border']}" stroke-width="2"/>
    <circle cx="956" cy="148" r="5" fill="{P['accent']}"/>
    <circle class="f-v1" cx="956" cy="173" r="4.5" fill="{P['accent']}"/>
    <circle class="f-v2" cx="956" cy="198" r="4.5" fill="{P['lineMid']}"/>
    <rect x="974" y="144" width="136" height="7" rx="3.5" fill="{P['lineStrong']}"/>
    <rect x="974" y="169" width="108" height="6.5" rx="3.2" fill="{P['lineMid']}"/>
    <rect x="974" y="194" width="90" height="6" rx="3" fill="{P['lineSoft']}"/>

    <!-- Card 3 (BL): AI Diff Review + Local Agents -->
    <rect x="28" y="274" width="562" height="226" rx="14" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1.2"/>
    <text class="sans" x="56" y="314" font-size="22" font-weight="700" fill="{P['ink']}">{T['f3_title']}</text>
    <text class="sans" x="56" y="342" font-size="15.5" fill="{P['secondary']}">{T['f3_sub']}</text>

    <rect x="56" y="362" width="506" height="114" rx="10" fill="{P['bg']}" stroke="{P['border']}" stroke-width="1"/>
    <rect x="72" y="374" width="356" height="24" rx="6" fill="{P['diffDelBg']}"/>
    <text class="mono" x="84" y="391" font-size="14" font-weight="700" fill="{P['diffDelFg']}">-</text>
    <rect x="102" y="383" width="212" height="6.5" rx="3.2" fill="{P['diffDelFg']}" opacity=".55"/>

    <rect x="72" y="404" width="356" height="24" rx="6" fill="{P['diffAddBg']}"/>
    <text class="mono" x="84" y="421" font-size="14" font-weight="700" fill="{P['diffAddFg']}">+</text>
    <rect x="102" y="413" width="268" height="6.5" rx="3.2" fill="{P['diffAddFg']}" opacity=".75"/>

    <g class="f-acc">
      <rect x="444" y="402" width="102" height="28" rx="14" fill="{P['accentBtn']}"/>
      <text class="sans" x="495" y="421" text-anchor="middle" font-size="14" font-weight="600" fill="#ffffff">{T['f3_btn']}</text>
    </g>

    <rect x="72" y="440" width="128" height="26" rx="13" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1"/>
    <text class="mono" x="136" y="458" text-anchor="middle" font-size="14" fill="{P['secondary']}">Claude Code</text>

    <rect x="208" y="440" width="78" height="26" rx="13" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1"/>
    <text class="mono" x="247" y="458" text-anchor="middle" font-size="14" fill="{P['secondary']}">Codex</text>

    <rect x="294" y="440" width="124" height="26" rx="13" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1"/>
    <text class="mono" x="356" y="458" text-anchor="middle" font-size="14" fill="{P['secondary']}">Antigravity</text>

    <rect x="426" y="440" width="54" height="26" rx="13" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1"/>
    <text class="mono" x="453" y="458" text-anchor="middle" font-size="14" fill="{P['secondary']}">pi</text>

    <rect x="488" y="440" width="58" height="26" rx="13" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1"/>
    <text class="mono" x="517" y="458" text-anchor="middle" font-size="14" fill="{P['secondary']}">dsh</text>

    <!-- Card 4 (BR): 12 Paper Export -->
    <rect x="610" y="274" width="562" height="226" rx="14" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1.2"/>
    <text class="sans" x="638" y="314" font-size="22" font-weight="700" fill="{P['ink']}">{T['f4_title']}</text>
    <text class="sans" x="638" y="342" font-size="15.5" fill="{P['secondary']}">{T['f4_sub']}</text>

    <rect x="638" y="362" width="506" height="114" rx="10" fill="{P['bg']}" stroke="{P['border']}" stroke-width="1"/>

    <g class="f-c1">
      <rect x="658" y="380" width="58" height="78" rx="7" fill="#ffffff" stroke="#d5dae3" stroke-width="1.2"/>
      <rect x="668" y="394" width="28" height="5" rx="2.5" fill="#161922"/>
      <rect x="668" y="407" width="38" height="4" rx="2" fill="#c4cad6"/>
      <rect x="668" y="416" width="30" height="4" rx="2" fill="#dfe3eb"/>
      <rect x="668" y="438" width="22" height="4" rx="2" fill="#547dd0"/>
    </g>
    <g class="f-c2">
      <rect x="724" y="380" width="58" height="78" rx="7" fill="#f7f1e3" stroke="#e2d5bc" stroke-width="1.2"/>
      <rect x="734" y="394" width="32" height="5" rx="2.5" fill="#2f57a6"/>
      <rect x="734" y="407" width="36" height="4" rx="2" fill="#c79f66"/>
      <rect x="734" y="416" width="28" height="4" rx="2" fill="#dccab0"/>
      <circle cx="766" cy="440" r="5" fill="#e06d53"/>
    </g>
    <g class="f-c3">
      <rect x="790" y="380" width="58" height="78" rx="7" fill="#eceae4" stroke="#cfcbb8" stroke-width="1.2"/>
      <rect x="800" y="392" width="38" height="7" rx="2" fill="#1f1e1d"/>
      <line x1="800" y1="405" x2="838" y2="405" stroke="#8a8477" stroke-width="1"/>
      <rect x="800" y="412" width="16" height="28" rx="2" fill="#c8c3b6"/>
      <rect x="820" y="412" width="18" height="28" rx="2" fill="#d8d4c8"/>
    </g>
    <g class="f-c4">
      <rect x="856" y="380" width="58" height="78" rx="7" fill="#1d2028" stroke="#363c4b" stroke-width="1.2"/>
      <rect x="866" y="394" width="30" height="5" rx="2.5" fill="#f0f2f5"/>
      <rect x="866" y="407" width="38" height="4" rx="2" fill="#5f677a"/>
      <rect x="866" y="416" width="26" height="4" rx="2" fill="#3f4656"/>
      <rect x="866" y="438" width="24" height="4" rx="2" fill="#779de8"/>
    </g>
    <g class="f-c5">
      <rect x="922" y="380" width="58" height="78" rx="7" fill="#0f1713" stroke="#223a2c" stroke-width="1.2"/>
      <rect x="932" y="394" width="14" height="4" rx="2" fill="#4ade80"/>
      <rect x="932" y="405" width="36" height="4" rx="2" fill="#2e7d4f"/>
      <rect x="932" y="415" width="28" height="4" rx="2" fill="#215437"/>
      <rect x="932" y="436" width="32" height="5" rx="2.5" fill="#4ade80" opacity=".85"/>
    </g>

    <rect x="996" y="386" width="62" height="28" rx="8" fill="{P['accentSoft']}"/>
    <text class="mono" x="1027" y="405" text-anchor="middle" font-size="14" font-weight="600" fill="{P['accentStrong']}">PNG</text>
    <rect x="1066" y="386" width="62" height="28" rx="8" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1"/>
    <text class="mono" x="1097" y="405" text-anchor="middle" font-size="14" fill="{P['secondary']}">PDF</text>

    <rect x="996" y="424" width="62" height="28" rx="8" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1"/>
    <text class="mono" x="1027" y="443" text-anchor="middle" font-size="14" fill="{P['secondary']}">Word</text>
    <rect x="1066" y="424" width="62" height="28" rx="8" fill="{P['paper']}" stroke="{P['border']}" stroke-width="1"/>
    <text class="mono" x="1097" y="443" text-anchor="middle" font-size="14" fill="{P['secondary']}">HTML</text>"""
    return frame(1200, 528, f"F{tag}", T["feat_label"], P, css, body)


# ------------------------------------------------------------------ 3. App Store / Ecosystem
def build_ecosystem(P: dict, T: dict, tag: str) -> str:
    css = """
  .e-d1{animation:e-dot 6s ease-in-out 0s infinite}
  .e-d2{animation:e-dot 6s ease-in-out 1.5s infinite}
  .e-d3{animation:e-dot 6s ease-in-out 3.0s infinite}
  .e-d4{animation:e-dot 6s ease-in-out 4.5s infinite}
  @keyframes e-dot{0%,100%{opacity:.28}25%,40%{opacity:1}}
"""
    icon_templates = [
        ("e-d1",
         f'<rect x="{{ix}}" y="{{iy}}" width="30" height="30" rx="8" fill="{P["duoFill"]}"/>'
         f'<path d="M{{x1}} {{y1}} h12 l-3-3 M{{x2}} {{y2}} h-12 l3 3" fill="none" stroke="{P["accent"]}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>'),
        ("e-d2",
         f'<rect x="{{ix}}" y="{{iy}}" width="30" height="30" rx="8" fill="{P["duoFill"]}"/>'
         f'<path d="M{{xc}} {{yb}} v-11 M{{xl}} {{ym}} l4-4 4 4" fill="none" stroke="{P["accent"]}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>'),
        ("e-d3",
         f'<rect x="{{ix}}" y="{{iy}}" width="30" height="30" rx="8" fill="{P["duoFill"]}"/>'
         f'<path d="M{{xl}} {{yc}} h10 l-3.5-3.5 M{{xr}} {{yc}} l-3.5 3.5" fill="none" stroke="{P["accent"]}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>'),
        ("e-d4",
         f'<rect x="{{ix}}" y="{{iy}}" width="30" height="30" rx="8" fill="{P["duoFill"]}"/>'
         f'<path d="M{{xa}} {{yb}} l4-6 4 3 5-8" fill="none" stroke="{P["accent"]}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>'),
    ]
    parts = [
        f'    <text class="sans" x="32" y="52" font-size="22" font-weight="700" fill="{P["ink"]}">{T["eco_title"]}</text>',
        f'    <text class="sans" x="1146" y="50" text-anchor="end" font-size="15.5" font-weight="600" fill="{P["accentStrong"]}">{T["eco_link"]}</text>',
        f'    {arrow_ne(1160, 44, P["accentStrong"], 0.9)}',
    ]
    cw = 272
    gap = 16
    x0 = 32
    y0 = 76
    for idx, ((title, sub), (dcls, icon_tpl)) in enumerate(zip(T["eco_cards"], icon_templates)):
        cx = x0 + idx * (cw + gap)
        ix = cx + 18
        iy = y0 + 18
        icon_svg = icon_tpl.format(
            ix=ix, iy=iy,
            x1=ix + 9, y1=iy + 12, x2=ix + 21, y2=iy + 19,
            xc=ix + 15, yc=iy + 15, xl=ix + 11, xr=ix + 21, ym=iy + 13, yb=iy + 21, xa=ix + 8,
        )
        parts.append(
            f'    <rect x="{cx}" y="{y0}" width="{cw}" height="108" rx="12" fill="{P["paper"]}" stroke="{P["border"]}" stroke-width="1.2"/>\n'
            f'    {icon_svg}\n'
            f'    <text class="sans" x="{cx + 58}" y="{y0 + 39}" font-size="18.5" font-weight="700" fill="{P["ink"]}">{title}</text>\n'
            f'    <circle class="{dcls}" cx="{cx + cw - 20}" cy="{y0 + 32}" r="4" fill="{P["accent"]}"/>\n'
            f'    <text class="sans" x="{cx + 18}" y="{y0 + 82}" font-size="15" fill="{P["secondary"]}">{sub}</text>'
        )
    return frame(1200, 212, f"E{tag}", T["eco_label"], P, css, "\n".join(parts))


# ------------------------------------------------------------------ 4. Compare / Promises Strip
def build_compare(P: dict, T: dict, tag: str) -> str:
    parts = []
    col_w = 236
    x0 = 36
    for idx, (head, sub) in enumerate(T["cmp_cols"]):
        cx = x0 + idx * col_w
        if idx > 0:
            parts.append(f'    <line x1="{cx - 18}" y1="30" x2="{cx - 18}" y2="96" stroke="{P["border"]}" stroke-width="1.2"/>')
        parts.append(
            f'    <text class="sans" x="{cx}" y="54" font-size="21" font-weight="700" fill="{P["ink"]}">{head}</text>\n'
            f'    <text class="sans" x="{cx}" y="84" font-size="15" fill="{P["secondary"]}">{sub}</text>'
        )
    parts.append(
        f'    <rect x="998" y="40" width="168" height="46" rx="23" fill="{P["accentSoft"]}"/>\n'
        f'    <text class="sans" x="{T["cmp_cta_tx"]}" y="69" text-anchor="middle" font-size="15.5" font-weight="600" fill="{P["accentStrong"]}">{T["cmp_cta"]}</text>\n'
        f'    {arrow_ne(1140, 63, P["accentStrong"], 0.92)}'
    )
    return frame(1200, 126, f"C{tag}", T["cmp_label"], P, "", "\n".join(parts))


def main() -> None:
    for lang, text in COPY.items():
        lang_root = OUT_DIR if lang == "zh" else OUT_DIR / "en"
        for theme_name, palette in THEMES.items():
            target_dir = lang_root / theme_name
            target_dir.mkdir(parents=True, exist_ok=True)
            tag = f"{lang[0].upper()}{'L' if theme_name == 'light' else 'D'}"
            plates = {
                "hero": build_hero(palette, text, tag),
                "features": build_features(palette, text, tag),
                "ecosystem": build_ecosystem(palette, text, tag),
                "compare": build_compare(palette, text, tag),
            }
            for name, svg in plates.items():
                ET.fromstring(svg)  # strict XML validation
                out = target_dir / f"{name}.svg"
                out.write_text(svg, encoding="utf-8")
                print(f"{out.relative_to(ROOT).as_posix():40s} {out.stat().st_size / 1024:5.1f} KB")


if __name__ == "__main__":
    main()
