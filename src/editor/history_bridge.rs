//! Editor bridge for Page History: quiesce autosave, hold DiskGate, apply one document.
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, MutexGuard};
use gpui::Context;
use crate::editor::markdown;
use super::lifecycle::DiskGate;
use super::storage::{self, Fingerprint};
use super::Editor;

#[derive(Clone)]
pub struct HistoryCapture {
    pub path: PathBuf,
    pub generation: u64,
}

pub enum HistoryCapturePoll {
    Pending,
    Ready { content: String, base: u64 },
    Failed(String),
    Superseded,
}

pub struct HistoryWrite {
    pub path: PathBuf,
    pub content: String,
    pub base: u64,
    pub epoch: u64,
    pub generation: u64,
}

pub enum HistoryCommit { Applied, Idle, Conflict }

pub struct HistoryDiskPermit {
    gate: Arc<Mutex<DiskGate>>,
    path: PathBuf,
    epoch: u64,
    expected_base: u64,
    generation: u64,
}

fn gate_lock(gate: &Mutex<DiskGate>) -> Result<MutexGuard<'_, DiskGate>, String> {
    gate.lock().map_err(|_| "LOCK".into())
}

pub fn fingerprint_of(bytes: &[u8]) -> u64 { storage::fingerprint(bytes).0 }

pub fn canonical_markdown(content: &str) -> String {
    markdown::serialize(&markdown::parse(content))
}

fn same_document(left: &str, right: &str) -> bool {
    left == right || canonical_markdown(left) == canonical_markdown(right)
}

impl HistoryDiskPermit {
    pub fn restore_version(self, version_id: &str) -> Result<HistoryWrite, String> {
        let path = self.path.clone();
        let expected = self.expected_base;
        self.finish(move || crate::page_history::restore_version_to_file(&path, version_id, expected))
    }

    pub fn restore_content(self, content: &str) -> Result<HistoryWrite, String> {
        let path = self.path.clone();
        let expected = self.expected_base;
        let content = content.to_string();
        self.finish(move || crate::page_history::restore_content_to_file(&path, &content, expected))
    }

    fn finish(self, op: impl FnOnce() -> Result<(String, u64), String>) -> Result<HistoryWrite, String> {
        let mut gate = gate_lock(&self.gate)?;
        if gate.epoch != self.epoch { return Err("SUPERSEDED".into()); }
        let saved = gate.saved.get(&self.path).map(|fp| fp.0).unwrap_or(self.expected_base);
        if saved != self.expected_base { return Err("EXTERNAL_CONFLICT".into()); }
        let (content, base) = op()?;
        gate.saved.insert(self.path.clone(), Fingerprint(base));
        gate.epoch = gate.epoch.wrapping_add(1);
        let epoch = gate.epoch;
        Ok(HistoryWrite { path: self.path, content, base, epoch, generation: self.generation })
    }
}

impl Editor {
    pub fn begin_history_capture(&mut self, cx: &mut Context<Self>) -> Option<HistoryCapture> {
        let path = self.path.clone()?;
        if self.persistence.loading || self.persistence.pending.is_some() { return None; }
        self.commit_composition(cx);
        self.persistence.timer = None;
        if self.dirty { self.save(cx); }
        Some(HistoryCapture { path, generation: self.persistence.generation })
    }

    /// Drop a finished debounce handle and flush the active note without touching other pages.
    pub fn pump_history_save(&mut self, cx: &mut Context<Self>) {
        self.persistence.timer = None;
        if self.dirty { self.save(cx); }
    }

    pub fn poll_history_capture(&self, cap: &HistoryCapture) -> HistoryCapturePoll {
        if self.persistence.generation != cap.generation || self.path.as_deref() != Some(cap.path.as_path()) {
            return HistoryCapturePoll::Superseded;
        }
        if self.persistence.loading || !self.persistence.saving.is_empty() {
            return HistoryCapturePoll::Pending;
        }
        if self.dirty {
            if history_status_waiting(&self.status) { return HistoryCapturePoll::Pending; }
            let message = if self.status.is_empty() {
                crate::theme::tr("保存失败，无法打开历史", "Save failed; history was not opened").into()
            } else {
                self.status.clone()
            };
            return HistoryCapturePoll::Failed(message);
        }
        HistoryCapturePoll::Ready { content: markdown::serialize(&self.doc), base: self.persistence.base.0 }
    }

    pub fn prepare_history_disk(&mut self, path: &Path, generation: u64) -> Result<HistoryDiskPermit, String> {
        if self.persistence.generation != generation || self.path.as_deref() != Some(path) {
            return Err("SUPERSEDED".into());
        }
        if self.dirty || !self.persistence.saving.is_empty() || self.persistence.loading {
            return Err(crate::theme::tr("正在保存…", "Saving…").into());
        }
        self.persistence.timer = None;
        let gate = self.persistence.gate.clone();
        let epoch = {
            let mut locked = gate_lock(&gate)?;
            let saved = locked.saved.get(path).copied().unwrap_or(self.persistence.base);
            if saved != self.persistence.base { return Err("EXTERNAL_CONFLICT".into()); }
            locked.epoch = locked.epoch.wrapping_add(1);
            let epoch = locked.epoch;
            self.persistence.write_epoch = epoch;
            epoch
        };
        Ok(HistoryDiskPermit { gate, path: path.to_path_buf(), epoch, expected_base: self.persistence.base.0, generation })
    }

