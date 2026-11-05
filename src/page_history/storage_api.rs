fn existing_page(store: &Store, path: &Path) -> Result<Option<String>, String> {
    let index = store.index.as_ref().ok_or_else(|| "history index unavailable".to_string())?;
    match lookup_key(index, path).map_err(|error| error.to_string())? {
        Some((_, id)) if safe_component(&id) => Ok(Some(id)),
        Some(_) => Err("history page id is invalid".into()),
        None => Ok(None),
    }
}

fn read_version(store: &Store, path: &Path, version_id: &str) -> Result<String, String> {
    if version_id == "v-current" {
        return Err("v-current is a synthetic checkpoint, not a stored snapshot".into());
    }
    let Some(page_id) = existing_page(store, path)? else { return Err("Version not found".into()); };
    let metas = load_metas(&page_id).map_err(|error| error.to_string())?;
    let meta = metas.iter().find(|item| item.id == version_id).ok_or_else(|| "Version not found".to_string())?;
    let target = page_dir(&page_id).map_err(|error| error.to_string())?.join(&meta.file_name);
    let text = fs::read_to_string(&target).map_err(|error| error.to_string())?;
    if sha256_hex(text.as_bytes()) != meta.sha256 {
        return Err("snapshot checksum mismatch".into());
    }
    Ok(text)
}

fn versions_for(path: &Path, current_content: &str) -> Result<Vec<HistoryVersion>, String> {
    let mut store = lock()?;
    boot(&mut store)?;
    promote(&mut store, path, true)?;
    let page_id = get_or_create(&mut store, path)?;
    let metas = load_metas(&page_id).map_err(|error| error.to_string())?;
    let current_sha = sha256_hex(current_content.as_bytes());
    let mut list = Vec::new();
    let mut placed = false;
    if let Some(newest) = metas.last() {
        if newest.sha256 == current_sha {
            list.push(version_row(newest, true));
            placed = true;
        }
    }
    if !placed {
        let now = now_seconds();
        list.push(HistoryVersion {
            id: "v-current".into(), timestamp: now, display_time: format_version_time(now),
            sha256: current_sha.clone(), is_current: true, same_as_current: true,
        });
    }
    for meta in metas.iter().rev() {
        if placed && meta.id == list[0].id { continue; }
        list.push(HistoryVersion {
            id: meta.id.clone(), timestamp: meta.timestamp, display_time: format_version_time(meta.timestamp),
            sha256: meta.sha256.clone(), is_current: false, same_as_current: meta.sha256 == current_sha,
        });
    }
    Ok(list)
}

fn version_row(meta: &SnapshotMeta, current: bool) -> HistoryVersion {
    HistoryVersion {
        id: meta.id.clone(), timestamp: meta.timestamp, display_time: format_version_time(meta.timestamp),
        sha256: meta.sha256.clone(), is_current: current, same_as_current: true,
    }
}

include!("storage_restore.rs");

pub fn record_baseline_if_empty(path: &Path, content: &str) -> Result<(), String> {
    let mut store = lock()?;
    boot(&mut store)?;
    promote(&mut store, path, false)?;
    let page_id = get_or_create(&mut store, path)?;
    let metas = load_metas(&page_id).map_err(|error| error.to_string())?;
    if metas.is_empty() {
        let sha = sha256_hex(content.as_bytes());
        commit_snapshot(&page_id, content, &sha).map_err(|error| error.to_string())?;
    }
    ensure_worker();
    Ok(())
}

pub fn on_file_saved(path: &Path, content: &str) -> Result<(), String> {
    stage(path, content)
}

pub fn force_snapshot(path: &Path, content: &str) -> Result<SnapshotMeta, String> {
    let mut store = lock()?;
    boot(&mut store)?;
    promote(&mut store, path, true)?;
    let page_id = get_or_create(&mut store, path)?;
    let sha = sha256_hex(content.as_bytes());
    commit_snapshot(&page_id, content, &sha).map_err(|error| error.to_string())
}

pub fn load_versions(path: &Path, current_content: &str) -> Result<Vec<HistoryVersion>, String> {
    versions_for(path, current_content)
}

pub fn load_version_content(path: &Path, version_id: &str) -> Result<String, String> {
    let mut store = lock()?;
    boot(&mut store)?;
    promote(&mut store, path, true)?;
    read_version(&mut store, path, version_id)
}

pub fn restore_version_to_file(path: &Path, version_id: &str, current_base: u64) -> Result<(String, u64), String> {
    let mut store = lock()?;
    boot(&mut store)?;
    let content = read_version(&mut store, path, version_id)?;
    restore_content_locked(&mut store, path, &content, current_base)
}

pub fn restore_content_to_file(path: &Path, content: &str, current_base: u64) -> Result<(String, u64), String> {
    let mut store = lock()?;
    boot(&mut store)?;
    restore_content_locked(&mut store, path, content, current_base)
}

pub fn on_path_moved(from: &Path, to: &Path, is_dir: bool) -> io::Result<()> {
    let moved = relocate(from, to, is_dir);
    if let Err(error) = &moved {
        if let Err(rollback) = fs::rename(to, from) {
            crate::log::error(&format!(
                "history index update failed ({error}); rollback {} -> {} also failed: {rollback}",
                to.display(), from.display()
            ));
        }
    }
    moved
}

fn relocate(from: &Path, to: &Path, is_dir: bool) -> io::Result<()> {
    let from_path = normalize_path(from)?;
    let to_path = normalize_path(to)?;
    let mut store = lock().map_err(|error| io::Error::other(error))?;
    boot(&mut store).map_err(|error| io::Error::other(error))?;
    let keys = store.index.as_ref().map(|index| index.pages.keys().cloned().collect::<Vec<_>>()).unwrap_or_default();
    let mut edits = Vec::new();
    for key in keys {
        let Some(rest) = rebase_rest(&key, &from_path, is_dir) else { continue };
        let new_key = if rest.as_os_str().is_empty() { path_key(&to_path)? } else { path_key(&to_path.join(rest))? };
        if new_key != key { edits.push((key, new_key)); }
    }
    if edits.is_empty() { return Ok(()); }
    let previous_index = store.index.clone();
    let previous_pending = store.pending.clone();
    if let Some(index) = store.index.as_mut() {
        for (key, new_key) in &edits {
            if let Some(id) = index.pages.remove(key) {
                index.pages.insert(new_key.clone(), id);
            }
        }
    }
    for (key, new_key) in &edits {
        if let Some(item) = store.pending.remove(key) {
            store.pending.insert(new_key.clone(), item);
        }
    }
    let saved = store.index.as_ref().ok_or_else(|| io::Error::other("history index unavailable")).and_then(save_index);
    if let Err(error) = saved {
        store.index = previous_index;
        store.pending = previous_pending;
        return Err(error);
    }
    Ok(())
}
