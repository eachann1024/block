# Block Design Language & Color Specification (Ocean Paper)

> Content-first, compact and restrained, strictly aligned, crisp outlines, globally consistent. This document is the mandatory visual and interaction baseline for the Block desktop MyGo implementation and stays strictly aligned with `internal/theme/theme.go`.

---

## 1. Visual Tone & Restraint Principles

A quiet, compact desktop writing tool: text takes center stage, navigation recedes to the edges, and controls respond on demand. Light mode reads like clean paper; dark mode uses layered neutral graphite. Selection is indicated with a crisp `1px` inner outline (`inset 0 0 0 1px`) paired with a subtle tinted fill—never heavy color blocks, glowing shadows, or gradients.

- **Dedicated views, never stacked**: Settings is a standalone full-window view (left category sidebar `260px`, right canvas for the active category). Clicking a category switches to that single page; **never stack all settings categories into one long scrollable page**.
- **Minimal copy**: Settings items and menus keep only informative labels—**no redundant subtitle text, obvious explanations, or decorative eyebrow badges**. Body text is never smaller than `14px`.
- **Flat solid surfaces**: Cards, buttons, inputs, and segmented controls use single-layer hairline borders and solid fills; colored outer glows, heavy drop shadows, and gradient backgrounds are forbidden.

---

## 2. Ocean Paper Semantic Palette (Aligned with `internal/theme/theme.go`)

All design canvases (`.dc.html`) and Go MyGo components (`internal/theme/theme.go`) must use these semantic tokens exclusively. Hardcoding off-palette hues (such as deep navy fills or oversaturated blue borders) is prohibited:

| Semantic Role | Token | Ocean Light | Ocean Dark | Usage |
| --- | --- | --- | --- | --- |
| Quiet shell | `shell` | `#f1f1f1` | `#262626` | Top titlebar, segmented track fill, secondary input fill |
| Sidebar surface | `sidebar` | `#fafafa` | `#222222` | Left file tree, standalone settings category sidebar |
| Settings canvas | `settings-bg` | `#f8f8f8` | `#181818` | Settings main content canvas background |
| Reading paper / card | `paper` | `#ffffff` | `#181818` (popover/card `#222222`) | Editor paper, settings group cards, white buttons |
| Menu surface | `menu` | `#ffffff` | `#222222` | Dropdown menus, context menus, popover panels |
| Primary text | `text-primary` | `#20242c` (editor `#1f1e1d`) | `#ececec` | Headings, body copy, primary form labels |
| Secondary text | `text-secondary` | `#526071` (editor `#52514e`) | `#b4b4b4` | Helper info, shortcut hints, inactive tabs |
| Ocean accent | `accent` | `#547dd0` (hover `#3d63b0`) | `#779de8` | Caret, text selection, links, drag drop-line, input focus, primary buttons & switches |
| Duo icon fill | `icon-fill` | `#dcdcd9` | `#474747` | Neutral gray fill surface in the Duo icon system |
| Subtle hover fill | `hover` | `#f0f0ef` | `#2c2c2c` | Hover feedback for list rows, buttons, and menu items |
| Selected surface | `selected` | `#e5e5e3` | `#3a3a3a` | Selected list rows, segmented items, and selected cards |
| Selected outline | `selected-border` | `rgba(0,0,0,.10)` | `rgba(255,255,255,.12)` | `1px` neutral inner outline for selected items distinguished from hover |
| Hairline / card border | `hairline` / `border` | `#e4e6ea` (shell divider `#e6e6e6`) | `#292929` (shell divider `#292929`) | Settings card borders and section dividers |
| Button & control border | `btn-border` | `#dcdcdc` | `#444444` | Secondary buttons, inputs, and keycap borders |
| Code block background | `code-bg` | `#f3f1ec` | `#242424` | Inline code and code block surface |
| Blockquote bar | `quote-bar` | `#dbd9d4` | `#484848` | Left vertical bar for blockquotes |
| Text selection | `selection` | `#cde2fb` (text `#184f95`) | `#2d426b` (text `#9dbcf5`) | Text selection highlight in editor and inputs |

---

## 3. Typography, Weights & Geometry Scale

