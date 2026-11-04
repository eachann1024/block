//! Preferences are independently fallible; storage paths can be isolated in checks.
use std::fs::{self, OpenOptions};
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize, Default, Clone, Copy, Debug, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum ThemeMode {
    #[default]
    Light,
    Dark,
    System,
}

#[derive(Serialize, Deserialize, Default, Clone, Copy, Debug, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum FontChoice {
    #[default]
    #[serde(alias = "mono")]
    Sans,
    Serif,
}

#[derive(Serialize, Deserialize, Default, Clone, Copy, Debug, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum LanguageMode {
    #[default]
    System,
    Zh,
    En,
}

fn default_color_name() -> String { "default".into() }

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct LastFormatColors {
    #[serde(default = "default_color_name")]
    pub text_color: String,
    #[serde(default = "default_color_name")]
    pub background_color: String,
}

impl Default for LastFormatColors {
    fn default() -> Self {
        Self { text_color: default_color_name(), background_color: default_color_name() }
    }
}

/// Unknown/missing keys default, so older state files keep loading.
#[derive(Serialize, Deserialize, Default, Clone, Debug)]
pub struct State {
    pub folder: Option<PathBuf>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub notebooks: Vec<PathBuf>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub sidebar_width: Option<f32>,
    #[serde(default)]
    pub theme: ThemeMode,
    #[serde(default)]
    pub language: LanguageMode,
    #[serde(default)]
    pub font_family: FontChoice,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub font_smoothing: Option<bool>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub shortcut_search: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub shortcut_quick: Option<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub open_tabs: Vec<PathBuf>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub active_tab: Option<PathBuf>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_format_colors: Option<LastFormatColors>,
}
static NEXT: AtomicU64 = AtomicU64::new(0);
static WRITE: std::sync::Mutex<()> = std::sync::Mutex::new(());
const MAX_NOTEBOOKS: usize = 12;

/// Per-user app data folder: `~/Library/Application Support` on macOS, `%APPDATA%` on Windows,
/// `$XDG_DATA_HOME` (or `~/.local/share`) on Linux.
pub fn app_data_dir(app_folder: &str) -> PathBuf {
    #[cfg(not(target_family = "wasm"))]
    let base = dirs::data_dir()
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".local/share")))
        .unwrap_or_else(std::env::temp_dir);
    #[cfg(target_family = "wasm")]
    let base = PathBuf::from("/");
    base.join(app_folder)
}

fn state_path() -> io::Result<PathBuf> {
    Ok(app_data_dir("thinkly-fast").join("state.json"))
}

pub fn load_state() -> io::Result<State> { load_state_at(&state_path()?) }

/// Read-modify-write under one lock so width and folder saves never clobber each other.
pub fn update_state(change: impl FnOnce(&mut State)) -> io::Result<State> {
    let _guard = WRITE.lock().unwrap_or_else(|poison| poison.into_inner());
    let path = state_path()?;
    let mut state = load_state_at(&path).unwrap_or_default();
    change(&mut state);
    write_state_at(&path, &state)?;
    Ok(state)
}

/// Opening a folder makes it the current notebook and the most recent one.
pub fn save_folder(folder: &Path) -> io::Result<State> {
    update_state(|state| {
        if state.folder.as_deref() != Some(folder) {
            state.open_tabs.clear();
            state.active_tab = None;
        }
        state.folder = Some(folder.to_owned());
        if !state.notebooks.iter().any(|known| known == folder) {
            state.notebooks.insert(0, folder.to_owned());
            state.notebooks.truncate(MAX_NOTEBOOKS);
        }
    })
}

pub fn load_state_at(path: &Path) -> io::Result<State> {
    let text = match fs::read(path) {
        Ok(text) => text,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(State::default()),
        Err(error) => return Err(error),
    };
    let mut state: State = serde_json::from_slice(&text)
        .map_err(|error| io::Error::new(io::ErrorKind::InvalidData, error))?;
    // A missing remembered folder is reported by scan, never silently discarded.
    if let Some(folder) = state.folder.clone() {
        if !state.notebooks.contains(&folder) { state.notebooks.insert(0, folder); }
    }
    Ok(state)
}

fn write_state_at(path: &Path, state: &State) -> io::Result<()> {
    let parent = path.parent().ok_or_else(|| io::Error::other(crate::theme::tr("无效的偏好路径", "Invalid preferences path")))?;
    fs::create_dir_all(parent)?;
    let bytes = serde_json::to_vec_pretty(state)?;
    let (temp, mut file) = (0..64).find_map(|_| {
        let temp = parent.join(format!(".state-{}-{}.tmp", std::process::id(), NEXT.fetch_add(1, Ordering::Relaxed)));
        match OpenOptions::new().write(true).create_new(true).open(&temp) {
            Ok(file) => Some(Ok((temp, file))),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => None,
            Err(error) => Some(Err(error)),
        }
    }).unwrap_or_else(|| Err(io::Error::other(crate::theme::tr("无法创建临时偏好文件", "Failed to create temporary preferences file"))))?;
    let result = (|| {
        file.write_all(&bytes)?;
        file.sync_all()?;
        drop(file);
        fs::rename(&temp, path)?;
        fs::File::open(parent)?.sync_all()
    })();
    if result.is_err() { let _ = fs::remove_file(&temp); }
    result
}
