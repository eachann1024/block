//! Heading fold state, governed block ranges, and per-note persistence.
use std::{
    collections::{BTreeMap, HashSet},
    fs,
    path::{Path, PathBuf},
};
use gpui::Context;
use serde::{Deserialize, Serialize};
use super::{
    input::transactions::{ListChange, Snapshot},
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
    Some(crate::files::app_data_dir("Block").join("heading-folds.json"))
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
    if block.meta.code_language.is_some() || block.meta.raw_source.is_some() || block.meta.table.is_some() {
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

    /// A folded heading reveals `index` when it sits in that heading's temporary tail.
    fn tail_reveals(&self, heading_index: usize, index: usize) -> bool {
        let heading = self.doc.blocks[heading_index].id;
        let Some(&(_, start)) = self.fold_tails.iter().find(|(id, _)| *id == heading) else { return false };
        let Some(start) = self.doc.index_of(start) else { return false };
        let end = fold_end(&self.doc, heading_index);
        start > heading_index && start < end && index >= start
    }

    pub(super) fn is_index_hidden(&self, index: usize) -> bool {
        // Hidden when any enclosing folded heading does not reveal this block.
        // A lower folded heading can reveal its own tail without opening its parent.
        (0..index).any(|heading| {
            let block = &self.doc.blocks[heading];
            self.folded.contains(&block.id)
                && heading_level(block).is_some()
                && index < fold_end(&self.doc, heading)
                && !self.tail_reveals(heading, index)
        })
    }

    pub(super) fn crosses_hidden_fold(&self, first: usize, last: usize) -> bool {
        first < last && (first + 1..last).any(|i| self.is_index_hidden(i))
    }

    pub(super) fn selection_covers_doc(&self, start: Position, end: Position, first: usize, last: usize) -> bool {
        first == 0
            && last + 1 == self.doc.blocks.len()
            && start.offset == 0
            && self.doc.blocks.get(last).is_some_and(|block| end.offset == block.text.len())
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
            self.discard_fold_tail(id);
        } else {
            self.discard_fold_tail(id);
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

    /// Folds or unfolds the heading at the caret, or the nearest heading that governs it (⌘⌥T).
    pub(super) fn toggle_fold_at_caret(&mut self, cx: &mut Context<Self>) {
        if !self.is_markdown_note() { return; }
        let id = self.caret.head.block;
        let Some(index) = self.doc.index_of(id) else { return };
        if heading_level(&self.doc.blocks[index]).is_some() {
            return self.toggle_fold(id, cx);
        }
        let governing = (0..index).rev().find(|&i| heading_level(&self.doc.blocks[i]).is_some() && index < fold_end(&self.doc, i));
        if let Some(h) = governing {
            let hid = self.doc.blocks[h].id;
            self.toggle_fold(hid, cx);
        }
    }

    pub(super) fn unfold_heading(&mut self, id: u64, cx: &mut Context<Self>) -> bool {
        if !self.folded.remove(&id) {
            return false;
        }
        self.discard_fold_tail(id);
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
        if !self.is_index_hidden(index) { return; }
        let mut changed = false;
        while let Some(h) = self.enclosing_folded_heading(index) {
            let hid = self.doc.blocks[h].id;
            let end = fold_end(&self.doc, h);
            self.folded.remove(&hid);
            self.discard_fold_tail(hid);
            self.list.splice(h..end, end - h);
            changed = true;
        }
        if changed {
            self.persist_folds();
        }
    }

    /// Enter on a folded heading appends a paragraph after the hidden section and leaves the fold shut.
    pub(super) fn enter_on_folded_heading(&mut self, cx: &mut Context<Self>) -> bool {
        if self.minimal || !self.is_markdown_note() || self.editing_source() { return false; }
        if !self.caret.is_empty() || self.caret.composing.is_some() { return false; }
        let heading_id = self.caret.head.block;
        if !self.is_heading_folded(heading_id) { return false; }
        let Some(heading_index) = self.doc.index_of(heading_id) else { return false; };
        let end = fold_end(&self.doc, heading_index);
        let before = self.snapshot();
        self.doc.splice_blocks(end..end, vec![Block::new(0, Kind::Paragraph, String::new())]);
        let id = self.doc.blocks[end].id;
        let at = Position { block: id, offset: 0 };
        self.caret.anchor = at;
        self.caret.head = at;
        self.caret.composing = None;
        self.block_sel = None;
        self.preferred_x = None;
        self.note_fold_tail(heading_id, id);
        self.record_changes(before, vec![ListChange { start: end, before: 0, after: 1 }], true, cx);
        self.show_hidden_tail(id);
        true
    }

    /// Backspace/Delete must not swallow hidden blocks that sit between a heading and its visible tail.
    pub(super) fn fold_gap_edit(&mut self, forward: bool, cx: &mut Context<Self>) -> bool {
        if self.minimal || !self.caret.is_empty() || self.caret.composing.is_some() { return false; }
        let at = self.caret.head;
        let Some(index) = self.doc.index_of(at.block) else { return false; };
        let block = &self.doc.blocks[index];
        if forward {
            if at.offset != block.text.len() { return false; }
            let Some(next) = self.next_visible_index(index, true) else { return false; };
            return next > index + 1 && self.crosses_hidden_fold(index, next);
        }
        if at.offset != 0 { return false; }
        let Some(prev) = self.next_visible_index(index, false) else { return false; };
        if prev + 1 >= index || !self.crosses_hidden_fold(prev, index) { return false; }
        if heading_level(block).is_some() { return false; }
        let removable = block.text.is_empty()
            && block.meta.table.is_none()
            && block.meta.raw_source.is_none()
            && block.meta.code_language.is_none()
            && block.meta.html_paragraph.is_none();
        if removable {
            let prev_block = self.doc.blocks[prev].clone();
            let to = Position { block: prev_block.id, offset: prev_block.text.len() };
            let before = self.snapshot();
            self.doc.splice_blocks(index..index + 1, Vec::new());
            self.caret.anchor = to;
            self.caret.head = to;
            self.record_changes(before, vec![ListChange { start: index, before: 1, after: 0 }], true, cx);
        } else {
            let prev_block = &self.doc.blocks[prev];
            let to = Position { block: prev_block.id, offset: prev_block.text.len() };
            self.set_head(to, false, cx);
        }
        true
    }

    fn note_fold_tail(&mut self, heading: u64, block: u64) {
        if let Some(pos) = self.fold_tails.iter().position(|(id, _)| *id == heading) {
            let (_, start) = self.fold_tails[pos];
            if self.doc.index_of(start).is_some_and(|start_index| self.doc.index_of(block).is_some_and(|block_index| block_index >= start_index)) {
                return;
            }
            self.fold_tails[pos].1 = block;
            return;
        }
        self.fold_tails.push((heading, block));
    }

    fn set_fold_tail(&mut self, heading: u64, block: u64) {
        if let Some(pos) = self.fold_tails.iter().position(|(id, _)| *id == heading) {
            self.fold_tails[pos].1 = block;
        } else {
            self.fold_tails.push((heading, block));
        }
    }

    fn clear_fold_tail(&mut self, heading: u64) {
        self.fold_tails.retain(|(id, _)| *id != heading);
    }

    /// Show `block` as the tail of every fold that hides it, when nothing hidden follows it.
    /// Returns whether the block is visible afterward. Never opens the fold.
    pub(super) fn show_hidden_tail(&mut self, block: u64) -> bool {
        let Some(index) = self.doc.index_of(block) else { return false; };
        if !self.is_index_hidden(index) { return true; }
        for heading_index in 0..index {
            let heading = &self.doc.blocks[heading_index];
            if !self.is_heading_folded(heading.id) { continue; }
            let end = fold_end(&self.doc, heading_index);
            if index >= end || self.tail_reveals(heading_index, index) { continue; }
            if (index + 1..end).any(|i| self.is_index_hidden(i)) { continue; }
            self.note_fold_tail(heading.id, block);
        }
        self.doc.index_of(block).is_some_and(|index| !self.is_index_hidden(index))
    }

    fn was_fold_visible(before: &Snapshot, heading: u64, block: u64) -> bool {
        let Some(heading_index) = before.doc.index_of(heading) else { return false };
        let Some(index) = before.doc.index_of(block) else { return false };
        let Some(&(_, start)) = before.fold_tails.iter().find(|(id, _)| *id == heading) else { return false };
        let Some(start) = before.doc.index_of(start) else { return false };
        let end = fold_end(&before.doc, heading_index);
        start > heading_index && start < end && index >= start && index < end
    }

    fn innermost_folded(&self, index: usize) -> Option<usize> {
        (0..index).rev().find(|&heading| {
            let block = &self.doc.blocks[heading];
            self.is_heading_folded(block.id) && index < fold_end(&self.doc, heading)
        })
    }

    /// New blocks dropped into a hidden body move to that section's end so the
    /// old body stays folded and the new blocks can become the visible tail.
    fn park_new_blocks(&mut self, before: &Snapshot) {
        let new_ids: Vec<u64> = self.doc.blocks.iter()
            .filter(|block| before.doc.index_of(block.id).is_none())
            .map(|block| block.id)
            .collect();
        for id in new_ids {
            let Some(index) = self.doc.index_of(id) else { continue };
            let Some(inner) = self.innermost_folded(index) else { continue };
            let heading = self.doc.blocks[inner].id;
            let end = fold_end(&self.doc, inner);
            let last_hidden = (inner + 1..end).filter(|&i| {
                let block = self.doc.blocks[i].id;
                before.doc.index_of(block).is_some() && !Self::was_fold_visible(before, heading, block)
            }).max();
            let Some(last_hidden) = last_hidden else { continue };
            if index > last_hidden || index + 1 == end { continue; }
            let block = self.doc.blocks[index].clone();
            self.doc.splice_blocks(index..index + 1, Vec::new());
            let end = self.doc.index_of(heading)
                .map(|inner| fold_end(&self.doc, inner))
                .unwrap_or(self.doc.blocks.len())
                .min(self.doc.blocks.len());
            self.doc.splice_blocks(end..end, vec![block]);
        }
    }

    /// Keep each visible tail attached to blocks inserted or retargeted inside its folded section.
    /// A folded heading with no tail yet still claims a block appended after its hidden body,
    /// so focusing that block does not open the section or land in a hidden empty paragraph.
    pub(super) fn sync_fold_tail(&mut self, before: &Snapshot) {
        if self.folded.is_empty() {
            self.fold_tails.clear();
            return;
        }
        if self.doc.blocks.iter().any(|block| before.doc.index_of(block.id).is_none()) {
            self.park_new_blocks(before);
        }
        let mut headings: Vec<u64> = self.fold_tails.iter().map(|(id, _)| *id).collect();
        for (id, _) in &before.fold_tails {
            if !headings.contains(id) { headings.push(*id); }
        }
        for id in &self.folded {
            if !headings.contains(id) { headings.push(*id); }
        }
        for heading in headings {
            self.sync_one_fold_tail(heading, before);
        }
        if self.doc.index_of(self.caret.head.block).is_some_and(|index| self.is_index_hidden(index)) {
            self.show_hidden_tail(self.caret.head.block);
        }
    }

    fn sync_one_fold_tail(&mut self, heading: u64, before: &Snapshot) {
        let Some(heading_index) = self.doc.index_of(heading) else {
            self.clear_fold_tail(heading);
            return;
        };
        if !self.is_heading_folded(heading) {
            self.clear_fold_tail(heading);
            return;
        }
        let end = fold_end(&self.doc, heading_index);
        let inside = |index: usize| index > heading_index && index < end;
        let current = self.fold_tails.iter().find(|(id, _)| *id == heading).map(|(_, start)| *start);
        if let Some(start) = current
            && let Some(mut start_index) = self.doc.index_of(start).filter(|&index| inside(index))
        {
            while start_index > heading_index + 1
                && inside(start_index - 1)
                && before.doc.index_of(self.doc.blocks[start_index - 1].id).is_none()
            {
                start_index -= 1;
            }
            self.set_fold_tail(heading, self.doc.blocks[start_index].id);
            return;
        }
        let previous = before.fold_tails.iter().find(|(id, _)| *id == heading).map(|(_, start)| *start);
        if let Some(start) = previous
            && let Some(old_index) = before.doc.index_of(start)
            && let Some(next) = before.doc.blocks.get(old_index + 1)
            && let Some(index) = self.doc.index_of(next.id).filter(|&index| inside(index))
        {
            self.set_fold_tail(heading, self.doc.blocks[index].id);
            return;
        }
        if let Some(index) = (heading_index + 1..end).find(|&index| {
            let id = self.doc.blocks[index].id;
            before.doc.index_of(id).is_some() && Self::was_fold_visible(before, heading, id)
        }) {
            self.set_fold_tail(heading, self.doc.blocks[index].id);
            return;
        }
        let last_old_hidden = (heading_index + 1..end).filter(|&index| {
            let id = self.doc.blocks[index].id;
            before.doc.index_of(id).is_some() && !Self::was_fold_visible(before, heading, id)
        }).max();
        let from = last_old_hidden.map(|index| index + 1).unwrap_or(heading_index + 1);
        if let Some(index) = (from..end).find(|&index| before.doc.index_of(self.doc.blocks[index].id).is_none()) {
            self.set_fold_tail(heading, self.doc.blocks[index].id);
            return;
        }
        if let Some(caret) = self.doc.index_of(self.caret.head.block).filter(|&index| inside(index))
            && let Some(start) = previous
            && let Some(old_start) = before.doc.index_of(start)
            && before.doc.index_of(self.caret.head.block).is_some_and(|old| old >= old_start)
        {
            self.set_fold_tail(heading, self.doc.blocks[caret].id);
            return;
        }
        self.clear_fold_tail(heading);
    }
}
