use std::ops::Range;
use gpui::{Bounds, Context, EntityInputHandler, Pixels, Point, UTF16Selection, Window};
use crate::editor::input::{from_utf16, to_utf16};
use super::{Editor, selection::Position};

impl EntityInputHandler for Editor {
    fn text_for_range(&mut self, range: Range<usize>, actual: &mut Option<Range<usize>>, _: &mut Window, _: &mut Context<Self>) -> Option<String> {
        if self.path.is_none() { return None; }
        let text = self.block_text(); let range = from_utf16(text, &range);
        *actual = Some(to_utf16(text, &range)); Some(text[range].to_string())
    }
    fn selected_text_range(&mut self, _: bool, _: &mut Window, _: &mut Context<Self>) -> Option<UTF16Selection> {
        if self.path.is_none() { return None; }
        let range = self.selected_range(self.caret.head.block).unwrap_or(self.caret.head.offset..self.caret.head.offset);
        Some(UTF16Selection { range: to_utf16(self.block_text(), &range), reversed: self.caret.head == self.ordered().0 && !self.caret.is_empty() })
    }
    fn marked_text_range(&self, _: &mut Window, _: &mut Context<Self>) -> Option<Range<usize>> {
        self.caret.composing.as_ref().map(|range| to_utf16(self.block_text(), range))
    }
    fn unmark_text(&mut self, _: &mut Window, cx: &mut Context<Self>) { self.commit_composition(cx); }
    fn replace_text_in_range(&mut self, range: Option<Range<usize>>, text: &str, window: &mut Window, cx: &mut Context<Self>) {
        self.replace(range, text, window, cx);
    }
    fn replace_and_mark_text_in_range(&mut self, range: Option<Range<usize>>, text: &str, selected: Option<Range<usize>>, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        let range = match self.focused_image() { Some(id) if !text.is_empty() => { self.add_below(id, window, cx); None } _ => range };
        let before = self.snapshot();
        self.set_replacement_range(range);
        if !self.can_replace_selection() {
            self.caret = before.caret; self.table_edit_rejected(cx); return;
        }
        if self.composition_before.is_none() { self.composition_before = Some(before); }
        let start = self.ordered().0.offset;
        self.replace_selection(text, false, cx);
        let id = self.caret.head.block;
        let end = self.caret.head.offset;
        let start = start.min(end);
        self.caret.composing = (!text.is_empty()).then_some(start..end);
        if let Some(selected) = selected {
            let inserted = &self.block_text()[start..end];
            let range = from_utf16(inserted, &selected);
            self.caret.anchor = Position { block: id, offset: start + range.start };
            self.caret.head = Position { block: id, offset: start + range.end };
        }
        cx.notify();
    }
    fn bounds_for_range(&mut self, range: Range<usize>, _: Bounds<Pixels>, _: &mut Window, _: &mut Context<Self>) -> Option<Bounds<Pixels>> {
        let bytes = from_utf16(self.block_text(), &range);
        self.layouts.get(&self.caret.head.block)?.rects(bytes).into_iter().next()
    }
    fn character_index_for_point(&mut self, point: Point<Pixels>, _: &mut Window, _: &mut Context<Self>) -> Option<usize> {
        let layout = self.layouts.get(&self.caret.head.block)?;
        let offset = layout.hit(point).min(self.block_text().len());
        Some(to_utf16(self.block_text(), &(offset..offset)).start)
    }
}
