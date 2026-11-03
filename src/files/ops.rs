//! Disk operations behind the sidebar menu. Deletion only ever moves items to
//! the system Trash, so nothing here destroys data irreversibly.
use std::io;
use std::path::{Path, PathBuf};
use std::process::Command;

pub fn move_to_trash(path: &Path) -> io::Result<()> {
    if !path.exists() { return Err(io::Error::new(io::ErrorKind::NotFound, "文件已不存在")); }
    let quoted = path.to_string_lossy().replace('\\', "\\\\").replace('"', "\\\"");
    let script = format!("tell application \"Finder\" to delete POSIX file \"{quoted}\"");
    let output = Command::new("/usr/bin/osascript").arg("-e").arg(script).output()
        .map_err(|error| io::Error::other(format!("无法调用访达：{error}")))?;
    if output.status.success() && !path.exists() { return Ok(()); }
    let detail = String::from_utf8_lossy(&output.stderr).trim().to_owned();
    Err(io::Error::other(if detail.is_empty() { "无法移到废纸篓".to_owned() } else { detail }))
}

/// Validated destination of a rename, without touching the disk. Notes keep
/// their extension; the user only edits the visible stem.
pub fn rename_target(path: &Path, name: &str, dir: bool) -> io::Result<PathBuf> {
    let name = name.trim();
    if name.is_empty() { return Err(io::Error::other("名称不能为空")); }
    if name.contains('/') || name.contains(':') || name.starts_with('.') {
        return Err(io::Error::other("名称不能包含 / 或 :，也不能以 . 开头"));
    }
    let file_name = if dir { name.to_owned() } else {
        let ext = path.extension().map(|e| e.to_string_lossy().into_owned()).unwrap_or_else(|| "md".into());
        format!("{name}.{ext}")
    };
    let target = path.with_file_name(file_name);
    if target == path { return Ok(target); }
    // Case-only renames on a case-insensitive disk resolve to the same inode.
    let same = target.exists() && path.to_string_lossy().to_lowercase() == target.to_string_lossy().to_lowercase();
    if target.exists() && !same { return Err(io::Error::new(io::ErrorKind::AlreadyExists, "已存在同名项目")); }
    Ok(target)
}

pub fn rename(path: &Path, name: &str, dir: bool) -> io::Result<PathBuf> {
    let target = rename_target(path, name, dir)?;
    if target != path { std::fs::rename(path, &target)?; }
    Ok(target)
}

/// Move a note or folder into `dest`, refusing duplicates and moves into itself.
pub fn move_to(path: &Path, dest: &Path) -> io::Result<PathBuf> {
    if dest.starts_with(path) { return Err(io::Error::other("不能移动到自身内部")); }
    let name = path.file_name().ok_or_else(|| io::Error::other("无效路径"))?;
    let target = dest.join(name);
    if target == path { return Ok(target); }
    if target.exists() { return Err(io::Error::new(io::ErrorKind::AlreadyExists, "目标位置已存在同名项目")); }
    std::fs::rename(path, &target)?;
    Ok(target)
}

pub fn create_folder(parent: &Path) -> io::Result<PathBuf> {
    for attempt in 0..100 {
        let name = if attempt == 0 { "新建文件夹".to_owned() } else { format!("新建文件夹 {}", attempt + 1) };
        let path = parent.join(name);
        match std::fs::create_dir(&path) {
            Ok(()) => return Ok(path),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    Err(io::Error::new(io::ErrorKind::AlreadyExists, "无法分配唯一文件夹名称"))
}

pub fn reveal_in_finder(path: &Path) {
    let _ = Command::new("/usr/bin/open").arg("-R").arg(path).spawn();
}

/// Rebase a path that lived under `from` onto `to` (rename of an ancestor).
pub fn rebase(path: &Path, from: &Path, to: &Path) -> Option<PathBuf> {
    path.strip_prefix(from).ok().map(|rest| if rest.as_os_str().is_empty() { to.to_owned() } else { to.join(rest) })
}
