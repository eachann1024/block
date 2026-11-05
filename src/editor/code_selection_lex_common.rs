//! Common lexical scanning utilities for comments, simple strings, and identifiers.
use super::TokenKind;

pub fn is_ident_start(b: u8) -> bool {
    b.is_ascii_alphabetic() || b == b'_'
}

pub fn is_ident_continue(b: u8) -> bool {
    b.is_ascii_alphanumeric() || b == b'_'
}

pub fn scan_comment(_text: &str, b: &[u8], i: usize, lang: &str) -> Option<(usize, TokenKind)> {
    let rem = &b[i..];
    if matches!(lang, "html" | "xml" | "markdown") && rem.starts_with(b"<!--") {
        let end = rem.windows(3).position(|w| w == b"-->").map_or(rem.len(), |p| p + 3);
        return Some((end, TokenKind::Comment));
    }
    if lang == "lean" && rem.starts_with(b"/-") {
        let mut depth = 1;
        let mut pos = 2;
        while pos + 1 < rem.len() && depth > 0 {
            if &rem[pos..pos + 2] == b"/-" { depth += 1; pos += 2; }
            else if &rem[pos..pos + 2] == b"-/" { depth -= 1; pos += 2; }
            else { pos += 1; }
        }
        if depth > 0 { pos = rem.len(); }
        return Some((pos, TokenKind::Comment));
    }
    if lang == "rust" && rem.starts_with(b"/*") {
        let mut depth = 1;
        let mut pos = 2;
        while pos + 1 < rem.len() && depth > 0 {
            if &rem[pos..pos + 2] == b"/*" { depth += 1; pos += 2; }
            else if &rem[pos..pos + 2] == b"*/" { depth -= 1; pos += 2; }
            else { pos += 1; }
        }
        if depth > 0 { pos = rem.len(); }
        return Some((pos, TokenKind::Comment));
    }
    if matches!(lang, "javascript" | "typescript" | "css" | "sql" | "json") && rem.starts_with(b"/*") {
        let end = rem.windows(2).position(|w| w == b"*/").map_or(rem.len(), |p| p + 2);
        return Some((end, TokenKind::Comment));
    }
    let is_line_cmt = (matches!(lang, "javascript" | "typescript" | "rust" | "json") && rem.starts_with(b"//"))
        || (matches!(lang, "python" | "bash" | "yaml") && rem.starts_with(b"#"))
        || (matches!(lang, "sql" | "lean") && rem.starts_with(b"--"))
        || (lang == "mermaid" && rem.starts_with(b"%%"));
    if is_line_cmt {
        let len = rem.iter().position(|&c| c == b'\n').map_or(rem.len(), |p| p + 1);
        return Some((len, TokenKind::Comment));
    }
    if lang == "latex" && rem.starts_with(b"%") && !odd_backslash(b, i) {
        let len = rem.iter().position(|&c| c == b'\n').map_or(rem.len(), |p| p + 1);
        return Some((len, TokenKind::Comment));
    }
    None
}

pub fn odd_backslash(bytes: &[u8], i: usize) -> bool {
    let mut n = 0usize;
    let mut k = i;
    while k > 0 && bytes[k - 1] == b'\\' {
        n += 1;
        k -= 1;
    }
    n % 2 == 1
}

fn single_quote_lang(lang: &str) -> bool {
    matches!(lang, "javascript" | "typescript" | "css" | "html" | "xml" | "lean" | "mermaid")
}

pub fn scan_simple_string(rem: &[u8], lang: &str) -> Option<(usize, TokenKind)> {
    if lang == "csv" {
        return scan_csv_string(rem);
    }
    if rem.is_empty() { return None; }
    let q = rem[0];
    if q == b'\'' && !single_quote_lang(lang) { return None; }
    if q != b'"' && q != b'\'' { return None; }
    let mut pos = 1;
    let mut escaped = false;
    while pos < rem.len() {
        let c = rem[pos];
        if escaped { escaped = false; pos += 1; }
        else if c == b'\\' { escaped = true; pos += 1; }
        else if c == q { return Some((pos + 1, TokenKind::StringLit { open_len: 1, close_len: 1, closed: true })); }
        else if c == b'\n' && !matches!(lang, "python") { break; }
        else { pos += 1; }
    }
    Some((pos, TokenKind::StringLit { open_len: 1, close_len: 0, closed: false }))
}

fn scan_csv_string(rem: &[u8]) -> Option<(usize, TokenKind)> {
    if rem.is_empty() || rem[0] != b'"' { return None; }
    let mut pos = 1;
    while pos < rem.len() {
        if rem[pos] == b'"' {
            if pos + 1 < rem.len() && rem[pos + 1] == b'"' { pos += 2; }
            else { return Some((pos + 1, TokenKind::StringLit { open_len: 1, close_len: 1, closed: true })); }
        } else { pos += 1; }
    }
    Some((rem.len(), TokenKind::StringLit { open_len: 1, close_len: 0, closed: false }))
}
