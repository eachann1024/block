//! Input commands share cross-block positions and persistent edit transactions.
use std::ops::Range;
use gpui::{ClipboardItem, Context, Window};
use unicode_segmentation::GraphemeCursor;
use super::{model::{Kind, MarkKind}, view::{Editor, EditorAction, selection::{Position, boundary}}};
#[path = "transactions.rs"] pub(super) mod transactions;

impl Editor {
    pub(super) fn handle(&mut self, action: &EditorAction, window: &mut Window, cx: &mut Context<Self>) {
        use EditorAction::*;
        if self.path.is_none() { return; }
        self.pending_vertical = None;
        if !matches!(action, Cancel) { self.commit_composition(cx); }
        match action {
            Left | Right | SelectLeft | SelectRight => {
                self.preferred_x = None;
                let forward = matches!(action, Right | SelectRight);
                let extend = matches!(action, SelectLeft | SelectRight);
                let at = if !extend && !self.caret.is_empty() {
                    let (a,b,_,_) = self.ordered(); if forward { b } else { a }
                } else { self.step(forward) };
                self.set_head(at, extend, cx);
            }
            SelectAll => {
                self.caret.anchor = Position { block: self.doc.blocks[0].id, offset: 0 };
                let last = &self.doc.blocks[self.doc.blocks.len()-1];
                self.set_head(Position { block: last.id, offset: last.text.len() }, true, cx);
            }
            Up | Down | SelectUp | SelectDown => self.vertical(matches!(action, Down | SelectDown), matches!(action, SelectUp | SelectDown), cx),
            Enter => {
                let id = self.caret.head.block;
                if self.caret.is_empty() && self.block_text().is_empty() && self.doc.get(id).is_some_and(|b| b.kind != Kind::Paragraph) {
                    let before = self.snapshot(); self.doc.set_kind(id, Kind::Paragraph); self.record(before, cx);
                } else { self.replace_selection("\n", false, cx); }
            }
            Backspace | Delete => {
                if self.caret.is_empty() {
                    let at = self.step(matches!(action, Delete));
                    self.caret.head = at;
                }
                self.replace_selection("", false, cx);
            }
            Copy | Cut => {
                if !self.caret.is_empty() {
                    let (_, _, first, last) = self.ordered();
                    let mut text = String::new();
                    for i in first..=last {
                        if i > first { text.push('\n'); }
                        let block = &self.doc.blocks[i];
                        if let Some(range) = self.selected_range(block.id) { text.push_str(&block.text[range]); }
                    }
                    cx.write_to_clipboard(ClipboardItem::new_string(text));
                    if matches!(action, Cut) { self.replace_selection("", false, cx); }
                }
            }
            Paste => if let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) {
                self.replace_selection(&text, false, cx);
            },
            Bold => self.mark(MarkKind::Bold, cx), Italic => self.mark(MarkKind::Italic, cx),
            Strike => self.mark(MarkKind::Strike, cx), Code => self.mark(MarkKind::Code, cx),
            Undo => self.undo(false, cx), Redo => self.undo(true, cx), Save => self.save(cx),
            Cancel => {
                if let Some(before) = self.composition_before.take() {
                    self.doc = before.doc; self.caret = before.caret;
                    self.list.reset(self.doc.blocks.len()); self.changed(cx);
                }
                self.caret.composing = None;
                self.dragging = None; self.drag_over = None; self.selecting = false; cx.notify();
            }
        }
        let _ = window;
    }
    fn step(&self, forward: bool) -> Position {
        let at = self.caret.head;
        let text = self.block_text();
        let mut cursor = GraphemeCursor::new(boundary(text, at.offset), text.len(), true);
        let next = if forward { cursor.next_boundary(text, 0) } else { cursor.prev_boundary(text, 0) };
        if let Ok(Some(offset)) = next { return Position { offset, ..at }; }
        let index = self.doc.index_of(at.block).unwrap_or(0);
        let next = if forward { index.checked_add(1).filter(|i| *i < self.doc.blocks.len()) } else { index.checked_sub(1) };
        next.map(|i| { let b = &self.doc.blocks[i]; Position { block: b.id, offset: if forward { 0 } else { b.text.len() } } }).unwrap_or(at)
    }
    pub(super) fn replace(&mut self, range: Option<Range<usize>>, text: &str, _: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.set_replacement_range(range);
        let composing = self.composition_before.is_some();
        self.replace_selection(text, !composing, cx);
        if composing { self.commit_composition(cx); }
    }
    pub(super) fn set_replacement_range(&mut self, range: Option<Range<usize>>) {
        let range = range.map(|r| from_utf16(self.block_text(), &r)).or(self.caret.composing.clone());
        if let Some(range) = range {
            self.caret.anchor = Position { block: self.caret.head.block, offset: range.start };
            self.caret.head.offset = range.end;
        }
    }
    pub(super) fn add_below(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        let Some(index) = self.doc.index_of(id) else { return; };
        let before = self.snapshot();
        if let Some(id) = self.doc.insert_after(id, Kind::Paragraph, String::new()) {
            self.set_head(Position { block: id, offset: 0 }, false, cx);
            window.focus(&self.focus);
            self.record_changes(before, vec![transactions::ListChange { start: index + 1, before: 0, after: 1 }], true, cx);
        }
    }
    pub(super) fn finish_drag(&mut self, cx: &mut Context<Self>) {
        if let Some(id) = self.dragging.take() {
            if let Some(to) = self.drag_over.take() {
                if self.path.is_some() && self.doc.index_of(id) != Some(to) {
                    self.commit_composition(cx);
                    let Some(from) = self.doc.index_of(id) else { return; };
                    let to = to.min(self.doc.blocks.len()-1);
                    let before = self.snapshot(); self.doc.move_block(id, to);
                    self.record_changes(before, vec![
                        transactions::ListChange { start: from, before: 1, after: 0 },
                        transactions::ListChange { start: to, before: 0, after: 1 },
                    ], true, cx);
                }
            }
        }
        self.selecting = false; cx.notify();
    }
}
pub(super) fn to_utf16(text: &str, range: &Range<usize>) -> Range<usize> {
    let one = |offset| text[..boundary(text, offset)].encode_utf16().count();
    one(range.start)..one(range.end)
}
pub(super) fn from_utf16(text: &str, range: &Range<usize>) -> Range<usize> {
    fn one(text: &str, units: usize) -> usize {
        let mut seen = 0;
        for (index, ch) in text.char_indices() {
            if seen + ch.len_utf16() > units { return index; }
            seen += ch.len_utf16();
        }
        text.len()
    }
    let start = one(text, range.start);
    start..one(text, range.end).max(start)
}
