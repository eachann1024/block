use std::{collections::{HashMap, HashSet}, path::{Path, PathBuf}, sync::{Arc, Mutex}, time::Duration};
use gpui::{Context, Task};
use crate::theme::tr;
use super::{Editor, Doc, storage::{self, Fingerprint}};

#[derive(Clone)]
pub(super) enum Pending { Open(PathBuf), Close, ReleaseUnder }
#[derive(Default)]
pub(super) struct DiskGate { pub epoch: u64, pub saved: HashMap<PathBuf, Fingerprint> }
pub(super) struct Persistence {
    pub revision: u64,
    pub generation: u64,
    pub base: Fingerprint,
    pub pending: Option<Pending>,
    pub saving: HashSet<PathBuf>,
    pub loading: bool,
    pub timer: Option<Task<()>>,
    pub gate: Arc<Mutex<DiskGate>>,
    pub write_epoch: u64,
}
impl Default for Persistence {
    fn default() -> Self {
        Self { revision: 0, generation: 0, base: Fingerprint(0), pending: None,
            saving: HashSet::new(), loading: false, timer: None, gate: Arc::new(Mutex::new(DiskGate::default())), write_epoch: 0 }
    }
}
impl Editor {
    pub fn current_path(&self) -> Option<&Path> { self.path.as_deref() }
    pub fn is_loading(&self) -> bool { self.persistence.loading }
    pub fn interaction_epoch(&self) -> u64 { self.persistence.generation }
    pub fn recover_latest_draft(&mut self, cx: &mut Context<Self>) {
        self.persistence.generation += 1;
        self.persistence.loading = false;
        self.persistence.pending = None;
        if self.path.is_some() || !self.persistence.saving.is_empty() { cx.notify(); return; }
        let generation = self.persistence.generation;
        self.persistence.loading = true;
        self.report_status(tr("正在读取恢复草稿…", "Recovering draft…").into(), cx);
        let task = cx.background_executor().spawn(async { storage::latest_draft() });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                if this.persistence.generation != generation { return; }
                this.persistence.loading = false;
                match result {
                    Ok((path, loaded)) => {
                        this.persistence.base = loaded.base;
                        this.persistence.gate.lock().unwrap().saved.insert(path.clone(), loaded.base);
                        this.reset_document(loaded.doc);
                        this.folded = crate::editor::fold::restore_folds(&path, &this.doc);
                        this.path = Some(path); this.dirty = true;
                        this.status = tr("已恢复草稿。原文件若已删除或被外部修改仍受保护，可复制或导出备份", "Draft recovered. Copy or export a backup if the original file changed").into();
                        this.refresh_find_matches(cx);
                    }
                    Err(error) => this.status = error,
                }
                cx.notify();
            });
        }).detach();
    }
    pub fn is_busy(&self) -> bool { !self.persistence.saving.is_empty() || self.persistence.loading || self.persistence.pending.is_some() }
    pub fn report_status(&mut self, message: String, cx: &mut Context<Self>) {
        self.status = message; cx.notify();
    }
    pub fn open(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        crate::log::set_context(format!("note={}", path.display()));
        if self.path.as_ref() == Some(&path) && self.persistence.pending.is_none() {
            if self.status == "正在打开…" || self.status == "Opening…" {
                self.status = if self.persistence.saving.contains(&path) { tr("正在保存…", "Saving…").into() }
                    else if self.dirty { tr("未保存更改", "Unsaved changes").into() }
                    else { tr("已保存 · 内存会话就绪", "Saved · Ready").into() };
            }
            cx.notify(); return;
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
        self.persistence.loading = true;
        self.status = tr("正在打开…", "Opening…").into(); cx.notify();
        let request_path = path.clone();
        let task = cx.background_executor().spawn(async move { storage::load(&request_path) });
        cx.spawn(async move |this, cx| {
            let loaded = task.await;
            let _ = this.update(cx, |this, cx| {
                if this.persistence.generation != generation { return; }
                this.persistence.loading = false;
                match loaded {
                    Ok(loaded) => {
                        this.persistence.base = loaded.base;
                        this.persistence.gate.lock().unwrap().saved.insert(path.clone(), loaded.base);
                        this.reset_document(loaded.doc);
                        this.folded = crate::editor::fold::restore_folds(&path, &this.doc);
                        this.path = Some(path);
                        this.dirty = loaded.recovered;
                        this.status = if loaded.recovered { tr("已恢复未保存草稿，请确认后保存", "Recovered unsaved draft; please review and save").into() } else { tr("已保存 · 内存会话就绪", "Saved · Ready").into() };
                        this.refresh_find_matches(cx);
                    }
                    Err(error) => {
                        crate::log::error(&format!("failed to load note {}: {error}", path.display()));
                        if let Some(prev) = prev {
                            if let Some(sess) = this.sessions.remove(&prev) {
                                this.restore_session(prev, sess, cx);
                            }
                        }
                        this.status = error;
                    }
                }
                cx.notify();
            });
        }).detach();
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
                if this.persistence.revision == revision && this.caret.composing.is_none() { this.flush(cx); }
            });
        }));
        cx.notify();
    }
    pub fn save(&mut self, cx: &mut Context<Self>) {
        self.commit_composition(cx); self.flush(cx);
    }
    fn flush(&mut self, cx: &mut Context<Self>) {
        if !self.dirty { return; }
        let Some(path) = self.path.clone() else { return; };
        self.spawn_save(path, cx);
    }
    pub(super) fn spawn_save(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        if self.persistence.saving.contains(&path) { return; }
        let (doc, base, revision) = if self.path.as_ref() == Some(&path) {
            if !self.dirty { return; }
            self.status = tr("正在保存…", "Saving…").into();
            (self.doc.clone(), self.persistence.base, self.persistence.revision)
        } else if let Some(sess) = self.sessions.get_mut(&path) {
            if !sess.dirty { return; }
            sess.status = tr("正在保存…", "Saving…").into();
            (sess.doc.clone(), sess.base, sess.revision)
        } else { return; };
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
                if this.persistence.write_epoch != epoch { return; }
                this.persistence.saving.remove(&path);
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
                            if let Some(mut sess) = this.sessions.remove(&path) {
                                sess.status = error;
                                this.restore_session(path, sess, cx);
                            }
                        } else if let Some(sess) = this.sessions.get_mut(&path) { sess.status = error; }
                    }
                }
                cx.notify();
            });
        }).detach();
    }
    /// Synchronous final recovery checkpoint: GPUI quit futures have a 100ms budget.
    /// Invalidates queued workers under the same lock used for writes and draft deletion.
    pub fn prepare_to_quit(&mut self) -> bool {
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
            if let Err(error) = storage::save_draft(path, &sess.doc, base) {
                self.status = error;
                return false;
            }
        }
        if !self.dirty { return true; }
        let Some(path) = self.path.as_ref() else { return true; };
        let base = gate.saved.get(path).copied().unwrap_or(self.persistence.base);
        self.persistence.base = base;
        match storage::save_draft(path, &self.doc, base) {
            Ok(()) => true,
            Err(error) => { self.status = error; false }
        }
    }
    pub fn keep_draft_and_close(&mut self, cx: &mut Context<Self>) -> bool {
        self.commit_composition(cx);
        self.persistence.timer = None;
        if let Some(path) = self.path.clone() {
            self.persistence.saving.remove(&path);
            if self.dirty {
                let base = self.persistence.gate.lock().unwrap().saved.get(&path).copied().unwrap_or(self.persistence.base);
                self.persistence.base = base;
                if let Err(error) = storage::save_draft(&path, &self.doc, base) {
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
