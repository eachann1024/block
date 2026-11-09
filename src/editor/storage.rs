//! Disk work runs off the UI thread. One shared lock serializes autosave and quit.
use std::{fs, io::{self, Write}, path::{Path, PathBuf}, hash::{Hash, Hasher}};
use serde::{Deserialize, Serialize};
use crate::{editor::{markdown, model::Doc}, theme::tr};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(in crate::editor) struct Fingerprint(pub u64);
pub(in crate::editor) fn fingerprint(bytes: &[u8]) -> Fingerprint {
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
        self.surface = super::surface::Surface::Note;
        self.reset_document(markdown::parse(source), cx);
        self.folded.clear();
        self.path = Some(path);
        self.dirty = false;
        self.status.clear();
        cx.notify();
    }
}
#[derive(Serialize, Deserialize)]
struct Draft { path: PathBuf, source: String, base: u64, #[serde(default)] raw: bool, #[serde(default)] bom: bool }
pub(in crate::editor) struct Loaded { pub kind: crate::files::classify::OpenKind, pub base: Fingerprint, pub recovered: bool }
pub(in crate::editor) enum SaveDoc { Note(Doc), Raw { text: String, bom: bool } }
fn draft_path(path: &Path) -> PathBuf {
    let key = fingerprint(path.as_os_str().as_encoded_bytes()).0;
    crate::files::app_data_dir("Block").join("Recovery").join(format!("{key:016x}.json"))
}
pub(in crate::editor) fn latest_draft() -> Result<(PathBuf, Loaded), String> {
    let root = draft_path(Path::new("unused")).parent().unwrap().to_path_buf();
    let mut entries = fs::read_dir(root).map_err(|_| tr("未找到可恢复的草稿", "No recovery drafts found").to_string())?
        .filter_map(Result::ok).filter(|e| e.path().extension().is_some_and(|ext| ext == "json"))
        .collect::<Vec<_>>();
    entries.sort_by_key(|e| std::cmp::Reverse(e.metadata().ok().and_then(|m| m.modified().ok())));
    for entry in entries {
        if let Ok(bytes) = fs::read(entry.path()) {
            if let Ok(draft) = serde_json::from_slice::<Draft>(&bytes) {
                return Ok((draft.path.clone(), loaded_draft(&draft)));
            }
        }
    }
    Err(tr("未找到可读取的恢复草稿", "No readable recovery drafts found").into())
}
/// File bytes only. Recovery drafts stay out so a window refresh can follow an external edit.
pub(super) fn load_disk(path: &Path) -> Result<Loaded, String> {
    #[cfg(target_family = "wasm")]
    {
        let source = crate::files::vfs::read_to_string(path).unwrap_or_default();
        let base = fingerprint(source.as_bytes());
        Ok(Loaded { kind: crate::files::classify::OpenKind::Markdown(source), base, recovered: false })
    }
    #[cfg(not(target_family = "wasm"))]
    {
        let class = crate::files::classify::read_file(path).map_err(|e| e.to_string())?;
        let base = match &class.kind {
            crate::files::classify::OpenKind::Markdown(text) => fingerprint(text.as_bytes()),
            crate::files::classify::OpenKind::Source(file)
            | crate::files::classify::OpenKind::Preview { svg: Some(file) } => fingerprint(&crate::files::classify::raw_bytes(&file.text, file.bom)),
            _ if class.bytes.is_empty() => Fingerprint(0),
            _ => fingerprint(&class.bytes),
        };
        Ok(Loaded { kind: class.kind, base, recovered: false })
    }
}
pub(super) fn load(path: &Path) -> Result<Loaded, String> {
    #[cfg(target_family = "wasm")]
    {
        load_disk(path)
    }
    #[cfg(not(target_family = "wasm"))]
    {
        let mut loaded = load_disk(path)?;
        if let crate::files::classify::OpenKind::Markdown(text) = &loaded.kind {
            if let Err(error) = crate::page_history::record_baseline_if_empty(path, text) {
                crate::log::warn(&format!("history baseline was not recorded for {}: {error}", path.display()));
            }
        }
        if let Ok(bytes) = fs::read(draft_path(path)) {
            if let Ok(draft) = serde_json::from_slice::<Draft>(&bytes) && draft.path == path {
                if let Some(next) = apply_draft(loaded.kind.clone(), &draft) {
                    loaded.kind = next;
                    loaded.base = Fingerprint(draft.base);
                    loaded.recovered = true;
                }
            }
        }
        Ok(loaded)
    }
}
pub(super) fn discard_draft(path: &Path) {
    #[cfg(not(target_family = "wasm"))]
    {
        let draft = draft_path(path);
        if draft.exists() { let _ = fs::remove_file(draft); }
    }
    #[cfg(target_family = "wasm")]
    { let _ = path; }
}
fn atomic_write(path: &Path, bytes: &[u8], expected: Option<Fingerprint>) -> io::Result<()> {
    let parent = path.parent().ok_or_else(|| io::Error::other(tr("缺少父级目录", "Missing parent")))?;
    let mut temporary = None;
    for serial in 0..100 {
        let candidate = parent.join(format!(".block-{}-{serial}.tmp", std::process::id()));
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
        if let Ok(dir) = fs::File::open(parent) { let _ = dir.sync_all(); }
        Ok(())
    })();
    if result.is_err() { let _ = fs::remove_file(temp); }
    result
}
fn loaded_draft(draft: &Draft) -> Loaded {
    use crate::files::classify::{source_from_draft, OpenKind};
    let kind = if draft.raw {
        let file = source_from_draft(&draft.path, draft.source.clone(), draft.bom);
        if file.svg { OpenKind::Preview { svg: Some(file) } } else { OpenKind::Source(file) }
    } else {
        OpenKind::Markdown(draft.source.clone())
    };
    Loaded { kind, base: Fingerprint(draft.base), recovered: true }
}
fn apply_draft(kind: crate::files::classify::OpenKind, draft: &Draft) -> Option<crate::files::classify::OpenKind> {
    use crate::files::classify::{source_from_draft, OpenKind};
    match kind {
        OpenKind::Markdown(_) if !draft.raw => Some(OpenKind::Markdown(draft.source.clone())),
        OpenKind::Source(_) if draft.raw => Some(OpenKind::Source(source_from_draft(&draft.path, draft.source.clone(), draft.bom))),
        OpenKind::Preview { svg: Some(_) } if draft.raw => Some(OpenKind::Preview { svg: Some(source_from_draft(&draft.path, draft.source.clone(), draft.bom)) }),
        _ => None,
    }
}
fn payload(doc: &SaveDoc) -> (String, Vec<u8>, bool, bool) {
    match doc {
        SaveDoc::Note(doc) => {
            let source = markdown::serialize(doc);
            let bytes = source.as_bytes().to_vec();
            (source, bytes, false, false)
        }
        SaveDoc::Raw { text, bom } => (text.clone(), crate::files::classify::raw_bytes(text, *bom), true, *bom),
    }
}
pub(super) fn save_draft(path: &Path, doc: &SaveDoc, base: Fingerprint) -> Result<(), String> {
    let (source, _, raw, bom) = payload(doc);
    #[cfg(target_family = "wasm")]
    {
        let _ = crate::files::vfs::write_string(path, source);
        let _ = (base, raw, bom);
        return Ok(());
    }
    #[cfg(not(target_family = "wasm"))]
    write_draft(path, &source, base, raw, bom)
}
fn write_draft(path: &Path, source: &str, base: Fingerprint, raw: bool, bom: bool) -> Result<(), String> {
    let target = draft_path(path);
    fs::create_dir_all(target.parent().unwrap()).map_err(|e| e.to_string())?;
    #[derive(Serialize)]
    struct DraftRef<'a> { path: &'a Path, source: &'a str, base: u64, raw: bool, bom: bool }
    let draft = DraftRef { path, source, base: base.0, raw, bom };
    let bytes = serde_json::to_vec(&draft).map_err(|e| e.to_string())?;
    atomic_write(&target, &bytes, None).map_err(|e| format!("{}: {e}", tr("写入恢复草稿失败", "Recovery draft failed")))
}
pub(super) fn save(path: &Path, doc: &SaveDoc, base: Fingerprint) -> Result<Fingerprint, String> {
    let (source, bytes, raw, bom) = payload(doc);
    #[cfg(target_family = "wasm")]
    {
        let next = fingerprint(&bytes);
        let _ = crate::files::vfs::write_string(path, source);
        let _ = (base, raw, bom);
        return Ok(next);
    }
    #[cfg(not(target_family = "wasm"))]
    {
        let current = match fs::read(path) {
            Ok(bytes) => bytes,
            Err(e) => {
                let _ = write_draft(path, &source, base, raw, bom);
                return Err(format!("{}: {e}", tr("文件已移除或无法读取，已保留恢复草稿", "File removed or unreadable; recovery draft retained")));
            }
        };
        let current_fp = fingerprint(&current);
        let next_fp = fingerprint(&bytes);
        if current_fp == next_fp {
            let draft = draft_path(path);
            if draft.exists() { let _ = fs::remove_file(draft); }
            return Ok(next_fp);
        }
        if current_fp != base {
            let _ = write_draft(path, &source, base, raw, bom);
            return Err(tr("检测到外部更改与当前文档冲突，已保留恢复草稿，原文件未被修改", "External changes conflict with this document. Recovery draft retained; original file untouched.").into());
        }
        if let Err(e) = atomic_write(path, &bytes, Some(base)) {
            let _ = write_draft(path, &source, base, raw, bom);
            return Err(format!("{}: {e}", tr("保存失败，已保留恢复草稿", "Save failed; recovery draft retained")));
        }
        let draft = draft_path(path);
        if draft.exists() { let _ = fs::remove_file(draft); }
        if !raw {
            if let Err(error) = crate::page_history::on_file_saved(path, &source) {
                crate::log::warn(&format!("history was not recorded for {}: {error}", path.display()));
            }
        }
        Ok(fingerprint(&bytes))
    }
}
