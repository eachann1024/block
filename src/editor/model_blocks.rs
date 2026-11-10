use super::{Block, BlockMeta, Kind, Span, spans};
use std::ops::Range;
impl Block {
    pub fn new(id: u64, kind: Kind, text: String) -> Self {
        Self { id, kind, text, checked: false, spans: Vec::new(), meta: BlockMeta::default() }
    }
    /// A text selection has inline formatting, but no heading, list or table shell.
    pub fn text_slice(&self, range: Range<usize>) -> Option<Self> {
        if !spans::valid(&self.text, &range) { return None; }
        let mut out = Self::new(0, Kind::Paragraph, self.text[range.clone()].to_owned());
        if let Some(table) = &self.meta.table {
            for (row, cell, body) in table.cell_ranges() {
                let start = range.start.max(body.start);
                let end = range.end.min(body.end);
                if start >= end { continue; }
                let part = table.rows[row].cells[cell].content.slice(start - body.start..end - body.start);
                let offset = start - range.start;
                out.spans.extend(part.spans.into_iter().map(|s| Span { start:s.start + offset, end:s.end + offset, ..s }));
                out.meta.inline.extend(part.meta.inline.into_iter().map(|t| super::InlineTag { start:t.start + offset, end:t.end + offset, ..t }));
            }
        } else {
            let part = self.slice(range);
            out.spans = part.spans; out.meta.inline = part.meta.inline;
        }
        out.normalize_spans(); Some(out)
    }
    /// Rich-text replacement preserves the destination shell, including table-cell structure.
    pub fn replace_inline(&mut self, range: Range<usize>, fragment: &Self) -> bool {
        if !spans::valid(&self.text, &range) { return false; }
        if let Some(table) = &mut self.meta.table {
            let Some((row, cell, local)) = table.cell_at_range(&range) else { return false; };
            let changed = table.rows[row].cells[cell].content.replace_inline(local, fragment);
            if changed { self.sync_table_text(); }
            return changed;
        }
        if self.meta.code_language.is_some() || self.meta.raw_source.is_some() {
            return self.replace_range(range, &fragment.text);
        }
        let mut combined = self.slice(0..range.start);
        combined.append(fragment);
        combined.append(&self.slice(range.end..self.text.len()));
        if *self == combined { return false; }
        *self = combined; true
    }
    pub fn slice(&self, range: Range<usize>) -> Self {
        let valid = spans::valid(&self.text,&range);
        let text = if valid { self.text[range.clone()].to_owned() } else { String::new() };
        if valid && let Some(table) = &self.meta.table {
            let mut block = self.clone();
            if range != (0..self.text.len()) { block.meta.table = Some(table.slice(range)); }
            block.sync_table_text();
            return block;
        }
        let meta = BlockMeta { indent:self.meta.indent, list_start:self.meta.list_start,
            code_language:self.meta.code_language.clone(),
            list_code:self.meta.list_code.clone(),
            code_empty_line:valid && self.text.is_empty() && self.meta.code_empty_line,
            raw_source:self.meta.raw_source.as_ref().map(|_| text.clone()),
            inline:super::inline_ops::slice(&self.meta.inline, range.clone()),
            html_paragraph:self.meta.html_paragraph.clone(),
            unsupported_reason:self.meta.unsupported_reason.clone(),
            heading_level:self.meta.heading_level, callout:self.meta.callout,
            table:None, tight:self.meta.tight };
        let mut out = Self { id:self.id,kind:self.kind,text,checked:self.checked,spans:Vec::new(),meta };
        if !valid { return out; }
        // Canonical spans are ordered: visit only intersecting runs, not the full block.
        let first = self.spans.partition_point(|s| s.end<=range.start);
        out.spans = self.spans[first..].iter().take_while(|s|s.start<range.end).map(|s| {
            Span { start:s.start.max(range.start)-range.start,
                end:s.end.min(range.end)-range.start,marks:s.marks.clone() }
        }).collect();
        out.normalize_spans(); out
    }
    pub fn append(&mut self, other: &Block) {
        // Tables are structural blocks; callers must splice them rather than concatenate projections.
        if self.meta.table.is_some() || other.meta.table.is_some() { return; }
        let offset = self.text.len(); self.text.push_str(&other.text);
        if !self.text.is_empty() { self.meta.code_empty_line = false; }
        self.spans.extend(other.spans.iter().map(|s| Span { start:s.start+offset,end:s.end+offset,marks:s.marks.clone() }));
        self.meta.inline.extend(other.meta.inline.iter().map(|tag| {
            let mut tag = tag.clone(); tag.start += offset; tag.end += offset; tag
        }));
        self.normalize_spans();
    }
}
/// Shared nested-list counter. Indent is source columns, not an assumed nesting unit.
#[derive(Default)]
pub struct Numbering { levels: Vec<(usize,u64)> }
impl Numbering {
    pub fn next(&mut self, block: &Block) -> Option<u64> {
        self.advance(block).map(|(number, _)| number)
    }
    /// Source markers on continuation items are counters, not explicit list starts.
    pub(in crate::editor) fn normalize_imported_start(&mut self, block: &mut Block, after_blank: bool) {
        let Some((number, true)) = self.advance(block) else { return; };
        if after_blank && let Some(start) = block.meta.list_start && start != number {
            // A different marker after a blank explicitly starts another source run.
            self.levels.last_mut().unwrap().1 = start;
        } else if block.meta.raw_source.is_none() {
            block.meta.list_start = None;
        }
    }
    fn advance(&mut self, block: &Block) -> Option<(u64, bool)> {
        if let Some(code) = &block.meta.list_code {
            while self.levels.last().is_some_and(|(depth,_)| *depth > code.parent_indent) { self.levels.pop(); }
            return None;
        }
        if !matches!(block.kind,Kind::Numbered|Kind::Bullet|Kind::Todo) {
            self.levels.clear(); return None;
        }
        let indent = block.meta.indent;
        while self.levels.last().is_some_and(|(depth,_)| *depth > indent) { self.levels.pop(); }
        if block.kind != Kind::Numbered {
            if self.levels.last().is_some_and(|(depth,_)| *depth == indent) { self.levels.pop(); }
            return None;
        }
        let previous = self.levels.last().filter(|(depth,_)| *depth == indent).map(|(_,n)| *n);
        let n = previous.map_or_else(|| block.meta.list_start.unwrap_or(1), |n| n.saturating_add(1));
        if previous.is_some() { self.levels.last_mut().unwrap().1 = n; }
        else { self.levels.push((indent,n)); }
        Some((n, previous.is_some()))
    }
}
pub fn list_numbers<'a>(blocks: impl IntoIterator<Item = &'a Block>) -> Vec<Option<u64>> {
    let mut numbering = Numbering::default(); blocks.into_iter().map(|b| numbering.next(b)).collect()
}
