//! Smart double-click code selection for code blocks (HBuilderX-style).
use std::cell::RefCell;
use std::ops::Range;
use super::block_menu::canonical_lang_tag;
use super::code_selection_tokens::{CodeToken, TokenKind, scan_tokens};
use super::view::selection::boundary;

const MAX_SCAN_BYTES: usize = 128 * 1024;

struct TokenCacheEntry {
    content: String,
    lang: &'static str,
    tokens: Vec<CodeToken>,
}

thread_local! {
    static CACHED_TOKENS: RefCell<Option<TokenCacheEntry>> = const { RefCell::new(None) };
}

fn get_or_scan_tokens(text: &str, lang: &'static str) -> Vec<CodeToken> {
    CACHED_TOKENS.with(|cell| {
        let mut borrow = cell.borrow_mut();
        if let Some(entry) = borrow.as_ref() {
            if entry.lang == lang && entry.content == text {
                return entry.tokens.clone();
            }
        }
        let tokens = scan_tokens(text, lang);
        *borrow = Some(TokenCacheEntry {
            content: text.to_string(),
            lang,
            tokens: tokens.clone(),
        });
        tokens
    })
}

pub fn code_select(
    text: &str,
    raw_offset: usize,
    lang_tag: &str,
    include_delimiters: bool,
) -> Option<Range<usize>> {
    if text.is_empty() || text.len() > MAX_SCAN_BYTES { return None; }
    let lang = canonical_lang_tag(lang_tag);
    let offset = boundary(text, raw_offset);
    let tokens = get_or_scan_tokens(text, lang);
    let has_unclosed_str = tokens.iter().any(|t| matches!(t.kind, TokenKind::StringLit { closed: false, .. }));

    if !has_unclosed_str {
        if let Some(r) = super::code_selection_brackets::match_bracket_at(text, &tokens, offset, include_delimiters) {
            if let Some(valid) = validate_selection(text, r) { return Some(valid); }
        }
        if let Some(r) = match_string_at(text, &tokens, offset, include_delimiters) {
            if let Some(valid) = validate_selection(text, r) { return Some(valid); }
        }
    }

    if let Some(comment_token) = tokens.iter().find(|t| {
        t.kind == TokenKind::Comment && offset >= t.range.start && offset <= t.range.end
    }) {
        let comment_text = &text[comment_token.range.clone()];
        let open_len = comment_opening_len(comment_text);
        if open_len > 0 && offset <= comment_token.range.start + open_len {
            let trimmed_len = comment_text.trim_end_matches(['\r', '\n']).len();
            let r = comment_token.range.start..comment_token.range.start + trimmed_len;
            if let Some(valid) = validate_selection(text, r) { return Some(valid); }
        }
        if let Some(r) = match_identifier_at(text, offset) {
            if let Some(valid) = validate_selection(text, r) { return Some(valid); }
        }
        return None;
    }

    let in_str = tokens.iter().any(|t| {
        matches!(t.kind, TokenKind::StringLit { .. }) && offset >= t.range.start && offset <= t.range.end
    });
    if in_str {
        if let Some(r) = match_identifier_at(text, offset) {
            if let Some(valid) = validate_selection(text, r) { return Some(valid); }
        }
        return None;
    }

    if has_unclosed_str {
        if let Some(r) = match_identifier_at(text, offset) {
            if let Some(valid) = validate_selection(text, r) { return Some(valid); }
        }
        return None;
    }

    if let Some(r) = super::code_selection_tags::match_tag_at(text, offset, lang, include_delimiters) {
        if let Some(valid) = validate_selection(text, r) { return Some(valid); }
    }
    if let Some(r) = super::code_selection_brackets::match_comma_item_at(text, &tokens, offset, include_delimiters) {
        if let Some(valid) = validate_selection(text, r) { return Some(valid); }
    }
    if let Some(r) = super::code_selection_blocks::match_semicolon_at(text, &tokens, offset, include_delimiters) {
        if let Some(valid) = validate_selection(text, r) { return Some(valid); }
    }
    if let Some(r) = super::code_selection_blocks::match_block_or_line_at(text, &tokens, offset, lang, include_delimiters) {
        if let Some(valid) = validate_selection(text, r) { return Some(valid); }
    }
    if let Some(r) = match_identifier_at(text, offset) {
        if let Some(valid) = validate_selection(text, r) { return Some(valid); }
    }
    None
}

fn comment_opening_len(comment_text: &str) -> usize {
    if comment_text.starts_with("<!--") {
        4
    } else if comment_text.starts_with("///") || comment_text.starts_with("//!") || comment_text.starts_with("/**") {
        3
    } else if comment_text.starts_with("//") || comment_text.starts_with("/*") || comment_text.starts_with("/-") || comment_text.starts_with("--") || comment_text.starts_with("%%") {
        2
    } else if comment_text.starts_with('#') {
        comment_text.chars().take_while(|&c| c == '#').count()
    } else if comment_text.starts_with('%') {
        1
    } else {
        0
    }
}

fn match_string_at(
    _text: &str,
    tokens: &[CodeToken],
    offset: usize,
    include_delimiters: bool,
) -> Option<Range<usize>> {
    for token in tokens {
        if let TokenKind::StringLit { open_len, close_len, closed } = &token.kind {
            if !*closed {
                continue;
            }
            let start = token.range.start;
            let end = token.range.end;
            let open_inner = start + open_len;
            let close_inner = if end >= *close_len { end - close_len } else { end };

            let at_open = offset >= start && offset <= open_inner;
            let at_close = offset >= close_inner && offset <= end;

            if at_open || at_close {
                if include_delimiters {
                    return Some(start..end);
                }
                if open_inner <= close_inner {
                    return Some(open_inner..close_inner);
                }
                return Some(start..start);
            }
        }
    }
    None
}

pub fn validate_selection(text: &str, range: Range<usize>) -> Option<Range<usize>> {
    if range.start > range.end || range.end > text.len() {
        return None;
    }
    if !text.is_char_boundary(range.start) || !text.is_char_boundary(range.end) {
        return None;
    }
    Some(range)
}

pub fn match_identifier_at(text: &str, raw_offset: usize) -> Option<Range<usize>> {
    if text.is_empty() { return None; }
    let offset = boundary(text, raw_offset);
    let probe = if offset < text.len() && is_code_ident_char(text[offset..].chars().next()?) {
        offset
    } else if offset > 0 {
        let prev_ch = text[..offset].chars().next_back()?;
        if is_code_ident_char(prev_ch) {
            offset - prev_ch.len_utf8()
        } else {
            return None;
        }
    } else {
        return None;
    };

    let start = text[..probe]
        .char_indices()
        .rev()
        .take_while(|(_, c)| is_code_ident_char(*c))
        .last()
        .map(|(i, _)| i)
        .unwrap_or(probe);

    let end = text[probe..]
        .char_indices()
        .take_while(|(_, c)| is_code_ident_char(*c))
        .last()
        .map(|(i, c)| probe + i + c.len_utf8())
        .unwrap_or(probe);

    if start < end && end <= text.len() && text.is_char_boundary(start) && text.is_char_boundary(end) {
        Some(start..end)
    } else {
        None
    }
}

fn is_code_ident_char(c: char) -> bool {
    c.is_ascii_alphanumeric() || c == '_' || c == '$'
}
