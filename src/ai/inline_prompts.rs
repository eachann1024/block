//! Persists inline AI prompt chips per context (`edit` vs `empty`):
//! - Built-in presets start starred (`★`) by default.
//! - Unstarring (`☆`) moves an item behind starred items; as newer prompts accumulate past 3 rows,
//!   trailing unstarred items drop off automatically.
use std::sync::{Mutex, OnceLock};
use serde::{Deserialize, Serialize};
use super::session::{EDIT_PRESETS, EMPTY_PRESETS, Preset};

pub(in crate::editor) const CHIP_H: f32 = 26.;
pub(in crate::editor) const CHIP_GAP: f32 = 5.;
pub(in crate::editor) const MAX_CHIP_ROWS: usize = 3;
pub(in crate::editor) const CHIP_AREA_MAX_H: f32 =
    CHIP_H * (MAX_CHIP_ROWS as f32) + CHIP_GAP * ((MAX_CHIP_ROWS - 1) as f32);

const PANEL_INNER_W: f32 = crate::ui::ai_controls::PANEL_WIDTH - 16.;
const CHIP_CHROME_W: f32 = 40.;

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub(in crate::editor) enum PromptScope {
    Edit,
    Empty,
}

impl PromptScope {
    pub(in crate::editor) fn from_empty_ctx(empty_ctx: bool) -> Self {
        if empty_ctx { Self::Empty } else { Self::Edit }
    }

