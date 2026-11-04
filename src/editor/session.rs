//! In-memory tab sessions for zero-reload tab switching and per-tab state retention.
use std::{collections::HashSet, path::{Path, PathBuf}};
use gpui::{Context, ListAlignment, ListState, Pixels, px};
use super::{Doc, Editor, HistoryEntry, lifecycle::Pending, selection::Caret, storage::{self, Fingerprint}};

pub(super) struct TabSession {
    pub doc: Doc,
    pub caret: Caret,
    pub list: ListState,
    pub history: Vec<HistoryEntry>,
    pub redo: Vec<HistoryEntry>,
    pub nowrap: HashSet<u64>,
    pub folded: HashSet<u64>,
    pub preferred_x: Option<Pixels>,
    pub dirty: bool,
    pub status: String,
    pub revision: u64,
    pub base: Fingerprint,
}

impl Editor {
    pub(super) fn clear_transient_overlays(&mut self) {
        self.clear_pointer();
        self.toolbar_anchor = None;
        self.hover = None;
        self.block_menu = None;
        self.table_menu = None;
        self.table_sel = None;
        self.table_chrome = None;
        self.format = None;
        self.block_sel = None;
        self.layouts.clear();
        self.pending_vertical = None;
        self.composition_before = None;
    }

    pub(super) fn refresh_find_matches(&mut self, cx: &Context<Self>) {
        let Some(find) = &self.find else { return };
        let query = find.input.read(cx).value().to_string();
        let mut matches = Vec::new();
        for block in &self.doc.blocks {
            for range in crate::editor::find::find_all(&block.text, &query) {
                matches.push((block.id, range));
            }
        }
        let caret = self.caret.head;
        let at = (self.doc.index_of(caret.block).unwrap_or(0), caret.offset);
        let current = matches.iter().position(|(id, r)| (self.doc.index_of(*id).unwrap_or(0), r.start) >= at).unwrap_or(0);
        if let Some(find) = &mut self.find {
            find.matches = matches;
            find.current = current;
        }
    }

    pub(super) fn park_active(&mut self, cx: &mut Context<Self>) -> Option<PathBuf> {
        self.commit_composition(cx);
        self.persistence.timer = None;
        let path = self.path.take()?;
        self.clear_transient_overlays();
        let doc = std::mem::replace(&mut self.doc, Doc::empty());
        let caret = std::mem::replace(&mut self.caret, Caret::new(self.doc.blocks[0].id));
        let list = std::mem::replace(&mut self.list, ListState::new(1, ListAlignment::Top, px(120.)));
        let history = std::mem::take(&mut self.history);
        let redo = std::mem::take(&mut self.redo);
        let nowrap = std::mem::take(&mut self.nowrap);
        let folded = std::mem::take(&mut self.folded);
        let preferred_x = self.preferred_x.take();
        let dirty = std::mem::replace(&mut self.dirty, false);
        let status = std::mem::take(&mut self.status);
        let revision = self.persistence.revision;
        let base = self.persistence.base;
        self.sessions.insert(path.clone(), TabSession {
            doc, caret, list, history, redo, nowrap, folded, preferred_x, dirty, status, revision, base,
        });
        if dirty {
            self.spawn_save(path.clone(), cx);
        }
        Some(path)
    }

    pub(super) fn restore_session(&mut self, path: PathBuf, session: TabSession, cx: &mut Context<Self>) {
        self.clear_transient_overlays();
        self.doc = session.doc;
        self.caret = session.caret;
        self.list = session.list;
        self.history = session.history;
        self.redo = session.redo;
        self.nowrap = session.nowrap;
        self.folded = session.folded;
        self.preferred_x = session.preferred_x;
        self.dirty = session.dirty;
        self.status = session.status;
        self.persistence.revision = session.revision;
        self.persistence.base = session.base;
        self.persistence.timer = None;
        self.path = Some(path);
        self.link_ui.quiet = Some(self.caret.head);
        self.refresh_find_matches(cx);
        cx.notify();
    }

    pub fn is_path_dirty(&self, path: &Path) -> bool {
        if self.path.as_deref() == Some(path) { self.dirty }
        else { self.sessions.get(path).is_some_and(|s| s.dirty) }
    }

    pub fn has_open_session(&self, path: &Path) -> bool {
        self.path.as_deref() == Some(path) || self.sessions.contains_key(path)
    }

