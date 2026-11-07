use std::ops::Range;
use gpui::{Bounds, Context, EntityInputHandler, Pixels, Point, UTF16Selection, Window};
use crate::editor::input::{from_utf16, to_utf16};
use super::{Editor, selection::Position};

impl EntityInputHandler for Editor {
    fn text_for_range(&mut self, range: Range<usize>, actual: &mut Option<Range<usize>>, _: &mut Window, _: &mut Context<Self>) -> Option<String> {
        if let Some(text) = self.source_doc().map(|doc| doc.text.body().to_owned()) {
            let range = from_utf16(&text, &range);
            *actual = Some(to_utf16(&text, &range));
            return Some(text[range].to_string());
        }
        if !self.is_markdown_note() { return None; }
        let text = self.block_text(); let range = from_utf16(text, &range);
        *actual = Some(to_utf16(text, &range)); Some(text[range].to_string())
    }
    fn selected_text_range(&mut self, _: bool, _: &mut Window, _: &mut Context<Self>) -> Option<UTF16Selection> {
        if let Some(doc) = self.source_doc() {
            let text = doc.text.body();
            let range = doc.selection();
            return Some(UTF16Selection { range: to_utf16(text, &range), reversed: doc.head < doc.anchor });
        }
        if !self.is_markdown_note() { return None; }
        let range = self.selected_range(self.caret.head.block).unwrap_or(self.caret.head.offset..self.caret.head.offset);
        Some(UTF16Selection { range: to_utf16(self.block_text(), &range), reversed: self.caret.head == self.ordered().0 && !self.caret.is_empty() })
    }
    fn marked_text_range(&self, _: &mut Window, _: &mut Context<Self>) -> Option<Range<usize>> {
        if let Some(doc) = self.source_doc() {
            return doc.composing.as_ref().map(|range| to_utf16(doc.text.body(), range));
        }
        self.caret.composing.as_ref().map(|range| to_utf16(self.block_text(), range))
    }
    fn unmark_text(&mut self, _: &mut Window, cx: &mut Context<Self>) {
        #[cfg(not(target_family = "wasm"))] if crate::editor::ai::plan_link::create_preview_active(cx.entity_id()) { return; }
        if self.editing_source() { self.finish_source_composition(cx); return; }
        self.commit_composition(cx);
    }
    fn paste(&mut self, item: gpui::ClipboardItem, window: &mut Window, cx: &mut Context<Self>) {
        #[cfg(not(target_family = "wasm"))] if crate::editor::ai::plan_link::create_preview_active(cx.entity_id()) { return; }
        if self.editing_source() { self.source_paste(Some(item), cx); return; }
        if !self.is_markdown_note() { return; }
        self.commit_composition(cx);
        self.paste_clipboard_item(Some(item), window, cx);
    }
    fn replace_text_in_range(&mut self, range: Option<Range<usize>>, text: &str, window: &mut Window, cx: &mut Context<Self>) {
        #[cfg(not(target_family = "wasm"))] if crate::editor::ai::plan_link::create_preview_active(cx.entity_id()) { return; }
        if self.editing_source() { self.source_replace_utf16(range, text, cx); return; }
        if !self.is_markdown_note() { return; }
        self.replace(range, text, window, cx);
    }
    fn replace_and_mark_text_in_range(&mut self, range: Option<Range<usize>>, text: &str, selected: Option<Range<usize>>, window: &mut Window, cx: &mut Context<Self>) {
        #[cfg(not(target_family = "wasm"))] if crate::editor::ai::plan_link::create_preview_active(cx.entity_id()) { return; }
        if self.editing_source() { self.source_mark(range, text, selected, cx); return; }
        if !self.is_markdown_note() { return; }
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
        if let Some(layout) = self.layouts.get(&id) {
            let p = layout.point(start);
            let line_h = layout.cells.first().map_or(layout.line_height, |c| c.text.line_height);
            crate::sync_ime_cursor_area(f32::from(p.x), f32::from(p.y), f32::from(line_h));
        }
        cx.notify();
    }
    fn bounds_for_range(&mut self, range: Range<usize>, _: Bounds<Pixels>, _: &mut Window, _: &mut Context<Self>) -> Option<Bounds<Pixels>> {
        if self.editing_source() {
            let (index, start, vis_len, bytes) = {
                let doc = self.source_doc()?;
                let bytes = from_utf16(doc.text.body(), &range);
                let index = doc.text.line_of(bytes.start);
                let (start, visible) = doc.text.line_visible(index);
                (index, start, visible.len(), bytes)
            };
            let layout = self.source_layouts.get(&index)?;
            let a = bytes.start.saturating_sub(start);
            let b = bytes.end.min(start + vis_len).saturating_sub(start);
            return layout.rects(a..b.max(a)).into_iter().next();
        }
        if !self.is_markdown_note() { return None; }
        let bytes = from_utf16(self.block_text(), &range);
        self.layouts.get(&self.caret.head.block)?.rects(bytes).into_iter().next()
    }
    fn character_index_for_point(&mut self, point: Point<Pixels>, _: &mut Window, _: &mut Context<Self>) -> Option<usize> {
        if self.editing_source() {
            let at = self.source_hit(point)?;
            let text = self.source_doc()?.text.body().to_owned();
            return Some(to_utf16(&text, &(at..at)).start);
        }
        if !self.is_markdown_note() { return None; }
        let layout = self.layouts.get(&self.caret.head.block)?;
        let offset = layout.hit(point).min(self.block_text().len());
        Some(to_utf16(self.block_text(), &(offset..offset)).start)
    }
}
