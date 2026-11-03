//! macOS standard text navigation (word/line edges) and word/line deletion.
use gpui::{Context, Window};
use unicode_segmentation::UnicodeSegmentation;
use super::view::{Editor, EditorAction, selection::{Position, boundary}};

fn is_word(seg: &str) -> bool {
    seg.chars().any(|c| c.is_alphanumeric() || c == '_')
}

impl Editor {
    /// Bounds of the current cell if `block` is a table, else the whole block text range.
    fn active_slice_bounds(&self, block: u64, offset: usize) -> (usize, usize) {
        if let Some(layout) = self.layouts.get(&block).filter(|l| !l.cells.is_empty()) {
            if let Some(cell) = layout.cells.iter().find(|c| c.range.start <= offset && offset <= c.range.end) {
                return (cell.range.start, cell.range.end);
            }
        }
        let len = self.doc.get(block).map_or(0, |b| b.text.len());
        (0, len)
    }

    fn logical_line_bounds(&self, block: u64, offset: usize) -> (usize, usize) {
        let Some(b) = self.doc.get(block) else { return (0, 0) };
        let (min, max) = self.active_slice_bounds(block, offset);
        let pos = boundary(&b.text, offset).clamp(min, max);
        let start = b.text[min..pos].rfind('\n').map_or(min, |i| min + i + 1);
        let end = b.text[pos..max].find('\n').map_or(max, |i| pos + i);
        (start, end)
    }

    fn line_target(&self, to_end: bool, logical: bool) -> Position {
        let at = self.caret.head;
        let Some(block) = self.doc.get(at.block) else { return at };
        if super::image_source::spec_of(block).is_some() || super::block_menu::is_divider(block) {
            return Position { block: at.block, offset: 0 };
        }
        let pos = boundary(&block.text, at.offset);
        let (log_start, log_end) = self.logical_line_bounds(at.block, pos);
        if !logical && let Some(layout) = self.layouts.get(&at.block) {
            let (vis_start, vis_end) = layout.visual_line_bounds(pos);
            let vis_start = boundary(&block.text, vis_start).clamp(log_start, log_end);
            let vis_end = boundary(&block.text, vis_end).clamp(log_start, log_end);
            let offset = if to_end {
                if pos < vis_end { vis_end } else { log_end }
            } else if pos > vis_start {
                vis_start
            } else {
                log_start
            };
            Position { block: at.block, offset }
        } else {
            Position { block: at.block, offset: if to_end { log_end } else { log_start } }
        }
    }

    /// Cmd+Left / Cmd+Right / Ctrl+A / Ctrl+E (with optional Shift selection extension).
    pub(super) fn line_edge(&mut self, to_end: bool, extend: bool, logical: bool, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        self.pending_vertical = None;
        self.preferred_x = None;
        if let Some((a, b)) = self.block_sel.take() {
            let idx = if to_end { a.max(b) } else { a.min(b) }.min(self.doc.blocks.len().saturating_sub(1));
            let blk = &self.doc.blocks[idx];
            let off = if !to_end || super::image_source::spec_of(blk).is_some() || super::block_menu::is_divider(blk) { 0 } else { blk.text.len() };
            self.set_head(Position { block: blk.id, offset: off }, false, cx);
            return;
        }
        if !extend && !self.caret.is_empty() && self.caret.anchor.block == self.caret.head.block {
            let (a, b, _, _) = self.ordered();
            let (log_a, log_b) = self.logical_line_bounds(a.block, a.offset);
            if b.offset <= log_b && a.offset >= log_a {
                self.caret.head = if to_end { b } else { a };
            }
        }
        let target = self.line_target(to_end, logical);
        self.set_head(target, extend, cx);
    }

