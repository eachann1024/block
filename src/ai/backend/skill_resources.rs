//! Read-only access to explicitly authorized skill resource directories.
use std::{fmt, io::Read, path::{Component, Path, PathBuf}};

#[cfg(any(target_os = "macos", all(target_os = "linux", any(target_arch = "x86_64", target_arch = "aarch64"))))]
#[path = "skill_resources/unix.rs"]
mod unix;

/// Constructed by the host from its trusted skill catalog, never from model input.
/// The host canonicalizes the root when it grants access, including any root symlink.
#[derive(Clone, Debug)]
pub(crate) struct SkillResourceGrant {
    pub stable_id: String,
    pub canonical_root: PathBuf,
}

/// Maximum UTF-8 resource size in bytes.
pub(crate) const MAX_RESOURCE_BYTES: usize = 1024 * 1024;

#[allow(dead_code)]
#[derive(Debug)]
pub(crate) enum SkillResourceError {
    Unauthorized,
    InvalidPath,
    Unavailable,
    NotText,
    TooLarge,
    Unsupported,
}

impl fmt::Display for SkillResourceError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Self::Unauthorized => "Skill is not authorized",
            Self::InvalidPath => "Invalid skill resource path",
            Self::Unavailable => "Skill resource is unavailable or not a regular file",
            Self::NotText => "Skill resource is not UTF-8 text",
            Self::TooLarge => "Skill resource exceeds the size limit",
            Self::Unsupported => "Secure skill resource access is unavailable on this platform",
        })
    }
}

impl std::error::Error for SkillResourceError {}

/// Read one relative text resource using only an unambiguous host-provided grant.
pub(crate) fn read(grants: &[SkillResourceGrant], skill_id: &str, relative_path: &str)
    -> Result<String, SkillResourceError> {
    let mut matches = grants.iter().filter(|grant| grant.stable_id == skill_id);
    let grant = matches.next().ok_or(SkillResourceError::Unauthorized)?;
    if skill_id.is_empty() || matches.next().is_some() { return Err(SkillResourceError::Unauthorized); }
    let path = Path::new(relative_path);
    if relative_path.is_empty() || path.components().any(|part| !matches!(part, Component::Normal(_))) {
        return Err(SkillResourceError::InvalidPath);
    }
    read_file(&grant.canonical_root, path)
}

#[cfg(any(target_os = "macos", all(target_os = "linux", any(target_arch = "x86_64", target_arch = "aarch64"))))]
fn read_file(root: &Path, path: &Path) -> Result<String, SkillResourceError> {
    let file = unix::open_resource(root, path)?;
    let metadata = file.metadata().map_err(|_| SkillResourceError::Unavailable)?;
    if !metadata.is_file() { return Err(SkillResourceError::Unavailable); }
    if metadata.len() > MAX_RESOURCE_BYTES as u64 { return Err(SkillResourceError::TooLarge); }
    let mut bytes = Vec::new();
    file.take(MAX_RESOURCE_BYTES as u64 + 1).read_to_end(&mut bytes).map_err(|_| SkillResourceError::Unavailable)?;
    if bytes.len() > MAX_RESOURCE_BYTES { return Err(SkillResourceError::TooLarge); }
    if bytes.iter().any(|byte| *byte < 0x20 && !matches!(*byte, b'\n' | b'\r' | b'\t')) {
        return Err(SkillResourceError::NotText);
    }
    String::from_utf8(bytes).map_err(|_| SkillResourceError::NotText)
}

#[cfg(not(any(target_os = "macos", all(target_os = "linux", any(target_arch = "x86_64", target_arch = "aarch64")))))]
fn read_file(_root: &Path, _path: &Path) -> Result<String, SkillResourceError> {
    Err(SkillResourceError::Unsupported)
}
