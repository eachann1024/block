//! Exact source buffer. Newlines, BOM and trailing bytes stay in the string.
use std::ops::Range;
use gpui::{ListAlignment, ListState, TextRun, px};
use unicode_segmentation::{GraphemeCursor, UnicodeSegmentation};
use crate::files::classify::SourceFile;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum Newline { Lf, Crlf }

impl Newline {
    pub fn detect(text: &str) -> Self {
        let crlf = text.matches("\r\n").count();
        let bare = text.bytes().filter(|b| *b == b'\n').count().saturating_sub(crlf);
        if crlf > 0 && crlf >= bare { Self::Crlf } else { Self::Lf }
    }
    pub fn as_str(self) -> &'static str { match self { Self::Lf => "\n", Self::Crlf => "\r\n" } }
}

#[derive(Clone, Debug)]
pub(super) struct Edit { pub start: usize, pub removed: String, pub inserted: String }

pub(super) struct SourceText { body: String, starts: Vec<usize> }

impl SourceText {
    pub fn new(body: String) -> Self {
        let mut text = Self { body, starts: Vec::new() };
        text.reindex();
        text
    }
    pub fn body(&self) -> &str { &self.body }
    pub fn len(&self) -> usize { self.body.len() }
    pub fn line_count(&self) -> usize { self.starts.len().max(1) }
    pub fn line_of(&self, offset: usize) -> usize {
        let offset = offset.min(self.body.len());
        match self.starts.binary_search(&offset) {
            Ok(i) => i,
            Err(0) => 0,
            Err(i) => i - 1,
        }
    }
    /// Byte start of the line and the visible slice (no break, no trailing CR).
    pub fn line_visible(&self, index: usize) -> (usize, &str) {
        let start = self.starts.get(index).copied().unwrap_or(0).min(self.body.len());
        let end = self.break_at(index);
        let mut vis = end;
        if vis > start && self.body.as_bytes()[vis - 1] == b'\r' { vis -= 1; }
        (start, &self.body[start..vis])
    }
    pub fn break_at(&self, index: usize) -> usize {
        if index + 1 < self.starts.len() { self.starts[index + 1].saturating_sub(1) } else { self.body.len() }
    }
    pub fn line_limit(&self, index: usize) -> usize {
        if index + 1 < self.starts.len() { self.starts[index + 1] } else { self.body.len() }
    }
    pub fn snap(&self, offset: usize) -> usize {
        let mut offset = floor_char(&self.body, offset.min(self.body.len()));
        let bytes = self.body.as_bytes();
        if offset > 0 && offset < bytes.len() && bytes[offset - 1] == b'\r' && bytes[offset] == b'\n' { offset -= 1; }
        offset
    }
    pub fn replace(&mut self, range: Range<usize>, insert: &str) -> Range<usize> {
        let start = self.snap(range.start.min(range.end));
        let end = self.snap(range.end.max(range.start)).max(start);
        self.body.replace_range(start..end, insert);
        self.reindex();
        start..start + insert.len()
    }
    fn reindex(&mut self) {
        self.starts.clear();
        self.starts.push(0);
        for (i, b) in self.body.bytes().enumerate() {
            if b == b'\n' { self.starts.push(i + 1); }
        }
    }
}

pub(super) struct SourceDoc {
    pub text: SourceText,
    pub bom: bool,
    pub newline: Newline,
    pub language: String,
    pub highlight: bool,
    pub html: bool,
    pub svg: bool,
    pub anchor: usize,
    pub head: usize,
    pub preferred_col: Option<usize>,
    pub undo: Vec<Edit>,
    pub redo: Vec<Edit>,
    pub list: ListState,
    pub composing: Option<Range<usize>>,
    pub compose_before: Option<String>,
    pub rev: u64,
    pub runs: Vec<TextRun>,
    pub runs_rev: u64,
    /// Font and color theme the cached runs were built for.
    pub runs_theme: (u64, bool),
    pub reveal: bool,
}

