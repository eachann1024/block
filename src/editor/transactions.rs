use gpui::Context;
use crate::editor::{model::{Doc, Kind, MarkKind}, view::{Editor, selection::{Caret, Position}}};

// Doc is a persistent tree: snapshots share unaffected nodes and text. A mutation
// copies only its edited blocks/tree paths, never the entire document.
pub(in crate::editor) struct Snapshot {
    pub doc: Doc, pub caret: Caret,
    block_sel: Option<(usize, usize)>, table_sel: Option<(u64, super::super::table_actions::TableSel)>,
    /// Per folded heading: `(heading id, first block id shown after its hidden body)`.
    pub fold_tails: Vec<(u64, u64)>,
}
#[derive(Clone)]
pub(in crate::editor) struct ListChange { pub start: usize, pub before: usize, pub after: usize }
pub(in crate::editor) struct HistoryEntry { state: Snapshot, changes: Vec<ListChange> }
impl Editor {
    pub(in crate::editor) fn snapshot(&self) -> Snapshot {
        Snapshot { doc: self.doc.clone(), caret: self.caret.clone(), block_sel: self.block_sel, table_sel: self.table_sel, fold_tails: self.fold_tails.clone() }
    }
    fn selection_change(&self, before: &Snapshot) -> ListChange {
        let a = before.doc.index_of(before.caret.anchor.block).unwrap_or(0);
        let b = before.doc.index_of(before.caret.head.block).unwrap_or(a);
        let count = a.abs_diff(b) + 1;
        ListChange { start: a.min(b), before: count,
            after: (count as isize + self.doc.blocks.len() as isize - before.doc.blocks.len() as isize).max(0) as usize }
    }
    fn apply_changes(&mut self, changes: &[ListChange], forward: bool) {
        let mut diverged = false;
        if forward {
            for c in changes {
                if c.start + c.before <= self.list.item_count() { self.list.splice(c.start..c.start+c.before, c.after); }
                else { diverged = true; break; }
            }
        } else {
            for c in changes.iter().rev() {
                if c.start + c.after <= self.list.item_count() { self.list.splice(c.start..c.start+c.after, c.before); }
                else { diverged = true; break; }
            }
        }
        if diverged || self.list.item_count() != self.doc.blocks.len() {
            crate::log::warn(&format!("list splice diverged (list={}, doc={}); resetting", self.list.item_count(), self.doc.blocks.len()));
            self.list.reset(self.doc.blocks.len());
        }
        self.layouts.clear();
    }
    pub(in crate::editor) fn record_changes(&mut self, before: Snapshot, changes: Vec<ListChange>, apply: bool, cx: &mut Context<Self>) {
        if self.doc == before.doc { cx.notify(); return; }
        self.sync_fold_tail(&before);
        if apply { self.apply_changes(&changes, true); }
        self.history.push(HistoryEntry { state: before, changes });
        if self.history.len() > 200 { self.history.remove(0); }
        self.redo.clear(); self.changed(cx);
    }
    /// Drop the temporary tail for `heading` from the live editor and from undo snapshots,
    /// so expanding and folding again hides body text that was inserted while folded.
    pub(in crate::editor) fn discard_fold_tail(&mut self, heading: u64) {
        let clear = |tails: &mut Vec<(u64, u64)>| { tails.retain(|(id, _)| *id != heading); };
        clear(&mut self.fold_tails);
        for entry in &mut self.history { clear(&mut entry.state.fold_tails); }
        for entry in &mut self.redo { clear(&mut entry.state.fold_tails); }
        if let Some(pending) = &mut self.composition_before { clear(&mut pending.fold_tails); }
    }
    pub(in crate::editor) fn record(&mut self, before: Snapshot, cx: &mut Context<Self>) {
        let change = self.selection_change(&before);
        self.record_changes(before, vec![change], true, cx);
    }
    fn invalidate_edit(&mut self, before: &Snapshot) {
        self.apply_changes(&[self.selection_change(before)], true);
    }
    pub(in crate::editor) fn changed(&mut self, cx: &mut Context<Self>) {
        self.outline.clear_navigation();
        self.reveal_caret = true;
        self.touch(cx);
    }
    pub(in crate::editor) fn undo(&mut self, redo: bool, cx: &mut Context<Self>) {
        self.commit_composition(cx);
        self.composition_before = None;
        // An entry that restores the very same document would only move the selection
        // (select the text without removing it); keep popping until text really changes.
        loop {
            let item = if redo { self.redo.pop() } else { self.history.pop() };
            let Some(mut item) = item else { break };
            let same = item.state.doc == self.doc;
            let target = std::mem::replace(&mut item.state, self.snapshot());
            self.doc = target.doc; self.caret = target.caret; self.caret.composing = None;
            self.block_sel = target.block_sel; self.table_sel = target.table_sel;
            self.fold_tails = target.fold_tails;
            self.apply_changes(&item.changes, redo);
            if redo { self.history.push(item); } else { self.redo.push(item); }
            self.changed(cx);
            if !same { break; }
        }
    }
    pub(in crate::editor) fn commit_composition(&mut self, cx: &mut Context<Self>) {
        self.caret.composing = None;
        if let Some(before) = self.composition_before.take() {
            let change = self.selection_change(&before);
            self.record_changes(before, vec![change], false, cx);
        }
    }
    pub(in crate::editor) fn can_replace_selection(&self) -> bool {
        let (a, b, ai, bi) = self.ordered();
        if ai == bi && let Some(table) = &self.doc.blocks[ai].meta.table {
            return table.cell_at_range(&(a.offset..b.offset)).is_some()
                || (a.offset == 0 && b.offset == self.doc.blocks[ai].text.len());
        }
        (ai..=bi).all(|index| {
            let block = &self.doc.blocks[index];
            block.meta.table.is_none() || self.selected_range(block.id)
                .is_some_and(|range| range == (0..block.text.len()))
        })
    }
    #[allow(dead_code)]
    pub(in crate::editor) fn set_table_cell(&mut self, id: u64, row: usize, cell: usize,
        text: String, cx: &mut Context<Self>) {
        self.commit_composition(cx);
        let Some(index) = self.doc.index_of(id) else { return; };
        let before = self.snapshot();
        if self.doc.set_table_cell_text(id, row, cell, text) {
            self.record_changes(before, vec![ListChange { start:index, before:1, after:1 }], true, cx);
        }
    }
    pub(in crate::editor) fn replace_selection(&mut self, text: &str, marker: bool, cx: &mut Context<Self>) {
        self.replace_selection_impl(text, marker, true, cx);
    }
    pub(in crate::editor) fn replace_plain_selection(&mut self, text: &str, cx: &mut Context<Self>) {
        self.replace_selection_impl(text, false, false, cx);
    }
    fn replace_selection_impl(&mut self, text: &str, marker: bool, promote_images: bool, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.pending_vertical = None;
        let (a, b, ai, bi) = self.ordered();
        if a == b && text.is_empty() { return; }
        // A caret jump from a folded heading to its visible tail selects the hidden body.
        // Keep that body unless the user selected the whole note.
        if ai != bi && self.crosses_hidden_fold(ai, bi) && !self.selection_covers_doc(a, b, ai, bi) {
            cx.notify();
            return;
        }
        if !self.can_replace_selection() {
            self.status = crate::theme::tr(
                "请在单个表格单元格内选择，或选中整个表格以替换",
                "Select within one table cell, or select the entire table to replace it",
            ).into();
            cx.notify(); return;
        }
        let before = self.snapshot();
        let normalized = text.replace("\r\n", "\n").replace('\r', "\n");
        if ai == bi && self.doc.get(a.block).is_some_and(|blk| blk.meta.code_language.is_some()
            || blk.meta.raw_source.is_some() || blk.meta.table.as_ref().is_some_and(|t|
                t.cell_at_range(&(a.offset..b.offset)).is_some())) {
            self.doc.replace_range(a.block, a.offset..b.offset, &normalized);
            let at = Position { block: a.block, offset: a.offset + normalized.len() };
            self.caret.anchor = at; self.caret.head = at; self.caret.composing = None;
            if self.composition_before.is_none() { self.record(before, cx); }
            else { self.invalidate_edit(&before); self.changed(cx); }
            self.sync_block_menu_after_edit(cx);
            return;
        }
        let mut id = a.block;
        let mut offset = a.offset + normalized.len();
        if ai == bi && !normalized.contains('\n') && self.doc.blocks[ai].meta.table.is_none() {
            self.doc.replace_range(a.block, a.offset..b.offset, &normalized);
            if a == b { self.code_escape_strip(a, normalized.len(), cx); }
        } else {
            let starts_table = self.doc.blocks[ai].meta.table.is_some();
            let mut combined = if starts_table {
                crate::editor::model::Block::new(a.block, Kind::Paragraph, String::new())
            } else { self.doc.blocks[ai].clone() };
            if ai == bi {
                if starts_table { combined.replace_range(0..0, &normalized); offset = normalized.len(); }
                else { combined.replace_range(a.offset..b.offset, &normalized); }
            } else {
                let prefix = if starts_table { 0 } else { a.offset };
                combined.replace_range(prefix..combined.text.len(), &normalized); offset = prefix + normalized.len();
                if self.doc.blocks[bi].meta.table.is_none() {
                    let suffix = self.doc.blocks[bi].slice(b.offset..self.doc.blocks[bi].text.len());
                    combined.append(&suffix);
                }
            }
            let carry = match combined.kind { Kind::Bullet | Kind::Numbered | Kind::Todo | Kind::Quote => combined.kind, _ => Kind::Paragraph };
            let mut pieces = Vec::new(); let mut start = 0;
            for (newline, _) in normalized.match_indices('\n') {
                let end = if starts_table { newline } else { a.offset + newline };
                pieces.push(combined.slice(start..end)); start = end + 1;
            }
            pieces.push(combined.slice(start..combined.text.len()));
            for (i, piece) in pieces.iter_mut().enumerate() {
                if i > 0 {
                    piece.id = 0; piece.kind = carry; piece.checked = false;
                    piece.meta.list_start = None; piece.meta.heading_level = None;
                }
            }
            let count = pieces.len();
            self.doc.splice_blocks(ai..bi + 1, pieces);
            id = self.doc.blocks[ai + count - 1].id;
            if count > 1 { offset = normalized.rsplit('\n').next().unwrap_or("").len(); }
        }
        if marker && (normalized.ends_with(' ') || normalized.ends_with('\u{3000}')) && !normalized.contains('\n') && ai == bi && a == b {
            self.space_rule(&mut id, &mut offset);
        }
        if marker && matches!(normalized.as_str(), "-" | "*" | "_" | "－" | "＊" | "＿" | "—" | "——") && ai == bi && a == b {
            self.divider_rule(&mut id, &mut offset);
        }
        if !marker && promote_images && self.promote_image_block(id) {
            offset = 0;
        }
        let at = Position { block: id, offset };
        self.caret.anchor = at; self.caret.head = at; self.caret.composing = None;
        if self.composition_before.is_none() { self.record(before, cx); }
        else { self.invalidate_edit(&before); self.changed(cx); }
        self.sync_block_menu_after_edit(cx);
    }
    pub(in crate::editor) fn insert_in_block(&mut self, text: &str, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        self.pending_vertical = None; self.preferred_x = None;
        let (a, b, ai, bi) = self.ordered();
        if ai != bi { self.replace_selection(text, false, cx); return; }
        let before = self.snapshot();
        self.doc.replace_range(a.block, a.offset..b.offset, text);
        let at = Position { block: a.block, offset: a.offset + text.len() };
        self.caret.anchor = at; self.caret.head = at; self.caret.composing = None;
        self.record(before, cx);
        self.sync_block_menu_after_edit(cx);
    }
    pub(in crate::editor) fn mark(&mut self, mark: MarkKind, cx: &mut Context<Self>) {
        if self.path.is_none() || self.caret.is_empty() { return; }
        self.commit_composition(cx);
        let before = self.snapshot();
        let (_, _, start, end) = self.ordered();
        for i in start..=end {
            let id = self.doc.blocks[i].id;
            if let Some(range) = self.selected_range(id) { self.doc.toggle_mark(id, range, mark); }
        }
        self.record(before, cx);
    }
    pub(in crate::editor) fn toggle_checked(&mut self, id: u64, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        let Some(index) = self.doc.index_of(id) else { return; };
        let before = self.snapshot(); self.doc.toggle_checked(id);
        self.record_changes(before, vec![ListChange { start: index, before: 1, after: 1 }], true, cx);
    }
    #[allow(dead_code)]
    pub(in crate::editor) fn cycle_heading(&mut self, id: u64, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        if self.doc.get(id).is_some_and(|block| block.meta.table.is_some()) {
            self.status = crate::theme::tr(
                "表格单元格保持表格结构，不支持转换为标题",
                "Table cells keep their table structure; heading conversion is unavailable",
            ).into();
            cx.notify(); return;
        }
        self.commit_composition(cx);
        let Some(index) = self.doc.index_of(id) else { return; };
        let before = self.snapshot();
        let kind = self.doc.get(id).map(|b| b.kind);
        let next = match kind { Some(Kind::Paragraph) => Kind::H1, Some(Kind::H1) => Kind::H2, Some(Kind::H2) => Kind::H3, _ => Kind::Paragraph };
        self.doc.set_kind(id, next);
        self.record_changes(before, vec![ListChange { start: index, before: 1, after: 1 }], true, cx);
    }
}
