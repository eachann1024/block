//! In-memory virtual filesystem for WebAssembly so `app::Workspace` (`FileTree`,
//! multi-tab titlebar, global search, inline rename, new note/folder, settings,
//! and image export) runs with the exact same architecture as the desktop app.
use std::collections::{BTreeMap, BTreeSet};
use std::io;
use std::path::{Path, PathBuf};
use std::sync::{LazyLock, Mutex};
use super::scan::{Node, Scan, sort_nodes};
use super::search::{Hit, find_all};
use super::state::State;
use crate::theme::tr;

#[path = "vfs_seed.rs"]
mod vfs_seed;
use vfs_seed::*;

struct Vfs {
    dirs: BTreeSet<PathBuf>,
    files: BTreeMap<PathBuf, String>,
    state: State,
}

static VFS: LazyLock<Mutex<Vfs>> = LazyLock::new(|| Mutex::new(seed()));

fn seed() -> Vfs {
    let en = crate::theme::is_en();
    let root = PathBuf::from("/Block");
    let sub = root.join(if en { "Life Notes" } else { "生活手记" });
    let welcome = root.join(if en { "Weekend Getaway Plan.md" } else { "潮州周末漫游计划.md" });
    let arch = root.join(if en { "Luggage & Errands.md" } else { "出行行李与待办.md" });
    let guide = sub.join(if en { "Home Cafe & Recipes.md" } else { "周末家庭咖啡馆.md" });

    let mut dirs = BTreeSet::new();
    dirs.insert(PathBuf::from("/"));
    dirs.insert(root.clone());
    dirs.insert(sub);

    let mut files = BTreeMap::new();
    files.insert(welcome.clone(), (if en { NOTE_WELCOME_EN } else { NOTE_WELCOME }).to_owned());
    files.insert(arch.clone(), (if en { NOTE_ARCH_EN } else { NOTE_ARCH }).to_owned());
    files.insert(guide.clone(), (if en { NOTE_GUIDE_EN } else { NOTE_GUIDE }).to_owned());

    let state = State {
        folder: Some(root.clone()),
        notebooks: vec![root],
        language: crate::theme::language_mode(),
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
                if super::scan::ignored_dir(&name) { continue; }
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
                if super::scan::ignored(&name) { continue; }
                nodes.push(Node {
                    name,
                    path: path.clone(),
                    dir: false,
                    children: Vec::new(),
                });
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
        if path.strip_prefix(root).ok().is_some_and(|rel| {
            rel.components().any(|c| super::scan::ignored_dir(&c.as_os_str().to_string_lossy()))
        }) {
            continue;
        }
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
