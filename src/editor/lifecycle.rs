use std::{path::{Path, PathBuf}, sync::{Arc, Mutex}, time::Duration};
use gpui::{Context, Task};
use super::{Editor, Doc, storage::{self, Fingerprint}};

#[derive(Clone)]
pub(super) enum Pending { Open(PathBuf), Close }
#[derive(Default)]
pub(super) struct DiskGate { epoch: u64, saved: Option<(PathBuf, Fingerprint)> }
pub(super) struct Persistence {
    pub revision: u64,
    pub generation: u64,
    pub base: Fingerprint,
    pub pending: Option<Pending>,
    pub saving: bool,
    pub loading: bool,
    pub timer: Option<Task<()>>,
    pub gate: Arc<Mutex<DiskGate>>,
    pub write_epoch: u64,
}
impl Default for Persistence {
    fn default() -> Self {
        Self { revision: 0, generation: 0, base: Fingerprint(0), pending: None,
            saving: false, loading: false, timer: None, gate: Arc::new(Mutex::new(DiskGate::default())), write_epoch: 0 }
    }
}
impl Editor {
    pub fn current_path(&self) -> Option<&Path> { self.path.as_deref() }
    pub fn interaction_epoch(&self) -> u64 { self.persistence.generation }
    pub fn recover_latest_draft(&mut self, cx: &mut Context<Self>) {
        self.persistence.generation += 1;
        self.persistence.loading = false;
        self.persistence.pending = None;
        if self.path.is_some() || self.persistence.saving { cx.notify(); return; }
        let generation = self.persistence.generation;
        self.persistence.loading = true;
        self.report_status("Reading recovery drafts…".into(), cx);
        let task = cx.background_executor().spawn(async { storage::latest_draft() });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                if this.persistence.generation != generation { return; }
                this.persistence.loading = false;
                match result {
                    Ok((path, loaded)) => {
                        this.persistence.base = loaded.base;
                        this.persistence.gate.lock().unwrap().saved = Some((path.clone(), loaded.base));
                        this.reset_document(loaded.doc); this.path = Some(path); this.dirty = true;
                        this.status = "Recovered draft. Deleted or externally changed originals remain protected; copy or export to preserve elsewhere.".into();
                    }
                    Err(error) => this.status = error,
                }
                cx.notify();
            });
        }).detach();
    }
    pub fn is_busy(&self) -> bool { self.persistence.saving || self.persistence.loading || self.persistence.pending.is_some() }
    pub fn report_status(&mut self, message: String, cx: &mut Context<Self>) {
        self.status = message; cx.notify();
    }
    pub fn open(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        self.persistence.generation += 1;
        self.persistence.loading = false;
        if self.path.as_ref() == Some(&path) {
            self.persistence.pending = None;
            if self.status == "Opening document…" {
                self.status = if self.persistence.saving { "Saving…".into() }
                    else if self.dirty { "Unsaved changes".into() }
                    else { String::new() };
            }
            cx.notify(); return;
        }
        self.commit_composition(cx);
        self.persistence.pending = Some(Pending::Open(path));
        self.advance(cx);
    }
    pub fn close(&mut self, cx: &mut Context<Self>) -> bool {
        self.persistence.generation += 1;
        self.persistence.loading = false;
        self.commit_composition(cx);
        self.persistence.pending = Some(Pending::Close);
        self.advance(cx);
        self.path.is_none()
    }
    fn advance(&mut self, cx: &mut Context<Self>) {
        if self.persistence.saving { return; }
        if self.dirty { self.flush(cx); return; }
        let Some(pending) = self.persistence.pending.take() else { return; };
        self.persistence.generation += 1;
        let generation = self.persistence.generation;
        self.persistence.timer = None;
        match pending {
            Pending::Close => {
                self.persistence.loading = false;
                self.reset_document(Doc::empty()); self.path = None;
                self.status.clear(); cx.notify();
            }
            Pending::Open(path) => {
                self.persistence.loading = true;
                self.status = "Opening document…".into(); cx.notify();
                let request_path = path.clone();
                let task = cx.background_executor().spawn(async move { storage::load(&request_path) });
                cx.spawn(async move |this, cx| {
                    let loaded = task.await;
                    let _ = this.update(cx, |this, cx| {
                        if this.persistence.generation != generation { return; }
                        this.persistence.loading = false;
                        match loaded {
                            Ok(loaded) if !this.dirty => {
                                this.persistence.base = loaded.base;
                                this.persistence.gate.lock().unwrap().saved = Some((path.clone(), loaded.base));
                                this.reset_document(loaded.doc); this.path = Some(path);
                                this.dirty = loaded.recovered;
                                this.status = if loaded.recovered { "Recovered unsaved draft. Review before saving.".into() } else { String::new() };
                            }
                            Ok(_) => { this.persistence.pending = Some(Pending::Open(path)); this.advance(cx); }
                            Err(error) => this.status = error,
                        }
                        cx.notify();
                    });
                }).detach();
            }
        }
    }
    pub(in crate::editor) fn touch(&mut self, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.dirty = true;
        self.persistence.revision += 1;
        self.status = "Unsaved changes".into();
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
        if !self.dirty || self.persistence.saving { return; }
        let Some(path) = self.path.clone() else { return; };
        self.persistence.saving = true;
        let revision = self.persistence.revision;
        let doc = self.doc.clone();
        let base = self.persistence.base;
        let gate = self.persistence.gate.clone();
        let epoch = self.persistence.write_epoch;
        self.status = "Saving…".into(); cx.notify();
        let task = cx.background_executor().spawn(async move {
            let mut state = gate.lock().unwrap();
            if state.epoch != epoch { return Ok(None); }
            let result = storage::save(&path, &doc, base);
            if let Ok(hash) = result { state.saved = Some((path, hash)); }
            result.map(Some)
        });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                if this.persistence.write_epoch != epoch { return; }
                this.persistence.saving = false;
                match result {
                    Ok(Some(base)) => {
                        this.persistence.base = base;
                        if this.persistence.revision == revision {
                            this.dirty = false; this.status = "Saved".into();
                        }
                        if this.persistence.pending.is_some() { this.advance(cx); }
                        else if this.dirty { this.touch(cx); }
                    }
                    Ok(None) => {}
                    Err(error) => {
                        this.status = error; this.persistence.pending = None;
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
        self.persistence.saving = false;
        self.persistence.loading = false;
        self.persistence.pending = None;
        self.persistence.generation += 1;
        if !self.dirty { return true; }
        let Some(path) = self.path.as_ref() else { return true; };
        let base = gate.saved.as_ref().filter(|(p, _)| p == path).map(|(_, b)| *b).unwrap_or(self.persistence.base);
        self.persistence.base = base;
        match storage::save_draft(path, &self.doc, base) {
            Ok(()) => true,
            Err(error) => { self.status = error; false }
        }
    }
    pub fn keep_draft_and_close(&mut self, cx: &mut Context<Self>) -> bool {
        self.commit_composition(cx);
        if !self.prepare_to_quit() { cx.notify(); return false; }
        self.reset_document(Doc::empty());
        self.path = None; self.dirty = false;
        self.status = "Draft retained. Use Recover latest draft to reopen it.".into();
        cx.notify(); true
    }
}
