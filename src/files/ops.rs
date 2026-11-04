//! Disk operations behind the sidebar menu. Deletion only ever moves items to
//! the system Trash, so nothing here destroys data irreversibly.
use std::io;
use std::path::{Path, PathBuf};
use std::process::Command;
use crate::theme::tr;

pub fn reveal_label() -> &'static str {
    if cfg!(target_os = "macos") { tr("在访达中显示", "Reveal in Finder") }
    else if cfg!(target_os = "windows") { tr("在资源管理器中显示", "Show in Explorer") }
    else { tr("在文件管理器中显示", "Show in File Manager") }
}

pub fn trash_label() -> &'static str {
    if cfg!(target_os = "windows") { tr("移到回收站", "Move to Recycle Bin") } else { tr("移到废纸篓", "Move to Trash") }
}

pub fn trashed_notice() -> &'static str {
    if cfg!(target_os = "windows") { tr("已移到回收站", "Moved to Recycle Bin") } else { tr("已移到废纸篓", "Moved to Trash") }
}

pub fn move_to_trash(path: &Path) -> io::Result<()> {
    if !path.exists() { return Err(io::Error::new(io::ErrorKind::NotFound, tr("文件已不存在", "File no longer exists"))); }
    trash_platform(path)?;
    if path.exists() { return Err(io::Error::other(trash_label())); }
    Ok(())
}

#[cfg(target_os = "macos")]
fn trash_platform(path: &Path) -> io::Result<()> {
    let quoted = path.to_string_lossy().replace('\\', "\\\\").replace('"', "\\\"");
    let script = format!("tell application \"Finder\" to delete POSIX file \"{quoted}\"");
    let output = Command::new("/usr/bin/osascript").arg("-e").arg(script).output()
        .map_err(|error| io::Error::other(format!("{}: {error}", tr("无法调用访达", "Failed to invoke Finder"))))?;
    if output.status.success() { return Ok(()); }
    let detail = String::from_utf8_lossy(&output.stderr).trim().to_owned();
    Err(io::Error::other(if detail.is_empty() { tr("无法移到废纸篓", "Failed to move to Trash").to_owned() } else { detail }))
}

#[cfg(target_os = "windows")]
fn trash_platform(path: &Path) -> io::Result<()> {
    let quoted = path.to_string_lossy().replace('\'', "''");
    let call = if path.is_dir() { "DeleteDirectory" } else { "DeleteFile" };
    let script = format!(
        "Add-Type -AssemblyName Microsoft.VisualBasic; [Microsoft.VisualBasic.FileIO.FileSystem]::{call}('{quoted}', 'OnlyErrorDialogs', 'SendToRecycleBin')"
    );
    let output = Command::new("powershell.exe").args(["-NoProfile", "-NonInteractive", "-Command"]).arg(script).output()?;
    if output.status.success() { return Ok(()); }
    let detail = String::from_utf8_lossy(&output.stderr).trim().to_owned();
    Err(io::Error::other(if detail.is_empty() { tr("无法移到回收站", "Failed to move to Recycle Bin").to_owned() } else { detail }))
}

#[cfg(not(any(target_os = "macos", target_os = "windows")))]
fn trash_platform(path: &Path) -> io::Result<()> {
    if Command::new("gio").arg("trash").arg(path).status().is_ok_and(|status| status.success()) { return Ok(()); }
    // Freedesktop fallback: move into the user's Trash/files folder.
    let files = crate::files::app_data_dir("Trash").join("files");
    std::fs::create_dir_all(&files)?;
    let name = path.file_name().ok_or_else(|| io::Error::other(tr("无效路径", "Invalid path")))?;
    let mut target = files.join(name);
    for n in 2..1000 {
        if !target.exists() { break; }
        target = files.join(format!("{} {n}", name.to_string_lossy()));
    }
    std::fs::rename(path, target)
}

/// Windows device names that cannot be used as a file stem on any platform we sync to.
fn is_reserved_name(name: &str) -> bool {
    let stem = name.split('.').next().unwrap_or(name).trim_end().to_ascii_uppercase();
    matches!(stem.as_str(), "CON" | "PRN" | "AUX" | "NUL")
        || (stem.len() == 4 && (stem.starts_with("COM") || stem.starts_with("LPT")) && matches!(stem.as_bytes()[3], b'1'..=b'9'))
}

