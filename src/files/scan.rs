use std::cmp::Ordering;
use std::io;
use std::path::{Path, PathBuf};
use super::sort::natural_cmp;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Node {
    pub name: String,
    pub path: PathBuf,
    pub dir: bool,
    pub children: Vec<Node>,
}

#[derive(Debug)]
pub struct Scan {
    pub nodes: Vec<Node>,
    pub warnings: Vec<String>,
    pub warning_count: usize,
}

const IGNORED: &[&str] = &[
    "node_modules", "dist", "build", "target", "__pycache__", ".next", ".nuxt", ".venv", "venv",
];

pub fn ignored(name: &str) -> bool {
    name.starts_with('.') || name.starts_with("~$") || IGNORED.contains(&name)
        || name.eq_ignore_ascii_case("thumbs.db") || name.eq_ignore_ascii_case("desktop.ini")
}

pub fn ignored_dir(name: &str) -> bool {
    if ignored(name) {
        return true;
    }
    if name.eq_ignore_ascii_case(&crate::theme::assets_folder()) {
        return crate::theme::hide_assets_folder();
    }
    if let Some(hide) = crate::theme::matches_folder_rule(name) {
        return hide;
    }
    false
}

pub fn is_markdown(name: &str) -> bool {
    Path::new(name).extension().and_then(|e| e.to_str()).is_some_and(|ext| {
        matches!(
            ext.to_ascii_lowercase().as_str(),
            "md" | "markdown" | "mdown" | "mkd" | "mkdn" | "mdwn" | "mdtxt" | "mdtext" | "mdx"
        )
    })
}

pub fn compare(a: &Node, b: &Node) -> Ordering {
    compare_names(a.dir, &a.name, b.dir, &b.name).then_with(|| a.path.cmp(&b.path))
}

pub fn compare_names(a_dir: bool, a: &str, b_dir: bool, b: &str) -> Ordering {
    b_dir.cmp(&a_dir).then_with(|| natural_cmp(a, b)).then_with(|| a.cmp(b))
}

/// Sort this level exactly once. Descendants have already been sorted by scan.
pub fn sort_nodes(nodes: &mut [Node]) { nodes.sort_by(compare); }

/// Run on a worker. Root errors abort; child errors retain siblings and diagnostics.
/// Directory symlinks are not followed, avoiding cycles and workspace escapes.
pub fn scan(dir: &Path) -> io::Result<Scan> {
    #[cfg(target_family = "wasm")]
    { return super::vfs::scan_dir(dir); }
    #[cfg(not(target_family = "wasm"))]
    {
        let mut result = Scan { nodes: Vec::new(), warnings: Vec::new(), warning_count: 0 };
        result.nodes = read_level(dir, &mut result, 0)?;
        Ok(result)
    }
}

fn warn(result: &mut Scan, path: &Path, error: impl std::fmt::Display) {
    result.warning_count += 1;
    if result.warnings.len() < 8 {
        let sep = crate::theme::tr("：", ": ");
        result.warnings.push(format!("{}{sep}{error}", path.display()));
    }
}

fn read_level(dir: &Path, result: &mut Scan, depth: usize) -> io::Result<Vec<Node>> {
    if depth > 256 {
        return Err(io::Error::other(crate::theme::tr("目录层级超过 256 层", "Directory depth exceeds 256 levels")));
    }
    let entries = std::fs::read_dir(dir)?;
    let mut nodes = Vec::new();
    for entry in entries {
        let entry = match entry {
            Ok(entry) => entry,
            Err(error) => { warn(result, dir, error); continue; }
        };
        let name = entry.file_name().to_string_lossy().into_owned();
        if ignored(&name) { continue; }
        let path = entry.path();
        let kind = match entry.file_type() {
            Ok(kind) => kind,
            Err(error) => { warn(result, &path, error); continue; }
        };
        if kind.is_dir() {
            if ignored_dir(&name) { continue; }
            let children = read_level(&path, result, depth + 1).unwrap_or_else(|error| {
                warn(result, &path, error);
                Vec::new()
            });
            nodes.push(Node { name, path, dir: true, children });
        } else if (kind.is_file() || kind.is_symlink()) && is_markdown(&name) {
            nodes.push(Node { name, path, dir: false, children: Vec::new() });
        }
    }
    sort_nodes(&mut nodes);
    Ok(nodes)
}
