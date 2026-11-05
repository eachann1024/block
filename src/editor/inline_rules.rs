//! Consensus inline Markdown rules: live closing-delimiter conversion, selection wrapping, and URL-paste linking.
use std::ops::Range;
use gpui::Context;
use super::{
    format_cmds::normalize_url,
    model::{Block, MarkKind},
    view::{Editor, selection::Position},
};

fn in_code_span(block: &Block, pos: usize) -> bool {
    block.spans.iter().any(|s| s.marks.contains(&MarkKind::Code) && s.start <= pos && pos < s.end)
}

fn escaped(text: &str, idx: usize) -> bool {
    let bytes = text.as_bytes();
    let mut backslashes = 0usize;
    let mut i = idx;
    while i > 0 && bytes[i - 1] == b'\\' {
        backslashes += 1;
        i -= 1;
    }
    backslashes % 2 == 1
}

fn valid_inner(inner: &str) -> bool {
    !inner.is_empty()
        && !inner.contains('\n')
        && !inner.starts_with(char::is_whitespace)
        && !inner.ends_with(char::is_whitespace)
}

fn find_double_open(before: &str, delim: &str, block: &Block) -> Option<usize> {
    let ch = delim.as_bytes()[0];
    let bytes = before.as_bytes();
    if bytes.len() < 2 { return None; }
    for idx in (0..=bytes.len() - 2).rev() {
        if &bytes[idx..idx + 2] != delim.as_bytes() { continue; }
        if escaped(before, idx) || in_code_span(block, idx) { continue; }
        if idx > 0 && bytes[idx - 1] == ch { continue; }
        if bytes.get(idx + 2) == Some(&ch) { continue; }
        if ch == b'_' && idx > 0 && before[..idx].chars().next_back().is_some_and(|c| c.is_ascii_alphanumeric()) {
            continue;
        }
        return Some(idx);
    }
    None
}

fn find_single_open(before: &str, ch: u8, block: &Block) -> Option<usize> {
    let bytes = before.as_bytes();
    for idx in (0..bytes.len()).rev() {
        if bytes[idx] != ch { continue; }
        if escaped(before, idx) || in_code_span(block, idx) { continue; }
        if idx > 0 && bytes[idx - 1] == ch { continue; }
        if bytes.get(idx + 1) == Some(&ch) { continue; }
        if ch == b'_' && idx > 0 && before[..idx].chars().next_back().is_some_and(|c| c.is_ascii_alphanumeric()) {
            continue;
        }
        return Some(idx);
    }
    None
}

fn is_single_url(raw: &str) -> Option<&str> {
    let url = raw.trim();
    if url.is_empty() || url.contains(char::is_whitespace) { return None; }
    let lower = url.to_ascii_lowercase();
    (lower.starts_with("http://") || lower.starts_with("https://") || lower.starts_with("mailto:")).then_some(url)
}

fn is_yellow_highlighted(block: &Block, range: &Range<usize>) -> bool {
    let covered: usize = block.meta.inline.iter().filter(|tag| {
        tag.name == "span" && super::text_runs::css(&tag.attrs, "background-color") == Some("yellow")
    }).map(|tag| tag.end.min(range.end).saturating_sub(tag.start.max(range.start))).sum();
    covered >= range.len()
}

impl Editor {
    /// Wrap a non-empty single-block selection when typing a pairing delimiter or mark character.
    pub(super) fn wrap_selection(&mut self, text: &str, cx: &mut Context<Self>) -> bool {
        if self.caret.is_empty() || self.caret.composing.is_some() { return false; }
        let (a, b, ai, bi) = self.ordered();
        if ai != bi || a.offset >= b.offset { return false; }
        let Some(block) = self.doc.get(a.block) else { return false };
        if block.meta.code_language.is_some() || block.meta.raw_source.is_some() { return false; }
        match text {
            "`" => { self.mark(MarkKind::Code, cx); return true; }
            "~" => { self.mark(MarkKind::Strike, cx); return true; }
            "=" => {
                let on = !is_yellow_highlighted(block, &(a.offset..b.offset));
                self.apply_style("background-color", on.then_some("yellow"), cx);
                return true;
            }
            _ => {}
        }
        if block.meta.table.is_some() { return false; }
        let pair = match text {
            "(" => Some(("(", ")")),
            "[" => Some(("[", "]")),
            "{" => Some(("{", "}")),
            "\"" => Some(("\"", "\"")),
            "'" => Some(("'", "'")),
            "（" => Some(("（", "）")),
            "【" => Some(("【", "】")),
            "“" | "”" => Some(("“", "”")),
            "‘" | "’" => Some(("‘", "’")),
            "《" => Some(("《", "》")),
            _ => None,
        };
        let Some((open, close)) = pair else { return false };
        let before = self.snapshot();
        self.doc.replace_range(a.block, b.offset..b.offset, close);
        self.doc.replace_range(a.block, a.offset..a.offset, open);
        let shift = open.len();
        self.caret.anchor = Position { block: a.block, offset: a.offset + shift };
        self.caret.head = Position { block: a.block, offset: b.offset + shift };
        self.record(before, cx);
        true
    }

