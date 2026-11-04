//! Quick-note storage: one Markdown file per slot plus `names.json`, all under the app data
//! directory (never the user's notes folder).
use std::{fs, io, path::PathBuf};
use serde::{Deserialize, Serialize};

pub const SLOTS: usize = 3;

#[derive(Serialize, Deserialize, Clone, Default)]
pub struct Meta {
    #[serde(default)]
    pub names: Vec<String>,
    #[serde(default)]
    pub active: usize,
}

pub fn default_name(slot: usize) -> String {
    if slot == 0 { crate::theme::tr("灵感", "Ideas").into() }
    else if crate::theme::is_en() { format!("Quick {}", slot + 1) }
    else { format!("速记 {}", slot + 1) }
}

pub fn is_default_name(slot: usize, name: &str) -> bool {
    let trimmed = name.trim();
    trimmed == format!("速记 {}", slot + 1)
        || trimmed == format!("Quick {}", slot + 1)
        || (slot == 0 && matches!(trimmed, "灵感" | "Ideas"))
}

/// `~/Library/Application Support/Block/quick` on macOS.
pub fn dir() -> PathBuf {
    crate::files::app_data_dir("Block").join("quick")
}

pub fn slot_path(slot: usize) -> PathBuf { dir().join(format!("slot-{}.md", slot + 1)) }

/// Slot files are created lazily, the first time a slot is opened.
pub fn ensure_slot(slot: usize) -> io::Result<PathBuf> {
    let path = slot_path(slot);
    fs::create_dir_all(dir())?;
    match fs::OpenOptions::new().write(true).create_new(true).open(&path) {
        Ok(_) => {}
        Err(error) if error.kind() == io::ErrorKind::AlreadyExists => {}
        Err(error) => return Err(error),
    }
    Ok(path)
}

pub fn load_meta() -> Meta {
    let mut meta = fs::read(dir().join("names.json")).ok()
        .and_then(|bytes| serde_json::from_slice::<Meta>(&bytes).ok()).unwrap_or_default();
    for slot in 0..SLOTS {
        match meta.names.get(slot) {
            Some(name) if !name.trim().is_empty() && !(slot == 0 && name == "速记 1") => {}
            _ => { if slot < meta.names.len() { meta.names[slot] = default_name(slot); } else { meta.names.push(default_name(slot)); } }
        }
    }
    meta.names.truncate(SLOTS);
    meta.active = meta.active.min(SLOTS - 1);
    meta
}

pub fn save_meta(meta: &Meta) -> io::Result<()> {
    fs::create_dir_all(dir())?;
    let path = dir().join("names.json");
    let temp = path.with_extension("json.tmp");
    fs::write(&temp, serde_json::to_vec_pretty(meta).map_err(io::Error::other)?)?;
    fs::rename(temp, path)
}
