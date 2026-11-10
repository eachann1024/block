use std::{collections::HashSet, fs, path::{Path, PathBuf}, sync::{Arc, atomic::{AtomicBool, Ordering}}, thread::{self, JoinHandle}};

#[derive(Default)]
pub struct DiskPaths {
    pub app: Option<PathBuf>,
    pub data: Vec<PathBuf>,
    pub notebooks: Vec<PathBuf>,
    pub cache: Vec<PathBuf>,
}

#[derive(Clone, Debug)]
pub struct DiskCategory { pub id: &'static str, pub bytes: u64 }

#[derive(Default)]
pub struct DiskSnapshot {
    pub categories: Vec<DiskCategory>,
    pub partial: bool,
    pub cancelled: bool,
    pub error_count: u64,
    pub errors: Vec<String>,
}

pub fn scan_disk_with_cancel(paths: DiskPaths, cancel: Arc<AtomicBool>) -> JoinHandle<DiskSnapshot> {
    thread::spawn(move || scan(paths, &cancel))
}

fn scan(paths: DiskPaths, cancel: &AtomicBool) -> DiskSnapshot {
    let mut result = DiskSnapshot::default();
    result.categories = ["app", "notebook_markdown", "notebook_other", "history", "ai_sessions", "data_other", "cache"]
        .into_iter().map(|id| DiskCategory { id, bytes: 0 }).collect();
    let mut seen = HashSet::new();
    let groups = [paths.app.into_iter().collect::<Vec<_>>(), paths.cache, paths.data, paths.notebooks];
    for (group, mut roots) in groups.into_iter().enumerate() {
        roots.sort();
        for root in roots {
            if cancelled(cancel, &mut result) { return result; }
            let root = match fs::canonicalize(&root) {
                Ok(root) => root,
                Err(error) if group == 1 && error.kind() == std::io::ErrorKind::NotFound => continue,
                Err(error) => { record_error(&mut result, &root, &error.to_string()); continue; }
            };
            let mut pending = vec![root.clone()];
            while let Some(path) = pending.pop() {
                if cancelled(cancel, &mut result) { return result; }
                if group == 3 && path != root && should_skip_notebook_entry(&path) {
                    continue;
                }
                let metadata = match fs::symlink_metadata(&path) {
                    Ok(metadata) => metadata,
                    Err(error) => { record_error(&mut result, &path, &error.to_string()); continue; }
                };
                if metadata.file_type().is_symlink() { continue; }
                if group == 3 && path != root && metadata.is_dir() && should_skip_notebook_dir(&path) {
                    continue;
                }
                // Platforms without a file identity count every path without hard-link dedupe.
                if let Some(identity) = identity(&metadata) && !seen.insert(identity) { continue; }
                if metadata.is_file() {
                    let category = category(group, &root, &path);
                    match result.categories[category].bytes.checked_add(metadata.len()) {
                        Some(total) => result.categories[category].bytes = total,
                        None => record_error(&mut result, &path, "byte count overflow"),
                    }
                } else if metadata.is_dir() {
                    match fs::read_dir(&path) {
                        Ok(entries) => {
                            let mut children = Vec::new();
                            for entry in entries {
                                if cancelled(cancel, &mut result) { return result; }
                                match entry { Ok(entry) => children.push(entry.path()), Err(error) => record_error(&mut result, &path, &error.to_string()) }
                            }
                            children.sort_unstable_by(|left, right| right.cmp(left));
                            pending.extend(children);
                        }
                        Err(error) => record_error(&mut result, &path, &error.to_string()),
                    }
                }
            }
        }
    }
    result
}

fn category(group: usize, root: &Path, path: &Path) -> usize {
    match group {
        0 => 0,
        1 => 6,
        2 => {
            let relative = path.strip_prefix(root).unwrap_or(path);
            let mut components = relative.components();
            if components.next().is_some_and(|part| part.as_os_str() == "History")
                || root.components().any(|part| part.as_os_str() == "History") { return 3; }
            if (path == root || relative.parent().is_some_and(|parent| parent.as_os_str().is_empty()))
                && matches!(path.file_name().and_then(|name| name.to_str()), Some("ai-chats.json" | "ai-active-draft.json" | "ai-failed-drafts.json")) { return 4; }
            5
        }
        _ if path.file_name().and_then(|name| name.to_str()).is_some_and(crate::files::scan::is_markdown) => 1,
        _ => 2,
    }
}

fn should_skip_notebook_entry(path: &Path) -> bool {
    path.file_name()
        .and_then(|name| name.to_str())
        .is_some_and(crate::files::scan::ignored)
}

fn should_skip_notebook_dir(path: &Path) -> bool {
    path.file_name()
        .and_then(|name| name.to_str())
        .is_some_and(|name| crate::theme::matches_folder_rule(name) == Some(true))
}

fn cancelled(cancel: &AtomicBool, result: &mut DiskSnapshot) -> bool {
    if cancel.load(Ordering::Relaxed) { result.cancelled = true; result.partial = true; true } else { false }
}

fn record_error(result: &mut DiskSnapshot, path: &Path, error: &str) {
    result.partial = true;
    result.error_count += 1;
    if result.errors.len() < 20 { result.errors.push(format!("{}: {error}", path.display())); }
}

#[cfg(unix)]
fn identity(metadata: &fs::Metadata) -> Option<(u64, u64)> {
    use std::os::unix::fs::MetadataExt;
    Some((metadata.dev(), metadata.ino()))
}

#[cfg(not(unix))]
fn identity(_: &fs::Metadata) -> Option<(u64, u64)> { None }
