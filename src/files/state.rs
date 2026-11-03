//! Preferences are independently fallible; storage paths can be isolated in checks.
use std::fs::{self, OpenOptions};
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize, Default)]
struct State { folder: Option<PathBuf> }
static NEXT: AtomicU64 = AtomicU64::new(0);

fn state_path() -> io::Result<PathBuf> {
    let home = std::env::var_os("HOME").ok_or_else(|| io::Error::other("HOME 未设置"))?;
    Ok(PathBuf::from(home).join("Library/Application Support/thinkly-fast/state.json"))
}

pub fn load_folder() -> io::Result<Option<PathBuf>> { load_folder_at(&state_path()?) }
pub fn save_folder(folder: &Path) -> io::Result<()> { save_folder_at(&state_path()?, folder) }

pub fn load_folder_at(path: &Path) -> io::Result<Option<PathBuf>> {
    let text = match fs::read(path) {
        Ok(text) => text,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(None),
        Err(error) => return Err(error),
    };
    let state: State = serde_json::from_slice(&text)
        .map_err(|error| io::Error::new(io::ErrorKind::InvalidData, error))?;
    // A missing remembered folder is reported by scan, never silently discarded.
    Ok(state.folder)
}

pub fn save_folder_at(path: &Path, folder: &Path) -> io::Result<()> {
    let parent = path.parent().ok_or_else(|| io::Error::other("无效的偏好路径"))?;
    fs::create_dir_all(parent)?;
    let bytes = serde_json::to_vec_pretty(&State { folder: Some(folder.to_owned()) })?;
    let (temp, mut file) = (0..64).find_map(|_| {
        let temp = parent.join(format!(".state-{}-{}.tmp", std::process::id(), NEXT.fetch_add(1, Ordering::Relaxed)));
        match OpenOptions::new().write(true).create_new(true).open(&temp) {
            Ok(file) => Some(Ok((temp, file))),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => None,
            Err(error) => Some(Err(error)),
        }
    }).unwrap_or_else(|| Err(io::Error::other("无法创建临时偏好文件")))?;
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