    /// Pasting a plain URL over a non-empty text selection turns the selection into a link.
    pub(super) fn paste_url_over_selection(&mut self, text: &str, cx: &mut Context<Self>) -> bool {
        if self.caret.is_empty() || self.caret.composing.is_some() { return false; }
        let Some(url) = is_single_url(text) else { return false };
        let ranges = self.selection_ranges();
        if ranges.is_empty() { return false; }
        if ranges.iter().any(|(id, _)| self.doc.get(*id).is_some_and(|b| b.meta.code_language.is_some() || b.meta.raw_source.is_some())) {
            return false;
        }
        let before = self.snapshot();
        for (id, range) in ranges {
            self.doc.set_link(id, range, Some(url));
        }
        self.record(before, cx);
        true
    }

    /// Convert inline Markdown (`**bold**`, `*italic*`, `~~strike~~`, `` `code` ``, `==highlight==`, `[text](url)`)
    /// as soon as the closing delimiter is typed. Recorded as its own undo step so Cmd/Ctrl+Z restores the raw syntax.
    pub(super) fn inline_rule(&mut self, typed: &str, cx: &mut Context<Self>) {
        if !self.caret.is_empty() || self.caret.composing.is_some() { return; }
        let at = self.caret.head;
        let Some(block) = self.doc.get(at.block) else { return };
        if block.meta.code_language.is_some() || block.meta.raw_source.is_some() || block.meta.table.is_some() { return; }
        if at.offset < typed.len() || !block.text.is_char_boundary(at.offset) { return; }
        let close_pos = at.offset - typed.len();
        if block.spans.iter().any(|s| s.marks.contains(&MarkKind::Code) && s.start < close_pos && s.end >= at.offset) { return; }
        let before_caret = &block.text[..at.offset];
        match typed {
            "`" => {
                if before_caret.ends_with("``") || escaped(before_caret, close_pos) { return; }
                let search = &before_caret[..close_pos];
                let Some(open) = find_single_open(search, b'`', block) else { return };
                let inner = &before_caret[open + 1..close_pos];
                if inner.is_empty() || inner.contains('\n') || inner.contains('`') { return; }
                self.apply_inline_mark(at.block, open, 1, close_pos, at.offset, MarkKind::Code, cx);
                self.code_escape_after_close(cx);
            }
            "*" | "_" => {
                let ch = typed.as_bytes()[0];
                let d2 = if ch == b'*' { "**" } else { "__" };
                let d3 = if ch == b'*' { "***" } else { "___" };
                let after_ok = ch != b'_' || !block.text[at.offset..].starts_with(|c: char| c == '_' || c.is_ascii_alphanumeric());
                if !after_ok { return; }
                if before_caret.ends_with(d2) && !before_caret.ends_with(d3) && !escaped(before_caret, at.offset - 2) {
                    let close_start = at.offset - 2;
                    if let Some(open) = find_double_open(&before_caret[..close_start], d2, block) {
                        let inner = &before_caret[open + 2..close_start];
                        if valid_inner(inner) && !inner.contains(d2) {
                            self.apply_inline_mark(at.block, open, 2, close_start, at.offset, MarkKind::Bold, cx);
                            return;
                        }
                    }
                }
                if !before_caret.ends_with(d2) && !escaped(before_caret, close_pos) {
                    if let Some(open) = find_single_open(&before_caret[..close_pos], ch, block) {
                        let inner = &before_caret[open + 1..close_pos];
                        if valid_inner(inner) {
                            self.apply_inline_mark(at.block, open, 1, close_pos, at.offset, MarkKind::Italic, cx);
                        }
                    }
                }
            }
            "~" => {
                if !before_caret.ends_with("~~") || before_caret.ends_with("~~~") || escaped(before_caret, at.offset - 2) { return; }
                let close_start = at.offset - 2;
                let Some(open) = find_double_open(&before_caret[..close_start], "~~", block) else { return };
                let inner = &before_caret[open + 2..close_start];
                if valid_inner(inner) && !inner.contains("~~") {
                    self.apply_inline_mark(at.block, open, 2, close_start, at.offset, MarkKind::Strike, cx);
                }
            }
            "=" => {
                if !before_caret.ends_with("==") || before_caret.ends_with("===") || escaped(before_caret, at.offset - 2) { return; }
                let close_start = at.offset - 2;
                let Some(open) = find_double_open(&before_caret[..close_start], "==", block) else { return };
                let inner = &before_caret[open + 2..close_start];
                if !valid_inner(inner) || inner.contains("==") { return; }
                let len = inner.len();
                let before = self.snapshot();
                self.doc.replace_range(at.block, close_start..at.offset, "");
                self.doc.replace_range(at.block, open..open + 2, "");
                self.doc.set_style(at.block, open..open + len, "background-color", Some("yellow"));
                let to = Position { block: at.block, offset: open + len };
                self.caret.anchor = to; self.caret.head = to;
                self.record(before, cx);
            }
            ")" => {
                if escaped(before_caret, close_pos) { return; }
                if block.kind == super::model::Kind::Paragraph
                    && block.meta.table.is_none()
                    && block.meta.code_language.is_none()
                    && block.meta.raw_source.is_none()
                    && block.meta.callout.is_none()
                    && block.spans.is_empty()
                    && super::image_source::parse(&block.text).is_some()
                {
                    let before = self.snapshot();
                    self.promote_image_block(at.block);
                    let to = Position { block: at.block, offset: 0 };
                    self.caret.anchor = to; self.caret.head = to;
                    self.record(before, cx);
                    return;
                }
                let head = &before_caret[..close_pos];
                let Some(mid) = head.rfind("](") else { return };
                if escaped(head, mid) { return; }
                let url = &head[mid + 2..];
                if url.is_empty() || url.contains(char::is_whitespace) || url.contains(['(', ')', '[', ']']) { return; }
                let prefix = &head[..mid];
                let Some(open) = prefix.rfind('[') else { return };
                if escaped(prefix, open) || in_code_span(block, open) || (open > 0 && prefix.as_bytes()[open - 1] == b'!') { return; }
                let label = &prefix[open + 1..mid];
                if label.is_empty() || label.contains(['[', ']', '\n']) { return; }
                let label_len = label.len();
                let href = normalize_url(url);
                let before = self.snapshot();
                self.doc.replace_range(at.block, mid..at.offset, "");
                self.doc.replace_range(at.block, open..open + 1, "");
                self.doc.set_link(at.block, open..open + label_len, Some(&href));
                let to = Position { block: at.block, offset: open + label_len };
                self.caret.anchor = to; self.caret.head = to;
                self.record(before, cx);
            }
            _ => {}
        }
    }

