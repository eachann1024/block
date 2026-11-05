static TOKEN_SEQ: AtomicU64 = AtomicU64::new(1);

fn unique_token(prefix: &str) -> String {
    let seq = TOKEN_SEQ.fetch_add(1, Ordering::Relaxed);
    format!("{prefix}_{:x}_{seq:x}_{:x}", now_nanos(), std::process::id())
}

fn safe_component(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 96
        && value.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

fn lexical(path: PathBuf) -> PathBuf {
    let mut out = PathBuf::new();
    for component in path.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => { out.pop(); }
            other => out.push(other.as_os_str()),
        }
    }
    out
}

fn normalize_path(path: &Path) -> io::Result<PathBuf> {
    let absolute = if path.is_absolute() { path.to_path_buf() } else { std::env::current_dir()?.join(path) };
    Ok(lexical(absolute))
}

fn path_key(path: &Path) -> io::Result<String> {
    Ok(normalize_path(path)?.to_string_lossy().into_owned())
}

fn sync_dir(dir: &Path) -> io::Result<()> {
    fs::File::open(dir)?.sync_all()
}

fn write_atomic(target: &Path, bytes: &[u8]) -> io::Result<()> {
    let parent = target.parent().ok_or_else(|| io::Error::other("missing parent directory"))?;
    fs::create_dir_all(parent)?;
    let stem = target.file_name().and_then(|name| name.to_str()).unwrap_or("hist");
    let mut temp_path = None;
    for serial in 0..100 {
        let candidate = parent.join(format!(".{stem}-{}.{serial}.tmp", std::process::id()));
        match fs::OpenOptions::new().write(true).create_new(true).open(&candidate) {
            Ok(file) => {
                temp_path = Some((candidate, file));
                break;
            }
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    let Some((temp_path, mut file)) = temp_path else {
        return Err(io::Error::other("could not allocate a history temporary file"));
    };
    let written = (|| -> io::Result<()> {
        file.write_all(bytes)?;
        file.sync_all()?;
        drop(file);
        Ok(())
    })();
    if let Err(error) = written {
        let _ = fs::remove_file(&temp_path);
        return Err(error);
    }
    // Rename is the commit point. A later directory sync failure must not roll back.
    if let Err(error) = fs::rename(&temp_path, target) {
        let _ = fs::remove_file(&temp_path);
        return Err(error);
    }
    if let Err(error) = sync_dir(parent) {
        crate::log::warn(&format!("history directory sync failed after committing {}: {error}", target.display()));
    }
    Ok(())
}
