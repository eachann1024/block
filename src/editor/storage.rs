//! Disk work runs off the UI thread. One shared lock serializes autosave and quit.
use std::{fs, io::{self, Write}, path::{Path, PathBuf}, hash::{Hash, Hasher}};
use serde::{Deserialize, Serialize};
use crate::{editor::{markdown, model::Doc}, theme::tr};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) struct Fingerprint(pub u64);
fn fingerprint(bytes: &[u8]) -> Fingerprint {
    let mut hash = std::collections::hash_map::DefaultHasher::new(); bytes.hash(&mut hash);
    Fingerprint(hash.finish())
}
#[cfg(target_family = "wasm")]
impl super::Editor {
    pub fn open_web_markdown(&mut self, path: PathBuf, source: &str, cx: &mut gpui::Context<Self>) {
        let base = fingerprint(source.as_bytes());
        let _ = crate::files::vfs::write_string(&path, source.to_owned());
        self.persistence.base = base;
        self.persistence.gate.lock().unwrap().saved.insert(path.clone(), base);
        self.reset_document(markdown::parse(source));
        self.folded.clear();
        self.path = Some(path);
        self.dirty = false;
        self.status.clear();
        cx.notify();
    }
}
#[derive(Serialize, Deserialize)]
struct Draft { path: PathBuf, source: String, base: u64 }
pub(super) struct Loaded { pub doc: Doc, pub base: Fingerprint, pub recovered: bool }
fn draft_path(path: &Path) -> PathBuf {
    let key = fingerprint(path.as_os_str().as_encoded_bytes()).0;
    crate::files::app_data_dir("Thinkly Fast").join("Recovery").join(format!("{key:016x}.json"))
}
pub(super) fn latest_draft() -> Result<(PathBuf, Loaded), String> {
    let root = draft_path(Path::new("unused")).parent().unwrap().to_path_buf();
    let mut entries = fs::read_dir(root).map_err(|_| tr("未找到可恢复的草稿", "No recovery drafts found").to_string())?
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
    Err(tr("未找到可读取的恢复草稿", "No readable recovery drafts found").into())
}
pub(super) fn load(path: &Path) -> Result<Loaded, String> {
    #[cfg(target_family = "wasm")]
    {
        let source = crate::files::vfs::read_to_string(path).unwrap_or_default();
        let base = fingerprint(source.as_bytes());
        return Ok(Loaded { doc: markdown::parse(&source), base, recovered: false });
    }
    #[cfg(not(target_family = "wasm"))]
    {
        let bytes = fs::read(path).map_err(|e| format!("{} {}: {e}", tr("无法读取", "Cannot read"), path.display()))?;
        let source = String::from_utf8(bytes).map_err(|e| format!("{}: {e}", tr("文件不是有效的 UTF-8", "Not UTF-8")))?;
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
}
fn atomic_write(path: &Path, bytes: &[u8], expected: Option<Fingerprint>) -> io::Result<()> {
    let parent = path.parent().ok_or_else(|| io::Error::other(tr("缺少父级目录", "Missing parent")))?;
    let mut temporary = None;
    for serial in 0..100 {
        let candidate = parent.join(format!(".thinkly-{}-{serial}.tmp", std::process::id()));
        match fs::OpenOptions::new().write(true).create_new(true).open(&candidate) {
            Ok(file) => { temporary = Some((candidate, file)); break; }
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    let (temp, mut file) = temporary.ok_or_else(|| io::Error::other(tr("无法创建临时文件", "Cannot allocate temporary file")))?;
    let result = (|| {
        if let Ok(meta) = fs::metadata(path) { file.set_permissions(meta.permissions())?; }
        file.write_all(bytes)?; file.sync_all()?;
        if let Some(expected) = expected {
            if fingerprint(&fs::read(path)?) != expected {
                return Err(io::Error::other(tr("替换前检测到外部更改", "External changes detected before replacement")));
            }
        }
        fs::rename(&temp, path)?;
        fs::File::open(parent)?.sync_all()
    })();
    if result.is_err() { let _ = fs::remove_file(temp); }
    result
}
pub(super) fn save_draft(path: &Path, doc: &Doc, base: Fingerprint) -> Result<(), String> {
    #[cfg(target_family = "wasm")]
    {
        let _ = crate::files::vfs::write_string(path, markdown::serialize(doc));
        let _ = base;
        return Ok(());
    }
    #[cfg(not(target_family = "wasm"))]
    write_draft(path, &markdown::serialize(doc), base)
}
fn write_draft(path: &Path, source: &str, base: Fingerprint) -> Result<(), String> {
    let target = draft_path(path);
    fs::create_dir_all(target.parent().unwrap()).map_err(|e| e.to_string())?;
    #[derive(Serialize)]
    struct DraftRef<'a> { path: &'a Path, source: &'a str, base: u64 }
    let draft = DraftRef { path, source, base: base.0 };
    let bytes = serde_json::to_vec(&draft).map_err(|e| e.to_string())?;
    atomic_write(&target, &bytes, None).map_err(|e| format!("{}: {e}", tr("写入恢复草稿失败", "Recovery draft failed")))
}
pub(super) fn save(path: &Path, doc: &Doc, base: Fingerprint) -> Result<Fingerprint, String> {
    #[cfg(target_family = "wasm")]
    {
        let source = markdown::serialize(doc);
        let next = fingerprint(source.as_bytes());
        let _ = crate::files::vfs::write_string(path, source);
        let _ = base;
        return Ok(next);
    }
    #[cfg(not(target_family = "wasm"))]
    {
        let source = markdown::serialize(doc);
        let current = match fs::read(path) {
            Ok(bytes) => bytes,
            Err(e) => {
                let _ = write_draft(path, &source, base);
                return Err(format!("{}: {e}", tr("文件已移除或无法读取，已保留恢复草稿", "File removed or unreadable; recovery draft retained")));
            }
        };
        if fingerprint(&current) != base {
            let _ = write_draft(path, &source, base);
            return Err(tr("检测到外部更改与当前文档冲突，已保留恢复草稿，原文件未被修改", "External changes conflict with this document. Recovery draft retained; original file untouched.").into());
        }
        if let Err(e) = atomic_write(path, source.as_bytes(), Some(base)) {
            let _ = write_draft(path, &source, base);
            return Err(format!("{}: {e}", tr("保存失败，已保留恢复草稿", "Save failed; recovery draft retained")));
        }
        let draft = draft_path(path);
        if draft.exists() { let _ = fs::remove_file(draft); }
        Ok(fingerprint(source.as_bytes()))
    }
}