    fn default_presets(self) -> &'static [Preset] {
        match self {
            Self::Edit => EDIT_PRESETS,
            Self::Empty => EMPTY_PRESETS,
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub(in crate::editor) enum PromptTarget {
    Preset { preset: Preset },
    Custom { text: String },
}

impl PromptTarget {
    pub(in crate::editor) fn short_label(&self) -> String {
        match self {
            Self::Preset { preset } => preset.label().to_owned(),
            Self::Custom { text } => trunc12(text),
        }
    }

    pub(in crate::editor) fn full_label(&self) -> String {
        match self {
            Self::Preset { preset } => preset.label().to_owned(),
            Self::Custom { text } => text.trim().to_owned(),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub(in crate::editor) struct PromptEntry {
    #[serde(flatten)]
    pub target: PromptTarget,
    #[serde(default)]
    pub starred: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
struct StoredPrompts {
    edit: Vec<PromptEntry>,
    empty: Vec<PromptEntry>,
}

#[derive(Deserialize)]
struct LegacySavedPrompt {
    text: String,
    #[serde(default)]
    starred: bool,
}

impl StoredPrompts {
    fn default_seeded() -> Self {
        Self {
            edit: seeded_presets(PromptScope::Edit),
            empty: seeded_presets(PromptScope::Empty),
        }
    }

    fn slot_mut(&mut self, scope: PromptScope) -> &mut Vec<PromptEntry> {
        match scope {
            PromptScope::Edit => &mut self.edit,
            PromptScope::Empty => &mut self.empty,
        }
    }
}

fn seeded_presets(scope: PromptScope) -> Vec<PromptEntry> {
    scope
        .default_presets()
        .iter()
        .copied()
        .map(|preset| PromptEntry {
            target: PromptTarget::Preset { preset },
            starred: true,
        })
        .collect()
}

static CACHE: OnceLock<Mutex<Option<StoredPrompts>>> = OnceLock::new();

fn cache() -> &'static Mutex<Option<StoredPrompts>> {
    CACHE.get_or_init(|| Mutex::new(None))
}

#[cfg(not(target_family = "wasm"))]
fn storage_path() -> std::path::PathBuf {
    crate::files::app_data_dir("Block").join("ai-inline-prompts.json")
}

fn sanitize_list(items: Vec<PromptEntry>) -> Vec<PromptEntry> {
    let mut starred = Vec::new();
    let mut unstarred = Vec::new();
    for item in items {
        if let PromptTarget::Custom { text } = &item.target
            && text.trim().is_empty()
        {
            continue;
        }
        let dup = starred.iter().chain(unstarred.iter()).any(|x: &PromptEntry| x.target == item.target);
        if dup {
            continue;
        }
        if item.starred {
            starred.push(item);
        } else {
            unstarred.push(item);
        }
    }
    starred.extend(unstarred);
    starred
}

fn migrate_legacy(legacy: Vec<LegacySavedPrompt>) -> StoredPrompts {
    let mut edit = seeded_presets(PromptScope::Edit);
    for pass_starred in [true, false] {
        for item in &legacy {
            if item.starred != pass_starred {
                continue;
            }
            let clean = item.text.trim();
            if clean.is_empty() || EDIT_PRESETS.iter().any(|p| p.matches_input(clean)) {
                continue;
            }
            let target = PromptTarget::Custom { text: clean.to_owned() };
            if !edit.iter().any(|x| x.target == target) {
                edit.push(PromptEntry { target, starred: item.starred });
            }
        }
    }
    let mut edit = sanitize_list(edit);
    prune_unstarred_past_max_rows(&mut edit);
    StoredPrompts {
        edit,
        empty: seeded_presets(PromptScope::Empty),
    }
}

fn load_disk() -> StoredPrompts {
    #[cfg(target_family = "wasm")]
    {
        StoredPrompts::default_seeded()
    }
    #[cfg(not(target_family = "wasm"))]
    {
        let Ok(bytes) = std::fs::read(storage_path()) else {
            return StoredPrompts::default_seeded();
        };
        if let Ok(stored) = serde_json::from_slice::<StoredPrompts>(&bytes) {
            return StoredPrompts {
                edit: sanitize_list(stored.edit),
                empty: sanitize_list(stored.empty),
            };
        }
        if let Ok(legacy) = serde_json::from_slice::<Vec<LegacySavedPrompt>>(&bytes) {
            let migrated = migrate_legacy(legacy);
            save_disk(&migrated);
            return migrated;
        }
        StoredPrompts::default_seeded()
    }
}

fn save_disk(stored: &StoredPrompts) {
    #[cfg(target_family = "wasm")]
    {
        let _ = stored;
    }
    #[cfg(not(target_family = "wasm"))]
    {
        let path = storage_path();
        if let Some(dir) = path.parent() {
            let _ = std::fs::create_dir_all(dir);
        }
        if let Ok(json) = serde_json::to_string_pretty(stored) {
            let _ = std::fs::write(path, json);
        }
    }
}

fn with_store<R>(f: impl FnOnce(&mut StoredPrompts) -> (bool, R)) -> R {
    let mut guard = cache().lock().unwrap_or_else(|e| e.into_inner());
    let stored = guard.get_or_insert_with(load_disk);
    let (dirty, out) = f(stored);
    if dirty {
        save_disk(stored);
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

fn estimate_chip_width(short_text: &str) -> f32 {
    let text_w: f32 = short_text
        .chars()
        .map(|ch| {
            if ch.is_ascii() {
                match ch {
                    ' ' => 3.8,
                    'i' | 'l' | 'I' | '1' | '.' | ',' | ':' | ';' | '\'' | '!' | '|' => 4.2,
                    'A'..='Z' | 'm' | 'w' => 8.4,
                    _ => 7.2,
                }
            } else {
                13.0
            }
        })
        .sum();
    (CHIP_CHROME_W + text_w).min(PANEL_INNER_W)
}

fn chip_row_indices(items: &[PromptEntry]) -> Vec<usize> {
    let mut out = Vec::with_capacity(items.len());
    let mut row = 0usize;
    let mut x = 0.0f32;
    for item in items {
        let w = estimate_chip_width(&item.target.short_label());
        if x > 0.0 && x + CHIP_GAP + w > PANEL_INNER_W {
            row += 1;
            x = w;
        } else {
            if x > 0.0 {
                x += CHIP_GAP;
            }
            x += w;
        }
        out.push(row);
    }
    out
}

fn prune_unstarred_past_max_rows(items: &mut Vec<PromptEntry>) {
    let rows = chip_row_indices(items);
    let mut idx = 0usize;
    items.retain(|item| {
        let keep = item.starred || rows[idx] < MAX_CHIP_ROWS;
        idx += 1;
        keep
    });
}

/// Ordered list for a scope: starred favorites first, followed by unstarred items.
pub(in crate::editor) fn list(scope: PromptScope) -> Vec<PromptEntry> {
    with_store(|stored| (false, stored.slot_mut(scope).clone()))
}

/// Records a user-typed prompt in `scope`: places it at the front of unstarred items
/// and drops any trailing unstarred items pushed past row 3.
pub(in crate::editor) fn record(scope: PromptScope, raw: &str) {
    let clean = raw.trim();
    if clean.is_empty() {
        return;
    }
    with_store(|stored| {
        let items = stored.slot_mut(scope);
        let matched_pos = items.iter().position(|entry| match &entry.target {
            PromptTarget::Preset { preset } => preset.matches_input(clean),
            PromptTarget::Custom { text } => text == clean,
        });
        if let Some(pos) = matched_pos {
            if items[pos].starred {
                return (false, ());
            }
            let entry = items.remove(pos);
            let insert_at = items.iter().position(|x| !x.starred).unwrap_or(items.len());
            items.insert(insert_at, entry);
            prune_unstarred_past_max_rows(items);
            return (true, ());
        }
        let target = scope
            .default_presets()
            .iter()
            .copied()
            .find(|p| p.matches_input(clean))
            .map(|preset| PromptTarget::Preset { preset })
            .unwrap_or_else(|| PromptTarget::Custom { text: clean.to_owned() });
        let insert_at = items.iter().position(|x| !x.starred).unwrap_or(items.len());
        items.insert(insert_at, PromptEntry { target, starred: false });
        prune_unstarred_past_max_rows(items);
        (true, ())
    });
}

/// Toggles favorite (`☆` <-> `★`).
/// - Starring moves the item to the end of the starred group.
/// - Unstarring moves the item to the back, dropping older unstarred items if needed to stay within 3 rows.
pub(in crate::editor) fn toggle_star(scope: PromptScope, target: &PromptTarget) {
    with_store(|stored| {
        let items = stored.slot_mut(scope);
        let Some(pos) = items.iter().position(|x| &x.target == target) else {
            return (false, ());
        };
        let mut entry = items.remove(pos);
        if entry.starred {
            entry.starred = false;
            items.push(entry);
            while items.len() >= 2 {
                let rows = chip_row_indices(items);
                if rows.last().copied().unwrap_or(0) < MAX_CHIP_ROWS {
                    break;
                }
                let prev = items.len() - 2;
                if items[prev].starred {
                    break;
                }
                items.remove(prev);
            }
        } else {
            entry.starred = true;
            let insert_at = items.iter().position(|x| !x.starred).unwrap_or(items.len());
            items.insert(insert_at, entry);
        }
        (true, ())
    });
}
