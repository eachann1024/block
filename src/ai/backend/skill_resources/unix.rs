use std::{ffi::{c_char, c_int, CString}, fs::File, os::{fd::{AsRawFd, FromRawFd}, unix::ffi::OsStrExt}, path::{Component, Path}};
use super::SkillResourceError;

#[cfg(target_os = "macos")]
const FLAGS: (c_int, c_int, c_int, c_int) = (0x100, 0x100000, 0x1000000, 0x4);
#[cfg(target_os = "linux")]
const FLAGS: (c_int, c_int, c_int, c_int) = (0x20000, 0x10000, 0x80000, 0x800);

unsafe extern "C" { fn openat(dirfd: c_int, path: *const c_char, flags: c_int, ...) -> c_int; }

fn open_child(parent: &File, name: &std::ffi::OsStr, directory: bool) -> Result<File, SkillResourceError> {
    let name = CString::new(name.as_bytes()).map_err(|_| SkillResourceError::InvalidPath)?;
    let (nofollow, dir, cloexec, nonblock) = FLAGS;
    // Read-only open; O_NOFOLLOW applies independently to every component.
    let fd = unsafe { openat(parent.as_raw_fd(), name.as_ptr(), nofollow | cloexec | nonblock | if directory { dir } else { 0 }) };
    if fd < 0 { return Err(SkillResourceError::Unavailable); }
    // openat returned a new owned descriptor.
    Ok(unsafe { File::from_raw_fd(fd) })
}

pub(super) fn open_resource(root: &Path, relative: &Path) -> Result<File, SkillResourceError> {
    // The host resolved this absolute path when granting access. Never resolve it again.
    if !root.is_absolute() { return Err(SkillResourceError::InvalidPath); }
    let mut current = File::open("/").map_err(|_| SkillResourceError::Unavailable)?;
    for part in root.components() {
        match part {
            Component::RootDir => {},
            Component::Normal(name) => current = open_child(&current, name, true)?,
            _ => return Err(SkillResourceError::InvalidPath),
        }
    }
    let mut parts = relative.components().peekable();
    while let Some(part) = parts.next() {
        let Component::Normal(name) = part else { return Err(SkillResourceError::InvalidPath) };
        current = open_child(&current, name, parts.peek().is_some())?;
    }
    Ok(current)
}
