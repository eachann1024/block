//! Notion-parity block operations: subtree-aware move, duplicate, code/menu shortcuts, and Esc block selection keys.
use gpui::{ClipboardItem, Context, Window};
use super::{
    block_menu::is_divider,
    input::transactions::ListChange,
    model::{Block, Doc, Kind},
    view::{Editor, EditorAction, selection::Position},
};

pub(super) fn is_list(block: &Block) -> bool {
    matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo)
        && block.meta.table.is_none() && block.meta.raw_source.is_none() && block.meta.code_language.is_none()
}
fn is_atomic(block: &Block) -> bool {
    super::image_source::spec_of(block).is_some() || is_divider(block)
}

impl Editor {
    /// End index (inclusive) of `start`'s nested list subtree.
    pub(super) fn list_subtree_end(&self, start: usize) -> usize {
        let Some(head) = self.doc.blocks.get(start).filter(|b| is_list(b)) else { return start };
        let (base, col) = (head.meta.indent, head.meta.column);
        let mut end = start;
        while end + 1 < self.doc.blocks.len() && is_list(&self.doc.blocks[end + 1])
            && self.doc.blocks[end + 1].meta.column == col && self.doc.blocks[end + 1].meta.indent > base {
            end += 1;
        }
        end
    }

    /// Move `lo..=hi` (plus any nested list children of `hi`) up or down one logical block/subtree.
    pub(super) fn move_block_range(&mut self, lo: usize, hi: usize, up: bool, cx: &mut Context<Self>) -> Option<(usize, usize)> {
        let last_idx = self.doc.blocks.len().saturating_sub(1);
        let (lo, hi_sel) = (lo.min(last_idx), hi.min(last_idx));
        let hi_ext = self.list_subtree_end(hi_sel);
        let col = self.doc.blocks[lo].meta.column;
        let n = hi_ext - lo + 1;
        let to = if up {
            if lo == 0 || (col.is_some() && self.doc.blocks[lo - 1].meta.column != col) { return None; }
            let first = &self.doc.blocks[lo];
            if col.is_none() && let Some(r) = self.column_group_range(lo - 1) { r.start }
            else if is_list(first) && is_list(&self.doc.blocks[lo - 1]) && self.doc.blocks[lo - 1].meta.indent > first.meta.indent {
                let mut p = lo - 1;
                while p > 0 && is_list(&self.doc.blocks[p]) && self.doc.blocks[p].meta.column == col && self.doc.blocks[p].meta.indent > first.meta.indent { p -= 1; }
                p
            } else { lo - 1 }
        } else {
            if hi_ext + 1 >= self.doc.blocks.len() || (col.is_some() && self.doc.blocks[hi_ext + 1].meta.column != col) { return None; }
            if col.is_none() && let Some(r) = self.column_group_range(hi_ext + 1) { r.end - n }
            else { self.list_subtree_end(hi_ext + 1) + 1 - n }
        };
        if to == lo { return None; }
        self.commit_composition(cx);
        let before = self.snapshot();
        let moved: Vec<Block> = (lo..=hi_ext).map(|i| self.doc.blocks[i].clone()).collect();
        self.doc.splice_blocks(lo..hi_ext + 1, Vec::new());
        self.doc.splice_blocks(to..to, moved);
        self.record_changes(before, vec![ListChange { start: lo, before: n, after: 0 }, ListChange { start: to, before: 0, after: n }], true, cx);
        self.layouts.clear();
        let new_hi_sel = to + (hi_sel - lo);
        self.list.scroll_to_reveal_item(if up { to } else { new_hi_sel });
        Some((to, new_hi_sel))
    }

    pub(super) fn move_block_shortcut(&mut self, up: bool, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        if let Some((anchor, head)) = self.block_sel {
            if let Some((new_lo, new_hi)) = self.move_block_range(anchor.min(head), anchor.max(head), up, cx) {
                let next_sel = if anchor <= head { (new_lo, new_hi) } else { (new_hi, new_lo) };
                self.block_sel = Some(next_sel);
                let pos = Position { block: self.doc.blocks[next_sel.1].id, offset: 0 };
                self.caret.anchor = pos; self.caret.head = pos;
                window.focus(&self.focus); cx.notify();
            }
            return;
        }
        let (_, _, first, last) = self.ordered();
        if self.move_block_range(first, last, up, cx).is_some() {
            self.reveal_caret = true; window.focus(&self.focus); cx.notify();
        }
    }

