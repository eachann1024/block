//! Heading fold state, governed block ranges, and per-note persistence.
use std::{
    collections::{BTreeMap, HashSet},
    fs,
    path::{Path, PathBuf},
};
use gpui::Context;
use serde::{Deserialize, Serialize};
use super::{
    model::{Block, Doc, Kind},
    view::{Editor, selection::Position},
};

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub(super) struct FoldEntry {
    pub index: usize,
    pub level: u8,
    pub text: String,
}

#[derive(Default, Serialize, Deserialize)]
struct FoldStore {
    notes: BTreeMap<String, Vec<FoldEntry>>,
}

fn store_path() -> Option<PathBuf> {
    Some(crate::files::app_data_dir("Thinkly Fast").join("heading-folds.json"))
}

fn load_store() -> FoldStore {
    store_path()
        .and_then(|path| fs::read_to_string(path).ok())
        .and_then(|raw| serde_json::from_str(&raw).ok())
        .unwrap_or_default()
}

fn save_store(store: &FoldStore) {
    let Some(path) = store_path() else { return };
    if let Some(parent) = path.parent() {
        let _ = fs::create_dir_all(parent);
    }
    if let Ok(json) = serde_json::to_string_pretty(store) {
        let _ = fs::write(path, json);
    }
}

pub(super) fn heading_level(block: &Block) -> Option<u8> {
    if block.meta.column.is_some() || block.meta.code_language.is_some() || block.meta.raw_source.is_some() || block.meta.table.is_some() {
        return None;
    }
    match block.kind {
        Kind::H1 => Some(1),
        Kind::H2 => Some(2),
        Kind::H3 => Some(block.meta.heading_level.unwrap_or(3)),
        _ => None,
    }
}

/// Exclusive end block index governed by the heading at `heading_index` (J1).
pub(super) fn fold_end(doc: &Doc, heading_index: usize) -> usize {
    let Some(level) = doc.blocks.get(heading_index).and_then(heading_level) else {
        return (heading_index + 1).min(doc.blocks.len());
    };
    (heading_index + 1..doc.blocks.len())
        .find(|&i| heading_level(&doc.blocks[i]).is_some_and(|l| l <= level))
        .unwrap_or(doc.blocks.len())
}

pub(super) fn restore_folds(path: &Path, doc: &Doc) -> HashSet<u64> {
    let store = load_store();
    let key = path.to_string_lossy();
    let Some(entries) = store.notes.get(key.as_ref()) else {
        return HashSet::new();
    };
    let mut folded = HashSet::new();
    for entry in entries {
        if let Some(block) = doc.blocks.get(entry.index)
            && heading_level(block) == Some(entry.level)
            && block.text == entry.text
        {
            folded.insert(block.id);
            continue;
        }
        if let Some((_, block)) = doc
            .blocks
            .iter()
            .enumerate()
            .filter(|(_, b)| {
                !folded.contains(&b.id)
                    && heading_level(b) == Some(entry.level)
                    && b.text == entry.text
            })
            .min_by_key(|(i, _)| i.abs_diff(entry.index))
        {
            folded.insert(block.id);
        }
    }
    folded
}

pub(super) fn save_folds_for(path: &Path, doc: &Doc, folded: &HashSet<u64>) {
    let mut store = load_store();
    let key = path.to_string_lossy().into_owned();
    let entries: Vec<FoldEntry> = doc
        .blocks
        .iter()
        .enumerate()
        .filter_map(|(index, block)| {
            if !folded.contains(&block.id) {
                return None;
            }
            let level = heading_level(block)?;
            Some(FoldEntry { index, level, text: block.text.clone() })
        })
        .collect();
    if entries.is_empty() {
        store.notes.remove(&key);
    } else {
        store.notes.insert(key, entries);
    }
    save_store(&store);
}

impl Editor {
    pub(super) fn is_heading_folded(&self, id: u64) -> bool {
        self.folded.contains(&id) && self.doc.get(id).and_then(heading_level).is_some()
    }

    pub(super) fn enclosing_folded_heading(&self, index: usize) -> Option<usize> {
        if self.folded.is_empty() || index == 0 {
            return None;
        }
        (0..index).find(|&h| {
            let block = &self.doc.blocks[h];
            self.folded.contains(&block.id)
                && heading_level(block).is_some()
                && index < fold_end(&self.doc, h)
        })
    }

    pub(super) fn is_index_hidden(&self, index: usize) -> bool {
        self.enclosing_folded_heading(index).is_some()
    }

    pub(super) fn next_visible_index(&self, from: usize, forward: bool) -> Option<usize> {
        if forward {
            ((from + 1)..self.doc.blocks.len()).find(|&i| !self.is_index_hidden(i))
        } else {
            (0..from).rev().find(|&i| !self.is_index_hidden(i))
        }
    }

    pub(super) fn last_visible_index(&self) -> Option<usize> {
        (0..self.doc.blocks.len()).rev().find(|&i| !self.is_index_hidden(i))
    }

    pub(super) fn persist_folds(&self) {
        if self.minimal {
            return;
        }
        if let Some(path) = self.path.as_deref() {
            save_folds_for(path, &self.doc, &self.folded);
        }
    }

    pub(super) fn toggle_fold(&mut self, id: u64, cx: &mut Context<Self>) {
        let Some(index) = self.doc.index_of(id) else { return };
        if heading_level(&self.doc.blocks[index]).is_none() {
            return;
        }
        let end = fold_end(&self.doc, index);
        if self.folded.contains(&id) {
            self.folded.remove(&id);
        } else {
            self.folded.insert(id);
            for i in (index + 1)..end {
                self.layouts.remove(&self.doc.blocks[i].id);
            }
            let head_idx = self.doc.index_of(self.caret.head.block).unwrap_or(0);
            let anchor_idx = self.doc.index_of(self.caret.anchor.block).unwrap_or(0);
            if (index + 1..end).contains(&head_idx) || (index + 1..end).contains(&anchor_idx) {
                let len = self.doc.blocks[index].text.len();
                let pos = Position { block: id, offset: len };
                self.caret.anchor = pos;
                self.caret.head = pos;
                self.caret.composing = None;
            }
        }
        self.list.splice(index..end, end - index);
        self.persist_folds();
        cx.notify();
    }

    pub(super) fn unfold_heading(&mut self, id: u64, cx: &mut Context<Self>) -> bool {
        if !self.folded.remove(&id) {
            return false;
        }
        if let Some(index) = self.doc.index_of(id) {
            let end = fold_end(&self.doc, index);
            self.list.splice(index..end, end - index);
        }
        self.persist_folds();
        cx.notify();
        true
    }

    pub(super) fn ensure_block_visible(&mut self, block_id: u64) {
        let Some(index) = self.doc.index_of(block_id) else { return };
        let mut changed = false;
        while let Some(h) = self.enclosing_folded_heading(index) {
            let hid = self.doc.blocks[h].id;
            let end = fold_end(&self.doc, h);
            self.folded.remove(&hid);
            self.list.splice(h..end, end - h);
            changed = true;
        }
        if changed {
            self.persist_folds();
        }
    }
}