| Role | Font Stack | Size / Line Height | Weight |
| --- | --- | --- | --- |
| UI & controls | macOS `SF Pro` (`.SystemUIFont`), CJK fallback `PingFang SC`; Windows `Segoe UI` | `14px / 20px` (baseline) | Regular `400`, controls/nav `460`, group headings `560` |
| Settings page title | Same as UI stack | `24px / 32px` | `560` |
| Settings card title | Same as UI stack | `15px / 22px` | `560` |
| Editor body | Sans `.SystemUIFont`; Serif `LXGW WenKai` / `Songti SC` | `16px` (standard reading measure `672px`–`720px`) | `400` |
| Monospace code & keycaps | `JetBrains Mono, Menlo, ui-monospace, monospace` | Code `13px–14px`; keycaps `12px / 14px` | `400` / `500` |

### Layout & Control Baselines

- **Top bar height**: `40px`, with a `260px` left column (aligned with the sidebar, containing window traffic lights and the sidebar toggle button).
- **Left sidebar width**: `260px`; list row height `32px`, radius referencing `theme.RadiusControl` (`8px`), horizontal padding `0 8px`.
- **Settings content area**: Background `settings-bg` (`#f8f8f8` / `#181818`), centered container `max-width: 720px`, padding `40px 28px 64px`.
- **Settings group card**: Background `paper` (`#ffffff` / `#222222`), border `1px solid border` (`#e4e6ea` / `#292929`), radius referencing `theme.RadiusSurface` (`14px`), padding `18px 20px`, row gap `14px–16px`.
- **Segmented control**: Outer track `background: shell; border: 1px solid border; padding: 3px; gap: 2px`; both the outer track and inner `.seg` items (height `32px–34px`) reference `theme.RadiusControl` (`8px`); active state uses `background: selected; color: text-primary; box-shadow: inset 0 0 0 1px accent`.
- **Three-way provider cards (AI Provider Plan C)**: Keep only **DeepSeek**, **OpenAI Protocol**, and **Claude Protocol** in an equal-width horizontal row; default state `background: shell; border: 1px solid border`; selected state `background: selected; border: 1px solid accent; box-shadow: inset 0 0 0 1px accent`.
- **Global Pluck Switch**: Every boolean switch across the app must call `internal/ui.Switch` / `SwitchWithOptions` / `TogglePill` (never call `ui.SwitchBase` directly from feature packages). All sizes share a lightweight `28×16` track: ON uses a tinted accent fill (`Accent` at 24% opacity) with a `12px` accent knob; OFF uses `SwitchTrackOff` light gray with a `10px` `SwitchKnobOff` gray knob (no knob shadow or stroke). Primary slots (such as plugin settings titlebars) use `SwitchHero` (same track, `44px` hit height) without text status labels or `PluckDivider`. Covers idle, hover, bidirectional press stretch, starting spinner, error red-outline shake, disabled, and keyboard focus ring—zero idle repaints and respects reduced motion.
- **Plugin Titlebar Ghost Actions**: Primary action, retry, running, and `⋯` buttons in plugin settings titlebars (including third-party plugins) are locked by the host to lightweight ghost styling (`HeightChrome` `26px`, radius `RadiusSM` `6px`, transparent fill, hover `ShellChrome().Hover`, pressed `Selected`), separated from the switch by a `1×14px` vertical divider. Idle state omits verbose descriptions (showing only a compact `HH:MM` timestamp with full details on hover) and surfaces a red error pill with a detail popover only on warning or error.
- **Checklist rows (plugin checklists)**: Selection is conveyed solely by the accent checkbox; the row itself keeps a transparent fill with `1px solid border` and uses `hover` only on hover/press—**never fill the entire selected row with `selected` gray**.

---

## 4. Scheme A Two-Tone Semantic Icons (Duo Icons)

Global menus, sidebars, and settings group icons use **Scheme A two-tone semantic line icons**:
- **Primary stroke**: `1.5px–1.6px` round-cap stroke inheriting `currentColor` (adapts to primary/secondary text color).
- **Semantic fill**: `#c9d8f3` (dark `#31456a`), marking document surfaces, panel surfaces, or core geometry.
- **Accent detail stroke**: `#547dd0` (dark `#779de8`), highlighting arrows, plus marks, carets, or active focus cues.
