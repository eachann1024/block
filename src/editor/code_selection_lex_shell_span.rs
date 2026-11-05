//! Shell spans inside quotes, heredocs, and `$()` / `${}` so substitutions stay selectable.
use super::super::{BracketKind, CodeToken, TokenKind};
use super::super::code_selection_lex_common::{is_ident_continue, is_ident_start};

fn bump(pos: usize, limit: usize) -> usize {
    if pos + 1 < limit { pos + 2 } else { limit }
}

pub fn scan_double_quote(text: &str, i: usize) -> (usize, Vec<CodeToken>) {
    let bytes = text.as_bytes();
    let mut pos = i + 1;
    let mut inner = Vec::new();
    let mut closed = false;
    while pos < bytes.len() {
        let b = bytes[pos];
        if b == b'\\' {
            pos = bump(pos, bytes.len());
            continue;
        }
        if b == b'"' {
            pos += 1;
            closed = true;
            break;
        }
        if b == b'$' && pos + 1 < bytes.len() && matches!(bytes[pos + 1], b'(' | b'{') {
            pos = push_expansion(text, pos, bytes.len(), &mut inner);
            continue;
        }
        if b == b'`' {
            pos = push_backtick(text, pos, bytes.len(), &mut inner);
            continue;
        }
        pos += 1;
    }
    let mut all = Vec::with_capacity(1 + inner.len());
    all.push(CodeToken {
        range: i..pos,
        kind: TokenKind::StringLit { open_len: 1, close_len: if closed { 1 } else { 0 }, closed },
    });
    all.extend(inner);
    (pos - i, all)
}

pub fn push_expansions(text: &str, start: usize, end: usize, tokens: &mut Vec<CodeToken>) {
    let bytes = text.as_bytes();
    let end = end.min(bytes.len());
    let mut j = start;
    while j < end {
        if bytes[j] == b'\\' {
            j = bump(j, end);
            continue;
        }
        if bytes[j] == b'$' && j + 1 < end && matches!(bytes[j + 1], b'(' | b'{') {
            j = push_expansion(text, j, end, tokens);
            continue;
        }
        if bytes[j] == b'`' {
            j = push_backtick(text, j, end, tokens);
            continue;
        }
        j += 1;
    }
}

fn push_expansion(text: &str, dollar: usize, limit: usize, tokens: &mut Vec<CodeToken>) -> usize {
    let bytes = text.as_bytes();
    let open_b = bytes[dollar + 1];
    let close_b = if open_b == b'(' { b')' } else { b'}' };
    let kind = if open_b == b'(' { BracketKind::Paren } else { BracketKind::Brace };
    let open_at = dollar + 1;
    tokens.push(CodeToken { range: open_at..open_at + 1, kind: TokenKind::OpenBracket(kind) });
    let inner = open_at + 1;
    if let Some(close_at) = find_balanced(bytes, inner, limit, open_b, close_b) {
        tokenize_shell_span(text, inner, close_at, tokens);
        tokens.push(CodeToken { range: close_at..close_at + 1, kind: TokenKind::CloseBracket(kind) });
        close_at + 1
    } else {
        tokenize_shell_span(text, inner, limit, tokens);
        limit
    }
}

fn push_backtick(text: &str, start: usize, limit: usize, tokens: &mut Vec<CodeToken>) -> usize {
    let bytes = text.as_bytes();
    let mut k = start + 1;
    while k < limit {
        if bytes[k] == b'\\' {
            k = bump(k, limit);
        } else if bytes[k] == b'`' {
            tokens.push(CodeToken {
                range: start..k + 1,
                kind: TokenKind::StringLit { open_len: 1, close_len: 1, closed: true },
            });
            return k + 1;
        } else {
            k += 1;
        }
    }
    tokens.push(CodeToken {
        range: start..limit,
        kind: TokenKind::StringLit { open_len: 1, close_len: 0, closed: false },
    });
    limit
}

fn tokenize_shell_span(text: &str, mut j: usize, end: usize, tokens: &mut Vec<CodeToken>) {
    let bytes = text.as_bytes();
    let end = end.min(bytes.len());
    while j < end {
        if bytes[j] == b'#' {
            let rel = bytes[j..end].iter().position(|&c| c == b'\n').map_or(end - j, |p| p + 1);
            tokens.push(CodeToken { range: j..j + rel, kind: TokenKind::Comment });
            j += rel;
            continue;
        }
        if let Some((len, ts)) = super::scan_shell_token(text, j) {
            if j + len <= end {
                tokens.extend(ts);
                j += len;
                continue;
            }
        }
        let b = bytes[j];
        let kind = match b {
            b'(' => Some(TokenKind::OpenBracket(BracketKind::Paren)),
            b')' => Some(TokenKind::CloseBracket(BracketKind::Paren)),
            b'{' => Some(TokenKind::OpenBracket(BracketKind::Brace)),
            b'}' => Some(TokenKind::CloseBracket(BracketKind::Brace)),
            b'[' => Some(TokenKind::OpenBracket(BracketKind::Bracket)),
            b']' => Some(TokenKind::CloseBracket(BracketKind::Bracket)),
            b',' => Some(TokenKind::Comma),
            b';' => Some(TokenKind::Semicolon),
            b':' => Some(TokenKind::Colon),
            _ => None,
        };
        if let Some(kind) = kind {
            tokens.push(CodeToken { range: j..j + 1, kind });
            j += 1;
            continue;
        }
        if is_ident_start(b) || b.is_ascii_digit() {
            let start = j;
            j += 1;
            while j < end && (is_ident_continue(bytes[j]) || bytes[j] == b'.') { j += 1; }
            tokens.push(CodeToken { range: start..j, kind: TokenKind::Word });
            continue;
        }
        j += text[j..].chars().next().map_or(1, |c| c.len_utf8());
    }
}

fn find_balanced(bytes: &[u8], start: usize, limit: usize, open_b: u8, close_b: u8) -> Option<usize> {
    let limit = limit.min(bytes.len());
    let mut pos = start;
    let mut depth = 1isize;
    while pos < limit && depth > 0 {
        let b = bytes[pos];
        if b == b'\\' {
            pos = bump(pos, limit);
            continue;
        }
        if b == b'"' || b == b'\'' || b == b'`' {
            let q = b;
            pos += 1;
            while pos < limit && bytes[pos] != q {
                if q != b'\'' && bytes[pos] == b'\\' { pos = bump(pos, limit); } else { pos += 1; }
            }
            if pos < limit { pos += 1; }
            continue;
        }
        if b == open_b { depth += 1; }
        else if b == close_b {
            depth -= 1;
            if depth == 0 { return Some(pos); }
        }
        pos += 1;
    }
    None
}
