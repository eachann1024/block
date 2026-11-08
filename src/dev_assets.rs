//! Optional file assets for local development.
//!
//! Set `BLOCK_DEV_ASSETS=1` to read `icons/…`, `agents/…`, and `mascot/…`
//! from `<root>/assets` before falling back to the bytes compiled into the binary.
//! `<root>` is `BLOCK_DEV_ASSETS_ROOT` when that value is an absolute path;
//! otherwise it is the compile-time `CARGO_MANIFEST_DIR`. The process
//! working directory is never used.
//!
//! Any other flag value, an unsafe path, a missing file, or a file that
//! resolves outside the assets directory yields `None`. Callers then use
//! the embedded mascot and icon bytes, which is also the whole path when
//! the flag is unset.
//!
//! GPUI may keep bytes it has already loaded. Start the dev process again
//! after editing a file. This module does not watch the disk.

use std::path::{Component, Path, PathBuf};
use std::sync::OnceLock;

const ALLOWED_TOPS: &[&str] = &["icons", "agents", "mascot"];

/// Owned file bytes for `path`, or `None` so the caller keeps embedded data.
pub(crate) fn try_load(path: &str) -> Option<Vec<u8>> {
    let root = assets_root()?;
    load_under(root, path)
}

fn assets_root() -> Option<&'static Path> {
    static ROOT: OnceLock<Option<PathBuf>> = OnceLock::new();
    ROOT.get_or_init(resolve_assets_root).as_deref()
}

pub(crate) fn resolve_assets_root() -> Option<PathBuf> {
    let flag = std::env::var("BLOCK_DEV_ASSETS").unwrap_or_default();
    if flag.trim() != "1" {
        return None;
    }
    let base = std::env::var("BLOCK_DEV_ASSETS_ROOT")
        .ok()
        .map(|raw| raw.trim().to_owned())
        .filter(|raw| !raw.is_empty())
        .map(PathBuf::from)
        .filter(|path| path.is_absolute())
        .unwrap_or_else(manifest_dir);
    Some(base.join("assets"))
}

fn manifest_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
}

/// Map a GPUI asset path onto a relative file under the assets directory.
pub(crate) fn safe_relative(request: &str) -> Option<PathBuf> {
    if request.is_empty() || request.as_bytes().contains(&0) {
        return None;
    }
    if request.starts_with('/') || request.starts_with('\\') {
        return None;
    }
    // `C:…` is absolute on Windows. A leading `/` or `\` was already rejected.
    if request.as_bytes().get(1) == Some(&b':') {
        return None;
    }
    let mut relative = PathBuf::new();
    for component in Path::new(request).components() {
        match component {
            Component::Normal(segment) => {
                let name = segment.to_str()?;
                if name.is_empty() || name == "." || name == ".." {
                    return None;
                }
                relative.push(name);
            }
            Component::CurDir => {}
            Component::ParentDir | Component::RootDir | Component::Prefix(_) => return None,
        }
    }
    let mut parts = relative.components();
    let top = parts.next()?.as_os_str().to_str()?;
    if !ALLOWED_TOPS.contains(&top) || parts.next().is_none() {
        return None;
    }
    Some(relative)
}

pub(crate) fn load_under(root: &Path, request: &str) -> Option<Vec<u8>> {
    let relative = safe_relative(request)?;
    read_inside(root, &relative)
}

fn read_inside(root: &Path, relative: &Path) -> Option<Vec<u8>> {
    if relative.is_absolute()
        || relative.components().any(|component| !matches!(component, Component::Normal(_)))
    {
        return None;
    }
    let candidate = root.join(relative);
    if !candidate.starts_with(root) {
        return None;
    }
    let root_real = root.canonicalize().ok()?;
    let file_real = candidate.canonicalize().ok()?;
    if !file_real.starts_with(&root_real) || !file_real.is_file() {
        return None;
    }
    std::fs::read(file_real).ok()
}