    fn word_step_in_slice(slice: &str, rel_offset: usize, forward: bool) -> usize {
        if forward {
            let tail = &slice[rel_offset..];
            for (idx, seg) in tail.split_word_bound_indices() {
                if is_word(seg) {
                    return rel_offset + idx + seg.len();
                }
            }
            slice.len()
        } else {
            let head = &slice[..rel_offset];
            for (idx, seg) in head.split_word_bound_indices().rev() {
                if is_word(seg) {
                    return idx;
                }
            }
            0
        }
    }

    pub(super) fn word_step(&self, forward: bool) -> Position {
        let at = self.caret.head;
        let Some(block) = self.doc.get(at.block) else { return at };
        if super::image_source::spec_of(block).is_some() || super::block_menu::is_divider(block) {
            return self.step(forward);
        }
        let pos = boundary(&block.text, at.offset);
        let (min, max) = self.active_slice_bounds(at.block, pos);
        let clamped = pos.clamp(min, max);
        if (forward && clamped < max) || (!forward && clamped > min) {
            let next = min + Self::word_step_in_slice(&block.text[min..max], clamped - min, forward);
            return Position { block: at.block, offset: next };
        }
        self.step(forward)
    }

    /// Option+Left / Option+Right (with optional Shift selection extension).
    pub(super) fn move_word(&mut self, forward: bool, extend: bool, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        self.pending_vertical = None;
        self.preferred_x = None;
        if let Some((a, b)) = self.block_sel.take() {
            let idx = if forward { a.max(b) } else { a.min(b) }.min(self.doc.blocks.len().saturating_sub(1));
            let blk = &self.doc.blocks[idx];
            let off = if !forward || super::image_source::spec_of(blk).is_some() || super::block_menu::is_divider(blk) { 0 } else { blk.text.len() };
            self.set_head(Position { block: blk.id, offset: off }, false, cx);
            return;
        }
        let at = if !extend && !self.caret.is_empty() {
            let (a, b, _, _) = self.ordered();
            if forward { b } else { a }
        } else {
            self.word_step(forward)
        };
        self.set_head(at, extend, cx);
    }

    /// Option+Backspace / Option+Delete: delete one word backward or forward.
    pub(super) fn delete_word(&mut self, forward: bool, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        let fallback = if forward { EditorAction::Delete } else { EditorAction::Backspace };
        if self.block_sel.is_some() || !self.caret.is_empty() || self.focused_image().is_some() {
            self.handle(&fallback, window, cx);
            return;
        }
        let at = self.caret.head;
        let Some(block) = self.doc.get(at.block) else { return };
        if super::block_menu::is_divider(block) {
            self.handle(&fallback, window, cx);
            return;
        }
        let pos = boundary(&block.text, at.offset);
        let (min, max) = self.active_slice_bounds(at.block, pos);
        let clamped = pos.clamp(min, max);
        if (!forward && clamped == min) || (forward && clamped == max) {
            self.handle(&fallback, window, cx);
            return;
        }
        let target = min + Self::word_step_in_slice(&block.text[min..max], clamped - min, forward);
        let before_caret = self.caret.clone();
        self.caret.head = Position { block: at.block, offset: target };
        if !self.can_replace_selection() {
            self.caret = before_caret;
            self.table_edit_rejected(cx);
        } else {
            self.replace_selection("", false, cx);
        }
    }

    /// Cmd+Backspace / Cmd+Delete / Ctrl+K: delete to visual/logical line start or end.
    pub(super) fn delete_line(&mut self, to_end: bool, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        let fallback = if to_end { EditorAction::Delete } else { EditorAction::Backspace };
        if self.block_sel.is_some() || !self.caret.is_empty() || self.focused_image().is_some() {
            self.handle(&fallback, window, cx);
            return;
        }
        let at = self.caret.head;
        let target = self.line_target(to_end, false);
        if target.offset == at.offset {
            self.handle(&fallback, window, cx);
            return;
        }
        let before_caret = self.caret.clone();
        self.caret.head = target;
        if !self.can_replace_selection() {
            self.caret = before_caret;
            self.table_edit_rejected(cx);
        } else {
            self.replace_selection("", false, cx);
        }
    }
}
