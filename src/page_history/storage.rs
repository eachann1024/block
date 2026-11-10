//! Page history storage facade. Native targets keep a durable archive; others refuse explicitly.
#[cfg(target_family = "wasm")]
use std::{io, path::Path};
#[cfg(target_family = "wasm")]
use super::types::HistoryVersion;

#[cfg(not(target_family = "wasm"))]
#[path = "storage_native.rs"]
mod native;

#[cfg(not(target_family = "wasm"))]
pub use native::{
    is_file_readonly, load_version_content, load_versions, on_file_saved,
    on_path_moved, record_baseline_if_empty, restore_content_to_file, restore_version_to_file,
};

#[cfg(target_family = "wasm")]
fn unsupported() -> String {
    "HISTORY_UNSUPPORTED".into()
}

#[cfg(target_family = "wasm")]
pub fn load_versions(path: &Path, current_content: &str) -> Result<Vec<HistoryVersion>, String> {
    let _ = (path, current_content);
    Err(unsupported())
}
#[cfg(target_family = "wasm")]
pub fn load_version_content(path: &Path, version_id: &str) -> Result<String, String> {
    let _ = (path, version_id);
    Err(unsupported())
}
#[cfg(target_family = "wasm")]
pub fn record_baseline_if_empty(path: &Path, content: &str) -> Result<(), String> {
    let _ = (path, content);
    Err(unsupported())
}
#[cfg(target_family = "wasm")]
pub fn on_file_saved(path: &Path, content: &str) -> Result<(), String> {
    let _ = (path, content);
    Err(unsupported())
}
#[cfg(target_family = "wasm")]
pub fn is_file_readonly(path: &Path) -> bool {
    let _ = path;
    false
}
#[cfg(target_family = "wasm")]
pub fn restore_version_to_file(path: &Path, version_id: &str, current_base: u64) -> Result<(String, u64), String> {
    let _ = (path, version_id, current_base);
    Err(unsupported())
}
#[cfg(target_family = "wasm")]
pub fn restore_content_to_file(path: &Path, content: &str, current_base: u64) -> Result<(String, u64), String> {
    let _ = (path, content, current_base);
    Err(unsupported())
}
#[cfg(target_family = "wasm")]
pub fn on_path_moved(from: &Path, to: &Path, is_dir: bool) -> io::Result<()> {
    let _ = (from, to, is_dir);
    Err(io::Error::other("page history is not supported on this platform"))
}