    pub fn close_session(&mut self, path: &Path, cx: &mut Context<Self>) {
        if let Some(sess) = self.sessions.remove(path) {
            if sess.dirty && path.exists() && !self.persistence.saving.contains(path) {
                self.spawn_detached_save(path.to_path_buf(), sess.doc, sess.base, cx);
            }
        }
        if self.path.as_deref() == Some(path) {
            self.commit_composition(cx);
            self.persistence.timer = None;
            if self.dirty && path.exists() && !self.persistence.saving.contains(path) {
                let doc = self.doc.clone();
                let base = self.persistence.base;
                self.spawn_detached_save(path.to_path_buf(), doc, base, cx);
            }
            self.reset_document(Doc::empty());
            self.path = None;
            self.dirty = false;
            self.status.clear();
            cx.notify();
        }
    }

    fn spawn_detached_save(&mut self, path: PathBuf, doc: Doc, base: Fingerprint, cx: &mut Context<Self>) {
        self.persistence.saving.insert(path.clone());
        let gate = self.persistence.gate.clone();
        let epoch = self.persistence.write_epoch;
        let save_path = path.clone();
        let task = cx.background_executor().spawn(async move {
            let mut state = gate.lock().unwrap();
            if state.epoch != epoch { return Ok(None); }
            let result = storage::save(&save_path, &doc, base);
            if let Ok(hash) = result {
                state.saved.insert(save_path, hash);
            }
            result.map(Some)
        });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                if this.persistence.write_epoch != epoch { return; }
                this.persistence.saving.remove(&path);
                if let Err(error) = result {
                    crate::log::error(&format!("failed to save closed session {}: {error}", path.display()));
                }
                if this.persistence.saving.is_empty() && this.persistence.pending.is_some() {
                    this.advance(cx);
                }
                cx.notify();
            });
        }).detach();
    }

    pub fn rename_session(&mut self, from: &Path, to: &Path, cx: &mut Context<Self>) {
        let keys: Vec<PathBuf> = self.sessions.keys().filter(|k| k.starts_with(from)).cloned().collect();
        for old in keys {
            if let (Some(new_key), Some(sess)) = (crate::files::ops::rebase(&old, from, to), self.sessions.remove(&old)) {
                crate::editor::fold::save_folds_for(&new_key, &sess.doc, &sess.folded);
                self.sessions.insert(new_key, sess);
            }
        }
        if let Ok(mut gate) = self.persistence.gate.lock() {
            let saved_keys: Vec<PathBuf> = gate.saved.keys().filter(|k| k.starts_with(from)).cloned().collect();
            for old in saved_keys {
                if let (Some(new_key), Some(fp)) = (crate::files::ops::rebase(&old, from, to), gate.saved.remove(&old)) {
                    gate.saved.insert(new_key, fp);
                }
            }
        }
        if let Some(cur) = self.path.clone() {
            if let Some(rebased) = crate::files::ops::rebase(&cur, from, to) {
                self.path = Some(rebased);
                self.persist_folds();
                cx.notify();
            }
        }
    }

    pub fn has_dirty_under(&self, prefix: &Path) -> bool {
        (self.path.as_ref().is_some_and(|p| p.starts_with(prefix)) && self.dirty)
            || self.sessions.iter().any(|(k, s)| k.starts_with(prefix) && s.dirty)
    }

    pub fn flush_under(&mut self, prefix: &Path, cx: &mut Context<Self>) -> bool {
        self.persistence.generation += 1;
        self.persistence.loading = false;
        let dirty_keys: Vec<PathBuf> = self.sessions.iter()
            .filter(|(k, s)| k.starts_with(prefix) && s.dirty)
            .map(|(k, _)| k.clone()).collect();
        for key in dirty_keys { self.spawn_save(key, cx); }
        if let Some(cur) = self.path.clone().filter(|p| p.starts_with(prefix)) {
            self.commit_composition(cx);
            self.persistence.timer = None;
            if self.dirty { self.spawn_save(cur, cx); }
        }
        if !self.persistence.saving.is_empty() {
            self.persistence.pending = Some(Pending::ReleaseUnder);
            cx.notify();
            return false;
        }
        true
    }

    pub fn release_under(&mut self, prefix: &Path, cx: &mut Context<Self>) -> bool {
        self.persistence.generation += 1;
        self.persistence.loading = false;
        let dirty_keys: Vec<PathBuf> = self.sessions.iter()
            .filter(|(k, s)| k.starts_with(prefix) && s.dirty)
            .map(|(k, _)| k.clone()).collect();
        for key in dirty_keys {
            self.spawn_save(key, cx);
        }
        if self.path.as_ref().is_some_and(|p| p.starts_with(prefix)) {
            self.park_active(cx);
        }
        if !self.persistence.saving.is_empty() {
            self.persistence.pending = Some(Pending::ReleaseUnder);
            cx.notify();
            return false;
        }
        cx.notify();
        !self.path.as_ref().is_some_and(|p| p.starts_with(prefix))
    }
}
