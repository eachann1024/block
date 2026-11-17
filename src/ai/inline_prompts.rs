//! Persists inline AI user prompts: starred favorites stay permanently; unstarred keeps only the latest 1.
use std::sync::{Mutex, OnceLock};
use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub(in crate::editor) struct SavedPrompt {
    pub text: String,
    #[serde(default)]
    pub starred: bool,
}

static CACHE: OnceLock<Mutex<Option<Vec<SavedPrompt>>>> = OnceLock::new();

fn cache() -> &'static Mutex<Option<Vec<SavedPrompt>>> {
    CACHE.get_or_init(|| Mutex::new(None))
}

#[cfg(not(target_family = "wasm"))]
fn storage_path() -> std::path::PathBuf {
    crate::files::app_data_dir("Block").join("ai-inline-prompts.json")
}

fn load_disk() -> Vec<SavedPrompt> {
    #[cfg(target_family = "wasm")]
    {
        Vec::new()
    }
    #[cfg(not(target_family = "wasm"))]
    {
        std::fs::read(storage_path())
            .ok()
            .and_then(|bytes| serde_json::from_slice::<Vec<SavedPrompt>>(&bytes).ok())
            .unwrap_or_default()
            .into_iter()
            .filter(|item| !item.text.trim().is_empty())
            .collect()
    }
}

fn save_disk(items: &[SavedPrompt]) {
    #[cfg(target_family = "wasm")]
    {
        let _ = items;
    }
    #[cfg(not(target_family = "wasm"))]
    {
        let path = storage_path();
        if let Some(dir) = path.parent() {
            let _ = std::fs::create_dir_all(dir);
        }
        if let Ok(json) = serde_json::to_string_pretty(items) {
            let _ = std::fs::write(path, json);
        }
    }
}

fn with_items<R>(f: impl FnOnce(&mut Vec<SavedPrompt>) -> (bool, R)) -> R {
    let mut guard = cache().lock().unwrap_or_else(|e| e.into_inner());
    let items = guard.get_or_insert_with(load_disk);
    let (dirty, out) = f(items);
    if dirty {
        save_disk(items);
    }
    out
}

pub(in crate::editor) fn trunc12(raw: &str) -> String {
    let trimmed = raw.trim();
    let mut chars = trimmed.chars();
    let head: String = chars.by_ref().take(12).collect();
    if chars.next().is_some() {
        format!("{head}…")
    } else {
        head
    }
}

/// Ordered list: starred favorites first, followed by unstarred recent prompt(s).
pub(in crate::editor) fn list() -> Vec<SavedPrompt> {
    with_items(|items| {
        let mut out: Vec<SavedPrompt> = items.iter().filter(|x| x.starred).cloned().collect();
        out.extend(items.iter().filter(|x| !x.starred).cloned());
        (false, out)
    })
}

/// Records a user-typed prompt: keeps all starred items and replaces any unstarred item with the latest one.
pub(in crate::editor) fn record(raw: &str) {
    let clean = raw.trim();
    if clean.is_empty() {
        return;
    }
    with_items(|items| {
        if items.iter().any(|x| x.text == clean) {
            return (false, ());
        }
        items.retain(|x| x.starred);
        items.push(SavedPrompt {
            text: clean.to_owned(),
            starred: false,
        });
        (true, ())
    });
}

/// Toggles manual favorite (`☆` <-> `★`). Unstarring keeps at most one unstarred item.
pub(in crate::editor) fn toggle_star(target: &str) {
    with_items(|items| {
        let Some(pos) = items.iter().position(|x| x.text == target) else {
            return (false, ());
        };
        let next = !items[pos].starred;
        items[pos].starred = next;
        if !next {
            // Keep only this newly-unstarred item as the single unstarred slot so the row stays tidy.
            let keep_text = items[pos].text.clone();
            items.retain(|x| x.starred || x.text == keep_text);
        }
        (true, ())
    });
}