impl SourceDoc {
    pub fn open(file: SourceFile) -> Self {
        let newline = Newline::detect(&file.text);
        let text = SourceText::new(file.text);
        let lines = text.line_count();
        Self {
            text, bom: file.bom, newline, language: file.language, highlight: file.highlight,
            html: file.html, svg: file.svg, anchor: 0, head: 0, preferred_col: None,
            undo: Vec::new(), redo: Vec::new(),
            list: ListState::new(lines, ListAlignment::Top, px(24.)),
            composing: None, compose_before: None, rev: 1, runs: Vec::new(), runs_rev: 0,
            runs_theme: (0, false), reveal: false,
        }
    }
    pub fn selection(&self) -> Range<usize> {
        let (a, b) = if self.anchor <= self.head { (self.anchor, self.head) } else { (self.head, self.anchor) };
        a..b
    }
    pub fn sync_list(&mut self) {
        let n = self.text.line_count();
        if self.list.item_count() != n { self.list.reset(n); }
    }
    /// Highlighting stops once the buffer passes the cap, including after a paste.
    pub fn note_highlight(&mut self) {
        if self.text.len() > crate::files::classify::HIGHLIGHT_LIMIT { self.highlight = false; }
    }
}

pub(super) fn floor_char(text: &str, offset: usize) -> usize {
    let mut offset = offset.min(text.len());
    while !text.is_char_boundary(offset) { offset -= 1; }
    offset
}

pub(super) fn step_offset(text: &str, offset: usize, forward: bool) -> usize {
    let offset = floor_char(text, offset);
    if forward {
        if text[offset..].starts_with("\r\n") { return offset + 2; }
        let mut cursor = GraphemeCursor::new(offset, text.len(), true);
        cursor.next_boundary(text, 0).ok().flatten().unwrap_or(text.len())
    } else {
        if offset >= 2 && text[..offset].ends_with("\r\n") { return offset - 2; }
        let mut cursor = GraphemeCursor::new(offset, text.len(), true);
        cursor.prev_boundary(text, 0).ok().flatten().unwrap_or(0)
    }
}

pub(super) fn column_of(visible: &str, offset: usize) -> usize {
    visible[..offset.min(visible.len())].graphemes(true).count()
}

pub(super) fn offset_of_column(visible: &str, column: usize) -> usize {
    visible.grapheme_indices(true).nth(column).map(|(i, _)| i).unwrap_or(visible.len())
}

pub(super) fn word_range(text: &str, offset: usize) -> Range<usize> {
    let offset = floor_char(text, offset.min(text.len()));
    let start = (0..=offset).rev().find(|i| text.is_char_boundary(*i) && !word_byte(text, *i)).map(|i| step_offset(text, i, true).min(offset)).unwrap_or(0);
    let mut end = offset;
    while end < text.len() && word_byte(text, end) { end = step_offset(text, end, true); }
    if start == end {
        let next = step_offset(text, offset, true);
        return offset..next.max(offset);
    }
    start..end
}

fn word_byte(text: &str, offset: usize) -> bool {
    text[offset..].chars().next().is_some_and(|c| c.is_alphanumeric() || c == '_')
}

pub(super) fn diff_edit(before: &str, after: &str) -> Option<Edit> {
    if before == after { return None; }
    let mut prefix = before.bytes().zip(after.bytes()).take_while(|(a, b)| a == b).count();
    while !before.is_char_boundary(prefix) || !after.is_char_boundary(prefix) { prefix -= 1; }
    let mut suffix = 0usize;
    let (b, a) = (before.as_bytes(), after.as_bytes());
    while suffix < b.len() - prefix && suffix < a.len() - prefix && b[b.len() - 1 - suffix] == a[a.len() - 1 - suffix] {
        suffix += 1;
    }
    while suffix > 0 && (!before.is_char_boundary(before.len() - suffix) || !after.is_char_boundary(after.len() - suffix)) { suffix -= 1; }
    Some(Edit {
        start: prefix,
        removed: before[prefix..before.len() - suffix].to_owned(),
        inserted: after[prefix..after.len() - suffix].to_owned(),
    })
}