    fn apply_inline_mark(&mut self, block_id: u64, open: usize, delim_len: usize, close_start: usize, close_end: usize, mark: MarkKind, cx: &mut Context<Self>) {
        let len = close_start - (open + delim_len);
        let before = self.snapshot();
        self.doc.replace_range(block_id, close_start..close_end, "");
        self.doc.replace_range(block_id, open..open + delim_len, "");
        let range = open..open + len;
        let already = self.doc.get(block_id).is_some_and(|b| {
            b.spans.iter().filter(|s| s.marks.contains(&mark))
                .map(|s| s.end.min(range.end).saturating_sub(s.start.max(range.start))).sum::<usize>() >= range.len()
        });
        if !already { self.doc.toggle_mark(block_id, range.clone(), mark); }
        let to = Position { block: block_id, offset: range.end };
        self.caret.anchor = to; self.caret.head = to;
        self.record(before, cx);
    }

    pub(in crate::editor) fn promote_image_block(&mut self, id: u64) -> bool {
        let Some(block) = self.doc.get(id) else { return false };
        let is_plain = block.kind == super::model::Kind::Paragraph
            && block.meta.table.is_none()
            && block.meta.code_language.is_none()
            && block.meta.raw_source.is_none()
            && block.meta.callout.is_none()
            && block.spans.is_empty();
        if !is_plain || super::image_source::parse(&block.text).is_none() {
            return false;
        }
        if let Some(block) = self.doc.get_mut(id) {
            let line = format!("{}\n", block.text.trim_end_matches(['\r', '\n']));
            block.text = line.clone();
            block.meta.raw_source = Some(line);
            block.meta.unsupported_reason = Some("Image".to_owned());
        }
        self.doc.refresh_block(id);
        true
    }
}
