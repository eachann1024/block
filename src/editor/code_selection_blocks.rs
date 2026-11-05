//! Statement, indentation block, and line-end selection.
use std::ops::Range;
use super::code_selection_tokens::{BracketKind, CodeToken, TokenKind};

pub fn match_semicolon_at(
    text: &str,
    tokens: &[CodeToken],
    offset: usize,
    include_delimiters: bool,
) -> Option<Range<usize>> {
    let semi_idx = tokens.iter().position(|t| {
        t.kind == TokenKind::Semicolon && (offset == t.range.start || offset == t.range.end)
    })?;
    let semi_token = &tokens[semi_idx];
    let semi_pos = semi_token.range.start;

    let mut paren_depth = 0usize;
    let mut bracket_depth = 0usize;
    let mut brace_depth = 0usize;
    let mut stmt_start = 0;
    let mut seen_expr_token = false;

    for i in (0..semi_idx).rev() {
        let t = &tokens[i];
        match &t.kind {
            TokenKind::CloseBracket(BracketKind::Paren) => paren_depth += 1,
            TokenKind::OpenBracket(BracketKind::Paren) => {
                if paren_depth == 0 && bracket_depth == 0 && brace_depth == 0 {
                    stmt_start = t.range.end;
                    break;
                }
                paren_depth = paren_depth.saturating_sub(1);
                if paren_depth == 0 && bracket_depth == 0 && brace_depth == 0 {
                    seen_expr_token = true;
                }
            }
            TokenKind::CloseBracket(BracketKind::Bracket) => bracket_depth += 1,
            TokenKind::OpenBracket(BracketKind::Bracket) => {
                if paren_depth == 0 && bracket_depth == 0 && brace_depth == 0 {
                    stmt_start = t.range.end;
                    break;
                }
                bracket_depth = bracket_depth.saturating_sub(1);
                if paren_depth == 0 && bracket_depth == 0 && brace_depth == 0 {
                    seen_expr_token = true;
                }
            }
            TokenKind::CloseBracket(BracketKind::Brace) => {
                if paren_depth == 0 && bracket_depth == 0 && brace_depth == 0 && seen_expr_token {
                    if !is_do_while_clause(text, tokens, i) && !is_chained_brace(text, tokens, i, semi_idx) {
                        stmt_start = t.range.end;
                        break;
                    }
                }
                brace_depth += 1;
            }
            TokenKind::OpenBracket(BracketKind::Brace) => {
                if paren_depth == 0 && bracket_depth == 0 && brace_depth == 0 {
                    stmt_start = t.range.end;
                    break;
                }
                brace_depth = brace_depth.saturating_sub(1);
                seen_expr_token = true;
            }
            TokenKind::Semicolon => {
                if paren_depth == 0 && bracket_depth == 0 && brace_depth == 0 {
                    stmt_start = t.range.end;
                    break;
                }
            }
            TokenKind::Comment => {}
            TokenKind::Other if text[t.range.clone()].trim().is_empty() => {}
            _ => {
                if paren_depth == 0 && bracket_depth == 0 && brace_depth == 0 {
                    seen_expr_token = true;
                }
            }
        }
    }

    let sub = &text[stmt_start..semi_pos];
    let leading = sub.len() - sub.trim_start().len();
    let actual_start = (stmt_start + leading).min(semi_pos);

    if actual_start >= semi_pos {
        return None;
    }

    if include_delimiters {
        Some(actual_start..semi_token.range.end)
    } else {
        Some(actual_start..semi_token.range.start)
    }
}

pub fn match_block_or_line_at(
    text: &str,
    tokens: &[CodeToken],
    offset: usize,
    lang: &str,
    include_delimiters: bool,
) -> Option<Range<usize>> {
    if let Some(r) = match_keyword_block_at(text, tokens, offset, lang, include_delimiters) {
        return Some(r);
    }
    if let Some(r) = super::code_selection_pairs::match_word_pair_block(text, tokens, offset, lang, include_delimiters) {
        return Some(r);
    }
    if is_at_line_end(text, offset) {
        return match_line_end(text, offset);
    }
    if let Some(r) = super::code_selection_indent::match_indent_block(text, tokens, offset, lang, include_delimiters) {
        return Some(r);
    }
    None
}

