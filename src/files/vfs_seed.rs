//! Seed Markdown note templates for the in-memory VFS.

pub(super) const NOTE_WELCOME: &str = r#"# 欢迎使用 Block —— 毫秒级原生 Markdown 笔记

这是一份直接运行在浏览器 **WebAssembly + WebGPU/Canvas** 中的真实 **Block** 工作区。左侧文件树、多标签页、全局搜索（⌘K）、设置面板（⌘,）与生成长图（⇧⌘E）均由同一套 Rust + GPUI 代码直接驱动。

> [!TIP]
> 试着在左侧文件树切换笔记、新建笔记（⌘N）、按 / 呼出块菜单，或点击右上角 ··· → **生成图片…** 体验完整功能。

## 核心工作区体验

- [x] **左侧文件树与多标签页** — 支持新建笔记、新建文件夹、内联重命名、右键菜单与标签页切换
- [x] **所见即所得块级编辑** — 标题、待办清单、多列分栏、代码高亮、数学公式、表格交互全部实时响应
- [ ] 在下方任意段落直接输入文字，或选中文本体验浮动格式工具栏

## 性能与架构对比

| 维度 | Block (Rust + GPUI) | 传统 Electron 笔记 |
| :--- | :--- | :--- |
| 冷启动耗时 | **< 40 ms** | 1,200 – 2,800 ms |
| 空闲内存占用 | **~28 MB** | 350 – 800 MB |
| 本地存储格式 | **纯正 `.md` 文件** | 私有数据库 / 锁定云端 |

## 代码块与数学公式

```rust
pub fn render_workspace(note: &MarkdownDoc, gpu: &mut GpuCanvas) {
    for block in note.visible_blocks() {
        gpu.paint_block(block);
    }
}
```

$$
\mathcal{L}(\theta) = \mathbb{E}_{x \sim p_{\text{data}}}\bigl[-\log p_{\theta}(x)\bigr] + \lambda \|\theta\|_2^2
$$
"#;

pub(super) const NOTE_ARCH: &str = r#"# 毫秒级块级编辑器架构

Block 将每一篇 Markdown 文档拆分为独立的块级节点，配合虚拟列表与增量排版缓存，即使面对十万字长文也能保持 120fps 的丝滑滚动与输入。

## 快捷键速查

| 功能 | macOS 快捷键 | 说明 |
| :--- | :--- | :--- |
| 全局搜索 | `⌘K` | 按标题与正文毫秒级检索工作区全部笔记 |
| 新建笔记 | `⌘N` | 在当前目录立即创建新 Markdown 笔记并打开标签页 |
| 页内查找 | `⌘F` | 当前笔记内高亮匹配与替换 |
| 生成图片 | `⇧⌘E` | 导出 6 款精选纸张主题的高清 2× 视网膜长图 |

## 块级交互清单

1. 将鼠标悬停在任意段落左侧，点击 `+` 或拖拽 `⋮⋮` 把手调整顺序
2. 输入 `/` 即可过滤并插入标题、表格、代码块、标注框或数学公式
3. 点击右上角 `···` 菜单可一键复制 Markdown 源码或生成分享长图
"#;

pub(super) const NOTE_GUIDE: &str = r#"# 排版与导出卡片指南

Block 内置了由 `tiny-skia` 与 `cosmic-text` 驱动的离线排版渲染引擎，可将当前笔记或选中片段一键渲染为 2× 高清 PNG 长图、可交互 HTML、矢量 PDF 或标准 `.docx` 文档。

> [!NOTE]
> 点击窗口右上角的 `···` 按钮并选择 **生成图片…（⇧⌘E）**，即可实时切换「留白、暖纸、墨夜、赛博、手账、极光」六款主题。

## 支持导出的格式

- **PNG 长图（2×）** — 适合直接分享至社交媒体与即时通讯工具
- **HTML 网页** — 单文件内联样式，完美保留代码高亮、表格与排版细节
- **PDF / Word 文档** — 适合正式归档、打印与跨团队协作交付
"#;

pub(super) const NOTE_WELCOME_EN: &str = r#"# Welcome to Block — Instant Native Markdown Notes

You are using the live **Block** workspace compiled directly to **WebAssembly + WebGPU/Canvas**. The file tree, multi-tab bar, global search (`⌘K`), preferences (`⌘,`), and share-card export (`⇧⌘E`) all run on the exact same Rust + GPUI codebase as the macOS desktop app.

> [!TIP]
> Switch notes in the sidebar, create a new note (`⌘N`), type `/` on an empty line to open the block menu, or click `···` → **Export Image…** in the top-right corner to try it out.

## Workspace Highlights

- [x] **Sidebar file tree & multi-tab bar** — create notes or folders, rename inline, open context menus, and switch tabs effortlessly
- [x] **Live block-level editing** — headings, checklists, multi-column layouts, syntax-highlighted code, LaTeX math, and interactive tables respond on every keystroke
- [ ] Click into any paragraph below to start typing, or select text to bring up the floating formatting toolbar

## Architecture at a Glance

| Metric | Block (Rust + GPUI) | Typical Electron Note App |
| :--- | :--- | :--- |
| Cold launch | **< 40 ms** | 1,200 – 2,800 ms |
| Idle memory | **~28 MB** | 350 – 800 MB |
| On-disk format | **Plain `.md` files** | Proprietary DB / cloud lock-in |

## Code Blocks & Math

```rust
pub fn render_workspace(note: &MarkdownDoc, gpu: &mut GpuCanvas) {
    for block in note.visible_blocks() {
        gpu.paint_block(block);
    }
}
```

$$
\mathcal{L}(\theta) = \mathbb{E}_{x \sim p_{\text{data}}}\bigl[-\log p_{\theta}(x)\bigr] + \lambda \|\theta\|_2^2
$$
"#;

pub(super) const NOTE_ARCH_EN: &str = r#"# Sub-Millisecond Block Editor

Block parses every Markdown document into discrete structural nodes backed by a virtualized list and incremental layout cache, sustaining 120 fps scrolling and typing even on 100,000-word documents.

## Keyboard Shortcuts

| Action | macOS Shortcut | Description |
| :--- | :--- | :--- |
| Global Search | `⌘K` | Search note titles and full text across your workspace |
| New Note | `⌘N` | Create a new Markdown note in the current folder and open it |
| Find in Note | `⌘F` | Highlight and step through matches inside the active note |
| Export Image | `⇧⌘E` | Render a crisp 2× Retina share card across six paper themes |

## Working with Blocks

1. Hover to the left of any block to click `+` or drag the `⋮⋮` handle to reorder
2. Type `/` on an empty line to insert headings, tables, code blocks, callouts, or math
3. Open the top-right `···` menu to copy clean Markdown or export a shareable card
"#;

pub(super) const NOTE_GUIDE_EN: &str = r#"# Typography & Share Cards

Block includes an offline typesetting pipeline powered by `tiny-skia` and `cosmic-text` that turns any note—or selected passage—into a 2× Retina PNG card, self-contained HTML page, vector PDF, or standard `.docx` document.

> [!NOTE]
> Click `···` in the top-right corner and choose **Export Image… (`⇧⌘E`)** to preview all six curated paper themes: Minimal, Warm Paper, Ink Night, Cyber, Journal, and Aurora.

## Export Formats

- **PNG Card (2×)** — crisp long-form cards ready to share in chat or social feeds
- **HTML Document** — single-file inline styles preserving code highlighting, tables, and layout
- **PDF / Word (`.docx`)** — clean deliverables for archiving, printing, and team handoff
"#;
