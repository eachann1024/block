//! Keyboard shortcuts for block type (cmd-alt-0..9) and block editing boundaries.
use gpui::{Context, Window};
use super::{model::Kind, view::Editor};

impl Editor {
    pub(super) fn set_kind_shortcut(&mut self, kind: Kind, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        let last_idx = self.doc.blocks.len().saturating_sub(1);
        let (first, last) = if let Some((a, b)) = self.block_sel {
            (a.min(b).min(last_idx), a.max(b).min(last_idx))
        } else {
            let (_, _, f, l) = self.ordered();
            (f, l)
        };
        let toggle = matches!(kind, Kind::H1 | Kind::H2 | Kind::H3)
            && (first..=last).all(|i| {
                let b = &self.doc.blocks[i];
                b.kind == kind && b.meta.heading_level.is_none() && b.meta.code_language.is_none()
            });
        let target = if toggle { Kind::Paragraph } else { kind };
        let before = self.snapshot();
        for index in first..=last {
            let block = &self.doc.blocks[index];
            if block.meta.table.is_some() || block.meta.raw_source.is_some() { continue; }
            let id = block.id; self.doc.set_kind(id, target);
        }
        self.record(before, cx);
    }
    /// Cmd+Up / Cmd+Down: caret to the start of the first block or the end of the last one.
    pub(super) fn doc_edge(&mut self, end: bool, extend: bool, cx: &mut Context<Self>) {
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

fn is_list(block: &Block) -> bool { matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) }
fn is_divider(block: &Block) -> bool {
    block.meta.raw_source.is_some() && super::image_source::spec_of(block).is_none()
        && { let t = block.text.trim(); t.len() >= 3 && ["*", "-", "_"].iter().any(|c| t.chars().all(|v| v.to_string() == *c)) }
}
/// Blocks that must never be merged into or out of by Backspace/Delete.
fn structural(block: &Block) -> bool { block.meta.raw_source.is_some() || block.meta.code_language.is_some() }

impl Editor {
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
            self.doc.splice_blocks(0..1, vec![Block::new(0, Kind::Paragraph, String::new())]);
            let to = Position { block: self.doc.blocks[0].id, offset: 0 };
            self.caret.anchor = to; self.caret.head = to;
            self.record_changes(before, vec![ListChange { start: 0, before: 1, after: 1 }], true, cx);
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
        if block.meta.table.is_some() || structural(block) || block.meta.html_paragraph.is_some() { return false; }
        if block.kind == Kind::Quote {
            if self.caret.is_empty() && block.text.is_empty() { return false; }
            let at = self.caret.head;
            if self.caret.is_empty() && block.text.ends_with('\n') && at.offset == block.text.len() {
                let len = block.text.len();
                let before = self.snapshot();
                self.doc.replace_range(at.block, len - 1..len, "");
                self.doc.splice_blocks(index + 1..index + 1, vec![Block::new(0, Kind::Paragraph, String::new())]);
                let next = Position { block: self.doc.blocks[index + 1].id, offset: 0 };
                self.caret.anchor = next; self.caret.head = next;
                self.record_changes(before, vec![ListChange { start: index, before: 1, after: 2 }], true, cx);
                return true;
            }
            self.insert_in_block("\n", cx);
            return true;
        }
        if !self.caret.is_empty() { return false; }
        let at = self.caret.head;
        if block.text.is_empty() {
            if is_list(block) && block.meta.indent > 0 { self.tab_outside_table(false, cx); return true; }
            return false;
        }
        if at.offset != 0 { return false; }
        let list = is_list(block);
        let mut fresh = Block::new(0, if list { block.kind } else { Kind::Paragraph }, String::new());
        if list { fresh.meta.indent = block.meta.indent; fresh.meta.list_start = block.meta.list_start; }
        let id = block.id;
        let before = self.snapshot();
        if let Some(b) = self.doc.get_mut(id) { b.meta.list_start = None; }
        self.doc.refresh_block(id);
        self.doc.splice_blocks(index..index, vec![fresh]);
        self.record_changes(before, vec![ListChange { start: index, before: 1, after: 2 }], true, cx);
        true
    }
    /// Collapsed Backspace at a block start / Delete at a block end, before the generic merge.
    pub(super) fn boundary_rules(&mut self, forward: bool, cx: &mut Context<Self>) -> bool {
        let (_, _, ai, bi) = self.ordered();
        if ai == bi && self.doc.blocks.get(ai).and_then(|b| b.meta.table.as_ref())
            .is_some_and(|t| t.rows.iter().all(|r| r.cells.iter().all(|c| c.content.text.is_empty())))
        {
            self.remove_whole_block(ai, forward, cx);
            return true;
        }
        if !self.caret.is_empty() { return false; }
        let at = self.caret.head;
        let Some(index) = self.doc.index_of(at.block) else { return false };
        let block = self.doc.blocks[index].clone();
        if block.meta.table.is_some() { return false; }
        if is_divider(&block) {
            self.remove_whole_block(index, forward, cx);
            return true;
        }
        if forward {
            if at.offset != block.text.len() { return false; }
            let Some(next) = self.doc.blocks.get(index + 1).cloned() else { return false };
            if next.meta.table.is_some() { return false; }
            if structural(&block) { return true; }
            if !structural(&next) { return false; }
            if block.text.is_empty() && !is_divider(&next) && block.meta.html_paragraph.is_none() {
                self.drop_block(index, Position { block: next.id, offset: 0 }, cx);
            }
            return true;
        }
        if at.offset != 0 { return false; }
        if block.meta.code_language.is_some() {
            if block.text.is_empty() { self.demote(block.id, cx); }
            else if let Some(prev) = index.checked_sub(1).map(|i| self.doc.blocks[i].clone()) {
                self.set_head(Position { block: prev.id, offset: prev.text.len() }, false, cx);
            }
            return true;
        }
        if block.meta.raw_source.is_some() || block.meta.html_paragraph.is_some() { return false; }
        match block.kind {
            Kind::H1 | Kind::H2 | Kind::H3 | Kind::Quote => { self.demote(block.id, cx); return true; }
            Kind::Bullet | Kind::Numbered | Kind::Todo => {
                if block.meta.indent > 0 { self.tab_outside_table(false, cx); } else { self.demote(block.id, cx); }
                return true;
            }
            Kind::Paragraph => {}
        }
        let Some(prev) = index.checked_sub(1).map(|i| self.doc.blocks[i].clone()) else { return false };
        if prev.meta.table.is_some() || !structural(&prev) { return false; }
        if is_divider(&prev) {
            if block.text.is_empty() {
                let to = index.checked_sub(2).map(|i| &self.doc.blocks[i])
                    .filter(|b| b.meta.table.is_none() && !is_divider(b))
                    .map(|b| Position { block: b.id, offset: b.text.len() });
                if let Some(to) = to { self.drop_block(index, to, cx); }
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
        if self.doc.get(id).is_some_and(|b| b.meta.code_language.is_some()) {
            self.add_below(id, window, cx);
        } else if self.doc.get(id).is_some_and(|b| b.meta.table.is_some()) {
            self.replace(None, "\n", window, cx);
        } else if self.doc.get(id).is_some_and(|b|
            matches!(b.kind, Kind::Paragraph | Kind::Quote | Kind::Bullet | Kind::Numbered | Kind::Todo)
                && b.meta.raw_source.is_none() && super::image_source::spec_of(b).is_none()
        ) {
            self.insert_in_block("\n", cx);
        } else { self.handle(&super::view::EditorAction::Enter, window, cx); }
    }
    /// Deleting a selection that spans blocks keeps what is left of the end blocks as
    /// separate blocks (same type and nesting) instead of merging them into one.
    pub(super) fn delete_across(&mut self, cx: &mut Context<Self>) -> bool {
        if self.caret.is_empty() { return false; }
        let (a, b, ai, bi) = self.ordered();
        if ai == bi || !self.can_replace_selection() { return false; }
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
    /// Remember a copy that should paste back as blocks: several blocks, or one whole non-paragraph block.
    pub(super) fn remember_blocks(&mut self, plain: &str) {
        let (_, _, ai, bi) = self.ordered();
        let whole = |i: usize| self.selected_range(self.doc.blocks[i].id).is_some_and(|r| r == (0..self.doc.blocks[i].text.len()));
        let one = &self.doc.blocks[ai];
        let rich = ai != bi || (whole(ai) && (one.kind != Kind::Paragraph || one.meta.code_language.is_some()) && one.meta.table.is_none());
        if !rich { self.block_clip = None; return; }
        let blocks: Vec<Block> = (ai..=bi).filter_map(|i| {
            let block = &self.doc.blocks[i];
            self.selected_range(block.id).map(|r| if block.meta.raw_source.is_some() { block.clone() } else { block.slice(r) })
        }).collect();
        let markdown = super::markdown::serialize(&super::model::Doc::from_blocks(blocks));
        self.block_clip = Some((plain.to_owned(), markdown));
    }
    /// A selected image or HTML block has no text range: copy it whole so paste recreates the same block.
    pub(super) fn copy_whole_block(&mut self, cx: &mut Context<Self>) -> bool {
        let Some(block) = self.doc.get(self.caret.head.block).filter(|b| structural(b) && b.meta.code_language.is_none()).cloned() else { return false };
        let markdown = super::markdown::serialize(&super::model::Doc::from_blocks(vec![block]));
        let plain = markdown.trim_end().to_owned();
        self.block_clip = Some((plain.clone(), markdown));
        cx.write_to_clipboard(gpui::ClipboardItem::new_string(plain));
        true
    }
    /// Paste an in-app block copy back as blocks, splitting the caret's block when needed.
    pub(super) fn paste_blocks(&mut self, text: &str, cx: &mut Context<Self>) -> bool {
        let Some((plain, markdown)) = self.block_clip.clone() else { return false };
        if plain != text.replace("\r\n", "\n") { return false; }
        let mut blocks: Vec<Block> = super::markdown::parse(&markdown).blocks.iter().cloned().collect();
        let blank = |b: &Block| b.text.is_empty() && b.kind == Kind::Paragraph && b.meta.raw_source.is_none() && b.meta.table.is_none() && b.meta.code_language.is_none();
        while blocks.last().is_some_and(blank) { blocks.pop(); }
        while blocks.first().is_some_and(blank) { blocks.remove(0); }
        if blocks.is_empty() { return false; }
        if !self.caret.is_empty() {
            if !self.can_replace_selection() { return false; }
            if !self.delete_across(cx) { self.replace_selection("", false, cx); }
        }
        let at = self.caret.head;
        let Some(index) = self.doc.index_of(at.block) else { return false };
        let cur = self.doc.blocks[index].clone();
        if cur.meta.table.is_some() || structural(&cur) { return false; }
        let before = self.snapshot();
        for b in blocks.iter_mut() { b.id = 0; }
        let count = blocks.len();
        let mut pieces = Vec::new();
        let lead = if cur.text.is_empty() { 0 } else {
            let mut n = 0;
            if at.offset > 0 { pieces.push(cur.slice(0..at.offset)); n = 1; }
            n
        };
        pieces.extend(blocks);
        if !cur.text.is_empty() && at.offset < cur.text.len() {
            let mut tail = cur.slice(at.offset..cur.text.len());
            if at.offset > 0 { tail.id = 0; tail.meta.list_start = None; }
            pieces.push(tail);
        }
        let total = pieces.len();
        self.doc.splice_blocks(index..index + 1, pieces);
        let last = &self.doc.blocks[index + lead + count - 1];
        let to = Position { block: last.id, offset: last.text.len() };
        self.caret.anchor = to; self.caret.head = to;
        self.record_changes(before, vec![ListChange { start: index, before: 1, after: total }], true, cx);
        true
    }
}
