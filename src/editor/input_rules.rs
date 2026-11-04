//! Typing rules mirroring the Electron editor: block prefixes on space, dividers, arrow replacement.
use gpui::Context;
use super::{model::{Block, Kind, MarkKind, marker_kind}, view::{Editor, selection::Position}};

enum Rule { Code(String), Numbered(u64), Bullet, Todo(bool), Heading(u8), Quote }

/// Paragraph that holds ordinary rich text (no code, table, raw source or imported HTML shell).
pub(super) fn plain(block: &Block) -> bool {
    block.kind == Kind::Paragraph && block.meta.table.is_none() && block.meta.raw_source.is_none()
        && block.meta.code_language.is_none() && block.meta.html_paragraph.is_none()
}
fn strip_space(prefix: &str) -> &str {
    let mut chars = prefix.chars();
    match chars.next() { Some(c) if c.is_whitespace() => chars.as_str(), _ => prefix }
}
fn match_rule(prefix: &str) -> Option<Rule> {
    if let Some(lang) = prefix.strip_prefix("```").or_else(|| prefix.strip_prefix("~~~")) { return Some(Rule::Code(lang.trim().to_owned())); }
    let s = strip_space(prefix);
    if let Some(body) = s.strip_suffix(['.', '。', ')', '）']) {
        if !body.is_empty() && body.bytes().all(|b| b.is_ascii_digit()) { return body.parse().ok().map(Rule::Numbered); }
        if matches!(body, "a" | "i" | "A" | "I") { return Some(Rule::Numbered(1)); }
    }
    if matches!(s, "-" | "+" | "*") { return Some(Rule::Bullet); }
    for (open, close) in [('[', ']'), ('【', '】')] {
        if let Some(mid) = s.strip_prefix(open).and_then(|r| r.strip_suffix(close)) {
            if matches!(mid, "" | " " | "x" | "X") { return Some(Rule::Todo(mid.eq_ignore_ascii_case("x"))); }
        }
    }
    if (4..=6).contains(&prefix.len()) && prefix.bytes().all(|b| b == b'#') { return Some(Rule::Heading(prefix.len() as u8)); }
    if matches!(prefix, "\"" | "“" | "|" | "｜" | ">" | "＞") { return Some(Rule::Quote); }
    None
}
fn is_divider(text: &str) -> bool {
    text.len() >= 3 && matches!(text.as_bytes()[0], b'-' | b'*' | b'_') && text.bytes().all(|b| b == text.as_bytes()[0])
}
fn raw_divider() -> Block {
    let mut block = Block::new(0, Kind::Paragraph, "***\n".to_owned());
    block.meta.raw_source = Some("***\n".to_owned()); block
}

