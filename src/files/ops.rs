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

pub fn is_file(path: &Path) -> bool {
    #[cfg(target_family = "wasm")]
    { return super::vfs::is_file(path); }
    #[cfg(not(target_family = "wasm"))]
    path.is_file()
}

pub fn is_dir(path: &Path) -> bool {
    #[cfg(target_family = "wasm")]
    { return super::vfs::is_dir(path); }
    #[cfg(not(target_family = "wasm"))]
    path.is_dir()
}

pub fn exists(path: &Path) -> bool {
    #[cfg(target_family = "wasm")]
    { return super::vfs::exists(path); }
    #[cfg(not(target_family = "wasm"))]
    path.exists()
}

pub fn move_to_trash(path: &Path) -> io::Result<()> {
    #[cfg(target_family = "wasm")]
    { return super::vfs::remove_tree(path); }
    #[cfg(not(target_family = "wasm"))]
    {
        if !path.exists() { return Err(io::Error::new(io::ErrorKind::NotFound, tr("文件已不存在", "File no longer exists"))); }
        trash_platform(path)?;
        if path.exists() { return Err(io::Error::other(trash_label())); }
        Ok(())
    }
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
fn trash_platform(path: &Path) -> io::Result<()> {
    use cocoa::base::{BOOL, NO, SEL, id, nil, selector};
    use cocoa::foundation::{NSAutoreleasePool, NSString};
    unsafe extern "C" {
        fn objc_msgSend();
        fn objc_getClass(name: *const std::ffi::c_char) -> id;
    }
    unsafe {
        let pool = NSAutoreleasePool::new(nil);
        let get_id: unsafe extern "C" fn(id, SEL) -> id = std::mem::transmute(objc_msgSend as *const ());
        let get_url: unsafe extern "C" fn(id, SEL, id) -> id = std::mem::transmute(objc_msgSend as *const ());
        let trash_fn: unsafe extern "C" fn(id, SEL, id, *mut id, *mut id) -> BOOL =
            std::mem::transmute(objc_msgSend as *const ());
        let utf8_fn: unsafe extern "C" fn(id, SEL) -> *const std::ffi::c_char =
            std::mem::transmute(objc_msgSend as *const ());
        let ns_path = NSString::alloc(nil).init_str(&path.to_string_lossy()).autorelease();
        let url = get_url(objc_getClass(c"NSURL".as_ptr()), selector("fileURLWithPath:"), ns_path);
        let fm = get_id(objc_getClass(c"NSFileManager".as_ptr()), selector("defaultManager"));
        let mut err: id = nil;
        let ok = url != nil && fm != nil
            && trash_fn(fm, selector("trashItemAtURL:resultingItemURL:error:"), url, std::ptr::null_mut(), &mut err) != NO;
        let detail = if !ok && err != nil {
            let desc = get_id(err, selector("localizedDescription"));
            let cstr = if desc != nil { utf8_fn(desc, selector("UTF8String")) } else { std::ptr::null() };
            if !cstr.is_null() { std::ffi::CStr::from_ptr(cstr).to_string_lossy().trim().to_owned() } else { String::new() }
        } else {
            String::new()
        };
        pool.drain();
        if ok { return Ok(()); }
        if let Some(home) = dirs::home_dir() && move_into_trash_dir(path, &home.join(".Trash")).is_ok() {
            return Ok(());
        }
        Err(io::Error::other(if detail.is_empty() { tr("无法移到废纸篓", "Failed to move to Trash").to_owned() } else { detail }))
    }
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

#[cfg(not(any(target_os = "macos", target_os = "windows", target_family = "wasm")))]
fn trash_platform(path: &Path) -> io::Result<()> {
    if Command::new("gio").arg("trash").arg(path).status().is_ok_and(|status| status.success()) { return Ok(()); }
    move_into_trash_dir(path, &crate::files::app_data_dir("Trash").join("files"))
}

#[cfg(not(any(target_os = "windows", target_family = "wasm")))]
fn move_into_trash_dir(path: &Path, files: &Path) -> io::Result<()> {
    std::fs::create_dir_all(files)?;
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
        let full = path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
        if crate::files::scan::is_markdown(&full) {
            let ext = path.extension().map(|e| e.to_string_lossy().into_owned()).unwrap_or_else(|| "md".into());
            format!("{name}.{ext}")
        } else { name.to_owned() }
    };
    let target = path.with_file_name(file_name);
    if target == path { return Ok(target); }
    // Case-only renames on a case-insensitive disk resolve to the same inode.
    let same = exists(&target) && path.to_string_lossy().to_lowercase() == target.to_string_lossy().to_lowercase();
    if exists(&target) && !same { return Err(io::Error::new(io::ErrorKind::AlreadyExists, tr("已存在同名项目", "An item with this name already exists"))); }
    Ok(target)
}

pub fn rename(path: &Path, name: &str, dir: bool) -> io::Result<PathBuf> {
    let target = rename_target(path, name, dir)?;
    if target != path {
        #[cfg(target_family = "wasm")]
        super::vfs::rename_tree(path, &target)?;
        #[cfg(not(target_family = "wasm"))]
        std::fs::rename(path, &target)?;
        #[cfg(not(target_family = "wasm"))]
        crate::page_history::on_path_moved(path, &target, dir)?;
        super::order::path_moved(path, &target);
    }
    Ok(target)
}

/// Move a note or folder into `dest`, refusing duplicates and moves into itself.
pub fn move_to(path: &Path, dest: &Path) -> io::Result<PathBuf> {
    if dest.starts_with(path) { return Err(io::Error::other(tr("不能移动到自身内部", "Cannot move into itself"))); }
    let name = path.file_name().ok_or_else(|| io::Error::other(tr("无效路径", "Invalid path")))?;
    let target = dest.join(name);
    if target == path { return Ok(target); }
    if exists(&target) { return Err(io::Error::new(io::ErrorKind::AlreadyExists, tr("目标位置已存在同名项目", "Destination already has an item with this name"))); }
    let dir = is_dir(path);
    #[cfg(target_family = "wasm")]
    super::vfs::rename_tree(path, &target)?;
    #[cfg(not(target_family = "wasm"))]
    std::fs::rename(path, &target)?;
    #[cfg(not(target_family = "wasm"))]
    crate::page_history::on_path_moved(path, &target, dir)?;
    super::order::path_moved(path, &target);
    Ok(target)
}

pub fn create_folder(parent: &Path) -> io::Result<PathBuf> {
    let base = tr("新建文件夹", "New Folder");
    for attempt in 0..100 {
        let name = if attempt == 0 { base.to_owned() } else { format!("{base} {}", attempt + 1) };
        let path = parent.join(name);
        #[cfg(target_family = "wasm")]
        let res = super::vfs::create_dir_exclusive(&path);
        #[cfg(not(target_family = "wasm"))]
        let res = std::fs::create_dir(&path);
        match res {
            Ok(()) => return Ok(path),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    Err(io::Error::new(io::ErrorKind::AlreadyExists, tr("无法分配唯一文件夹名称", "Could not allocate a unique folder name")))
}

pub fn reveal_in_finder(path: &Path) {
    #[cfg(target_family = "wasm")]
    { let _ = path; return; }
    #[cfg(target_os = "macos")]
    let _ = Command::new("open").arg("-R").arg(path).spawn();
    #[cfg(target_os = "windows")]
    let _ = Command::new("explorer.exe").arg(format!("/select,{}", path.display())).spawn();
    #[cfg(not(any(target_os = "macos", target_os = "windows", target_family = "wasm")))]
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
