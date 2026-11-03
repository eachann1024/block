use std::path::{Path, PathBuf};

use super::sort::natural_cmp;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Node {
    pub name: String,
    pub path: PathBuf,
    pub dir: bool,
    pub children: Vec<Node>,
}

const IGNORED: &[&str] = &[
    "node_modules", "dist", "build", "target", "__pycache__", ".next", ".nuxt", ".venv", "venv",
];

pub fn ignored(name: &str) -> bool {
    name.starts_with('.')
        || name.starts_with("~$")
        || IGNORED.iter().any(|item| *item == name)
        || matches!(name.to_ascii_lowercase().as_str(), "thumbs.db" | "desktop.ini")
}

pub fn is_markdown(name: &str) -> bool {
    let lower = name.to_ascii_lowercase();
    lower.ends_with(".md") || lower.ends_with(".markdown")
}

/// Folders first, then case-insensitive natural order by name.
pub fn sort_nodes(nodes: &mut [Node]) {
    nodes.sort_by(|a, b| b.dir.cmp(&a.dir).then_with(|| natural_cmp(&a.name, &b.name)));
    for node in nodes {
        sort_nodes(&mut node.children);
    }
}

/// Reads `dir` one level deep worth of recursion. Only markdown files and
/// folders survive; hidden and tooling directories are dropped.
pub fn scan(dir: &Path) -> Vec<Node> {
    let mut nodes = Vec::new();
    let Ok(entries) = std::fs::read_dir(dir) else {
        return nodes;
    };
    for entry in entries.flatten() {
        let name = entry.file_name().to_string_lossy().to_string();
        if ignored(&name) {
            continue;
        }
        let path = entry.path();
        let dir = entry.file_type().map(|t| t.is_dir()).unwrap_or(false);
        if dir {
            nodes.push(Node { children: scan(&path), name, path, dir: true });
        } else if is_markdown(&name) {
            nodes.push(Node { children: Vec::new(), name, path, dir: false });
        }
    }
    sort_nodes(&mut nodes);
    nodes
}

/// `未命名.md`, then `未命名 2.md`, `未命名 3.md`, ...
pub fn fresh_note_path(dir: &Path) -> PathBuf {
    let base = "未命名";
    let plain = dir.join(format!("{base}.md"));
    if !plain.exists() {
        return plain;
    }
    for n in 2..10_000 {
        let candidate = dir.join(format!("{base} {n}.md"));
        if !candidate.exists() {
            return candidate;
        }
    }
    dir.join(format!("{base} {}.md", std::process::id()))
}
