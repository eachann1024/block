//! Keyboard shortcuts for block type (cmd-alt-0..9) and block editing boundaries.
use gpui::{Context, Window};
use super::{model::Kind, view::Editor};

impl Editor {
    pub(super) fn set_kind_shortcut(&mut self, kind: Kind, cx: &mut Context<Self>) {
        if !self.is_markdown_note() { return; }
        self.commit_composition(cx);
        let last_idx = self.doc.blocks.len().saturating_sub(1);
        let (first, last) = match self.block_sel {
            Some((a, b)) => (a.min(b).min(last_idx), a.max(b).min(last_idx)),
            None => { let (_, _, f, l) = self.ordered(); (f, l) }
        };
        let toggle = matches!(kind, Kind::H1 | Kind::H2 | Kind::H3) && (first..=last).all(|i| {
            let b = &self.doc.blocks[i];
            b.kind == kind && b.meta.heading_level.is_none() && b.meta.code_language.is_none() && b.meta.callout.is_none()
        });
        let target = if toggle { Kind::Paragraph } else { kind };
        let before = self.snapshot();
        for index in first..=last {
            let block = &self.doc.blocks[index];
            if block.meta.table.is_some() || block.meta.raw_source.is_some() || block.meta.code_language.is_some() { continue; }
            let id = block.id; self.doc.set_kind(id, target);
        }
        self.record(before, cx);
    }
    /// Cmd+Up / Cmd+Down: caret to the start of the first block or the end of the last one.
    pub(super) fn doc_edge(&mut self, end: bool, extend: bool, cx: &mut Context<Self>) {
        if self.editing_source() { self.finish_source_composition(cx); self.source_doc_edge(end, extend, cx); return; }
        if !self.is_markdown_note() { return; }
        let block = if end { self.last_visible_index().and_then(|i| self.doc.blocks.get(i)) } else { self.doc.blocks.first() };
        let Some(at) = block.map(|b| super::view::selection::Position { block: b.id, offset: if end { b.text.len() } else { 0 } }) else { return };
        self.table_sel = None; self.preferred_x = None; self.block_sel = None;
        self.set_head(at, extend, cx);
        // The first frame only brings the far block into view; once it has a layout, reveal the caret exactly.
        cx.spawn(async move |this, cx| {
            for _ in 0..2 {
                cx.background_executor().timer(std::time::Duration::from_millis(40)).await;
                if this.update(cx, |this, cx| { this.reveal_caret = true; cx.notify(); }).is_err() { return; }
            }
        }).detach();
    }
}

use super::{input::transactions::ListChange, model::Block, view::selection::Position};

thread_local! { static FIRST_LINE_ARMED: std::cell::RefCell<Option<u64>> = const { std::cell::RefCell::new(None) }; }
pub(in crate::editor) fn clear_first_line_delete() { FIRST_LINE_ARMED.with(|slot| *slot.borrow_mut() = None); }

fn is_list(block: &Block) -> bool { matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) }
fn is_divider(block: &Block) -> bool {
    super::image_source::spec_of(block).is_none() && super::block_menu::is_divider(block)
}
/// Blocks that must never be merged into or out of by Backspace/Delete.
fn structural(block: &Block) -> bool { block.meta.raw_source.is_some() || block.meta.code_language.is_some() }

