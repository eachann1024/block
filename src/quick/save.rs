//! 保存到笔记: copy a slot into a fresh note in the main window's current folder.
use std::{fs, io, path::{Path, PathBuf}};
use crate::files::{create_note_at, note_candidates};
use super::store;

/// The main window's current folder, as persisted by the workspace.
pub fn target_folder() -> Option<PathBuf> {
    crate::files::load_state().ok().and_then(|state| state.folder).filter(|folder| folder.is_dir())
}

fn sanitize(text: &str) -> String {
    let line = text.lines().map(str::trim).find(|line| !line.trim_start_matches(['#', '>', '-', '*', ' ']).is_empty()).unwrap_or("");
    let line = line.trim_start_matches(['#', '>', '-', '*', ' ']);
    let cleaned: String = line.chars().filter(|c| !matches!(c, '/' | '\\' | ':' | '\0' | '\n' | '\r' | '*' | '?' | '"' | '<' | '>' | '|')).take(40).collect();
    cleaned.trim().trim_start_matches('.').trim().to_owned()
}

/// Slot name unless it is still the default, then the first line of content.
pub fn title_for(slot: usize, name: &str, content: &str) -> String {
    let from_name = sanitize(name);
    if !store::is_default_name(slot, name) && !from_name.is_empty() { return from_name; }
    let from_line = sanitize(content);
    if !from_line.is_empty() { from_line } else if !from_name.is_empty() { from_name } else { crate::theme::tr("速记", "Quick Note").into() }
}

fn create_unique(folder: &Path, title: &str) -> io::Result<PathBuf> {
    let named = (0..100).map(|n| if n == 0 { format!("{title}.md") } else { format!("{title} {}.md", n + 1) }).map(|name| folder.join(name));
    for path in named.chain(note_candidates(folder)) {
        match create_note_at(&path) {
            Ok(()) => return Ok(path),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    Err(io::Error::new(
        io::ErrorKind::AlreadyExists,
        crate::theme::tr("无法分配唯一笔记名称", "Could not allocate a unique note name"),
    ))
}

/// Blocking; call from a background task. Returns the new note path.
pub fn write_note(folder: &Path, slot: usize, name: &str, content: &str) -> io::Result<PathBuf> {
    let path = create_unique(folder, &title_for(slot, name, content))?;
    if let Err(error) = fs::write(&path, content) {
        let _ = fs::remove_file(&path);
        return Err(error);
    }
    Ok(path)
}
