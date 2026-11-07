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
    pub app_bytes: u64,
    pub data_bytes: u64,
    pub notebook_bytes: u64,
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
                let metadata = match fs::symlink_metadata(&path) {
                    Ok(metadata) => metadata,
                    Err(error) => { record_error(&mut result, &path, &error.to_string()); continue; }
                };
                if metadata.file_type().is_symlink() { continue; }
                let Some(identity) = identity(&metadata) else {
                    record_error(&mut result, &path, "file identity unavailable");
                    continue;
                };
                if !seen.insert(identity) { continue; }
                if metadata.is_file() {
                    let category = category(group, &root, &path);
                    let amount = metadata.len();
                    let category_total = result.categories[category].bytes.checked_add(amount);
                    let group_total = match group { 0 => result.app_bytes, 1 | 2 => result.data_bytes, _ => result.notebook_bytes }.checked_add(amount);
                    if let (Some(category_total), Some(group_total)) = (category_total, group_total) {
                        result.categories[category].bytes = category_total;
                        match group { 0 => result.app_bytes = group_total, 1 | 2 => result.data_bytes = group_total, _ => result.notebook_bytes = group_total }
                    } else { record_error(&mut result, &path, "byte count overflow"); }
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
        _ if path.extension().is_some_and(|ext| ext.eq_ignore_ascii_case("md")) => 1,
        _ => 2,
    }
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
