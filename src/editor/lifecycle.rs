use std::{collections::{HashMap, HashSet}, path::{Path, PathBuf}, sync::{Arc, Mutex}, time::Duration};
use gpui::{Context, ListOffset, Pixels, Task};
use crate::theme::tr;
use super::{Editor, Doc, selection::Position, storage::{self, Fingerprint}};

#[derive(Clone)]
pub(in crate::editor) enum Pending { Open(PathBuf), Close, ReleaseUnder }
enum DiskPlace {
    None,
    Note { anchor_ix: usize, anchor_off: usize, head_ix: usize, head_off: usize, item_ix: usize, offset: Pixels },
    Source { anchor: usize, head: usize, item_ix: usize, offset: Pixels },
}
#[derive(Default)]
pub(in crate::editor) struct DiskGate { pub epoch: u64, pub saved: HashMap<PathBuf, Fingerprint> }
pub(in crate::editor) struct Persistence {
    pub revision: u64,
    pub generation: u64,
    pub base: Fingerprint,
    pub pending: Option<Pending>,
    pub saving: HashSet<PathBuf>,
    pub loading: bool,
    pub loading_path: Option<(PathBuf, u64)>,
    pub timer: Option<Task<()>>,
    pub gate: Arc<Mutex<DiskGate>>,
    pub write_epoch: u64,
    /// Window activation asked to re-read the open file, but a save was still in flight.
    pub refresh_after_save: bool,
    pub disk_refresh: u64,
}
impl Default for Persistence {
    fn default() -> Self {
        Self { revision: 0, generation: 0, base: Fingerprint(0), pending: None,
            saving: HashSet::new(), loading: false, loading_path: None, timer: None, gate: Arc::new(Mutex::new(DiskGate::default())), write_epoch: 0,
            refresh_after_save: false, disk_refresh: 0 }
    }
}
impl Editor {
    pub fn current_path(&self) -> Option<&Path> { self.path.as_deref() }
    pub fn is_loading(&self) -> bool { self.persistence.loading }
    pub fn interaction_epoch(&self) -> u64 { self.persistence.generation }
    pub fn is_busy(&self) -> bool { !self.persistence.saving.is_empty() || self.persistence.loading || self.persistence.pending.is_some() }
    pub fn report_status(&mut self, message: String, cx: &mut Context<Self>) {
        self.status = message; cx.notify();
    }
    pub fn open(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        if self.search_jump.as_ref().is_some_and(|(target, _)| target != &path) {
            self.search_jump = None;
        }
        crate::log::set_context(format!("note={}", path.display()));
        let pending_open = match &self.persistence.pending { Some(Pending::Open(pending)) => Some(pending.as_path()), _ => None };
        if super::open_dedup::duplicate_open(
            &path, pending_open, self.persistence.pending.is_none(), self.persistence.loading,
            self.persistence.loading_path.as_ref(), self.persistence.generation,
        ) { return; }
        if self.path.as_ref() == Some(&path) && self.persistence.pending.is_none() {
            if self.status == "正在打开…" || self.status == "Opening…" {
                self.status = if self.persistence.saving.contains(&path) { tr("正在保存…", "Saving…").into() }
                    else if self.dirty { tr("未保存更改", "Unsaved changes").into() }
                    else { String::new() };
                cx.notify();
            }
            return;
        }
        self.persistence.generation += 1;
        self.persistence.loading = false;
        self.persistence.pending = None;
        let prev = self.park_active(cx);
        if let Some(session) = self.sessions.remove(&path) {
            self.restore_session(path, session, cx);
            return;
        }
        if self.persistence.saving.contains(&path) {
            self.persistence.pending = Some(Pending::Open(path));
            cx.notify();
            return;
        }
        self.load_path(path, prev, cx);
    }
    pub fn close(&mut self, cx: &mut Context<Self>) -> bool {
        self.persistence.generation += 1;
        self.persistence.loading = false;
        self.park_active(cx);
        self.sessions.retain(|_, s| s.dirty);
        let dirty_keys: Vec<PathBuf> = self.sessions.keys().cloned().collect();
        for key in dirty_keys { self.spawn_save(key, cx); }
        self.persistence.pending = Some(Pending::Close);
        self.advance(cx);
        !self.is_busy() && self.path.is_none()
    }
    pub(super) fn advance(&mut self, cx: &mut Context<Self>) {
        if !self.persistence.saving.is_empty() { return; }
        if self.dirty { self.flush(cx); return; }
        let Some(pending) = self.persistence.pending.take() else { return; };
        self.persistence.generation += 1;
        self.persistence.timer = None;
        match pending {
            Pending::Close => {
                self.persistence.loading = false;
                self.sessions.clear();
                self.surface = crate::editor::surface::Surface::Note;
                self.source_layouts.clear();
                self.reset_document(Doc::empty()); self.path = None; self.dirty = false;
                self.status.clear(); cx.notify();
            }
            Pending::ReleaseUnder => {
                self.persistence.loading = false;
                cx.notify();
            }
            Pending::Open(path) => {
                if let Some(session) = self.sessions.remove(&path) {
                    self.restore_session(path, session, cx);
                } else {
                    self.load_path(path, None, cx);
                }
            }
        }
    }
    pub(super) fn load_path(&mut self, path: PathBuf, prev: Option<PathBuf>, cx: &mut Context<Self>) {
        self.persistence.generation += 1;
        let generation = self.persistence.generation;
        if self.minimal {
            self.persistence.loading = false;
            self.persistence.loading_path = None;
            match storage::load(&path) {
                Ok(loaded) => self.show_loaded(path, loaded, cx),
                Err(error) => self.fail_open(path, prev, error, cx),
            }
            return;
        }
        self.persistence.loading = true;
        self.persistence.loading_path = Some((path.clone(), generation));
        self.status = tr("正在打开…", "Opening…").into(); cx.notify();
        let request_path = path.clone();
        let task = cx.background_executor().spawn(async move { storage::load(&request_path) });
        cx.spawn(async move |this, cx| {
            let loaded = task.await;
            let _ = this.update(cx, |this, cx| {
                if this.persistence.generation != generation { return; }
                this.persistence.loading = false;
                this.persistence.loading_path = None;
                match loaded {
                    Ok(loaded) => {
                        this.show_loaded(path, loaded, cx);
                    }
                    Err(error) => this.fail_open(path, prev, error, cx),
                }
                cx.notify();
            });
        }).detach();
    }
    /// Missing or unreadable files stay on the selected path instead of the previous note.
    fn fail_open(&mut self, path: PathBuf, prev: Option<PathBuf>, error: String, cx: &mut Context<Self>) {
        self.search_jump = None;
        crate::log::error(&format!("failed to load {}: {error}", path.display()));
        let missing = error.contains("No such file") || error.contains("找不到")
            || error.contains("Permission denied") || error.contains("权限") || error.contains("Operation not permitted");
        if missing {
            self.show_loaded(path, storage::Loaded {
                kind: crate::files::classify::OpenKind::Blocked { reason: error.clone() },
                base: storage::Fingerprint(0), recovered: false,
            }, cx);
        } else if let Some(prev) = prev && let Some(sess) = self.sessions.remove(&prev) {
            self.restore_session(prev, sess, cx);
        }
        self.status = error;
    }
    pub(in crate::editor) fn touch(&mut self, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.dirty = true;
        self.persistence.revision += 1;
        self.status = tr("未保存更改", "Unsaved changes").into();
        let revision = self.persistence.revision;
        self.persistence.timer = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(Duration::from_millis(400)).await;
            let _ = this.update(cx, |this, cx| {
                if this.persistence.revision != revision { return; }
                this.persistence.timer = None;
                if this.caret.composing.is_none() && !this.source_composing() { this.flush(cx); }
            });
        }));
        cx.notify();
    }
    pub fn save(&mut self, cx: &mut Context<Self>) {
        self.persistence.timer = None;
        self.finish_source_composition(cx);
        self.commit_composition(cx); self.flush(cx);
    }
    fn flush(&mut self, cx: &mut Context<Self>) {
        if !self.dirty { return; }
        let Some(path) = self.path.clone() else { return; };
        self.spawn_save(path, cx);
    }
    pub(super) fn spawn_save(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        if self.persistence.saving.contains(&path) { return; }
        let Some((doc, base, revision)) = self.save_payload(&path) else {
            if self.path.as_ref() == Some(&path) { self.dirty = false; }
            else if let Some(sess) = self.sessions.get_mut(&path) { sess.dirty = false; }
            return;
        };
        if self.path.as_ref() == Some(&path) { self.status = tr("正在保存…", "Saving…").into(); }
        else if let Some(sess) = self.sessions.get_mut(&path) { sess.status = tr("正在保存…", "Saving…").into(); }
        self.persistence.saving.insert(path.clone());
        let gate = self.persistence.gate.clone();
        let epoch = self.persistence.write_epoch;
        cx.notify();
        let save_path = path.clone();
        let task = cx.background_executor().spawn(async move {
            let mut state = gate.lock().unwrap();
            if state.epoch != epoch { return Ok(None); }
            let result = storage::save(&save_path, &doc, base);
            if let Ok(hash) = result { state.saved.insert(save_path, hash); }
            result.map(Some)
        });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                this.persistence.saving.remove(&path);
                if this.persistence.write_epoch != epoch { return; }
                match result {
                    Ok(Some(base)) => {
                        if this.path.as_ref() == Some(&path) {
                            this.persistence.base = base;
                            if this.persistence.revision == revision { this.dirty = false; this.status = tr("已保存", "Saved").into(); }
                            else if this.dirty { this.touch(cx); }
                        } else if let Some(sess) = this.sessions.get_mut(&path) {
                            sess.base = base;
                            if sess.revision == revision { sess.dirty = false; sess.status = tr("已保存", "Saved").into(); }
                            else if sess.dirty { this.spawn_save(path.clone(), cx); }
                        }
                        if this.persistence.saving.is_empty() && this.persistence.pending.is_some() { this.advance(cx); }
                    }
                    Ok(None) => {}
                    Err(error) => {
                        crate::log::error(&format!("failed to save note {}: {error}", path.display()));
                        let was_closing = matches!(this.persistence.pending, Some(Pending::Close | Pending::ReleaseUnder));
                        this.persistence.pending = None;
                        if this.path.as_ref() == Some(&path) { this.status = error; }
                        else if was_closing && this.path.is_none() {
                            if let Some(mut sess) = this.sessions.remove(&path) { sess.status = error; this.restore_session(path, sess, cx); }
                        } else if let Some(sess) = this.sessions.get_mut(&path) { sess.status = error; }
                    }
                }
                this.finish_disk_refresh(cx);
                cx.notify();
            });
        }).detach();
    }
    /// Re-read the open file after the window becomes active.
    /// Unchanged disk keeps unsaved keystrokes. A changed file replaces the buffer.
    pub fn refresh_from_disk(&mut self, cx: &mut Context<Self>) {
        if self.caret.composing.is_some() || self.source_composing() { return; }
        if self.persistence.loading || self.persistence.pending.is_some() { return; }
        let Some(path) = self.path.clone() else { return; };
        if self.persistence.saving.contains(&path) {
            self.persistence.refresh_after_save = true;
            return;
        }
        self.persistence.timer = None;
        self.persistence.disk_refresh = self.persistence.disk_refresh.wrapping_add(1);
        let token = self.persistence.disk_refresh;
        let revision = self.persistence.revision;
        let request = path.clone();
        let task = cx.background_executor().spawn(async move { storage::load_disk(&request) });
        cx.spawn(async move |this, cx| {
            let loaded = task.await;
            let _ = this.update(cx, |this, cx| {
                if this.persistence.disk_refresh != token { return; }
                if this.path.as_deref() != Some(path.as_path()) { return; }
                if this.caret.composing.is_some() || this.source_composing() {
                    if this.dirty && this.persistence.revision == revision { this.touch(cx); }
                    return;
                }
                if this.persistence.saving.contains(&path) {
                    this.persistence.refresh_after_save = true;
                    return;
                }
                if this.persistence.revision != revision { return; }
                match loaded {
                    Ok(loaded) => this.adopt_external(path, loaded, cx),
                    Err(_) => { if this.dirty { this.flush(cx); } }
                }
            });
        }).detach();
    }
    fn finish_disk_refresh(&mut self, cx: &mut Context<Self>) {
        if !self.persistence.refresh_after_save { return; }
        if self.path.as_ref().is_some_and(|path| self.persistence.saving.contains(path)) { return; }
        self.persistence.refresh_after_save = false;
        self.refresh_from_disk(cx);
    }
    /// Disk is the source of truth once it diverges from the last load or save.
    fn adopt_external(&mut self, path: PathBuf, loaded: storage::Loaded, cx: &mut Context<Self>) {
        if loaded.base == self.persistence.base {
            if self.dirty { self.flush(cx); }
            return;
        }
        storage::discard_draft(&path);
        self.persistence.timer = None;
        if self.buffer_matches_disk(&loaded) {
            self.persistence.base = loaded.base;
            self.persistence.gate.lock().unwrap().saved.insert(path, loaded.base);
            self.dirty = false;
            self.persistence.revision = self.persistence.revision.wrapping_add(1);
            self.status.clear();
            cx.notify();
            return;
        }
        let place = self.capture_place();
        self.show_loaded(path, loaded, cx);
        self.restore_place(place);
        self.dirty = false;
        self.persistence.timer = None;
        self.persistence.revision = self.persistence.revision.wrapping_add(1);
        self.status.clear();
        cx.notify();
    }
    fn buffer_matches_disk(&self, loaded: &storage::Loaded) -> bool {
        use crate::editor::surface::Surface;
        use crate::files::classify::OpenKind;
        match &loaded.kind {
            OpenKind::Markdown(text) => matches!(self.surface, Surface::Note) && crate::editor::markdown::serialize(&self.doc) == *text,
            OpenKind::Source(file) => matches!(self.surface, Surface::Source(_))
                && self.raw_save().is_some_and(|(text, bom)| text == file.text && bom == file.bom),
            OpenKind::Preview { svg: Some(file) } => matches!(self.surface, Surface::Preview { source: Some(_), .. })
                && self.raw_save().is_some_and(|(text, bom)| text == file.text && bom == file.bom),
            _ => false,
        }
    }
    fn capture_place(&self) -> DiskPlace {
        use crate::editor::surface::Surface;
        if let Surface::Source(doc) | Surface::Preview { source: Some(doc), .. } = &self.surface {
            let top = doc.list.logical_scroll_top();
            return DiskPlace::Source { anchor: doc.anchor, head: doc.head, item_ix: top.item_ix, offset: top.offset_in_item };
        }
        if matches!(self.surface, Surface::Note) {
            let top = self.list.logical_scroll_top();
            return DiskPlace::Note {
                anchor_ix: self.doc.index_of(self.caret.anchor.block).unwrap_or(0),
                anchor_off: self.caret.anchor.offset,
                head_ix: self.doc.index_of(self.caret.head.block).unwrap_or(0),
                head_off: self.caret.head.offset,
                item_ix: top.item_ix,
                offset: top.offset_in_item,
            };
        }
        DiskPlace::None
    }
    fn restore_place(&mut self, place: DiskPlace) {
        use crate::editor::surface::Surface;
        match place {
            DiskPlace::None => {}
            DiskPlace::Note { anchor_ix, anchor_off, head_ix, head_off, item_ix, offset } => {
                if !matches!(self.surface, Surface::Note) || self.doc.blocks.is_empty() { return; }
                let n = self.doc.blocks.len();
                let at = |ix: usize, off: usize| {
                    let block = &self.doc.blocks[ix.min(n - 1)];
                    Position { block: block.id, offset: super::selection::boundary(&block.text, off) }
                };
                self.caret.anchor = at(anchor_ix, anchor_off);
                self.caret.head = at(head_ix, head_off);
                self.caret.composing = None;
                self.reveal_caret = false;
                self.link_ui.quiet = Some(self.caret.head);
                let item_ix = item_ix.min(n - 1);
                self.list.scroll_to(ListOffset { item_ix, offset_in_item: offset });
                self.scroll_top = (item_ix, offset);
            }
            DiskPlace::Source { anchor, head, item_ix, offset } => {
                let Some(doc) = (match &mut self.surface {
                    Surface::Source(doc) => Some(doc),
                    Surface::Preview { source: Some(doc), .. } => Some(doc),
                    _ => None,
                }) else { return; };
                doc.anchor = doc.text.snap(anchor);
                doc.head = doc.text.snap(head);
                let lines = doc.text.line_count().max(1);
                let item_ix = item_ix.min(lines - 1);
                doc.list.scroll_to(ListOffset { item_ix, offset_in_item: offset });
            }
        }
    }
    /// Final recovery checkpoint under the write lock. GPUI quit futures only have a 100ms budget.
    pub fn prepare_to_quit(&mut self) -> bool {
        let active_doc = self.current_save_doc();
        let mut gate = self.persistence.gate.lock().unwrap();
        gate.epoch += 1;
        self.persistence.write_epoch = gate.epoch;
        self.persistence.timer = None;
        self.persistence.saving.clear();
        self.persistence.loading = false;
        self.persistence.pending = None;
        self.persistence.generation += 1;
        for (path, sess) in &mut self.sessions {
            if !sess.dirty { continue; }
            let base = gate.saved.get(path).copied().unwrap_or(sess.base);
            sess.base = base;
            let Some(doc) = super::session::session_doc(sess) else { continue; };
            if let Err(error) = storage::save_draft(path, &doc, base) { self.status = error; return false; }
        }
        if !self.dirty { return true; }
        let Some(path) = self.path.as_ref() else { return true; };
        let base = gate.saved.get(path).copied().unwrap_or(self.persistence.base);
        self.persistence.base = base;
        let Some(doc) = active_doc else { return true; };
        match storage::save_draft(path, &doc, base) {
            Ok(()) => true,
            Err(error) => { self.status = error; false }
        }
    }
    pub fn keep_draft_and_close(&mut self, cx: &mut Context<Self>) -> bool {
        self.finish_source_composition(cx);
        self.commit_composition(cx);
        self.persistence.timer = None;
        if let Some(path) = self.path.clone() {
            self.persistence.saving.remove(&path);
            if self.dirty {
                let doc = self.current_save_doc();
                let base = self.persistence.gate.lock().unwrap().saved.get(&path).copied().unwrap_or(self.persistence.base);
                self.persistence.base = base;
                let Some(doc) = doc else {
                    self.sessions.remove(&path);
                    self.surface = crate::editor::surface::Surface::Note;
                    self.reset_document(Doc::empty());
                    self.path = None; self.dirty = false;
                    cx.notify(); return true;
                };
                if let Err(error) = storage::save_draft(&path, &doc, base) {
                    self.status = error;
                    cx.notify();
                    return false;
                }
            }
            self.sessions.remove(&path);
        }
        self.reset_document(Doc::empty());
        self.path = None; self.dirty = false;
        self.status = tr("草稿已保留，可通过「恢复最近草稿」重新打开", "Draft kept; reopen via 'Recover Latest Draft'").into();
        cx.notify(); true
    }
}