    /// Cmd+D: duplicate the active block(s) (and any nested list children) directly below.
    pub(super) fn duplicate_blocks(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        let last_idx = self.doc.blocks.len().saturating_sub(1);
        let (lo, hi_sel, had_block_sel) = if let Some((a, b)) = self.block_sel {
            (a.min(b).min(last_idx), a.max(b).min(last_idx), true)
        } else {
            let (_, _, first, last) = self.ordered();
            (first.min(last_idx), last.min(last_idx), false)
        };
        let hi_ext = self.list_subtree_end(hi_sel);
        let copies: Vec<Block> = (lo..=hi_ext).map(|i| { let mut b = self.doc.blocks[i].clone(); b.id = 0; b }).collect();
        if copies.is_empty() { return; }
        let (count, insert_at) = (copies.len(), hi_ext + 1);
        let before = self.snapshot();
        self.doc.splice_blocks(insert_at..insert_at, copies);
        let (new_lo, new_hi_sel) = (insert_at, insert_at + (hi_sel - lo));
        let b = &self.doc.blocks[new_lo];
        let offset = if had_block_sel || lo != hi_sel || is_atomic(b) { 0 } else { self.caret.head.offset.min(b.text.len()) };
        let pos = Position { block: b.id, offset };
        self.caret.anchor = pos; self.caret.head = pos;
        if had_block_sel { self.block_sel = Some((new_lo, new_hi_sel)); }
        self.record_changes(before, vec![ListChange { start: insert_at, before: 0, after: count }], true, cx);
        self.layouts.clear();
        self.list.scroll_to_reveal_item(new_hi_sel);
        window.focus(&self.focus); cx.notify();
    }

    /// Cmd+Alt+8: toggle code block on selected block(s).
    pub(super) fn set_code_shortcut(&mut self, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        let last_idx = self.doc.blocks.len().saturating_sub(1);
        let (first, last) = if let Some((a, b)) = self.block_sel {
            (a.min(b).min(last_idx), a.max(b).min(last_idx))
        } else { let (_, _, f, l) = self.ordered(); (f, l) };
        let all_code = (first..=last).all(|i| self.doc.blocks[i].meta.code_language.is_some());
        let before = self.snapshot();
        for index in first..=last {
            let block = &self.doc.blocks[index];
            if block.meta.table.is_some() || block.meta.raw_source.is_some() { continue; }
            let id = block.id;
            self.doc.set_kind(id, Kind::Paragraph);
            if !all_code {
                if let Some(b) = self.doc.get_mut(id) { b.meta.code_language = Some(String::new()); }
                self.doc.refresh_block(id);
            }
        }
        self.record(before, cx);
    }

    /// Cmd+Enter: toggle `checked` when on a Todo block (or Todo blocks in `block_sel`), else `exit_block`.
    pub(super) fn cmd_enter(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        if let Some((a, b)) = self.block_sel {
            let last_idx = self.doc.blocks.len().saturating_sub(1);
            let (lo, hi) = (a.min(b).min(last_idx), a.max(b).min(last_idx));
            if (lo..=hi).any(|i| self.doc.blocks[i].kind == Kind::Todo) {
                let before = self.snapshot();
                for i in lo..=hi {
                    if self.doc.blocks[i].kind == Kind::Todo { let id = self.doc.blocks[i].id; self.doc.toggle_checked(id); }
                }
                self.record(before, cx);
                return;
            }
        }
        let id = self.caret.head.block;
        if self.doc.get(id).is_some_and(|b| b.kind == Kind::Todo && b.meta.table.is_none() && b.meta.raw_source.is_none() && b.meta.code_language.is_none()) {
            self.commit_composition(cx);
            let before = self.snapshot();
            self.doc.toggle_checked(id);
            self.record(before, cx);
            return;
        }
        self.exit_block(window, cx);
    }

