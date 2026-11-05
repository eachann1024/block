//! Python-specific lexical scanner: prefixes, escape-aware triple quotes, and f-string nesting.
use super::{BracketKind, CodeToken, TokenKind};

pub fn scan_python_token(text: &str, i: usize) -> Option<(usize, Vec<CodeToken>)> {
    let rem = text[i..].as_bytes();
    let (prefix_len, q, is_triple, is_fstring) = check_python_prefix(rem)?;

    if is_fstring {
        Some(scan_python_fstring(text, i, prefix_len, q, is_triple))
    } else if is_triple {
        Some(scan_python_triple_str(rem, i, prefix_len, q))
    } else {
        Some(scan_python_single_str(rem, i, prefix_len, q))
    }
}

fn check_python_prefix(rem: &[u8]) -> Option<(usize, u8, bool, bool)> {
    let mut p = 0;
    while p < rem.len() && p < 2 && matches!(rem[p], b'r'|b'R'|b'u'|b'U'|b'f'|b'F'|b'b'|b'B') {
        p += 1;
    }
    if p < rem.len() && (rem[p] == b'"' || rem[p] == b'\'') {
        let q = rem[p];
        let is_triple = p + 2 < rem.len() && rem[p + 1] == q && rem[p + 2] == q;
        let is_fstring = rem[..p].iter().any(|&c| c == b'f' || c == b'F');
        Some((p, q, is_triple, is_fstring))
    } else {
        None
    }
}

fn scan_python_triple_str(rem: &[u8], i: usize, prefix_len: usize, q: u8) -> (usize, Vec<CodeToken>) {
    let open_len = prefix_len + 3;
    let mut pos = open_len;
    let delim = [q; 3];

    while pos + 2 < rem.len() {
        if rem[pos..].starts_with(&delim) {
            let mut bs = 0;
            while bs < pos && rem[pos - 1 - bs] == b'\\' {
                bs += 1;
            }
            if bs % 2 == 0 {
                let total = pos + 3;
                return (total, vec![CodeToken {
                    range: i..i + total,
                    kind: TokenKind::StringLit { open_len, close_len: 3, closed: true },
                }]);
            }
        }
        pos += 1;
    }

    (rem.len(), vec![CodeToken {
        range: i..i + rem.len(),
        kind: TokenKind::StringLit { open_len, close_len: 0, closed: false },
    }])
}

fn scan_python_single_str(rem: &[u8], i: usize, prefix_len: usize, q: u8) -> (usize, Vec<CodeToken>) {
    let open_len = prefix_len + 1;
    let mut pos = open_len;
    let mut escaped = false;

    while pos < rem.len() {
        let c = rem[pos];
        if escaped {
            escaped = false;
            pos += 1;
        } else if c == b'\\' {
            escaped = true;
            pos += 1;
        } else if c == q {
            let total = pos + 1;
            return (total, vec![CodeToken {
                range: i..i + total,
                kind: TokenKind::StringLit { open_len, close_len: 1, closed: true },
            }]);
        } else if c == b'\n' {
            break;
        } else {
            pos += 1;
        }
    }

    (pos, vec![CodeToken {
        range: i..i + pos,
        kind: TokenKind::StringLit { open_len, close_len: 0, closed: false },
    }])
}

fn scan_python_fstring(
    text: &str,
    i: usize,
    prefix_len: usize,
    q: u8,
    is_triple: bool,
) -> (usize, Vec<CodeToken>) {
    let open_len = prefix_len + if is_triple { 3 } else { 1 };
    let close_len = if is_triple { 3 } else { 1 };
    let rem = text[i..].as_bytes();
    let mut pos = open_len;
    let mut inner_tokens = Vec::new();
    let mut closed = false;

    while pos < rem.len() {
        if is_triple {
            if rem[pos..].starts_with(&[q, q, q]) {
                let mut bs = 0;
                while bs < pos && rem[pos - 1 - bs] == b'\\' { bs += 1; }
                if bs % 2 == 0 {
                    pos += 3;
                    closed = true;
                    break;
                }
            }
        } else {
            let mut bs = 0;
            while bs < pos && rem[pos - 1 - bs] == b'\\' { bs += 1; }
            if rem[pos] == q && bs % 2 == 0 {
                pos += 1;
                closed = true;
                break;
            }
            if rem[pos] == b'\n' && bs % 2 == 0 {
                break;
            }
        }

        if rem[pos] == b'{' {
            if pos + 1 < rem.len() && rem[pos + 1] == b'{' {
                pos += 2;
                continue;
            }
            let brace_start = i + pos;
            inner_tokens.push(CodeToken {
                range: brace_start..brace_start + 1,
                kind: TokenKind::OpenBracket(BracketKind::Brace),
            });
            pos += 1;
            pos += scan_fstring_expr(text, i + pos, &mut inner_tokens);
            continue;
        }

        if rem[pos] == b'}' {
            if pos + 1 < rem.len() && rem[pos + 1] == b'}' {
                pos += 2;
                continue;
            }
        }

        pos += 1;
    }

    let mut all_tokens = Vec::with_capacity(1 + inner_tokens.len());
    all_tokens.push(CodeToken {
        range: i..i + pos,
        kind: TokenKind::StringLit { open_len, close_len: if closed { close_len } else { 0 }, closed },
    });
    all_tokens.extend(inner_tokens);
    (pos, all_tokens)
}

fn scan_fstring_expr(text: &str, start_offset: usize, tokens: &mut Vec<CodeToken>) -> usize {
    let bytes = text[start_offset..].as_bytes();
    let mut pos = 0;
    let mut depth = 1;

    while pos < bytes.len() && depth > 0 {
        let cur = start_offset + pos;
        let sub = &bytes[pos..];

        if let Some((len, ts)) = scan_python_token(text, cur) {
            tokens.extend(ts);
            pos += len;
            continue;
        }

        match sub[0] {
            b'{' => {
                depth += 1;
                tokens.push(CodeToken { range: cur..cur + 1, kind: TokenKind::OpenBracket(BracketKind::Brace) });
                pos += 1;
            }
            b'}' => {
                depth -= 1;
                tokens.push(CodeToken { range: cur..cur + 1, kind: TokenKind::CloseBracket(BracketKind::Brace) });
                pos += 1;
            }
            b'(' => {
                tokens.push(CodeToken { range: cur..cur + 1, kind: TokenKind::OpenBracket(BracketKind::Paren) });
                pos += 1;
            }
            b')' => {
                tokens.push(CodeToken { range: cur..cur + 1, kind: TokenKind::CloseBracket(BracketKind::Paren) });
                pos += 1;
            }
            b'[' => {
                tokens.push(CodeToken { range: cur..cur + 1, kind: TokenKind::OpenBracket(BracketKind::Bracket) });
                pos += 1;
            }
            b']' => {
                tokens.push(CodeToken { range: cur..cur + 1, kind: TokenKind::CloseBracket(BracketKind::Bracket) });
                pos += 1;
            }
            b',' => {
                tokens.push(CodeToken { range: cur..cur + 1, kind: TokenKind::Comma });
                pos += 1;
            }
            b':' => {
                tokens.push(CodeToken { range: cur..cur + 1, kind: TokenKind::Colon });
                pos += 1;
            }
            _ => pos += 1,
        }
    }
    pos
}
