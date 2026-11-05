//! Caret just outside an inline code chip. A collapsed caret at a chip edge has two places:
//! inside the chip (typing extends the code) or outside it (typing is plain text).
//! → at the chip's end and ← at its start step outside without moving the offset.
use std::cell::RefCell;
use gpui::{Context, Pixels, Point, px};
use super::view::{Editor, layout::TextLayout, selection::Position};

thread_local! {
    /// (editor, caret position, at chip end): valid only while the caret stays exactly there.
    static OUT: RefCell<Option<(gpui::EntityId, Position, bool)>> = const { RefCell::new(None) };
}

pub(in crate::editor) fn clear() { OUT.with(|o| *o.borrow_mut() = None); }

/// Some(true) when the caret sits outside a chip's end, Some(false) outside its start.
pub(in crate::editor) fn side(editor: gpui::EntityId, at: Position) -> Option<bool> {
    OUT.with(|o| o.borrow().as_ref().filter(|(e, p, _)| *e == editor && *p == at).map(|(_, _, end)| *end))
}

/// Caret point drawn outside the chip edge, past its padding and border.
pub(in crate::editor) fn caret_point(layout: &TextLayout, p: Point<Pixels>, at_end: bool) -> Point<Pixels> {
    let shift = super::inline_code_chip::metrics(layout).pad + px(2.);
    Point { x: if at_end { p.x + shift } else { p.x - shift }, y: p.y }
}

impl Editor {
    /// The chip edge the collapsed caret sits on: Some(true) at an end, Some(false) at a start.
    fn code_edge(&self) -> Option<bool> {
        if !self.caret.is_empty() || self.caret.composing.is_some() { return None; }
        let at = self.caret.head;
        let spans = super::inline_code::code_spans(self.doc.get(at.block)?);
        if spans.iter().any(|s| s.end == at.offset) { return Some(true); }
        spans.iter().any(|s| s.start == at.offset).then_some(false)
    }

    fn set_out(&self, at_end: bool, cx: &Context<Self>) {
        let entry = (cx.entity_id(), self.caret.head, at_end);
        OUT.with(|o| *o.borrow_mut() = Some(entry));
    }

    /// Left/Right on a chip edge toggles between inside and outside; true when the key was used.
    pub(in crate::editor) fn code_escape_key(&mut self, forward: bool, cx: &mut Context<Self>) -> bool {
        let Some(at_end) = self.code_edge() else { return false };
        let out = side(cx.entity_id(), self.caret.head);
        let used = match (out, forward) {
            (None, true) if at_end => { self.set_out(true, cx); true }
            (None, false) if !at_end => { self.set_out(false, cx); true }
            (Some(true), false) | (Some(false), true) => { clear(); true }
            _ => false,
        };
        if used { self.preferred_x = None; cx.notify(); }
        used
    }

    /// Arriving at a chip edge from plain text keeps the caret on the plain side.
    pub(in crate::editor) fn code_escape_arrive(&mut self, forward: bool, cx: &Context<Self>) {
        match self.code_edge() {
            Some(false) if forward => self.set_out(false, cx),
            Some(true) if !forward => self.set_out(true, cx),
            _ => {}
        }
    }

    /// A click in a chip's padding or just past its edge puts the caret on the plain side.
    pub(in crate::editor) fn code_escape_click(&mut self, position: Point<Pixels>, cx: &Context<Self>) {
        clear();
        let Some(at_end) = self.code_edge() else { return };
        let Some(layout) = self.layouts.get(&self.caret.head.block) else { return };
        let p = layout.point(self.caret.head.offset);
        if if at_end { position.x > p.x } else { position.x < p.x } { self.set_out(at_end, cx); }
    }

    /// Caret right after a chip just made by typing its closing backtick: stay outside.
    pub(in crate::editor) fn code_escape_after_close(&mut self, cx: &Context<Self>) {
        if self.code_edge() == Some(true) { self.set_out(true, cx); }
    }

    /// Text typed outside a chip's end must not inherit the code mark.
    pub(in crate::editor) fn code_escape_strip(&mut self, at: Position, len: usize, cx: &Context<Self>) {
        if side(cx.entity_id(), at) != Some(true) || len == 0 { return; }
        if let Some(block) = self.doc.get_mut(at.block) {
            let range = at.offset..at.offset + len;
            if block.spans.iter().any(|s| s.marks.contains(&super::model::MarkKind::Code) && s.start <= range.start && range.end <= s.end) {
                block.toggle_mark(range, super::model::MarkKind::Code);
            }
        }
    }
}
