//! Workspace-wide note search. Runs on a worker: walks every Markdown file,
//! matches case-insensitively in titles first and bodies second.
use std::collections::HashSet;
use std::ops::Range;
use std::path::{Path, PathBuf};
use super::scan::{ignored, is_markdown};

const MAX_FILE_BYTES: u64 = 4 * 1024 * 1024;
const BEFORE: usize = 24;
const AFTER: usize = 56;

#[derive(Clone, Debug)]
pub struct Hit {
    pub path: PathBuf,
    pub title: String,
    /// Subfolder + note label relative to the workspace root ("" at the root).
    pub folder: String,
    pub title_match: bool,
    pub title_marks: Vec<Range<usize>>,
    pub snippet: String,
    pub marks: Vec<Range<usize>>,
    pub count: usize,
}

/// Lowercase that keeps byte offsets valid (falls back to ASCII-only folding).
fn fold(text: &str) -> String {
    let lower = text.to_lowercase();
    if lower.len() == text.len() { lower } else { text.to_ascii_lowercase() }
}

pub fn find_all(text: &str, needle: &str) -> Vec<Range<usize>> {
    let (hay, needle) = (fold(text), fold(needle));
    if needle.is_empty() { return Vec::new(); }
    hay.match_indices(&needle)
        .map(|(start, found)| start..start + found.len())
        .filter(|range| text.is_char_boundary(range.start) && text.is_char_boundary(range.end))
        .collect()
}

fn snippet_around(text: &str, at: Range<usize>) -> String {
    let start = text[..at.start].char_indices().rev().nth(BEFORE - 1).map_or(0, |(i, _)| i);
    let end = text[at.end..].char_indices().nth(AFTER).map_or(text.len(), |(i, _)| at.end + i);
    let flat: String = text[start..end].chars().map(|c| if c.is_control() { ' ' } else { c }).collect();
    let prefix = if start > 0 { "…" } else { "" };
    let suffix = if end < text.len() { "…" } else { "" };
    format!("{prefix}{flat}{suffix}")
}

fn first_line(text: &str) -> String {
    let body = crate::editor::markdown::strip_frontmatter(text);
    let source = if body.trim().is_empty() { text } else { body };
    let line = source.lines().map(str::trim).find(|l| !l.is_empty()).unwrap_or("");
    line.trim_start_matches('#').trim().chars().take(70).collect()
}

fn folder_label(root: &Path, path: &Path, stem: &str, include_root_name: bool) -> String {
    let rel_folder = path.parent().and_then(|p| p.strip_prefix(root).ok()).map(|p| {
        p.components().map(|c| c.as_os_str().to_string_lossy().into_owned()).collect::<Vec<_>>().join(" / ")
    }).unwrap_or_default();
    let folder = if rel_folder.is_empty() { String::new() } else { format!("{rel_folder} / {stem}") };
    if !include_root_name { return folder; }
    let root_name = root.file_name().map(|name| name.to_string_lossy().into_owned()).unwrap_or_default();
    if root_name.is_empty() { folder } else if folder.is_empty() { root_name } else { format!("{root_name} / {folder}") }
}

fn inspect(root: &Path, path: &Path, query: &str, include_root_name: bool) -> Option<Hit> {
    let stem = path.file_stem()?.to_string_lossy().into_owned();
    let title_marks = find_all(&stem, query);
    let size = std::fs::metadata(path).map(|m| m.len()).unwrap_or(0);
    let body = if size <= MAX_FILE_BYTES { std::fs::read_to_string(path).unwrap_or_default() } else { String::new() };
    let body_matches = find_all(&body, query);
    let count = body_matches.len();
    let found = body_matches.into_iter().next();
    if title_marks.is_empty() && found.is_none() { return None; }
    let (snippet, marks) = match found {
        Some(range) => {
            let snippet = snippet_around(&body, range);
            let marks = find_all(&snippet, query);
            (snippet, marks)
        }
        None => (first_line(&body), Vec::new()),
    };
    let folder = folder_label(root, path, &stem, include_root_name);
    Some(Hit { path: path.to_owned(), title: stem, folder, title_match: !title_marks.is_empty(),
        title_marks, snippet, marks, count })
}

fn finish(mut titles: Vec<Hit>, mut bodies: Vec<Hit>, limit: usize) -> Vec<Hit> {
    let key = |hit: &Hit| (hit.title.to_lowercase(), hit.path.clone());
    titles.sort_by_key(key);
    bodies.sort_by_key(key);
    titles.extend(bodies);
    titles.truncate(limit);
    titles
}

fn walk_root(root: &Path, query: &str, limit: usize, include_root_name: bool, seen: &mut HashSet<PathBuf>, titles: &mut Vec<Hit>, bodies: &mut Vec<Hit>) {
    let mut stack = vec![root.to_owned()];
    let mut found = 0usize;
    while let Some(dir) = stack.pop() {
        if found >= limit.saturating_mul(4) { break; }
        let Ok(entries) = std::fs::read_dir(&dir) else { continue };
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().into_owned();
            if ignored(&name) { continue; }
            let Ok(kind) = entry.file_type() else { continue };
            let path = entry.path();
            if kind.is_dir() {
                if super::scan::ignored_dir(&name) { continue; }
                stack.push(path);
            } else if (kind.is_file() || kind.is_symlink()) && is_markdown(&name) && seen.insert(path.clone()) {
                if let Some(hit) = inspect(root, &path, query, include_root_name) {
                    found += 1;
                    if hit.title_match { titles.push(hit) } else { bodies.push(hit) }
                }
            }
        }
    }
}

fn gather(roots: &[PathBuf], query: &str, limit: usize, include_root_name: bool) -> Vec<Hit> {
    let query = query.trim();
    if query.is_empty() || limit == 0 { return Vec::new(); }
    #[cfg(target_family = "wasm")]
    {
        let mut seen = HashSet::new();
        let (mut titles, mut bodies) = (Vec::new(), Vec::new());
        for root in roots {
            for mut hit in super::vfs::search_all(root, query, limit) {
                if !seen.insert(hit.path.clone()) { continue; }
                if include_root_name {
                    hit.folder = folder_label(root, &hit.path, &hit.title, true);
                }
                if hit.title_match { titles.push(hit); } else { bodies.push(hit); }
            }
        }
        return finish(titles, bodies, limit);
    }
    #[cfg(not(target_family = "wasm"))]
    {
        let mut seen_roots = HashSet::new();
        let mut seen = HashSet::new();
        let (mut titles, mut bodies) = (Vec::new(), Vec::new());
        for root in roots {
            if !seen_roots.insert(root.clone()) { continue; }
            walk_root(root, query, limit, include_root_name, &mut seen, &mut titles, &mut bodies);
        }
        finish(titles, bodies, limit)
    }
}

/// Title matches first, then body matches; at most `limit` hits in total.
pub fn search_notes(root: &Path, query: &str, limit: usize) -> Vec<Hit> {
    let roots = [root.to_path_buf()];
    gather(&roots, query, limit, false)
}

/// Search every root. Duplicate note paths are skipped. Root names are prefixed only when more than one root is searched.
pub fn search_notes_in_folders(roots: &[PathBuf], query: &str, limit: usize) -> Vec<Hit> {
    gather(roots, query, limit, roots.len() > 1)
}