impl Editor {
    pub(in crate::editor) fn can_arm_first_line_delete(&self, id: u64) -> bool {
        !self.minimal && self.path.is_some() && self.is_markdown_note() && !self.editing_source()
            && self.doc.blocks.len() > 1 && self.caret.is_empty() && self.caret.composing.is_none()
            && self.block_sel.is_none() && self.table_sel.is_none() && self.caret.head == (Position { block: id, offset: 0 })
            && self.doc.blocks.first().is_some_and(|b| b.id == id && b.kind == Kind::Paragraph && b.text.is_empty()
                && b.meta.indent == 0 && b.meta.code_language.is_none() && b.meta.table.is_none()
                && b.meta.raw_source.is_none() && b.meta.callout.is_none() && b.meta.html_paragraph.is_none())
    }
    pub(in crate::editor) fn is_first_line_delete_armed(&self, id: u64) -> bool {
        FIRST_LINE_ARMED.with(|slot| *slot.borrow() == Some(id)) && self.can_arm_first_line_delete(id)
    }
    pub(in crate::editor) fn confirm_first_line_delete(&mut self, id: u64, cx: &mut Context<Self>) -> bool {
        clear_first_line_delete();
        if !self.can_arm_first_line_delete(id) { return false; }
        let Some(next) = self.doc.blocks.get(1).cloned() else { return false };
        self.drop_block(0, Position { block: next.id, offset: 0 }, cx);
        true
    }
    fn drop_block(&mut self, index: usize, to: Position, cx: &mut Context<Self>) {
        let before = self.snapshot();
        self.doc.splice_blocks(index..index + 1, Vec::new());
        self.caret.anchor = to; self.caret.head = to;
        self.record_changes(before, vec![ListChange { start: index, before: 1, after: 0 }], true, cx);
    }
    fn demote(&mut self, id: u64, cx: &mut Context<Self>) {
        let before = self.snapshot(); self.doc.set_kind(id, Kind::Paragraph); self.record(before, cx);
    }
    fn remove_whole_block(&mut self, index: usize, forward: bool, cx: &mut Context<Self>) {
        self.table_sel = None; self.table_menu = None;
        if self.doc.blocks.len() <= 1 {
            let before = self.snapshot();
            let empty = Block::new(0, Kind::Paragraph, String::new());
            self.doc.splice_blocks(index..index + 1, vec![empty]);
            let to = Position { block: self.doc.blocks[index].id, offset: 0 };
            self.caret.anchor = to; self.caret.head = to;
            self.record_changes(before, vec![ListChange { start: index, before: 1, after: 1 }], true, cx);
            return;
        }
        let prev = index.checked_sub(1).map(|i| self.doc.blocks[i].clone());
        let next = self.doc.blocks.get(index + 1).cloned();
        let to = match (forward, prev, next) {
            (true, _, Some(n)) | (false, None, Some(n)) => Position { block: n.id, offset: 0 },
            (_, Some(p), _) => {
                let atomic = super::image_source::spec_of(&p).is_some() || is_divider(&p) || p.meta.table.is_some();
                Position { block: p.id, offset: if atomic { 0 } else { p.text.len() } }
            }
            _ => return,
        };
        self.drop_block(index, to, cx);
    }
    /// Enter on a nested empty list item outdents it; in a non-empty quote it inserts an in-block
    /// newline (or exits on a trailing empty line); at offset 0 of other blocks it inserts above.
    pub(super) fn enter_rules(&mut self, cx: &mut Context<Self>) -> bool {
        let (_, _, ai, bi) = self.ordered();
        if ai != bi { return false; }
        let index = ai;
        let block = &self.doc.blocks[index];
        if block.meta.table.is_some() || structural(block) { return false; }
        if block.kind == Kind::Quote {
            if self.caret.is_empty() && block.text.is_empty() { return false; }
            let at = self.caret.head;
            if self.caret.is_empty() && block.text.ends_with('\n') && at.offset == block.text.len() {
                let (len, before) = (block.text.len(), self.snapshot());
                self.doc.replace_range(at.block, len - 1..len, "");
                let next_blk = Block::new(0, Kind::Paragraph, String::new());
                self.doc.splice_blocks(index + 1..index + 1, vec![next_blk]);
                let next = Position { block: self.doc.blocks[index + 1].id, offset: 0 };
                self.caret.anchor = next; self.caret.head = next;
                self.record_changes(before, vec![ListChange { start: index, before: 1, after: 2 }], true, cx);
                return true;
            }
            self.insert_in_block("\n", cx); return true;
        }
        if !self.caret.is_empty() { return false; }
        let at = self.caret.head;
        if block.text.is_empty() {
            return if is_list(block) && block.meta.indent > 0 { self.tab_outside_table(false, cx); true } else { false };
        }
        if at.offset != 0 { return false; }
        let list = is_list(block);
        let mut fresh = Block::new(0, if list { block.kind } else { Kind::Paragraph }, String::new());
        if list || block.kind == Kind::Paragraph { fresh.meta.indent = block.meta.indent; }
        if list { fresh.meta.list_start = block.meta.list_start; }
        let (id, before) = (block.id, self.snapshot());
        if let Some(b) = self.doc.get_mut(id) { b.meta.list_start = None; }
        self.doc.refresh_block(id); self.doc.splice_blocks(index..index, vec![fresh]);
        self.record_changes(before, vec![ListChange { start: index, before: 1, after: 2 }], true, cx);
        true
    }
    /// Collapsed Backspace at a block start / Delete at a block end, before the generic merge.
    pub(super) fn boundary_rules(&mut self, forward: bool, cx: &mut Context<Self>) -> bool {
        let (_, _, ai, bi) = self.ordered();
        if ai == bi && let Some(t) = self.doc.blocks.get(ai).and_then(|b| b.meta.table.as_ref()) {
            if t.rows.iter().all(|r| r.cells.iter().all(|c| c.content.text.is_empty())) {
                self.remove_whole_block(ai, forward, cx); return true;
            }
        }
        if !self.caret.is_empty() { return false; }
        let at = self.caret.head;
        let Some(index) = self.doc.index_of(at.block) else { return false };
        let block = self.doc.blocks[index].clone();
        let top_of_flow = index == 0;
        if block.meta.table.is_some() { return !forward && top_of_flow && at.offset == 0 && self.insert_above(block.id, cx); }
        if is_divider(&block) { self.remove_whole_block(index, forward, cx); return true; }
        if forward {
            if at.offset != block.text.len() { return false; }
            let Some(next) = self.doc.blocks.get(index + 1).cloned() else { return false };
            if next.meta.table.is_some() { return false; }
            if structural(&block) { return true; }
            if !structural(&next) {
                if !(block.text.is_empty() && block.kind == Kind::Paragraph && block.meta.html_paragraph.is_none()) { return false; }
                self.drop_block(index, Position { block: next.id, offset: 0 }, cx); return true;
            }
            if block.text.is_empty() && !is_divider(&next) && block.meta.html_paragraph.is_none() {
                self.drop_block(index, Position { block: next.id, offset: 0 }, cx);
            }
            return true;
        }
        if at.offset != 0 { return false; }
        if block.meta.code_language.is_some() {
            if block.text.is_empty() { self.demote(block.id, cx); }
            else if top_of_flow { self.insert_above(block.id, cx); }
            else if let Some(prev) = index.checked_sub(1).map(|i| self.doc.blocks[i].clone()) {
                self.set_head(Position { block: prev.id, offset: prev.text.len() }, false, cx);
            }
            return true;
        }
        if block.meta.raw_source.is_some() || block.meta.html_paragraph.is_some() { return top_of_flow && self.insert_above(block.id, cx); }
        match block.kind {
            Kind::H1 | Kind::H2 | Kind::H3 | Kind::Quote => { self.demote(block.id, cx); return true; }
            Kind::Bullet | Kind::Numbered | Kind::Todo => {
                if block.meta.indent > 0 { self.tab_outside_table(false, cx); } else { self.demote(block.id, cx); }
                return true;
            }
            Kind::Paragraph if block.meta.indent > 0 => { self.tab_outside_table(false, cx); return true; }
            Kind::Paragraph => {}
        }
        if top_of_flow && self.can_arm_first_line_delete(block.id) {
            if self.is_first_line_delete_armed(block.id) { return self.confirm_first_line_delete(block.id, cx); }
            FIRST_LINE_ARMED.with(|slot| *slot.borrow_mut() = Some(block.id));
            cx.notify(); return true;
        }
        let Some(prev) = index.checked_sub(1).map(|i| self.doc.blocks[i].clone()) else { return false };
        if prev.meta.table.is_some() || !structural(&prev) { return false; }
        if is_divider(&prev) {
            if block.text.is_empty() && let Some(to) = index.checked_sub(2).map(|i| &self.doc.blocks[i])
                .filter(|b| b.meta.table.is_none() && !is_divider(b))
                .map(|b| Position { block: b.id, offset: b.text.len() }) {
                self.drop_block(index, to, cx);
            }
            return true;
        }
        let to = Position { block: prev.id, offset: prev.text.len() };
        if block.text.is_empty() { self.drop_block(index, to, cx); } else { self.set_head(to, false, cx); }
        true
    }
    /// Shift/Cmd+Enter: leave a code block for a new paragraph below; soft break in paragraph/quote/list.
    pub(super) fn exit_block(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        let id = self.caret.head.block;
        if self.doc.get(id).is_some_and(|b| b.meta.code_language.is_some()) { self.add_below(id, window, cx); }
        else if self.doc.get(id).is_some_and(|b| b.meta.table.is_some()) { self.replace(None, "\n", window, cx); }
        else if self.doc.get(id).is_some_and(|b| matches!(b.kind, Kind::Paragraph | Kind::Quote | Kind::Bullet | Kind::Numbered | Kind::Todo) && b.meta.raw_source.is_none() && super::image_source::spec_of(b).is_none()) { self.insert_in_block("\n", cx); }
        else { self.handle(&super::view::EditorAction::Enter, window, cx); }
    }
    /// Deleting a selection that spans blocks keeps what is left of the end blocks as
    /// separate blocks (same type and nesting) instead of merging them into one.
    pub(super) fn delete_across(&mut self, cx: &mut Context<Self>) -> bool {
        if self.caret.is_empty() { return false; }
        let (a, b, ai, bi) = self.ordered();
        if ai == bi || !self.can_replace_selection() { return false; }
        if self.crosses_hidden_fold(ai, bi) && !self.selection_covers_doc(a, b, ai, bi) { return false; }
        let before = self.snapshot();
        let first = self.doc.blocks[ai].clone(); let last = self.doc.blocks[bi].clone();
        // Source-backed blocks (images, dividers, HTML) are never cut in half.
        let head = (a.offset > 0).then(|| if first.meta.raw_source.is_some() { first.clone() } else { first.slice(0..a.offset) });
        let tail = (b.offset < last.text.len()).then(|| if last.meta.raw_source.is_some() { last.clone() } else { last.slice(b.offset..last.text.len()) });
        let mut pieces = Vec::new();
        let at = match (&head, &tail) {
            (Some(h), _) => Position { block: h.id, offset: h.text.len() },
            (None, Some(t)) => Position { block: t.id, offset: 0 },
            (None, None) => {
                let empty = if first.meta.table.is_some() || first.meta.raw_source.is_some() {
                    Block::new(first.id, Kind::Paragraph, String::new())
                } else { first.slice(0..0) };
                pieces.push(empty);
                Position { block: first.id, offset: 0 }
            }
        };
        pieces.extend(head); pieces.extend(tail);
        self.doc.splice_blocks(ai..bi + 1, pieces);
        self.caret.anchor = at; self.caret.head = at;
        self.record(before, cx);
        true
    }
    /// Cross-block selections and whole atomic content retain their block structure.
    /// Selecting all characters in a heading/list is still a text selection.
    pub(super) fn remember_blocks(&mut self) {
        let (_, _, ai, bi) = self.ordered();
        let whole = |i: usize| self.selected_range(self.doc.blocks[i].id).is_some_and(|r| r == (0..self.doc.blocks[i].text.len()));
        let one = &self.doc.blocks[ai];
        let rich = ai != bi || (whole(ai) && (one.meta.raw_source.is_some() || one.meta.table.is_some()));
        if !rich { self.block_clip = None; return; }
        let blocks: Vec<Block> = (ai..=bi).filter_map(|i| {
            let block = &self.doc.blocks[i];
            self.selected_range(block.id).map(|r| {
                if block.meta.raw_source.is_some() || r == (0..block.text.len()) {
                    block.clone()
                } else {
                    block.slice(r)
                }
            })
        }).collect();
        let markdown = super::markdown::serialize(&super::model::Doc::from_blocks(blocks));
        let plain = markdown.trim_end_matches('\n').to_owned();
        self.block_clip = Some((plain, markdown));
    }
    /// A selected image or HTML block has no text range: copy it whole so paste recreates the same block.
    pub(super) fn copy_whole_block(&mut self, cx: &mut Context<Self>) -> bool {
        let Some(block) = self.doc.get(self.caret.head.block).filter(|b| structural(b) && b.meta.code_language.is_none()).cloned() else { return false };
        let markdown = super::markdown::serialize(&super::model::Doc::from_blocks(vec![block]));
        let plain = markdown.trim_end().to_owned();
        cx.write_to_clipboard(super::paste_clipboard::block_item(plain.clone(), &markdown));
        self.block_clip = Some((plain, markdown));
        true
    }
}
