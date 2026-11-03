use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize, Default)]
struct State {
    folder: Option<PathBuf>,
}

fn state_path() -> Option<PathBuf> {
    let home = std::env::var_os("HOME")?;
    Some(PathBuf::from(home).join("Library/Application Support/thinkly-fast/state.json"))
}

pub fn load_folder() -> Option<PathBuf> {
    let path = state_path()?;
    let text = std::fs::read_to_string(path).ok()?;
    let state: State = serde_json::from_str(&text).ok()?;
    state.folder.filter(|folder| folder.is_dir())
}

pub fn save_folder(folder: &Path) {
    let Some(path) = state_path() else { return };
    if let Some(parent) = path.parent()
        && std::fs::create_dir_all(parent).is_err()
    {
        return;
    }
    let state = State { folder: Some(folder.to_path_buf()) };
    if let Ok(text) = serde_json::to_string_pretty(&state) {
        let _ = std::fs::write(path, text);
    }
}
