//! Rust-specific lexical scanner: raw strings (r, br, cr), byte/C strings, char literals, and lifetimes.
use super::{CodeToken, TokenKind};
use super::code_selection_lex_common::is_ident_continue;

pub fn scan_rust_token(text: &str, i: usize) -> Option<(usize, Vec<CodeToken>)> {
    let rem = text[i..].as_bytes();
    if rem.is_empty() { return None; }

    if let Some((len, open_len, close_len, closed)) = scan_rust_raw_str(rem) {
        return Some((len, vec![CodeToken {
            range: i..i + len,
            kind: TokenKind::StringLit { open_len, close_len, closed },
        }]));
    }

    if let Some((len, open_len, close_len, closed)) = scan_rust_byte_or_c_str(rem) {
        return Some((len, vec![CodeToken {
            range: i..i + len,
            kind: TokenKind::StringLit { open_len, close_len, closed },
        }]));
    }

    if let Some((len, kind)) = scan_rust_char_or_lifetime(text, i) {
        return Some((len, vec![CodeToken { range: i..i + len, kind }]));
    }

    None
}

fn scan_rust_raw_str(rem: &[u8]) -> Option<(usize, usize, usize, bool)> {
    let prefix_len = if rem.starts_with(b"br\"") || rem.starts_with(b"br#")
        || rem.starts_with(b"cr\"") || rem.starts_with(b"cr#") {
        2
    } else if rem.starts_with(b"r\"") || rem.starts_with(b"r#") {
        1
    } else {
        return None;
    };

    let mut hashes = 0;
    let mut pos = prefix_len;
    while pos < rem.len() && rem[pos] == b'#' {
        hashes += 1;
        pos += 1;
    }
    if pos >= rem.len() || rem[pos] != b'"' {
        return None;
    }
    pos += 1;
    let open_len = pos;

    let mut end_seq = Vec::with_capacity(1 + hashes);
    end_seq.push(b'"');
    end_seq.extend(std::iter::repeat_n(b'#', hashes));

    if let Some(p) = rem[pos..].windows(end_seq.len()).position(|w| w == end_seq.as_slice()) {
        let total = pos + p + end_seq.len();
        Some((total, open_len, end_seq.len(), true))
    } else {
        Some((rem.len(), open_len, 0, false))
    }
}

fn scan_rust_byte_or_c_str(rem: &[u8]) -> Option<(usize, usize, usize, bool)> {
    let prefix_len = if (rem.starts_with(b"b\"") || rem.starts_with(b"c\""))
        && !rem.starts_with(b"br") && !rem.starts_with(b"cr") {
        1
    } else {
        return None;
    };

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
        } else if c == b'"' {
            return Some((pos + 1, open_len, 1, true));
        } else if c == b'\n' {
            break;
        } else {
            pos += 1;
        }
    }
    Some((pos, open_len, 0, false))
}

fn scan_rust_char_or_lifetime(text: &str, i: usize) -> Option<(usize, TokenKind)> {
    let rem = text[i..].as_bytes();
    if rem.is_empty() { return None; }

    if rem.starts_with(b"b'") {
        return Some(scan_rust_byte_char(rem));
    }

    if !rem.starts_with(b"'") {
        return None;
    }

    let sub = &text[i + 1..];
    if sub.is_empty() {
        return Some((1, TokenKind::StringLit { open_len: 1, close_len: 0, closed: false }));
    }

    if let Some((len, closed)) = try_scan_char_literal(sub) {
        return Some((1 + len, TokenKind::StringLit {
            open_len: 1,
            close_len: if closed { 1 } else { 0 },
            closed,
        }));
    }

    if let Some(lifetime_len) = try_scan_lifetime(sub) {
        return Some((1 + lifetime_len, TokenKind::Word));
    }

    let char_len = sub.chars().next().map_or(1, |c| c.len_utf8());
    Some((1 + char_len, TokenKind::StringLit { open_len: 1, close_len: 0, closed: false }))
}

fn scan_rust_byte_char(rem: &[u8]) -> (usize, TokenKind) {
    let mut pos = 2;
    if pos < rem.len() {
        if rem[pos] == b'\\' {
            pos += 1;
            if pos < rem.len() && rem[pos] == b'x' {
                pos += 3;
            } else if pos < rem.len() {
                pos += 1;
            }
        } else {
            pos += 1;
        }
    }
    if pos < rem.len() && rem[pos] == b'\'' {
        (pos + 1, TokenKind::StringLit { open_len: 2, close_len: 1, closed: true })
    } else {
        (pos.min(rem.len()), TokenKind::StringLit { open_len: 2, close_len: 0, closed: false })
    }
}

fn try_scan_char_literal(sub: &str) -> Option<(usize, bool)> {
    let bytes = sub.as_bytes();
    if bytes.is_empty() { return None; }

    if bytes[0] == b'\\' {
        if bytes.len() < 2 { return Some((1, false)); }
        let mut pos = 2;
        if bytes[1] == b'x' {
            pos = 4;
        } else if bytes[1] == b'u' {
            if let Some(close_brace) = sub.find('}') {
                pos = close_brace + 1;
            } else {
                return Some((sub.len(), false));
            }
        }
        if pos < bytes.len() && bytes[pos] == b'\'' {
            return Some((pos + 1, true));
        }
        return Some((pos.min(bytes.len()), false));
    }

    let first_char = sub.chars().next()?;
    if first_char == '\n' || first_char == '\'' {
        return None;
    }
    let char_len = first_char.len_utf8();
    if char_len < bytes.len() && bytes[char_len] == b'\'' {
        Some((char_len + 1, true))
    } else {
        None
    }
}

fn try_scan_lifetime(sub: &str) -> Option<usize> {
    let bytes = sub.as_bytes();
    if bytes.is_empty() { return None; }
    let c0 = bytes[0];
    if !c0.is_ascii_alphabetic() && c0 != b'_' {
        return None;
    }
    let mut p = 1;
    while p < bytes.len() && is_ident_continue(bytes[p]) {
        p += 1;
    }
    if p < bytes.len() && bytes[p] == b'\'' {
        return None;
    }
    Some(p)
}