fn match_keyword_block_at(
    text: &str,
    tokens: &[CodeToken],
    offset: usize,
    lang: &str,
    _include_delimiters: bool,
) -> Option<Range<usize>> {
    let (kw_idx, kw_token) = tokens.iter().enumerate().find(|(idx, t)| {
        if offset >= t.range.start && offset <= t.range.end {
            if t.kind == TokenKind::Word && is_decl_keyword(&text[t.range.clone()], lang) {
                return true;
            }
            if lang == "css" && t.kind == TokenKind::Other && &text[t.range.clone()] == "@" {
                if let Some(next) = tokens.get(idx + 1) {
                    return next.kind == TokenKind::Word && is_decl_keyword(&text[next.range.clone()], lang);
                }
            }
        }
        false
    })?;

    let line_start = text[..kw_token.range.start].rfind('\n').map_or(0, |p| p + 1);
    let line_prefix = &text[line_start..kw_token.range.start];
    let decl_start = line_start + (line_prefix.len() - line_prefix.trim_start().len());

    let mut paren_depth = 0usize;
    let mut bracket_depth = 0usize;
    let mut brace_token_idx = None;

    for i in (kw_idx + 1)..tokens.len() {
        let t = &tokens[i];
        match &t.kind {
            TokenKind::OpenBracket(BracketKind::Paren) => paren_depth += 1,
            TokenKind::CloseBracket(BracketKind::Paren) => paren_depth = paren_depth.saturating_sub(1),
            TokenKind::OpenBracket(BracketKind::Bracket) => bracket_depth += 1,
            TokenKind::CloseBracket(BracketKind::Bracket) => bracket_depth = bracket_depth.saturating_sub(1),
            TokenKind::OpenBracket(BracketKind::Brace) if paren_depth == 0 && bracket_depth == 0 => {
                brace_token_idx = Some(i);
                break;
            }
            TokenKind::Semicolon if paren_depth == 0 && bracket_depth == 0 => return Some(decl_start..t.range.end),
            _ => {}
        }
    }

    let open_brace_idx = brace_token_idx?;
    let mut brace_depth = 1usize;
    let mut close_brace_end = None;

    for i in (open_brace_idx + 1)..tokens.len() {
        let t = &tokens[i];
        match &t.kind {
            TokenKind::OpenBracket(BracketKind::Brace) => brace_depth += 1,
            TokenKind::CloseBracket(BracketKind::Brace) => {
                brace_depth -= 1;
                if brace_depth == 0 {
                    close_brace_end = Some(t.range.end);
                    break;
                }
            }
            _ => {}
        }
    }

    let block_end = close_brace_end?;
    Some(decl_start..block_end)
}

fn is_decl_keyword(word: &str, lang: &str) -> bool {
    match lang {
        "rust" => matches!(word, "fn" | "impl" | "struct" | "enum" | "trait" | "if" | "else" | "match" | "for" | "while" | "loop"),
        "javascript" | "typescript" => matches!(word, "function" | "class" | "if" | "else" | "for" | "while" | "switch" | "try" | "catch" | "finally"),
        "css" => matches!(word, "media" | "keyframes" | "supports"),
        _ => false,
    }
}

fn is_chained_brace(text: &str, tokens: &[CodeToken], close_brace_idx: usize, semi_idx: usize) -> bool {
    let next = tokens[close_brace_idx + 1..semi_idx].iter().find(|t| {
        t.kind != TokenKind::Comment && !text[t.range.clone()].trim().is_empty()
    });
    let Some(next) = next else { return false; };
    let s = &text[next.range.clone()];
    s == "." || s == "?." || matches!(next.kind, TokenKind::OpenBracket(BracketKind::Bracket | BracketKind::Paren))
}

fn is_do_while_clause(text: &str, tokens: &[CodeToken], close_brace_idx: usize) -> bool {
    let next_tok = tokens[close_brace_idx + 1..].iter().find(|t| {
        t.kind != TokenKind::Comment && !text[t.range.clone()].trim().is_empty()
    });
    let Some(next_tok) = next_tok else { return false; };
    if next_tok.kind != TokenKind::Word || &text[next_tok.range.clone()] != "while" {
        return false;
    }
    let mut depth = 1usize;
    for j in (0..close_brace_idx).rev() {
        match &tokens[j].kind {
            TokenKind::CloseBracket(BracketKind::Brace) => depth += 1,
            TokenKind::OpenBracket(BracketKind::Brace) => {
                depth -= 1;
                if depth == 0 {
                    let prev = tokens[..j].iter().rev().find(|t| {
                        t.kind != TokenKind::Comment && !text[t.range.clone()].trim().is_empty()
                    });
                    if let Some(p) = prev {
                        return p.kind == TokenKind::Word && &text[p.range.clone()] == "do";
                    }
                    return false;
                }
            }
            _ => {}
        }
    }
    false
}

fn is_at_line_end(text: &str, offset: usize) -> bool {
    let line_start = text[..offset].rfind('\n').map_or(0, |p| p + 1);
    let line_end = text[offset..].find('\n').map_or(text.len(), |p| offset + p);
    let line = &text[line_start..line_end];
    let non_ws_end = line.trim_end().len();
    (offset - line_start) >= non_ws_end
}

fn match_line_end(text: &str, offset: usize) -> Option<Range<usize>> {
    let line_start = text[..offset].rfind('\n').map_or(0, |p| p + 1);
    let line_end = text[offset..].find('\n').map_or(text.len(), |p| offset + p);
    let line = &text[line_start..line_end];
    let content_len = line.trim_end().len();
    let end = line_start + content_len;
    if line_start <= end {
        Some(line_start..end)
    } else {
        Some(line_start..line_start)
    }
}
