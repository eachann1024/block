use std::ops::Range;
use gpui::{Context, Pixels};
use super::{Editor, Doc};
use crate::editor::model::Block;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(in crate::editor) struct Position { pub block: u64, pub offset: usize }
#[derive(Clone, Debug, PartialEq, Eq)]
pub(in crate::editor) struct Caret {
    pub anchor: Position,
    pub head: Position,
    pub composing: Option<Range<usize>>,
}
impl Caret {
    pub fn new(block: u64) -> Self {
        let at = Position { block, offset: 0 };
        Self { anchor: at, head: at, composing: None }
    }
    pub fn is_empty(&self) -> bool { self.anchor == self.head }
}
pub(in crate::editor) fn boundary(text: &str, offset: usize) -> usize {
    let mut offset = offset.min(text.len());
    while !text.is_char_boundary(offset) { offset -= 1; }
    offset
}
impl Editor {
    pub(in crate::editor) fn ordered(&self) -> (Position, Position, usize, usize) {
        let a = self.caret.anchor;
        let h = self.caret.head;
        let ai = self.doc.index_of(a.block).unwrap_or(0);
        let hi = self.doc.index_of(h.block).unwrap_or(0);
        if (ai, a.offset) <= (hi, h.offset) { (a, h, ai, hi) }
        else { (h, a, hi, ai) }
    }
    pub(in crate::editor) fn selected_range(&self, id: u64) -> Option<Range<usize>> {
        if self.caret.is_empty() { return None; }
        let (a, b, ai, bi) = self.ordered();
        let i = self.doc.index_of(id)?;
        if i < ai || i > bi { return None; }
        let len = self.doc.blocks[i].text.len();
        Some(if i == ai { a.offset } else { 0 }..if i == bi { b.offset } else { len })
    }
    pub(in crate::editor) fn set_head(&mut self, mut at: Position, extend: bool, cx: &mut Context<Self>) {
        if let Some(block) = self.doc.get(at.block) { at.offset = boundary(&block.text, at.offset); }
        else { return; }
        self.ensure_block_visible(at.block);
        if self.caret.head != at { crate::editor::code_escape::clear(); }
        self.caret.head = at;
        if !extend { self.caret.anchor = at; }
        self.caret.composing = None;
        self.reveal_caret = true;
        if let Some(layout) = self.layouts.get(&at.block) {
            let p = layout.point(at.offset);
            let line_h = layout.cells.first().map_or(layout.line_height, |c| c.text.line_height);
            crate::sync_ime_cursor_area(f32::from(p.x), f32::from(p.y), f32::from(line_h));
        }
        cx.notify();
    }
    pub(in crate::editor) fn block_text(&self) -> &str {
        self.doc.get(self.caret.head.block).map(|b| b.text.as_str()).unwrap_or("")
    }
    pub(in crate::editor) fn export_indices(&self, selected: bool) -> Range<usize> {
        if self.path.is_none() || (selected && self.caret.is_empty()) { return 0..0; }
        if !selected { return 0..self.doc.blocks.len(); }
        let (_, end, start, last) = self.ordered();
        start..if end.offset == 0 && last > start { last } else { last + 1 }
    }
    pub fn export_block_count(&self, selected_only: bool) -> usize {
        self.export_indices(selected_only).len()
    }
    pub fn export_contains_table(&self, selected_only: bool) -> bool {
        self.export_indices(selected_only).any(|i| self.doc.blocks[i].meta.table.is_some())
    }
    pub fn export_budget(&self, selected_only: bool) -> (usize, usize) {
        self.export_indices(selected_only).fold((0usize, 0usize), |(bytes, spans), i| {
            let b = &self.doc.blocks[i];
            let range = if selected_only { self.selected_range(b.id).unwrap_or(0..0) } else { 0..b.text.len() };
            let metadata = b.meta.code_language.as_ref().map_or(0, |s| s.len())
                .saturating_add(b.meta.raw_source.as_ref().map_or(0, |s| if selected_only { range.len() } else { s.len() }));
            let count = b.spans.iter().filter(|s| s.start < range.end && s.end > range.start).count();
            (bytes.saturating_add(range.len()).saturating_add(metadata), spans.saturating_add(count))
        })
    }
    pub fn export_blocks(&self, selected_only: bool) -> Vec<Block> {
        self.export_indices(selected_only).map(|i| {
            let b = &self.doc.blocks[i];
            if selected_only {
                let mut block = b.slice(self.selected_range(b.id).unwrap_or(0..0));
                if let Some(number) = self.doc.list_number(b.id) { block.meta.list_start = Some(number); }
                block
            } else { b.clone() }
        }).collect()
    }
    pub(in crate::editor) fn reset_document(&mut self, doc: Doc) {
        self.caret = Caret::new(doc.blocks[0].id);
        self.doc = doc;
        self.history.clear(); self.redo.clear(); self.nowrap.clear(); self.folded.clear();
        self.preferred_x = None::<Pixels>;
        self.clear_transient_overlays();
        self.list.reset(self.doc.blocks.len());
        self.link_ui.quiet = Some(self.caret.head);
    }
}
