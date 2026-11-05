pub fn is_file_readonly(path: &Path) -> bool {
    if fs::metadata(path).ok().is_some_and(|meta| meta.permissions().readonly()) {
        return true;
    }
    match fs::OpenOptions::new().write(true).open(path) {
        Err(error) if error.kind() == std::io::ErrorKind::PermissionDenied => true,
        _ => false,
    }
}

fn replace_file(path: &Path, bytes: &[u8], expected: u64) -> Result<(), String> {
    let parent = path.parent().ok_or_else(|| "Missing parent directory".to_string())?;
    let mut chosen = None;
    for serial in 0..100 {
        let candidate = parent.join(format!(".block-restore-{}-{serial}.tmp", std::process::id()));
        match fs::OpenOptions::new().write(true).create_new(true).open(&candidate) {
            Ok(file) => { chosen = Some((candidate, file)); break; }
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(format!("WRITE_FAIL: {error}")),
        }
    }
    let Some((temp, mut file)) = chosen else {
        return Err("WRITE_FAIL: could not allocate a temporary file".into());
    };
    let replaced = (|| -> Result<(), String> {
        if let Ok(meta) = fs::metadata(path) {
            file.set_permissions(meta.permissions()).map_err(|error| format!("WRITE_FAIL: {error}"))?;
        }
        file.write_all(bytes).map_err(|error| format!("WRITE_FAIL: {error}"))?;
        file.sync_all().map_err(|error| format!("WRITE_FAIL: {error}"))?;
        drop(file);
        let disk = fs::read(path).map_err(|error| format!("READ_FAIL: {error}"))?;
        if fingerprint_bytes(&disk) != expected {
            return Err("EXTERNAL_CONFLICT".into());
        }
        fs::rename(&temp, path).map_err(|error| format!("WRITE_FAIL: {error}"))?;
        if let Err(error) = sync_dir(parent) {
            crate::log::warn(&format!("history restore committed but directory sync failed for {}: {error}", path.display()));
        }
        Ok(())
    })();
    if replaced.is_err() {
        let _ = fs::remove_file(&temp);
    }
    replaced
}

fn restore_content_locked(store: &mut Store, path: &Path, content: &str, expected: u64) -> Result<(String, u64), String> {
    if is_file_readonly(path) {
        return Err("READ_ONLY".into());
    }
    let disk = fs::read(path).map_err(|error| format!("READ_FAIL: {error}"))?;
    if fingerprint_bytes(&disk) != expected {
        return Err("EXTERNAL_CONFLICT".into());
    }
    let disk_text = String::from_utf8(disk).map_err(|error| format!("UTF8_FAIL: {error}"))?;
    promote(store, path, true)?;
    let page_id = get_or_create(store, path)?;
    let disk_sha = sha256_hex(disk_text.as_bytes());
    commit_snapshot(&page_id, &disk_text, &disk_sha).map_err(|error| format!("SNAPSHOT_FAIL: {error}"))?;
    let target_sha = sha256_hex(content.as_bytes());
    let mut metas = load_metas(&page_id).map_err(|error| format!("SNAPSHOT_FAIL: {error}"))?;
    let staged = if metas.last().is_some_and(|meta| meta.sha256 == target_sha) {
        None
    } else {
        Some(stage_snapshot_file(&page_id, content, &target_sha).map_err(|error| format!("SNAPSHOT_FAIL: {error}"))?)
    };
    let written = content.as_bytes();
    let new_base = fingerprint_bytes(written);
    match replace_file(path, written, expected) {
        Ok(()) => {
            let now = now_seconds();
            if let Err(error) = write_pending(&page_id, content, &target_sha, now, now) {
                crate::log::error(&format!("history restore committed {} but the restored pending record failed: {error}", path.display()));
            } else if let Ok(key) = path_key(path) {
                store.pending.insert(key, Pending {
                    content: content.to_string(), sha: target_sha.clone(), first_at: now, last_at: now,
                });
            }
            let published = match staged {
                Some(staged) => publish_staged(&page_id, &mut metas, staged).map(|_| ()),
                None => Ok(()),
            };
            if let Err(error) = published {
                crate::log::error(&format!("history restore committed {} but snapshot metadata was not published: {error}", path.display()));
            } else if let Err(error) = clear_pending(&page_id) {
                crate::log::warn(&format!("history pending cleanup failed for {}: {error}", path.display()));
            } else if let Ok(key) = path_key(path) {
                store.pending.remove(&key);
            }
            Ok((content.to_string(), new_base))
        }
        Err(error) => {
            if let Some(staged) = staged {
                if let Err(remove) = fs::remove_file(&staged.path) {
                    crate::log::warn(&format!("history staged snapshot cleanup failed for {}: {remove}", staged.path.display()));
                }
            }
            Err(error)
        }
    }
}