impl Editor {
    /// Run inside `replace_selection` right after a space was typed (caret at `offset`).
    /// Mutates the block, `id` (divider moves the caret to the trailing paragraph) and `offset`.
    pub(in crate::editor) fn space_rule(&mut self, id: &mut u64, offset: &mut usize) {
        let Some(block) = self.doc.get(*id) else { return };
        if block.kind != Kind::Paragraph { return; }
        if let Some((kind, eaten)) = marker_kind(&block.text) {
            if *offset == eaten {
                self.doc.replace_range(*id, 0..eaten, ""); self.doc.set_kind(*id, kind); *offset = 0;
            }
            return;
        }
        if !plain(block) || *offset == 0 || !block.text.is_char_boundary(*offset) { return; }
        let prefix = block.text[..*offset - 1].to_owned();
        if is_divider(&prefix) && *offset == block.text.len() {
            *id = self.insert_divider(*id); *offset = 0; return;
        }
        let Some(rule) = match_rule(&prefix) else { return };
        self.doc.replace_range(*id, 0..*offset, ""); *offset = 0;
        match rule {
            Rule::Code(lang) => { self.doc.set_kind(*id, Kind::Paragraph); self.set_meta(*id, |b| b.meta.code_language = Some(lang)); }
            Rule::Numbered(n) => { self.doc.set_kind(*id, Kind::Numbered); self.set_meta(*id, |b| b.meta.list_start = (n != 1).then_some(n)); }
            Rule::Bullet => self.doc.set_kind(*id, Kind::Bullet),
            Rule::Todo(done) => { self.doc.set_kind(*id, Kind::Todo); self.set_meta(*id, |b| b.checked = done); }
            Rule::Heading(level) => { self.doc.set_kind(*id, Kind::H3); self.set_meta(*id, |b| b.meta.heading_level = Some(level)); }
            Rule::Quote => self.doc.set_kind(*id, Kind::Quote),
        }
    }
    fn set_meta(&mut self, id: u64, edit: impl FnOnce(&mut Block)) {
        if let Some(block) = self.doc.get_mut(id) { edit(block); }
        self.doc.refresh_block(id);
    }
    /// Convert a plain paragraph whose entire text just became `---` or `***` into a divider immediately.
    pub(in crate::editor) fn divider_rule(&mut self, id: &mut u64, offset: &mut usize) {
        let Some(block) = self.doc.get(*id) else { return };
        if plain(block) && *offset == block.text.len() && matches!(block.text.as_str(), "---" | "***") {
            *id = self.insert_divider(*id); *offset = 0;
        }
    }
    /// Replace the paragraph with a divider plus a fresh paragraph; returns the new paragraph id.
    fn insert_divider(&mut self, id: u64) -> u64 {
        let index = self.doc.index_of(id).unwrap_or(0);
        self.doc.splice_blocks(index..index + 1, vec![raw_divider(), Block::new(0, Kind::Paragraph, String::new())]);
        self.doc.blocks[index + 1].id
    }
    /// Enter on `---` (caret at end) becomes a divider; returns whether it handled the key.
    pub(super) fn divider_on_enter(&mut self, cx: &mut Context<Self>) -> bool {
        let at = self.caret.head;
        if !self.caret.is_empty() || self.caret.composing.is_some() { return false; }
        let Some(block) = self.doc.get(at.block) else { return false };
        if !plain(block) || at.offset != block.text.len() { return false; }
        let before = self.snapshot();
        // A typed fence (```lang or ~~~lang) plus Enter turns the paragraph into a code block.
        if let Some(lang) = block.text.strip_prefix("```").or_else(|| block.text.strip_prefix("~~~")) {
            let lang = lang.trim().to_owned();
            self.doc.set_text(at.block, String::new());
            self.set_meta(at.block, |b| b.meta.code_language = Some(lang));
            let to = Position { block: at.block, offset: 0 };
            self.caret.anchor = to; self.caret.head = to;
            self.record(before, cx); return true;
        }
        if !is_divider(&block.text) { return false; }
        let new = self.insert_divider(at.block);
        let to = Position { block: new, offset: 0 };
        self.caret.anchor = to; self.caret.head = to;
        self.record(before, cx); true
    }
    /// `->` `-》` `=>` `<-` become arrows right after the last character; never inside code.
    pub(super) fn arrow_rule(&mut self, cx: &mut Context<Self>) {
        let at = self.caret.head;
        if !self.caret.is_empty() || self.caret.composing.is_some() { return; }
        let Some(block) = self.doc.get(at.block) else { return };
        if block.meta.table.is_some() || block.meta.raw_source.is_some() || block.meta.code_language.is_some() { return; }
        let Some(before_caret) = block.text.get(..at.offset) else { return };
        let Some((pattern, arrow)) = [("->", "→"), ("-》", "→"), ("=>", "⇒"), ("<-", "←")]
            .into_iter().find(|(p, _)| before_caret.ends_with(p)) else { return };
        let start = at.offset - pattern.len();
        if block.spans.iter().any(|s| s.marks.contains(&MarkKind::Code) && s.start < at.offset && s.end > start) { return; }
        let before = self.snapshot();
        self.doc.replace_range(at.block, start..at.offset, arrow);
        let to = Position { block: at.block, offset: start + arrow.len() };
        self.caret.anchor = to; self.caret.head = to;
        self.record(before, cx);
    }
    /// A typed `、` at the start of an empty paragraph opens the slash menu like `/`.
    pub(super) fn slash_alias_ready(&self) -> bool {
        let at = self.caret.head;
        self.caret.is_empty() && at.offset == 0 && self.doc.get(at.block).is_some_and(|b| plain(b) && b.text.is_empty())
    }
}
