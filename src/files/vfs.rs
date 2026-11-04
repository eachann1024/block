//! In-memory virtual filesystem for WebAssembly so `app::Workspace` (`FileTree`,
//! multi-tab titlebar, global search, inline rename, new note/folder, settings,
//! and image export) runs with the exact same architecture as the desktop app.
use std::collections::{BTreeMap, BTreeSet};
use std::io;
use std::path::{Path, PathBuf};
use std::sync::{LazyLock, Mutex};
use super::scan::{Node, Scan, is_markdown, sort_nodes};
use super::search::{Hit, find_all};
use super::state::State;
use crate::theme::tr;

struct Vfs {
    dirs: BTreeSet<PathBuf>,
    files: BTreeMap<PathBuf, String>,
    state: State,
}

static VFS: LazyLock<Mutex<Vfs>> = LazyLock::new(|| Mutex::new(seed()));

fn seed() -> Vfs {
    let root = PathBuf::from("/Yu");
    let sub = root.join("设计笔记");
    let welcome = root.join("欢迎使用 Yu.md");
    let arch = root.join("毫秒级块级编辑器.md");
    let guide = sub.join("排版与导出卡片指南.md");

    let mut dirs = BTreeSet::new();
    dirs.insert(PathBuf::from("/"));
    dirs.insert(root.clone());
    dirs.insert(sub);

    let mut files = BTreeMap::new();
    files.insert(welcome.clone(), NOTE_WELCOME.to_owned());
    files.insert(arch.clone(), NOTE_ARCH.to_owned());
    files.insert(guide.clone(), NOTE_GUIDE.to_owned());

    let state = State {
        folder: Some(root.clone()),
        notebooks: vec![root],
        open_tabs: vec![welcome.clone(), arch, guide],
        active_tab: Some(welcome),
        ..State::default()
    };
    Vfs { dirs, files, state }
}

pub fn load_state() -> io::Result<State> {
    Ok(VFS.lock().unwrap().state.clone())
}

pub fn update_state(change: impl FnOnce(&mut State)) -> io::Result<State> {
    let mut vfs = VFS.lock().unwrap();
    change(&mut vfs.state);
    Ok(vfs.state.clone())
}

pub fn is_file(path: &Path) -> bool {
    VFS.lock().unwrap().files.contains_key(path)
}

pub fn is_dir(path: &Path) -> bool {
    VFS.lock().unwrap().dirs.contains(path)
}

pub fn exists(path: &Path) -> bool {
    let vfs = VFS.lock().unwrap();
    vfs.files.contains_key(path) || vfs.dirs.contains(path)
}

pub fn read_to_string(path: &Path) -> io::Result<String> {
    VFS.lock()
        .unwrap()
        .files
        .get(path)
        .cloned()
        .ok_or_else(|| io::Error::new(io::ErrorKind::NotFound, tr("文件已不存在", "File not found")))
}

pub fn write_string(path: &Path, content: String) -> io::Result<()> {
    let mut vfs = VFS.lock().unwrap();
    let mut cur = path.parent();
    while let Some(p) = cur {
        vfs.dirs.insert(p.to_path_buf());
        if p == Path::new("/") { break; }
        cur = p.parent();
    }
    vfs.files.insert(path.to_path_buf(), content);
    Ok(())
}

pub fn create_note_exclusive(path: &Path) -> io::Result<()> {
    let mut vfs = VFS.lock().unwrap();
    if vfs.files.contains_key(path) || vfs.dirs.contains(path) {
        return Err(io::Error::new(io::ErrorKind::AlreadyExists, "exists"));
    }
    if let Some(parent) = path.parent() {
        vfs.dirs.insert(parent.to_path_buf());
    }
    vfs.files.insert(path.to_path_buf(), String::new());
    Ok(())
}

pub fn create_dir_exclusive(path: &Path) -> io::Result<()> {
    let mut vfs = VFS.lock().unwrap();
    if vfs.dirs.contains(path) || vfs.files.contains_key(path) {
        return Err(io::Error::new(io::ErrorKind::AlreadyExists, "exists"));
    }
    vfs.dirs.insert(path.to_path_buf());
    Ok(())
}

pub fn remove_tree(path: &Path) -> io::Result<()> {
    let mut vfs = VFS.lock().unwrap();
    let had_file = vfs.files.remove(path).is_some();
    let sub_files: Vec<PathBuf> = vfs.files.keys().filter(|k| k.starts_with(path)).cloned().collect();
    for k in sub_files { vfs.files.remove(&k); }
    let sub_dirs: Vec<PathBuf> = vfs.dirs.iter().filter(|d| d.starts_with(path)).cloned().collect();
    let had_dir = !sub_dirs.is_empty();
    for d in sub_dirs { vfs.dirs.remove(&d); }
    if !had_file && !had_dir {
        return Err(io::Error::new(io::ErrorKind::NotFound, tr("文件已不存在", "File no longer exists")));
    }
    Ok(())
}

pub fn rename_tree(from: &Path, to: &Path) -> io::Result<()> {
    let mut vfs = VFS.lock().unwrap();
    if let Some(body) = vfs.files.remove(from) {
        vfs.files.insert(to.to_path_buf(), body);
        return Ok(());
    }
    if !vfs.dirs.contains(from) {
        return Err(io::Error::new(io::ErrorKind::NotFound, tr("文件已不存在", "File no longer exists")));
    }
    let dir_list: Vec<PathBuf> = vfs.dirs.iter().filter(|d| d.starts_with(from)).cloned().collect();
    for old in dir_list {
        vfs.dirs.remove(&old);
        if let Some(rebased) = super::ops::rebase(&old, from, to) {
            vfs.dirs.insert(rebased);
        }
    }
    let file_list: Vec<PathBuf> = vfs.files.keys().filter(|k| k.starts_with(from)).cloned().collect();
    for old in file_list {
        if let (Some(rebased), Some(body)) = (super::ops::rebase(&old, from, to), vfs.files.remove(&old)) {
            vfs.files.insert(rebased, body);
        }
    }
    Ok(())
}

