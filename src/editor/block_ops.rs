//! Notion-parity block operations: subtree-aware move, duplicate, code/menu shortcuts, and Esc block selection keys.
use gpui::{Context, Pixels, Point, Window, px};
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
    fn insert_paragraph_at(&mut self, index: usize, cx: &mut Context<Self>) -> bool {
        if self.path.is_none() || index > self.doc.blocks.len() { return false; }
        self.commit_composition(cx);
        let before = self.snapshot();
        self.doc.splice_blocks(index..index, vec![Block::new(0, Kind::Paragraph, String::new())]);
        let new_id = self.doc.blocks[index].id;
        self.table_sel = None; self.table_menu = None; self.block_sel = None; self.preferred_x = None;
        self.record_changes(before, vec![ListChange { start: index, before: 0, after: 1 }], true, cx);
        // Appending inside a fold must focus the visible tail, never a hidden blank or the opened section.
        if !self.show_hidden_tail(new_id) { cx.notify(); return true; }
        let index = self.doc.index_of(new_id).unwrap_or(index);
        self.ensure_block_visible(new_id);
        self.set_head(Position { block: new_id, offset: 0 }, false, cx);
        self.layouts.clear(); self.list.scroll_to_reveal_item(index);
        true
    }
    /// Insert a new plain paragraph directly above `id` and focus it.
    pub(in crate::editor) fn insert_above(&mut self, id: u64, cx: &mut Context<Self>) -> bool {
        let Some(index) = self.doc.index_of(id) else { return false };
        self.insert_paragraph_at(index, cx)
    }
    /// 聚焦顶部已有的普通空段落，否则在索引 0 插入空段落。
    pub(in crate::editor) fn insert_top_paragraph(&mut self, cx: &mut Context<Self>) -> bool {
        let Some(first) = self.doc.blocks.first().filter(|_| self.path.is_some()) else { return false };
        if first.kind == Kind::Paragraph && first.text.is_empty() && first.meta.table.is_none()
            && first.meta.code_language.is_none() && first.meta.raw_source.is_none() && first.meta.html_paragraph.is_none() && !is_atomic(first) {
            let id = first.id; self.table_sel = None; self.table_menu = None; self.block_sel = None; self.preferred_x = None;
            self.set_head(Position { block: id, offset: 0 }, false, cx); self.list.scroll_to_reveal_item(0); return true;
        }
        self.insert_paragraph_at(0, cx)
    }
    /// 在文档末尾之后插入空段落并聚焦。折叠区一直延伸到文末时，新行成为可见尾，
    /// 原有正文和已经藏在尾部的空段落保持折叠，不会被聚焦或展开。
    pub(in crate::editor) fn insert_bottom_paragraph(&mut self, cx: &mut Context<Self>) -> bool {
        !self.doc.blocks.is_empty() && self.insert_paragraph_at(self.doc.blocks.len(), cx)
    }
    /// 在 `hit()` 吸附前处理文档顶部（64px）与底部（>=160px）留白点击插入。
    pub(in crate::editor) fn try_blank_margin_insert(&mut self, pos: Point<Pixels>, window: &mut Window, cx: &mut Context<Self>) -> bool {
        if self.minimal || self.path.is_none() || !self.is_markdown_note() || self.editing_source() || self.doc.blocks.is_empty() { return false; }
        // GPUI `List::bounds_for_item` 相对 viewport 顶部计算，不含列表自身的 `pt(px(64.))`。
        let (top_pad, first_id) = (px(64.), self.doc.blocks[0].id);
        let first_top = self.list.bounds_for_item(0).map(|b| b.top() + top_pad)
            .or_else(|| (self.list.logical_scroll_top().item_ix == 0).then(|| self.layouts.get(&first_id).map(|l| l.bounds.top())).flatten());
        if first_top.is_some_and(|top_y| pos.y < top_y) && self.insert_top_paragraph(cx) {
            self.selecting = false; self.toolbar_anchor = None; window.focus(&self.focus, cx); return true;
        }
        let Some(last_idx) = self.last_visible_index() else { return false };
        let last_id = self.doc.blocks[last_idx].id;
        let row_b = self.list.bounds_for_item(last_idx).map(|b| b.bottom() + top_pad);
        let lay_b = self.layouts.get(&last_id).map(|l| l.bounds.bottom());
        let last_bottom = row_b.zip(lay_b).map(|(a, b)| a.max(b)).or(row_b).or(lay_b);
        if last_bottom.is_some_and(|bottom_y| pos.y > bottom_y) && self.insert_bottom_paragraph(cx) {
            self.selecting = false; self.toolbar_anchor = None; window.focus(&self.focus, cx); return true;
        }
        false
    }
    pub(super) fn sync_reveal_after_paint(&mut self, pad: (f32, f32)) {
        if !self.reveal_caret { return; }
        self.reveal_caret = false;
        if self.minimal { return; }
        if self.doc.index_of(self.caret.head.block) == Some(0) && self.caret.head.offset == 0 {
            self.list.scroll_to_reveal_item(0);
        } else if let Some(last_idx) = self.last_visible_index().filter(|&i| self.doc.blocks[i].id == self.caret.head.block)
            && self.list.bounds_for_item(last_idx).is_some_and(|b| b.size.height <= self.list.viewport_bounds().size.height - px(pad.0 + pad.1))
        {
            self.list.scroll_to_reveal_item(last_idx);
        }
    }
    /// End index (inclusive) of `start`'s nested list subtree.
    pub(super) fn list_subtree_end(&self, start: usize) -> usize {
        let Some(head) = self.doc.blocks.get(start).filter(|b| is_list(b)) else { return start };
        let base = head.meta.indent;
        let mut end = start;
        while end + 1 < self.doc.blocks.len() && is_list(&self.doc.blocks[end + 1])
            && self.doc.blocks[end + 1].meta.indent > base {
            end += 1;
        }
        end
    }

    /// Move `lo..=hi` (plus any nested list children of `hi`) up or down one logical block/subtree.
    pub(super) fn move_block_range(&mut self, lo: usize, hi: usize, up: bool, cx: &mut Context<Self>) -> Option<(usize, usize)> {
        let last_idx = self.doc.blocks.len().saturating_sub(1);
        let (lo, hi_sel) = (lo.min(last_idx), hi.min(last_idx));
        let hi_ext = self.list_subtree_end(hi_sel);
        let n = hi_ext - lo + 1;
        let to = if up {
            if lo == 0 { return None; }
            let first = &self.doc.blocks[lo];
            if is_list(first) && is_list(&self.doc.blocks[lo - 1]) && self.doc.blocks[lo - 1].meta.indent > first.meta.indent {
                let mut p = lo - 1;
                while p > 0 && is_list(&self.doc.blocks[p]) && self.doc.blocks[p].meta.indent > first.meta.indent { p -= 1; }
                p
            } else { lo - 1 }
        } else {
            if hi_ext + 1 >= self.doc.blocks.len() { return None; }
            self.list_subtree_end(hi_ext + 1) + 1 - n
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
        if !self.is_markdown_note() { return; }
        if let Some((anchor, head)) = self.block_sel {
            if let Some((new_lo, new_hi)) = self.move_block_range(anchor.min(head), anchor.max(head), up, cx) {
                let next_sel = if anchor <= head { (new_lo, new_hi) } else { (new_hi, new_lo) };
                self.block_sel = Some(next_sel);
                let pos = Position { block: self.doc.blocks[next_sel.1].id, offset: 0 };
                self.caret.anchor = pos; self.caret.head = pos;
                window.focus(&self.focus, cx); cx.notify();
            }
            return;
        }
        let (_, _, first, last) = self.ordered();
        if self.move_block_range(first, last, up, cx).is_some() {
            self.reveal_caret = true; window.focus(&self.focus, cx); cx.notify();
        }
    }

    /// Cmd+D: duplicate the active block(s) (and any nested list children) directly below.
    pub(super) fn duplicate_blocks(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.is_markdown_note() { return; }
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
        window.focus(&self.focus, cx); cx.notify();
    }

    /// Cmd+Alt+8: toggle code block on selected block(s).
    pub(super) fn set_code_shortcut(&mut self, cx: &mut Context<Self>) {
        if !self.is_markdown_note() { return; }
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
        if !self.is_markdown_note() { return; }
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
                let head = head.min(last);
                let next = self.next_visible_index(head, !up).unwrap_or(head);
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
                    cx.write_to_clipboard(super::paste_clipboard::block_item(plain.clone(), &markdown));
                    self.block_clip = Some((plain, markdown));
                }
                if !matches!(action, Copy) {
                    self.block_sel = None;
                    let before = self.snapshot();
                    let n = hi - lo + 1;
                    self.doc.splice_blocks(lo..hi + 1, Vec::new());
                    let added = if self.doc.blocks.is_empty() {
                        self.doc.splice_blocks(0..0, vec![Block::new(0, Kind::Paragraph, String::new())]); 1
                    } else { 0 };
                    let at = lo.min(self.doc.blocks.len() - 1);
                    let pos = Position { block: self.doc.blocks[at].id, offset: 0 };
                    self.caret.anchor = pos; self.caret.head = pos;
                    self.record_changes(before, vec![ListChange { start: lo, before: n, after: added }], true, cx);
                    self.list.scroll_to_reveal_item(at);
                }
                cx.notify(); true
            }
            Paste => false,
            SelectAll => {
                self.block_sel = Some((0, last));
                self.caret.anchor = Position { block: self.doc.blocks[0].id, offset: 0 };
                let end = &self.doc.blocks[last];
                self.caret.head = Position { block: end.id, offset: end.text.len() };
                cx.notify(); true
            }
            Cancel => { self.block_sel = None; cx.notify(); true }
            Undo | Redo => false,
            Save => { self.block_sel = None; false }
            _ => { self.block_sel = None; cx.notify(); false }
        }
    }
}
