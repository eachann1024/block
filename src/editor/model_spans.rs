use super::{Block, MarkKind, Span};
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
        self.text.replace_range(range,text); self.spans = out;
        // This flag preserves an imported blank body only until its first content edit.
        self.meta.code_empty_line = false; true
    }
}