/// Validated destination of a rename, without touching the disk. Notes keep
/// their extension; the user only edits the visible stem.
pub fn rename_target(path: &Path, name: &str, dir: bool) -> io::Result<PathBuf> {
    let name = name.trim();
    if name.is_empty() { return Err(io::Error::other(tr("名称不能为空", "Name cannot be empty"))); }
    if name.starts_with('.') || name.ends_with(['.', ' ']) || name.chars().any(|c| c.is_control() || "/\\:*?\"<>|".contains(c)) {
        return Err(io::Error::other(tr("名称不能包含 / \\ : * ? \" < > |，不能以 . 开头，也不能以点或空格结尾", "Name cannot contain / \\ : * ? \" < > |, start with ., or end with a dot or space")));
    }
    if is_reserved_name(name) { return Err(io::Error::other(tr("该名称是系统保留名称", "This name is reserved by the system"))); }
    let file_name = if dir { name.to_owned() } else {
        let ext = path.extension().map(|e| e.to_string_lossy().into_owned()).unwrap_or_else(|| "md".into());
        format!("{name}.{ext}")
    };
    let target = path.with_file_name(file_name);
    if target == path { return Ok(target); }
    // Case-only renames on a case-insensitive disk resolve to the same inode.
    let same = target.exists() && path.to_string_lossy().to_lowercase() == target.to_string_lossy().to_lowercase();
    if target.exists() && !same { return Err(io::Error::new(io::ErrorKind::AlreadyExists, tr("已存在同名项目", "An item with this name already exists"))); }
    Ok(target)
}

pub fn rename(path: &Path, name: &str, dir: bool) -> io::Result<PathBuf> {
    let target = rename_target(path, name, dir)?;
    if target != path { std::fs::rename(path, &target)?; }
    Ok(target)
}

/// Move a note or folder into `dest`, refusing duplicates and moves into itself.
pub fn move_to(path: &Path, dest: &Path) -> io::Result<PathBuf> {
    if dest.starts_with(path) { return Err(io::Error::other(tr("不能移动到自身内部", "Cannot move into itself"))); }
    let name = path.file_name().ok_or_else(|| io::Error::other(tr("无效路径", "Invalid path")))?;
    let target = dest.join(name);
    if target == path { return Ok(target); }
    if target.exists() { return Err(io::Error::new(io::ErrorKind::AlreadyExists, tr("目标位置已存在同名项目", "Destination already has an item with this name"))); }
    std::fs::rename(path, &target)?;
    Ok(target)
}

pub fn create_folder(parent: &Path) -> io::Result<PathBuf> {
    let base = tr("新建文件夹", "New Folder");
    for attempt in 0..100 {
        let name = if attempt == 0 { base.to_owned() } else { format!("{base} {}", attempt + 1) };
        let path = parent.join(name);
        match std::fs::create_dir(&path) {
            Ok(()) => return Ok(path),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    Err(io::Error::new(io::ErrorKind::AlreadyExists, tr("无法分配唯一文件夹名称", "Could not allocate a unique folder name")))
}

pub fn reveal_in_finder(path: &Path) {
    #[cfg(target_os = "macos")]
    let _ = Command::new("open").arg("-R").arg(path).spawn();
    #[cfg(target_os = "windows")]
    let _ = Command::new("explorer.exe").arg(format!("/select,{}", path.display())).spawn();
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        let shown = Command::new("gdbus")
            .args(["call", "--session", "--dest", "org.freedesktop.FileManager1", "--object-path", "/org/freedesktop/FileManager1", "--method", "org.freedesktop.FileManager1.ShowItems"])
            .arg(format!("['file://{}']", path.display())).arg("")
            .output().is_ok_and(|out| out.status.success());
        if !shown { let _ = Command::new("xdg-open").arg(path.parent().unwrap_or(path)).spawn(); }
    }
}

/// Rebase a path that lived under `from` onto `to` (rename of an ancestor).
pub fn rebase(path: &Path, from: &Path, to: &Path) -> Option<PathBuf> {
    path.strip_prefix(from).ok().map(|rest| if rest.as_os_str().is_empty() { to.to_owned() } else { to.join(rest) })
}
