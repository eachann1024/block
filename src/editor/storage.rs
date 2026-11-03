//! Disk work runs off the UI thread. One shared lock serializes autosave and quit.
use std::{fs, io::{self, Write}, path::{Path, PathBuf}, hash::{Hash, Hasher}};
use serde::{Deserialize, Serialize};
use crate::editor::{markdown, model::Doc};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) struct Fingerprint(pub u64);
fn fingerprint(bytes: &[u8]) -> Fingerprint {
    let mut hash = std::collections::hash_map::DefaultHasher::new(); bytes.hash(&mut hash);
    Fingerprint(hash.finish())
}
#[derive(Serialize, Deserialize)]
struct Draft { path: PathBuf, source: String, base: u64 }
pub(super) struct Loaded { pub doc: Doc, pub base: Fingerprint, pub recovered: bool }
fn draft_path(path: &Path) -> PathBuf {
    let key = fingerprint(path.as_os_str().as_encoded_bytes()).0;
    let root = std::env::var_os("HOME").map(PathBuf::from).unwrap_or_else(std::env::temp_dir);
    root.join("Library/Application Support/Thinkly Fast/Recovery").join(format!("{key:016x}.json"))
}
pub(super) fn latest_draft() -> Result<(PathBuf, Loaded), String> {
    let root = draft_path(Path::new("unused")).parent().unwrap().to_path_buf();
    let mut entries = fs::read_dir(root).map_err(|_| "No recovery drafts found".to_string())?
        .filter_map(Result::ok).filter(|e| e.path().extension().is_some_and(|ext| ext == "json"))
        .collect::<Vec<_>>();
    entries.sort_by_key(|e| std::cmp::Reverse(e.metadata().ok().and_then(|m| m.modified().ok())));
    for entry in entries {
        if let Ok(bytes) = fs::read(entry.path()) {
            if let Ok(draft) = serde_json::from_slice::<Draft>(&bytes) {
                return Ok((draft.path, Loaded { doc: markdown::parse(&draft.source), base: Fingerprint(draft.base), recovered: true }));
            }
        }
    }
    Err("No readable recovery drafts found".into())
}
pub(super) fn load(path: &Path) -> Result<Loaded, String> {
    let bytes = fs::read(path).map_err(|e| format!("Cannot read {}: {e}", path.display()))?;
    let source = String::from_utf8(bytes).map_err(|e| format!("Not UTF-8: {e}"))?;
    let base = fingerprint(source.as_bytes());
    if let Ok(bytes) = fs::read(draft_path(path)) {
        if let Ok(draft) = serde_json::from_slice::<Draft>(&bytes) {
            if draft.path == path {
                return Ok(Loaded { doc: markdown::parse(&draft.source), base: Fingerprint(draft.base), recovered: true });
            }
        }
    }
    Ok(Loaded { doc: markdown::parse(&source), base, recovered: false })
}
fn atomic_write(path: &Path, bytes: &[u8], expected: Option<Fingerprint>) -> io::Result<()> {
    let parent = path.parent().ok_or_else(|| io::Error::other("Missing parent"))?;
    let mut temporary = None;
    for serial in 0..100 {
        let candidate = parent.join(format!(".thinkly-{}-{serial}.tmp", std::process::id()));
        match fs::OpenOptions::new().write(true).create_new(true).open(&candidate) {
            Ok(file) => { temporary = Some((candidate, file)); break; }
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    let (temp, mut file) = temporary.ok_or_else(|| io::Error::other("Cannot allocate temporary file"))?;
    let result = (|| {
        if let Ok(meta) = fs::metadata(path) { file.set_permissions(meta.permissions())?; }
        file.write_all(bytes)?; file.sync_all()?;
        if let Some(expected) = expected {
            if fingerprint(&fs::read(path)?) != expected {
                return Err(io::Error::other("External changes detected before replacement"));
            }
        }
        fs::rename(&temp, path)?;
        fs::File::open(parent)?.sync_all()
    })();
    if result.is_err() { let _ = fs::remove_file(temp); }
    result
}
pub(super) fn save_draft(path: &Path, doc: &Doc, base: Fingerprint) -> Result<(), String> {
    write_draft(path, &markdown::serialize(doc), base)
}
fn write_draft(path: &Path, source: &str, base: Fingerprint) -> Result<(), String> {
    let target = draft_path(path);
    fs::create_dir_all(target.parent().unwrap()).map_err(|e| e.to_string())?;
    #[derive(Serialize)]
    struct DraftRef<'a> { path: &'a Path, source: &'a str, base: u64 }
    let draft = DraftRef { path, source, base: base.0 };
    let bytes = serde_json::to_vec(&draft).map_err(|e| e.to_string())?;
    atomic_write(&target, &bytes, None).map_err(|e| format!("Recovery draft failed: {e}"))
}
pub(super) fn save(path: &Path, doc: &Doc, base: Fingerprint) -> Result<Fingerprint, String> {
    let source = markdown::serialize(doc);
    let current = match fs::read(path) {
        Ok(bytes) => bytes,
        Err(e) => {
            let _ = write_draft(path, &source, base);
            return Err(format!("File removed or unreadable; recovery draft retained: {e}"));
        }
    };
    if fingerprint(&current) != base {
        let _ = write_draft(path, &source, base);
        return Err("External changes conflict with this document. Recovery draft retained; original file untouched.".into());
    }
    if let Err(e) = atomic_write(path, source.as_bytes(), Some(base)) {
        let _ = write_draft(path, &source, base);
        return Err(format!("Save failed; recovery draft retained: {e}"));
    }
    let draft = draft_path(path);
    if draft.exists() { let _ = fs::remove_file(draft); }
    Ok(fingerprint(source.as_bytes()))
}
