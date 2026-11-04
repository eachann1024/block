use super::{Block, BlockMeta, Kind, Span, spans};
use std::ops::Range;
impl Block {
    pub fn new(id: u64, kind: Kind, text: String) -> Self {
        Self { id, kind, text, checked: false, spans: Vec::new(), meta: BlockMeta::default() }
    }
    pub fn slice(&self, range: Range<usize>) -> Self {
        let valid = spans::valid(&self.text,&range);
        let text = if valid { self.text[range.clone()].to_owned() } else { String::new() };
        if valid && let Some(table) = &self.meta.table {
            return Block::from_table(self.id, if range == (0..self.text.len()) {
                table.clone()
            } else { table.slice(range) });
        }
        let meta = BlockMeta { indent:self.meta.indent, list_start:self.meta.list_start,
            code_language:self.meta.code_language.clone(),
            code_empty_line:valid && self.text.is_empty() && self.meta.code_empty_line,
            raw_source:self.meta.raw_source.as_ref().map(|_| text.clone()),
            inline:super::inline_ops::slice(&self.meta.inline, range.clone()),
            html_paragraph:self.meta.html_paragraph.clone(),
            unsupported_reason:self.meta.unsupported_reason.clone(),
            heading_level:self.meta.heading_level, callout:self.meta.callout, table:None, tight:self.meta.tight };
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
    pub fn split_off(&mut self, at: usize, new_id: u64, kind: Kind) -> Option<Block> {
        if self.meta.table.is_some() || !self.text.is_char_boundary(at) { return None; }
        let mut right = self.slice(at..self.text.len()); right.id = new_id; right.kind = kind;
        right.checked = false; right.meta.list_start = None;
        if kind != Kind::Quote { right.meta.callout = None; }
        // A split must not duplicate the imported blank-body sentinel.
        right.meta.code_empty_line = false;
        *self = self.slice(0..at); Some(right)
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
        Some(n)
    }
}
pub fn list_numbers<'a>(blocks: impl IntoIterator<Item = &'a Block>) -> Vec<Option<u64>> {
    let mut numbering = Numbering::default(); blocks.into_iter().map(|b| numbering.next(b)).collect()
}
