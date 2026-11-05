//! Bracket matching (inner/outer) and same-level comma item selection.
use std::ops::Range;
use super::code_selection_tokens::{BracketKind, CodeToken, TokenKind};

#[derive(Debug, Clone)]
pub struct BracketPair {
    pub open_range: Range<usize>,
    pub close_range: Range<usize>,
}

pub fn match_bracket_at(
    _text: &str,
    tokens: &[CodeToken],
    offset: usize,
    include_delimiters: bool,
) -> Option<Range<usize>> {
    let pairs = collect_bracket_pairs(tokens);
    let matched = pairs.into_iter().filter(|pair| {
        let at_open_inner = offset == pair.open_range.end;
        let at_close_inner = offset == pair.close_range.start;
        at_open_inner || at_close_inner
    }).min_by_key(|pair| pair.close_range.end.saturating_sub(pair.open_range.start))?;

    if include_delimiters {
        Some(matched.open_range.start..matched.close_range.end)
    } else {
        Some(matched.open_range.end..matched.close_range.start)
    }
}

pub fn match_comma_item_at(
    text: &str,
    tokens: &[CodeToken],
    offset: usize,
    include_delimiters: bool,
) -> Option<Range<usize>> {
    let comma_idx = tokens.iter().position(|t| {
        t.kind == TokenKind::Comma && (offset == t.range.start || offset == t.range.end)
    })?;
    let comma_token = &tokens[comma_idx];

    let pairs = collect_bracket_pairs(tokens);
    let enclosing = pairs.iter().filter(|p| {
        p.open_range.end <= comma_token.range.start && comma_token.range.end <= p.close_range.start
    }).min_by_key(|p| p.close_range.start.saturating_sub(p.open_range.end));

    let (content_start, content_end) = if let Some(p) = enclosing {
        (p.open_range.end, p.close_range.start)
    } else {
        let line_start = text[..comma_token.range.start].rfind('\n').map_or(0, |p| p + 1);
        let line_end = text[comma_token.range.end..].find('\n').map_or(text.len(), |p| comma_token.range.end + p);
        (line_start, line_end)
    };

    let mut level = 0isize;
    let mut comma_positions = Vec::new();
    for (idx, t) in tokens.iter().enumerate() {
        if t.range.start < content_start || t.range.end > content_end { continue; }
        match &t.kind {
            TokenKind::OpenBracket(_) => level += 1,
            TokenKind::CloseBracket(_) => level -= 1,
            TokenKind::Comma if level == 0 => comma_positions.push(idx),
            _ => {}
        }
    }

    let pos_in_commas = comma_positions.iter().position(|&idx| idx == comma_idx)?;
    let item_start = if pos_in_commas == 0 {
        content_start
    } else {
        tokens[comma_positions[pos_in_commas - 1]].range.end
    };
    let item_end = comma_token.range.start;

    let next_item_start = comma_token.range.end;
    let next_item_end = if pos_in_commas + 1 < comma_positions.len() {
        tokens[comma_positions[pos_in_commas + 1]].range.start
    } else {
        content_end
    };

    if offset > comma_token.range.start {
        let raw = trim_span(text, next_item_start..next_item_end);
        if include_delimiters {
            Some(comma_token.range.start..raw.end)
        } else {
            Some(raw)
        }
    } else {
        let raw = trim_span(text, item_start..item_end);
        if include_delimiters {
            Some(raw.start..comma_token.range.end)
        } else {
            Some(raw)
        }
    }
}

pub fn collect_bracket_pairs(tokens: &[CodeToken]) -> Vec<BracketPair> {
    let mut stack: Vec<(BracketKind, Range<usize>)> = Vec::new();
    let mut pairs = Vec::new();

    for token in tokens {
        match &token.kind {
            TokenKind::OpenBracket(k) => stack.push((*k, token.range.clone())),
            TokenKind::CloseBracket(k) => {
                if stack.last().map(|(open_k, _)| open_k == k) == Some(true) {
                    let (_, open_range) = stack.pop().unwrap();
                    pairs.push(BracketPair {
                        open_range,
                        close_range: token.range.clone(),
                    });
                } else {
                    stack.clear();
                }
            }
            _ => {}
        }
    }
    pairs
}

pub fn trim_span(text: &str, span: Range<usize>) -> Range<usize> {
    if span.start >= span.end || span.end > text.len() {
        let pos = span.start.min(text.len());
        return pos..pos;
    }
    let sub = &text[span.start..span.end];
    let trimmed = sub.trim();
    if trimmed.is_empty() {
        return span.start..span.start;
    }
    let leading = sub.len() - sub.trim_start().len();
    let trailing = sub.len() - sub.trim_end().len();
    let start = (span.start + leading).min(span.end);
    let end = span.end.saturating_sub(trailing).max(start);
    start..end
}
