//! Workspace-wide note search. Runs on a worker: walks every Markdown file,
//! matches case-insensitively in titles first and bodies second.
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
    /// Folder relative to the workspace root ("" at the root).
    pub folder: String,
    pub title_match: bool,
    pub title_marks: Vec<Range<usize>>,
    pub snippet: String,
    pub marks: Vec<Range<usize>>,
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
    let line = text.lines().map(str::trim).find(|l| !l.is_empty()).unwrap_or("");
    line.trim_start_matches('#').trim().chars().take(70).collect()
}

fn inspect(root: &Path, path: &Path, query: &str) -> Option<Hit> {
    let stem = path.file_stem()?.to_string_lossy().into_owned();
    let title_marks = find_all(&stem, query);
    let size = std::fs::metadata(path).map(|m| m.len()).unwrap_or(0);
    let body = if size <= MAX_FILE_BYTES { std::fs::read_to_string(path).unwrap_or_default() } else { String::new() };
    let found = find_all(&body, query).into_iter().next();
    if title_marks.is_empty() && found.is_none() { return None; }
    let (snippet, marks) = match found {
        Some(range) => {
            let snippet = snippet_around(&body, range);
            let marks = find_all(&snippet, query);
            (snippet, marks)
        }
        None => (first_line(&body), Vec::new()),
    };
    let folder = path.parent().and_then(|p| p.strip_prefix(root).ok()).map(|p| {
        p.components().map(|c| c.as_os_str().to_string_lossy().into_owned()).collect::<Vec<_>>().join(" / ")
    }).unwrap_or_default();
    Some(Hit { path: path.to_owned(), title: stem, folder, title_match: !title_marks.is_empty(),
        title_marks, snippet, marks })
}

/// Title matches first, then body matches; at most `limit` hits in total.
pub fn search_notes(root: &Path, query: &str, limit: usize) -> Vec<Hit> {
    let query = query.trim();
    if query.is_empty() { return Vec::new(); }
    let (mut titles, mut bodies) = (Vec::new(), Vec::new());
    let mut stack = vec![root.to_owned()];
    while let Some(dir) = stack.pop() {
        let Ok(entries) = std::fs::read_dir(&dir) else { continue };
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().into_owned();
            if ignored(&name) { continue; }
            let Ok(kind) = entry.file_type() else { continue };
            let path = entry.path();
            if kind.is_dir() { stack.push(path); }
            else if (kind.is_file() || kind.is_symlink()) && is_markdown(&name) {
                if let Some(hit) = inspect(root, &path, query) {
                    if hit.title_match { titles.push(hit) } else { bodies.push(hit) }
                }
            }
        }
        if titles.len() + bodies.len() >= limit * 4 { break; }
    }
    let key = |h: &Hit| (h.title.to_lowercase(), h.path.clone());
    titles.sort_by_key(key);
    bodies.sort_by_key(key);
    titles.extend(bodies);
    titles.truncate(limit);
    titles
}
