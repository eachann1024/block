#[derive(Clone, Default, Serialize, Deserialize)]
struct PathIndex {
    pages: HashMap<String, String>,
}

fn history_root() -> PathBuf {
    crate::files::app_data_dir("Block").join("History")
}

fn page_dir(page_id: &str) -> io::Result<PathBuf> {
    if !safe_component(page_id) {
        return Err(io::Error::other("invalid history page id"));
    }
    let root = lexical(history_root());
    let dir = lexical(root.join(page_id));
    if !dir.starts_with(&root) {
        return Err(io::Error::other("history page id escapes the history root"));
    }
    Ok(dir)
}

fn index_path() -> PathBuf {
    history_root().join("index.json")
}

fn load_index() -> io::Result<PathIndex> {
    match fs::read(index_path()) {
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(PathIndex::default()),
        Err(error) => Err(error),
        Ok(bytes) => serde_json::from_slice(&bytes).map_err(|error| io::Error::new(io::ErrorKind::InvalidData, error.to_string())),
    }
}

fn save_index(index: &PathIndex) -> io::Result<()> {
    let bytes = serde_json::to_vec_pretty(index).map_err(|error| io::Error::other(error.to_string()))?;
    write_atomic(&index_path(), &bytes)
}

fn alloc_page_id() -> io::Result<String> {
    let root = history_root();
    fs::create_dir_all(&root)?;
    for _ in 0..64 {
        let id = unique_token("p");
        if !safe_component(&id) { continue; }
        let dir = page_dir(&id)?;
        match fs::create_dir(&dir) {
            Ok(()) => {}
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
        let marker = dir.join("page.id");
        match fs::OpenOptions::new().write(true).create_new(true).open(&marker) {
            Ok(mut file) => {
                use std::io::Write;
                file.write_all(id.as_bytes())?;
                file.sync_all()?;
                return Ok(id);
            }
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => {
                let _ = fs::remove_dir_all(&dir);
                continue;
            }
            Err(error) => {
                let _ = fs::remove_dir_all(&dir);
                return Err(error);
            }
        }
    }
    Err(io::Error::other("could not allocate a unique history page id"))
}

fn lookup_key(index: &PathIndex, path: &Path) -> io::Result<Option<(String, String)>> {
    let key = path_key(path)?;
    if let Some(id) = index.pages.get(&key) {
        return Ok(Some((key, id.clone())));
    }
    let raw = path.to_string_lossy().into_owned();
    if raw != key {
        if let Some(id) = index.pages.get(&raw) {
            return Ok(Some((raw, id.clone())));
        }
    }
    Ok(None)
}

fn get_or_create(store: &mut Store, path: &Path) -> Result<String, String> {
    let key = path_key(path).map_err(|error| error.to_string())?;
    let found = {
        let index = store.index.as_ref().ok_or_else(|| "history index unavailable".to_string())?;
        lookup_key(index, path).map_err(|error| error.to_string())?
    };
    if let Some((found_key, id)) = found {
        if !safe_component(&id) {
            return Err("history page id is invalid".into());
        }
        page_dir(&id).map_err(|error| error.to_string())?;
        if found_key != key {
            let previous = store.index.clone();
            if let Some(index) = store.index.as_mut() {
                index.pages.remove(&found_key);
                index.pages.insert(key, id.clone());
            }
            let saved = store.index.as_ref().ok_or_else(|| "history index unavailable".to_string()).and_then(|index| save_index(index).map_err(|error| error.to_string()));
            if let Err(error) = saved {
                store.index = previous;
                return Err(error);
            }
        }
        return Ok(id);
    }
    let id = alloc_page_id().map_err(|error| error.to_string())?;
    let index = store.index.as_mut().ok_or_else(|| "history index unavailable".to_string())?;
    index.pages.insert(key, id.clone());
    if let Err(error) = save_index(index) {
        index.pages.retain(|_, value| value != &id);
        if let Ok(dir) = page_dir(&id) {
            if let Err(remove) = fs::remove_dir_all(&dir) {
                crate::log::error(&format!("history page rollback failed for {}: {remove}", dir.display()));
            }
        }
        return Err(error.to_string());
    }
    Ok(id)
}

fn rebase_rest(key: &str, from: &Path, is_dir: bool) -> Option<PathBuf> {
    let cleaned = lexical(PathBuf::from(key));
    if is_dir {
        cleaned.strip_prefix(from).ok().map(Path::to_path_buf)
    } else if cleaned == from {
        Some(PathBuf::new())
    } else {
        None
    }
}
