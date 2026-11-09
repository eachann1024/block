//! Persist directory grants and keep them alive for all notebook consumers.
use std::path::Path;
use serde::{Deserialize, Serialize};

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Bookmark {
    data: Vec<u8>,
    security_scoped: bool,
}

#[cfg(target_os = "macos")]
#[path = "access_macos.rs"]
mod native;

#[cfg(target_os = "macos")]
static SCOPES: std::sync::Mutex<std::collections::BTreeMap<std::path::PathBuf, native::Scope>> =
    std::sync::Mutex::new(std::collections::BTreeMap::new());

/// Restore every loaded notebook, including those used by search and Quick Note.
pub fn restore(state: &super::state::State) {
    #[cfg(target_os = "macos")]
    for (path, bookmark) in &state.folder_bookmarks {
        if state.folder.as_deref() != Some(path) && !state.notebooks.contains(path) { continue; }
        let result = (|| {
            let mut scopes = SCOPES.lock().unwrap_or_else(|e| e.into_inner());
            if scopes.contains_key(path) { return Ok(()); }
            let (scope, stale) = native::resolve(bookmark)?;
            let refreshed = stale.then(|| scope.bookmark()).transpose();
            scopes.insert(path.clone(), scope);
            drop(scopes);
            if let Some(bookmark) = refreshed? {
                super::update_state(|state| { state.folder_bookmarks.insert(path.clone(), bookmark); })?;
            }
            Ok::<_, std::io::Error>(())
        })();
        if let Err(error) = result {
            crate::log::warn(&format!("could not restore folder access {}: {error}", path.display()));
        }
    }
    #[cfg(not(target_os = "macos"))]
    let _ = state;
}

/// Called on a worker after a successful scan, while the picker grant is valid.
/// Also migrates existing path-only notebooks without requiring a new selection.
pub fn remember(path: &Path) -> std::io::Result<()> {
    #[cfg(target_os = "macos")]
    {
        let mut scopes = SCOPES.lock().unwrap_or_else(|e| e.into_inner());
        let bookmark = native::capture(path)?;
        let (scope, _) = native::resolve(&bookmark)?;
        super::update_state(|state| {
            state.folder_bookmarks.insert(path.to_owned(), bookmark);
            state.folder_bookmarks.retain(|known, _| known == path
                || state.folder.as_ref() == Some(known) || state.notebooks.contains(known));
        })?;
        // Keep grants for the session: open tabs and background saves may still
        // use a previous notebook after the workspace switches to another one.
        scopes.insert(path.to_owned(), scope);
    }
    #[cfg(not(target_os = "macos"))]
    let _ = path;
    Ok(())
}

pub fn needs_authorization(error: &std::io::Error) -> bool {
    cfg!(target_os = "macos") && error.kind() == std::io::ErrorKind::PermissionDenied
}