pub fn scan_dir(root: &Path) -> io::Result<Scan> {
    let vfs = VFS.lock().unwrap();
    if !vfs.dirs.contains(root) {
        return Err(io::Error::new(io::ErrorKind::NotFound, tr("文件夹不存在", "Folder not found")));
    }
    Ok(Scan {
        nodes: build_level(&vfs, root),
        warnings: Vec::new(),
        warning_count: 0,
    })
}

fn build_level(vfs: &Vfs, dir: &Path) -> Vec<Node> {
    let mut nodes = Vec::new();
    for sub in &vfs.dirs {
        if sub != dir && sub.parent() == Some(dir) {
            if let Some(name) = sub.file_name().map(|n| n.to_string_lossy().into_owned()) {
                nodes.push(Node {
                    name,
                    path: sub.clone(),
                    dir: true,
                    children: build_level(vfs, sub),
                });
            }
        }
    }
    for path in vfs.files.keys() {
        if path.parent() == Some(dir) {
            if let Some(name) = path.file_name().map(|n| n.to_string_lossy().into_owned()) {
                if is_markdown(&name) {
                    nodes.push(Node {
                        name,
                        path: path.clone(),
                        dir: false,
                        children: Vec::new(),
                    });
                }
            }
        }
    }
    sort_nodes(&mut nodes);
    nodes
}

pub fn search_all(root: &Path, query: &str, limit: usize) -> Vec<Hit> {
    let query = query.trim();
    if query.is_empty() { return Vec::new(); }
    let vfs = VFS.lock().unwrap();
    let (mut titles, mut bodies) = (Vec::new(), Vec::new());
    for (path, body) in &vfs.files {
        if !path.starts_with(root) { continue; }
        let Some(stem) = path.file_stem().map(|s| s.to_string_lossy().into_owned()) else { continue };
        let title_marks = find_all(&stem, query);
        let body_matches = find_all(body, query);
        let count = body_matches.len();
        let found = body_matches.into_iter().next();
        if title_marks.is_empty() && found.is_none() { continue; }
        let snippet = body.lines().map(str::trim).find(|l| !l.is_empty()).unwrap_or("").trim_start_matches('#').trim().chars().take(70).collect::<String>();
        let marks = find_all(&snippet, query);
        let rel_folder = path.parent().and_then(|p| p.strip_prefix(root).ok()).map(|p| {
            p.components().map(|c| c.as_os_str().to_string_lossy().into_owned()).collect::<Vec<_>>().join(" / ")
        }).unwrap_or_default();
        let folder = if rel_folder.is_empty() { String::new() } else { format!("{rel_folder} / {stem}") };
        let hit = Hit { path: path.clone(), title: stem, folder, title_match: !title_marks.is_empty(), title_marks, snippet, marks, count };
        if hit.title_match { titles.push(hit); } else { bodies.push(hit); }
    }
    titles.extend(bodies);
    titles.truncate(limit);
    titles
}

const NOTE_WELCOME: &str = r#"# 欢迎使用 Yu —— 毫秒级原生 Markdown 笔记

这是一份直接运行在浏览器 **WebAssembly + WebGPU/Canvas** 中的真实 **Yu (Thinkly Fast)** 工作区。左侧文件树、多标签页、全局搜索（`⌘K`）、设置面板（`⌘,`）与生成长图（`⇧⌘E`）均由同一套 Rust + GPUI 代码直接驱动。

> [!TIP]
> 试着在左侧文件树切换笔记、新建笔记（`⌘N`）、按 `/` 呼出块菜单，或点击右上角 `···` → **生成图片…** 体验完整功能。

## 核心工作区体验

- [x] **左侧文件树与多标签页** — 支持新建笔记、新建文件夹、内联重命名、右键菜单与标签页切换
- [x] **所见即所得块级编辑** — 标题、待办清单、多列分栏、代码高亮、数学公式、表格交互全部实时响应
- [ ] 在下方任意段落直接输入文字，或选中文本体验浮动格式工具栏

## 性能与架构对比

| 维度 | Yu (Rust + GPUI) | 传统 Electron 笔记 |
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

const NOTE_ARCH: &str = r#"# 毫秒级块级编辑器架构

Yu 将每一篇 Markdown 文档拆分为独立的块级节点（Block），配合虚拟列表与增量排版缓存，即使面对十万字长文也能保持 120fps 的丝滑滚动与输入。

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

const NOTE_GUIDE: &str = r#"# 排版与导出卡片指南

Yu 内置了由 `tiny-skia` 与 `cosmic-text` 驱动的离线排版渲染引擎，可将当前笔记或选中片段一键渲染为 2× 高清 PNG 长图、可交互 HTML、矢量 PDF 或标准 `.docx` 文档。

> [!NOTE]
> 点击窗口右上角的 `···` 按钮并选择 **生成图片…（⇧⌘E）**，即可实时切换「留白、暖纸、墨夜、赛博、手账、极光」六款主题。

## 支持导出的格式

- **PNG 长图（2×）** — 适合直接分享至社交媒体与即时通讯工具
- **HTML 网页** — 单文件内联样式，完美保留代码高亮、表格与排版细节
- **PDF / Word 文档** — 适合正式归档、打印与跨团队协作交付
"#;
