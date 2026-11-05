//! Smart double-click code selection for code blocks (HBuilderX-style).
use std::cell::RefCell;
use std::ops::Range;
use super::block_menu::canonical_lang_tag;
use super::code_selection_tokens::{CodeToken, TokenKind, scan_tokens};
use super::view::selection::boundary;

#[path = "code_selection_markdown.rs"]
mod markdown_span;

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

    if unclosed_covers(&tokens, offset) {
        return ident_or_none(text, offset);
    }
    if let Some(valid) = checked(text, super::code_selection_brackets::match_bracket_at(text, &tokens, offset, include_delimiters)) {
        return Some(valid);
    }
    if let Some(valid) = checked(text, match_string_at(text, &tokens, offset, include_delimiters)) {
        return Some(valid);
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
        return ident_or_none(text, offset);
    }
    if offset_in_string_text(&tokens, offset) {
        return ident_or_none(text, offset);
    }
    if let Some(valid) = checked(text, super::code_selection_tags::match_tag_at(text, offset, lang, include_delimiters)) {
        return Some(valid);
    }
    if let Some(valid) = checked(text, super::code_selection_brackets::match_comma_item_at(text, &tokens, offset, include_delimiters)) {
        return Some(valid);
    }
    if let Some(valid) = checked(text, super::code_selection_blocks::match_semicolon_at(text, &tokens, offset, include_delimiters)) {
        return Some(valid);
    }
    if !on_delim_edge(&tokens, offset) {
        if let Some(valid) = checked(text, super::code_selection_blocks::match_block_or_line_at(text, &tokens, offset, lang, include_delimiters)) {
            return Some(valid);
        }
    }
    if lang == "markdown" {
        if let Some(valid) = checked(text, markdown_span::match_markdown_span(text, offset, include_delimiters)) {
            return Some(valid);
        }
    }
    if let Some(valid) = checked(text, match_lifetime_token(text, &tokens, offset)) {
        return Some(valid);
    }
    ident_or_none(text, offset)
}

fn on_delim_edge(tokens: &[CodeToken], offset: usize) -> bool {
    tokens.iter().any(|t| {
        // Comma and semicolon stay selectable. A bracket, quote, or other
        // one-byte mark that did not match must not fall through to the line.
        let structural = matches!(t.kind, TokenKind::OpenBracket(_) | TokenKind::CloseBracket(_)
            | TokenKind::StringLit { .. })
            || (t.kind == TokenKind::Other && t.range.len() == 1);
        structural && (offset == t.range.start || offset == t.range.end)
    })
}

fn checked(text: &str, range: Option<Range<usize>>) -> Option<Range<usize>> {
    validate_selection(text, range?)
}

fn ident_or_none(text: &str, offset: usize) -> Option<Range<usize>> {
    checked(text, match_identifier_at(text, offset))
}

fn unclosed_covers(tokens: &[CodeToken], offset: usize) -> bool {
    tokens.iter().any(|t| {
        matches!(t.kind, TokenKind::StringLit { closed: false, .. })
            && offset >= t.range.start
            && offset <= t.range.end
    })
}

fn offset_in_string_text(tokens: &[CodeToken], offset: usize) -> bool {
    let pairs = super::code_selection_brackets::collect_bracket_pairs(tokens);
    let mut best: Option<&CodeToken> = None;
    for token in tokens {
        let TokenKind::StringLit { closed: true, .. } = token.kind else { continue };
        if offset <= token.range.start || offset >= token.range.end { continue; }
        if best.is_none_or(|b| token.range.len() < b.range.len()) {
            best = Some(token);
        }
    }
    let Some(string) = best else { return false };
    !pairs.iter().any(|pair| {
        pair.open_range.start >= string.range.start
            && pair.close_range.end <= string.range.end
            && offset > pair.open_range.start
            && offset < pair.close_range.end
    })
}

fn match_lifetime_token(text: &str, tokens: &[CodeToken], offset: usize) -> Option<Range<usize>> {
    tokens.iter().find(|t| {
        t.kind == TokenKind::Word
            && offset >= t.range.start
            && offset <= t.range.end
            && text.get(t.range.clone()).is_some_and(|s| s.starts_with('\'') || s.contains('#'))
    }).map(|t| t.range.clone())
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
    let mut best: Option<(usize, Range<usize>)> = None;
    for token in tokens {
        let TokenKind::StringLit { open_len, close_len, closed } = token.kind else { continue };
        if !closed || (open_len == 0 && close_len == 0) { continue; }
        let start = token.range.start;
        let end = token.range.end;
        let open_inner = start + open_len;
        let close_inner = end.saturating_sub(close_len);
        // Inner delimiter edges only. opening.start and closing.end stay with
        // the neighboring comma, operator, or word. Empty quotes still match:
        // both inners are the same offset.
        if offset != open_inner && offset != close_inner { continue; }
        if offset == end && offset != close_inner && delim_boundary(tokens, offset) { continue; }
        if offset == start && offset != open_inner && delim_boundary(tokens, offset) { continue; }
        let chosen = if include_delimiters {
            start..end
        } else if open_inner <= close_inner {
            open_inner..close_inner
        } else {
            start..start
        };
        if best.as_ref().is_none_or(|pair| token.range.len() < pair.0) {
            best = Some((token.range.len(), chosen));
        }
    }
    best.map(|(_, range)| range)
}

fn delim_boundary(tokens: &[CodeToken], offset: usize) -> bool {
    tokens.iter().any(|t| {
        matches!(t.kind, TokenKind::Comma | TokenKind::Semicolon)
            && (t.range.start == offset || t.range.end == offset)
    })
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
    if offset < text.len() {
        let ch = text[offset..].chars().next()?;
        if !ch.is_ascii() { return None; }
    }
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
