use super::{Block, Doc, MarkKind, Span, inline_ops};
use std::ops::Range;

pub(super) fn valid(text: &str, range: &Range<usize>) -> bool {
    range.start <= range.end && range.end <= text.len()
        && text.is_char_boundary(range.start) && text.is_char_boundary(range.end)
}
pub(super) fn push(out: &mut Vec<Span>, start: usize, end: usize, marks: Vec<MarkKind>) {
    if start == end || marks.is_empty() { return; }
    if let Some(last) = out.last_mut() && last.end == start && last.marks == marks { last.end = end; }
    else { out.push(Span { start, end, marks }); }
}
impl Block {
    pub fn normalize_spans(&mut self) {
        if self.meta.code_language.is_some() || self.meta.raw_source.is_some() { self.spans.clear(); return; }
        let mut events = Vec::with_capacity(self.spans.len() * 4);
        for span in &self.spans {
            if !valid(&self.text, &(span.start..span.end)) || span.start == span.end { continue; }
            let mut marks = span.marks.clone(); marks.sort(); marks.dedup();
            for mark in marks { events.push((span.start, mark, 1i32)); events.push((span.end, mark, -1)); }
        }
        events.sort_unstable_by_key(|e| e.0);
        let mut counts = [0i32; 4]; let mut previous = 0; let mut out = Vec::new();
        for (at, mark, delta) in events {
            let marks = [MarkKind::Bold,MarkKind::Italic,MarkKind::Strike,MarkKind::Code]
                .into_iter().filter(|m| counts[*m as usize] > 0).collect();
            push(&mut out, previous, at, marks); counts[mark as usize] += delta; previous = at;
        }
        self.spans = out;
    }
    pub fn toggle_mark(&mut self, range: Range<usize>, mark: MarkKind) {
        if !valid(&self.text, &range) || range.is_empty() || self.meta.code_language.is_some()
            || self.meta.raw_source.is_some() { return; }
        if let Some(table) = &mut self.meta.table {
            for (row, cell, body) in table.cell_ranges() {
                let start = range.start.max(body.start).min(body.end) - body.start;
                let end = range.end.max(body.start).min(body.end) - body.start;
                if start < end { table.rows[row].cells[cell].content.toggle_mark(start..end, mark); }
            }
            return;
        }
        self.normalize_spans();
        let covered: usize = self.spans.iter().filter(|s| s.marks.contains(&mark))
            .map(|s| s.end.min(range.end).saturating_sub(s.start.max(range.start))).sum();
        let add = covered < range.len();
        let mut boundaries = vec![0, self.text.len(), range.start, range.end];
        for s in &self.spans { boundaries.extend([s.start,s.end]); }
        boundaries.sort_unstable(); boundaries.dedup();
        let mut out = Vec::new(); let mut i = 0;
        for edges in boundaries.windows(2) {
            let (start,end) = (edges[0],edges[1]);
            while i < self.spans.len() && self.spans[i].end <= start { i += 1; }
            let mut marks = self.spans.get(i).filter(|s| s.start <= start).map_or_else(Vec::new, |s| s.marks.clone());
            if range.start <= start && end <= range.end {
                marks.retain(|m| *m != mark); if add { marks.push(mark); marks.sort(); }
            }
            push(&mut out,start,end,marks);
        }
        self.spans = out;
    }
    /// Insertions inherit the mark set immediately to their left (right at byte zero).
    /// Replacements inherit the first replaced character. Invalid UTF-8 ranges are rejected.
    pub fn replace_range(&mut self, range: Range<usize>, text: &str) -> bool {
        if !valid(&self.text, &range) { return false; }
        if self.text[range.clone()] == *text { return false; }
        if let Some(table) = &mut self.meta.table {
            let changed = table.replace_range(range, text);
            if changed { self.sync_table_text(); }
            return changed;
        }
        self.normalize_spans();
        let inherited = self.spans.iter().find(|s| {
            if range.is_empty() && range.start > 0 { s.start < range.start && range.start <= s.end }
            else { s.start <= range.start && range.start < s.end }
        }).map_or_else(Vec::new, |s| s.marks.clone());
        let mut out = Vec::new();
        for s in &self.spans {
            if s.start < range.start { push(&mut out,s.start,s.end.min(range.start),s.marks.clone()); }
        }
        push(&mut out,range.start,range.start+text.len(),inherited);
        for s in &self.spans {
            if s.end > range.end {
                push(&mut out,s.start.max(range.end)-range.end+range.start+text.len(),
                    s.end-range.end+range.start+text.len(),s.marks.clone());
            }
        }
        super::inline_ops::replace(&mut self.meta.inline, range.clone(), text.len());
        self.text.replace_range(range,text); self.spans = out;
        // This flag preserves an imported blank body only until its first content edit.
        self.meta.code_empty_line = false; true
    }
}

