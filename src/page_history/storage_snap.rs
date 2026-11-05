const MAX_SNAPSHOTS: usize = 100;

#[derive(Clone, Serialize, Deserialize)]
struct PendingDisk {
    content: String,
    sha: String,
    first_at: u64,
    last_at: u64,
}

#[derive(Deserialize)]
struct LegacyPending {
    sha: String,
    first_at: u64,
    last_at: u64,
}

struct StagedSnapshot {
    meta: SnapshotMeta,
    path: std::path::PathBuf,
}

fn versions_path(page_id: &str) -> io::Result<std::path::PathBuf> {
    Ok(page_dir(page_id)?.join("versions.json"))
}

fn validate_meta(page_id: &str, meta: &SnapshotMeta) -> io::Result<()> {
    if !safe_component(&meta.id) {
        return Err(io::Error::other("invalid snapshot id"));
    }
    if meta.file_name != format!("{}.md", meta.id) || !safe_component(&meta.id) {
        return Err(io::Error::other("snapshot file name escapes the page directory"));
    }
    let dir = page_dir(page_id)?;
    let path = dir.join(&meta.file_name);
    if path.parent() != Some(dir.as_path()) {
        return Err(io::Error::other("snapshot path escapes the page directory"));
    }
    Ok(())
}

fn load_metas(page_id: &str) -> io::Result<Vec<SnapshotMeta>> {
    match fs::read(versions_path(page_id)?) {
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(Vec::new()),
        Err(error) => Err(error),
        Ok(bytes) => {
            let metas: Vec<SnapshotMeta> = serde_json::from_slice(&bytes)
                .map_err(|error| io::Error::new(io::ErrorKind::InvalidData, error.to_string()))?;
            for meta in &metas {
                validate_meta(page_id, meta)?;
            }
            Ok(metas)
        }
    }
}

fn save_metas(page_id: &str, metas: &[SnapshotMeta]) -> io::Result<()> {
    let bytes = serde_json::to_vec_pretty(metas).map_err(|error| io::Error::other(error.to_string()))?;
    write_atomic(&versions_path(page_id)?, &bytes)
}

fn trim_metas(metas: &mut Vec<SnapshotMeta>) -> Vec<SnapshotMeta> {
    if metas.len() <= MAX_SNAPSHOTS { return Vec::new(); }
    let excess = metas.len() - MAX_SNAPSHOTS;
    if excess == 0 || excess >= metas.len() { return Vec::new(); }
    metas.drain(1..=excess).collect()
}

fn delete_snapshots(page_id: &str, doomed: &[SnapshotMeta]) {
    for meta in doomed {
        let Ok(path) = page_dir(page_id).map(|dir| dir.join(&meta.file_name)) else { continue };
        if let Err(error) = fs::remove_file(&path) {
            if error.kind() != io::ErrorKind::NotFound {
                crate::log::warn(&format!("history prune failed for {}: {error}", path.display()));
            }
        }
    }
}

fn commit_snapshot(page_id: &str, content: &str, sha: &str) -> io::Result<SnapshotMeta> {
    let mut metas = load_metas(page_id)?;
    if let Some(last) = metas.last() {
        if last.sha256 == sha { return Ok(last.clone()); }
    }
    let staged = stage_snapshot_file(page_id, content, sha)?;
    publish_staged(page_id, &mut metas, staged)
}

fn stage_snapshot_file(page_id: &str, content: &str, sha: &str) -> io::Result<StagedSnapshot> {
    let dir = page_dir(page_id)?;
    fs::create_dir_all(&dir)?;
    let bytes = content.as_bytes();
    for _ in 0..64 {
        let id = unique_token("v");
        if !safe_component(&id) { continue; }
        let file_name = format!("{id}.md");
        let path = dir.join(&file_name);
        if path.parent() != Some(dir.as_path()) {
            return Err(io::Error::other("snapshot path escapes the page directory"));
        }
        match fs::OpenOptions::new().write(true).create_new(true).open(&path) {
            Ok(mut file) => {
                let wrote = (|| -> io::Result<()> {
                    file.write_all(bytes)?;
                    file.sync_all()?;
                    Ok(())
                })();
                if let Err(error) = wrote {
                    let _ = fs::remove_file(&path);
                    return Err(error);
                }
                if let Err(error) = sync_dir(&dir) {
                    crate::log::warn(&format!("history directory sync failed after creating {}: {error}", path.display()));
                }
                return Ok(StagedSnapshot {
                    meta: SnapshotMeta { id, timestamp: now_seconds(), sha256: sha.to_string(), file_name },
                    path,
                });
            }
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    Err(io::Error::other("could not allocate an immutable snapshot file"))
}

fn publish_staged(page_id: &str, metas: &mut Vec<SnapshotMeta>, staged: StagedSnapshot) -> io::Result<SnapshotMeta> {
    metas.push(staged.meta.clone());
    let doomed = trim_metas(metas);
    save_metas(page_id, metas)?;
    delete_snapshots(page_id, &doomed);
    Ok(staged.meta)
}

fn pending_file(page_id: &str) -> io::Result<PathBuf> {
    Ok(page_dir(page_id)?.join("pending.json"))
}

fn write_pending(page_id: &str, content: &str, sha: &str, first_at: u64, last_at: u64) -> io::Result<()> {
    let disk = PendingDisk { content: content.to_string(), sha: sha.to_string(), first_at, last_at };
    let bytes = serde_json::to_vec(&disk).map_err(|error| io::Error::other(error.to_string()))?;
    write_atomic(&pending_file(page_id)?, &bytes)
}

fn read_pending(page_id: &str) -> io::Result<Option<PendingDisk>> {
    let dir = page_dir(page_id)?;
    match fs::read(pending_file(page_id)?) {
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(error),
        Ok(bytes) => parse_pending_bytes(&dir, &bytes),
    }
}

fn parse_pending_bytes(dir: &Path, bytes: &[u8]) -> io::Result<Option<PendingDisk>> {
    if let Ok(disk) = serde_json::from_slice::<PendingDisk>(bytes) {
        if disk.sha == sha256_hex(disk.content.as_bytes()) {
            return Ok(Some(disk));
        }
        return Err(io::Error::new(io::ErrorKind::InvalidData, "history pending checksum mismatch"));
    }
    if let Ok(legacy) = serde_json::from_slice::<LegacyPending>(bytes) {
        return read_legacy_body(dir, legacy);
    }
    Err(io::Error::new(io::ErrorKind::InvalidData, "history pending is unreadable"))
}

fn read_legacy_body(dir: &Path, legacy: LegacyPending) -> io::Result<Option<PendingDisk>> {
    match fs::read_to_string(dir.join("pending.md")) {
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            Err(io::Error::new(io::ErrorKind::InvalidData, "history pending body is missing"))
        }
        Err(error) => Err(error),
        Ok(content) => {
            if legacy.sha != sha256_hex(content.as_bytes()) {
                return Err(io::Error::new(io::ErrorKind::InvalidData, "history pending checksum mismatch"));
            }
            Ok(Some(PendingDisk { content, sha: legacy.sha, first_at: legacy.first_at, last_at: legacy.last_at }))
        }
    }
}

fn clear_pending(page_id: &str) -> io::Result<()> {
    match fs::remove_file(pending_file(page_id)?) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(error),
    }
}