    pub fn commit_history_document(&mut self, write: &HistoryWrite, cx: &mut Context<Self>) -> Result<HistoryCommit, String> {
        self.persistence.timer = None;
        if let Ok(gate) = gate_lock(&self.persistence.gate) {
            let epoch = gate.epoch;
            drop(gate);
            if epoch > self.persistence.write_epoch { self.persistence.write_epoch = epoch; }
        }
        let base = Fingerprint(write.base);
        let active = self.path.as_deref() == Some(write.path.as_path());
        if !active && !self.sessions.contains_key(&write.path) { return Ok(HistoryCommit::Idle); }
        let dirty = if active { self.dirty } else { self.sessions.get(&write.path).is_some_and(|session| session.dirty) };
        let generation_ok = !active || self.persistence.generation == write.generation;
        if dirty || !generation_ok {
            let message = crate::theme::tr(
                "恢复已写入磁盘，但当前编辑未能应用，请重新打开该页。",
                "Restore was written, but this editor could not apply it. Reopen the page.",
            );
            if active { self.status = message.into(); cx.notify(); }
            else if let Some(session) = self.sessions.get_mut(&write.path) { session.status = message.into(); }
            return Ok(HistoryCommit::Conflict);
        }
        if active {
            self.apply_clean_doc(&write.content, base, cx);
            return Ok(HistoryCommit::Applied);
        }
        let Some(session) = self.sessions.get_mut(&write.path) else { return Ok(HistoryCommit::Idle); };
        session.doc = markdown::parse(&write.content);
        session.base = base;
        session.dirty = false;
        session.history.clear();
        session.redo.clear();
        session.revision = session.revision.wrapping_add(1);
        session.status.clear();
        Ok(HistoryCommit::Applied)
    }

    pub fn adopt_saved_baseline(&mut self, path: &Path, content: &str, generation: u64, cx: &mut Context<Self>) -> Result<u64, String> {
        if self.is_path_dirty(path) || self.persistence.saving.contains(path) { return Err("DIRTY".into()); }
        let base = storage::fingerprint(content.as_bytes());
        if self.path.as_deref() == Some(path) {
            if self.persistence.generation != generation { return Err("SUPERSEDED".into()); }
            self.persistence.timer = None;
            if !same_document(&markdown::serialize(&self.doc), content) { self.apply_clean_doc(content, base, cx); }
            else { let _ = self.mark_clean_base(path, base); cx.notify(); }
            return Ok(base.0);
        }
        let Some(session) = self.sessions.get_mut(path) else { return Err("SUPERSEDED".into()); };
        if !same_document(&markdown::serialize(&session.doc), content) { session.doc = markdown::parse(content); }
        session.base = base;
        session.dirty = false;
        session.history.clear();
        session.redo.clear();
        session.revision = session.revision.wrapping_add(1);
        session.status.clear();
        gate_lock(&self.persistence.gate)?.saved.insert(path.to_path_buf(), base);
        Ok(base.0)
    }

    pub fn history_saved_base(&self, path: &Path) -> Option<u64> {
        if self.path.as_deref() == Some(path) { Some(self.persistence.base.0) }
        else { self.sessions.get(path).map(|session| session.base.0) }
    }

    pub fn history_anchor_matches(&self, path: &Path, content: &str, base: u64) -> bool {
        if self.is_path_dirty(path) || self.persistence.saving.contains(path) { return false; }
        if self.path.as_deref() == Some(path) {
            return self.persistence.base.0 == base && same_document(&markdown::serialize(&self.doc), content);
        }
        self.sessions.get(path).is_some_and(|session| {
            !session.dirty && session.base.0 == base && same_document(&markdown::serialize(&session.doc), content)
        })
    }

    fn apply_clean_doc(&mut self, content: &str, base: Fingerprint, cx: &mut Context<Self>) {
        let path = self.path.clone();
        self.clear_transient_overlays();
        self.reset_document(markdown::parse(content));
        self.persistence.base = base;
        self.persistence.revision = self.persistence.revision.wrapping_add(1);
        self.dirty = false;
        self.status.clear();
        if let Some(path) = path { let _ = self.mark_clean_base(&path, base); }
        self.refresh_find_matches(cx);
        cx.notify();
    }

    fn mark_clean_base(&mut self, path: &Path, base: Fingerprint) -> Result<(), String> {
        self.persistence.base = base;
        self.dirty = false;
        self.status.clear();
        gate_lock(&self.persistence.gate)?.saved.insert(path.to_path_buf(), base);
        Ok(())
    }
}

fn history_status_waiting(status: &str) -> bool {
    matches!(status, "" | "未保存更改" | "Unsaved changes" | "正在保存…" | "Saving…" | "已保存" | "Saved")
}