    /// Keys while an Esc block selection is active. Returns true when the key was consumed.
    pub(super) fn block_selection_key(&mut self, action: &EditorAction, window: &mut Window, cx: &mut Context<Self>) -> bool {
        use EditorAction::*;
        let Some((anchor, head)) = self.block_sel else { return false };
        let last = self.doc.blocks.len().saturating_sub(1);
        let (lo, hi) = (anchor.min(head).min(last), anchor.max(head).min(last));
        match action {
            Up | Down | SelectUp | SelectDown => {
                let up = matches!(action, Up | SelectUp);
                let extend = matches!(action, SelectUp | SelectDown);
                let next = if up { head.saturating_sub(1) } else { (head + 1).min(last) };
                self.block_sel = Some((if extend { anchor.min(last) } else { next }, next));
                let pos = Position { block: self.doc.blocks[next].id, offset: 0 };
                self.caret.anchor = pos; self.caret.head = pos;
                self.list.scroll_to_reveal_item(next); cx.notify(); true
            }
            Left | SelectLeft | Right | SelectRight => {
                self.block_sel = None;
                let to_end = matches!(action, Right | SelectRight);
                let b = &self.doc.blocks[if to_end { hi } else { lo }];
                let offset = if !to_end || is_atomic(b) { 0 } else { b.text.len() };
                self.set_head(Position { block: b.id, offset }, false, cx); true
            }
            Enter => {
                self.block_sel = None;
                let b = &self.doc.blocks[hi];
                let id = b.id;
                if lo == hi && is_atomic(b) { self.add_below(id, window, cx); }
                else { self.set_head(Position { block: id, offset: if is_atomic(b) { 0 } else { b.text.len() } }, false, cx); }
                true
            }
            NextCell | PreviousCell => {
                self.caret.anchor = Position { block: self.doc.blocks[lo].id, offset: 0 };
                self.caret.head = Position { block: self.doc.blocks[hi].id, offset: self.doc.blocks[hi].text.len() };
                self.tab_outside_table(matches!(action, NextCell), cx);
                self.block_sel = Some((anchor.min(last), head.min(last))); cx.notify(); true
            }
            Backspace | Delete | Cut | Copy => {
                if !matches!(action, Backspace | Delete) {
                    let blocks: Vec<Block> = (lo..=hi).map(|i| self.doc.blocks[i].clone()).collect();
                    let markdown = super::markdown::serialize(&Doc::from_blocks(blocks));
                    let plain = markdown.trim_end().to_owned();
                    self.block_clip = Some((plain.clone(), markdown));
                    cx.write_to_clipboard(ClipboardItem::new_string(plain));
                }
                if !matches!(action, Copy) {
                    self.block_sel = None;
                    let before = self.snapshot();
                    let n = hi - lo + 1;
                    self.doc.splice_blocks(lo..hi + 1, Vec::new());
                    let repaired = self.repair_column_groups();
                    let added = if self.doc.blocks.is_empty() {
                        self.doc.splice_blocks(0..0, vec![Block::new(0, Kind::Paragraph, String::new())]); 1
                    } else { repaired };
                    let at = lo.min(self.doc.blocks.len() - 1);
                    let pos = Position { block: self.doc.blocks[at].id, offset: 0 };
                    self.caret.anchor = pos; self.caret.head = pos;
                    self.record_changes(before, vec![ListChange { start: lo, before: n, after: added }], true, cx);
                    self.list.scroll_to_reveal_item(at);
                }
                cx.notify(); true
            }
            Paste => {
                self.block_sel = None;
                let before = self.snapshot();
                let n = hi - lo + 1;
                self.doc.splice_blocks(lo..hi + 1, vec![Block::new(0, Kind::Paragraph, String::new())]);
                let pos = Position { block: self.doc.blocks[lo].id, offset: 0 };
                self.caret.anchor = pos; self.caret.head = pos;
                self.record_changes(before, vec![ListChange { start: lo, before: n, after: 1 }], true, cx);
                false
            }
            SelectAll => { self.block_sel = Some((0, last)); cx.notify(); true }
            Cancel => { self.block_sel = None; cx.notify(); true }
            Undo | Redo | Save => { self.block_sel = None; false }
            _ => { self.block_sel = None; cx.notify(); false }
        }
    }
}