impl Block {
    /// Runs `f` on every leaf block (the block itself, or each table cell) that the range touches.
    fn edit_leaf(&mut self, range: Range<usize>, f: &mut dyn FnMut(&mut Block, Range<usize>)) {
        if !valid(&self.text, &range) || range.is_empty() || self.meta.code_language.is_some()
            || self.meta.raw_source.is_some() { return; }
        if let Some(table) = &mut self.meta.table {
            for (row, cell, body) in table.cell_ranges() {
                let start = range.start.max(body.start).min(body.end) - body.start;
                let end = range.end.max(body.start).min(body.end) - body.start;
                if start < end { table.rows[row].cells[cell].content.edit_leaf(start..end, f); }
            }
            return;
        }
        f(self, range);
    }
    /// Removes bold/italic/strike/code over the range, keeping marks outside it.
    fn clear_marks(&mut self, range: Range<usize>) {
        self.normalize_spans();
        let mut out = Vec::new();
        for s in std::mem::take(&mut self.spans) {
            if s.start < range.start { push(&mut out, s.start, s.end.min(range.start), s.marks.clone()); }
            if s.end > range.end { push(&mut out, s.start.max(range.end), s.end, s.marks.clone()); }
        }
        self.spans = out;
    }
    pub fn link_at(&self, offset: usize) -> Option<(Range<usize>, String)> { inline_ops::link_at(&self.meta.inline, offset) }
}
impl Doc {
    fn edit_format(&mut self, id: u64, range: Range<usize>, f: &mut dyn FnMut(&mut Block, Range<usize>)) {
        if let Some(block) = self.get_mut(id) { block.edit_leaf(range, f); }
        self.refresh_block(id);
    }
    pub fn is_underlined(&self, id: u64, range: Range<usize>) -> bool {
        self.get(id).is_some_and(|b| inline_ops::is_underlined(&b.meta.inline, &range))
    }
    pub fn toggle_underline(&mut self, id: u64, range: Range<usize>, on: bool) {
        self.edit_format(id, range, &mut |b, r| {
            if inline_ops::is_underlined(&b.meta.inline, &r) != on { inline_ops::toggle_underline(&mut b.meta.inline, r); }
        });
    }
    /// `prop` is `color` or `background-color`; values are thinkly palette names.
    pub fn set_style(&mut self, id: u64, range: Range<usize>, prop: &str, value: Option<&str>) {
        self.edit_format(id, range, &mut |b, r| inline_ops::set_style(&mut b.meta.inline, r, prop, value));
    }
    pub fn set_link(&mut self, id: u64, range: Range<usize>, href: Option<&str>) {
        self.edit_format(id, range, &mut |b, r| inline_ops::set_link(&mut b.meta.inline, r, href));
    }
    pub fn clear_format(&mut self, id: u64, range: Range<usize>) {
        self.edit_format(id, range, &mut |b, r| { b.clear_marks(r.clone()); inline_ops::clear_format(&mut b.meta.inline, r); });
    }
}
