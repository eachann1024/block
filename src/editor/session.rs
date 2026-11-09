//! In-memory tab sessions for zero-reload tab switching and per-tab state retention.
use std::{collections::HashSet, path::{Path, PathBuf}};
use gpui::{Context, ListAlignment, ListState, Pixels, px};
use super::{Doc, Editor, HistoryEntry, lifecycle::Pending, selection::Caret, storage::{self, Fingerprint, SaveDoc}};
use crate::editor::surface::Surface;

pub(in crate::editor) struct TabSession {
    pub doc: Doc,
    pub presentation: crate::editor::presentation::State,
    pub caret: Caret,
    pub list: ListState,
    pub history: Vec<HistoryEntry>,
    pub redo: Vec<HistoryEntry>,
    pub nowrap: HashSet<u64>,
    pub folded: HashSet<u64>,
    pub fold_tails: Vec<(u64, u64)>,
    pub preferred_x: Option<Pixels>,
    pub dirty: bool,
    pub status: String,
    pub revision: u64,
    pub base: Fingerprint,
    pub surface: Surface,
}

pub(super) fn session_doc(sess: &TabSession) -> Option<SaveDoc> {
    match &sess.surface {
        Surface::Note => Some(SaveDoc::Note(sess.doc.clone())),
        Surface::Source(doc) | Surface::Preview { source: Some(doc), .. } => {
            Some(SaveDoc::Raw { text: doc.text.body().to_owned(), bom: doc.bom })
        }
        _ => None,
    }
}

impl Editor {
    pub(crate) fn has_parked_session(&self, path: &Path) -> bool { self.sessions.contains_key(path) }
    pub(crate) fn clear_transient_overlays(&mut self) {
        self.outline.clear_navigation();
        crate::editor::block_shortcuts::clear_first_line_delete();
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

    pub(in crate::editor) fn refresh_find_matches(&mut self, cx: &Context<Self>) {
        let Some(find) = &self.find else { return };
        let query = find.input.read(cx).value().to_string();
        let mut matches = Vec::new();
        if let Some(doc) = self.source_doc() {
            for range in crate::editor::find::find_all(doc.text.body(), &query) { matches.push((0, range)); }
        } else {
            for block in &self.doc.blocks {
                for range in crate::editor::find::find_all(&block.text, &query) { matches.push((block.id, range)); }
            }
        }
        let at = if let Some(doc) = self.source_doc() { (0usize, doc.head) } else {
            let caret = self.caret.head;
            (self.doc.index_of(caret.block).unwrap_or(0), caret.offset)
        };
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
        let presentation = crate::editor::presentation::take(cx.entity_id(), cx);
        self.clear_transient_overlays();
        let doc = std::mem::replace(&mut self.doc, Doc::empty());
        let caret = std::mem::replace(&mut self.caret, Caret::new(self.doc.blocks[0].id));
        let list = std::mem::replace(&mut self.list, ListState::new(1, ListAlignment::Top, px(120.)));
        let history = std::mem::take(&mut self.history);
        let redo = std::mem::take(&mut self.redo);
        let nowrap = std::mem::take(&mut self.nowrap);
        let folded = std::mem::take(&mut self.folded);
        let fold_tails = std::mem::take(&mut self.fold_tails);
        let preferred_x = self.preferred_x.take();
        let dirty = std::mem::replace(&mut self.dirty, false);
        let status = std::mem::take(&mut self.status);
        let revision = self.persistence.revision;
        let base = self.persistence.base;
        let surface = std::mem::replace(&mut self.surface, Surface::Note);
        self.source_layouts.clear();
        self.source_drag = false;
        self.sessions.insert(path.clone(), TabSession {
            doc, presentation, caret, list, history, redo, nowrap, folded, fold_tails, preferred_x, dirty, status, revision, base, surface,
        });
        if dirty {
            self.spawn_save(path.clone(), cx);
        }
        Some(path)
    }

    pub(in crate::editor) fn restore_session(&mut self, path: PathBuf, session: TabSession, cx: &mut Context<Self>) {
        self.clear_transient_overlays();
        crate::editor::presentation::restore(cx.entity_id(), session.presentation, cx);
        self.doc = session.doc;
        self.caret = session.caret;
        self.list = session.list;
        self.history = session.history;
        self.redo = session.redo;
        self.nowrap = session.nowrap;
        self.folded = session.folded;
        self.fold_tails = session.fold_tails;
        self.preferred_x = session.preferred_x;
        self.dirty = session.dirty;
        self.status = session.status;
        self.surface = session.surface;
        self.source_layouts.clear();
        self.source_drag = false;
        self.persistence.revision = session.revision;
        self.persistence.base = session.base;
        self.persistence.timer = None;
        self.path = Some(path);
        self.link_ui.quiet = Some(self.caret.head);
        self.refresh_find_matches(cx);
        self.apply_search_jump(cx);
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
            if sess.dirty && crate::files::ops::exists(path) && !self.persistence.saving.contains(path) {
                if let Some(doc) = session_doc(&sess) { self.spawn_detached_save(path.to_path_buf(), doc, sess.base, cx); }
            }
        }
        if self.path.as_deref() == Some(path) {
            self.finish_source_composition(cx);
            self.commit_composition(cx);
            self.persistence.timer = None;
            if self.dirty && crate::files::ops::exists(path) && !self.persistence.saving.contains(path) {
                if let Some(doc) = self.current_save_doc() {
                    let base = self.persistence.base;
                    self.spawn_detached_save(path.to_path_buf(), doc, base, cx);
                }
            }
            self.surface = Surface::Note;
            self.source_layouts.clear();
            self.reset_document(Doc::empty(), cx);
            self.path = None;
            self.dirty = false;
            self.status.clear();
            cx.notify();
        }
    }

    pub(in crate::editor) fn parked_save_payload(&self, path: &Path) -> Option<(SaveDoc, Fingerprint, u64)> {
        let sess = self.sessions.get(path)?;
        if !sess.dirty { return None; }
        Some((session_doc(sess)?, sess.base, sess.revision))
    }

    fn spawn_detached_save(&mut self, path: PathBuf, doc: SaveDoc, base: Fingerprint, cx: &mut Context<Self>) {
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
