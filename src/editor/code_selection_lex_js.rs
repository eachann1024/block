//! JavaScript / TypeScript lexical scanner: regex vs division disambiguation and template literal nesting.
use super::{BracketKind, CodeToken, TokenKind};

pub fn can_be_regex(tokens: &[CodeToken], text: &str) -> bool {
    let last = tokens.iter().rev().find(|t| {
        if t.kind == TokenKind::Comment { return false; }
        let slice = &text[t.range.clone()];
        !slice.trim().is_empty()
    });
    let Some(prev) = last else { return true; };
    match &prev.kind {
        TokenKind::Word => {
            let w = &text[prev.range.clone()];
            matches!(
                w,
                "return" | "case" | "default" | "yield" | "await" | "throw"
                    | "typeof" | "void" | "delete" | "instanceof" | "in" | "of" | "do" | "else"
            )
        }
        TokenKind::CloseBracket(BracketKind::Paren | BracketKind::Bracket) => false,
        TokenKind::StringLit { closed: true, .. } => false,
        _ => true,
    }
}

pub fn scan_js_regex(rem: &[u8]) -> Option<(usize, TokenKind)> {
    if rem.is_empty() || rem[0] != b'/' { return None; }
    if rem.len() > 1 && (rem[1] == b'/' || rem[1] == b'*') { return None; }

    let mut pos = 1;
    let mut in_class = false;

    while pos < rem.len() {
        let b = rem[pos];
        if b == b'\n' {
            return None;
        } else if b == b'\\' {
            pos += 2;
        } else if b == b'[' {
            in_class = true;
            pos += 1;
        } else if b == b']' && in_class {
            in_class = false;
            pos += 1;
        } else if b == b'/' && !in_class {
            pos += 1;
            let flag_start = pos;
            while pos < rem.len() && matches!(rem[pos], b'g'|b'i'|b'm'|b's'|b'u'|b'y'|b'v'|b'd') {
                pos += 1;
            }
            let flags_len = pos - flag_start;
            return Some((pos, TokenKind::StringLit {
                open_len: 1,
                close_len: 1 + flags_len,
                closed: true,
            }));
        } else {
            pos += 1;
        }
    }
    None
}

pub fn scan_js_template(text: &str, i: usize) -> (usize, Vec<CodeToken>) {
    let rem = text[i..].as_bytes();
    let mut pos = 1;
    let mut inner_tokens = Vec::new();
    let mut closed = false;

    while pos < rem.len() {
        let b = rem[pos];
        if b == b'\\' {
            if pos + 1 < rem.len() { pos += 2; } else { pos = rem.len(); break; }
        } else if b == b'`' {
            pos += 1;
            closed = true;
            break;
        } else if b == b'$' && pos + 1 < rem.len() && rem[pos + 1] == b'{' {
            let interp_start = i + pos;
            inner_tokens.push(CodeToken {
                range: interp_start..interp_start + 2,
                kind: TokenKind::OpenBracket(BracketKind::Brace),
            });
            pos += 2;
            pos += scan_template_expr(text, i + pos, &mut inner_tokens);
        } else {
            pos += 1;
        }
    }

    let mut all = Vec::with_capacity(1 + inner_tokens.len());
    all.push(CodeToken {
        range: i..i + pos,
        kind: TokenKind::StringLit { open_len: 1, close_len: if closed { 1 } else { 0 }, closed },
    });
    all.extend(inner_tokens);
    (pos, all)
}

fn scan_template_expr(text: &str, start_offset: usize, tokens: &mut Vec<CodeToken>) -> usize {
    let bytes = text[start_offset..].as_bytes();
    let mut pos = 0;
    let mut depth = 1;

    while pos < bytes.len() && depth > 0 {
        let cur = start_offset + pos;
        let sub = &bytes[pos..];

        if sub.starts_with(b"//") {
            let len = sub.iter().position(|&c| c == b'\n').map_or(sub.len(), |p| p + 1);
            tokens.push(CodeToken { range: cur..cur + len, kind: TokenKind::Comment });
            pos += len;
        } else if sub.starts_with(b"/*") {
            let len = sub.windows(2).position(|w| w == b"*/").map_or(sub.len(), |p| p + 2);
            tokens.push(CodeToken { range: cur..cur + len, kind: TokenKind::Comment });
            pos += len;
        } else if sub.starts_with(b"`") {
            let (t_len, t_tokens) = scan_js_template(text, cur);
            tokens.extend(t_tokens);
            pos += t_len;
        } else if sub[0] == b'"' || sub[0] == b'\'' {
            let q = sub[0];
            let mut end = 1;
            let mut escaped = false;
            let mut str_closed = false;
            while end < sub.len() {
                let c = sub[end];
                if escaped { escaped = false; end += 1; }
                else if c == b'\\' { escaped = true; end += 1; }
                else if c == q { end += 1; str_closed = true; break; }
                else if c == b'\n' { break; }
                else { end += 1; }
            }
            tokens.push(CodeToken {
                range: cur..cur + end,
                kind: TokenKind::StringLit { open_len: 1, close_len: if str_closed { 1 } else { 0 }, closed: str_closed },
            });
            pos += end;
        } else if sub[0] == b'/' && can_be_regex(tokens, text) {
            if let Some((r_len, r_kind)) = scan_js_regex(sub) {
                tokens.push(CodeToken { range: cur..cur + r_len, kind: r_kind });
                pos += r_len;
            } else {
                pos += 1;
            }
        } else {
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
                b';' => {
                    tokens.push(CodeToken { range: cur..cur + 1, kind: TokenKind::Semicolon });
                    pos += 1;
                }
                b':' => {
                    tokens.push(CodeToken { range: cur..cur + 1, kind: TokenKind::Colon });
                    pos += 1;
                }
                b if b.is_ascii_digit() => {
                    let mut w_len = 1;
                    while pos + w_len < bytes.len() && (bytes[pos + w_len].is_ascii_alphanumeric() || bytes[pos + w_len] == b'.' || bytes[pos + w_len] == b'_') {
                        w_len += 1;
                    }
                    tokens.push(CodeToken { range: cur..cur + w_len, kind: TokenKind::Word });
                    pos += w_len;
                }
                b if b.is_ascii_alphabetic() || b == b'_' => {
                    let mut w_len = 1;
                    while pos + w_len < bytes.len() && (bytes[pos + w_len].is_ascii_alphanumeric() || bytes[pos + w_len] == b'_') {
                        w_len += 1;
                    }
                    tokens.push(CodeToken { range: cur..cur + w_len, kind: TokenKind::Word });
                    pos += w_len;
                }
                _ => pos += 1,
            }
        }
    }
    pos
}
