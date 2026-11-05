//! Markup, XML, Markdown, LaTeX, and Lean lexical scanners.
use std::ops::Range;
use super::{BracketKind, CodeToken, TokenKind};
use super::code_selection_lex_common::odd_backslash;

pub fn scan_markdown_token(text: &str, i: usize) -> Option<(usize, Vec<CodeToken>)> {
    let rem = text[i..].as_bytes();
    if rem.is_empty() { return None; }

    if (rem.starts_with(b"```") || rem.starts_with(b"~~~")) && is_at_line_start_or_spaces(text, i) {
        if let Some((len, kind)) = scan_markdown_fence(text, i) {
            return Some((len, vec![CodeToken { range: i..i + len, kind }]));
        }
    }

    if rem[0] == b'`' {
        let (len, kind) = scan_markdown_backtick_span(rem);
        return Some((len, vec![CodeToken { range: i..i + len, kind }]));
    }

    None
}

fn is_at_line_start_or_spaces(text: &str, i: usize) -> bool {
    let line_start = text[..i].rfind('\n').map_or(0, |p| p + 1);
    text[line_start..i].chars().all(|c| c == ' ' || c == '\t')
}

fn scan_markdown_fence(text: &str, i: usize) -> Option<(usize, TokenKind)> {
    let rem = text[i..].as_bytes();
    let fence_char = rem[0];
    let mut fence_len = 0;
    while fence_len < rem.len() && rem[fence_len] == fence_char { fence_len += 1; }
    if fence_len < 3 { return None; }

    let nl = text[i..].find('\n');
    let open_len = nl.map_or(rem.len(), |p| p + 1);
    let Some(nl_pos) = nl else {
        return Some((rem.len(), TokenKind::StringLit { open_len, close_len: 0, closed: false }));
    };

    let mut scan_offset = i + nl_pos + 1;
    while scan_offset <= text.len() {
        let next_nl = text[scan_offset..].find('\n').map_or(text.len(), |p| scan_offset + p);
        let raw_line = &text[scan_offset..next_nl];
        let line = raw_line.trim();
        let bytes = line.as_bytes();

        if bytes.len() >= fence_len && bytes.iter().all(|&b| b == fence_char) {
            let fence_rel = raw_line.find(line).unwrap_or(0);
            let fence_end = scan_offset + fence_rel + line.len();
            let total_len = fence_end - i;
            let close_len = fence_end - scan_offset;
            return Some((total_len, TokenKind::StringLit {
                open_len,
                close_len,
                closed: true,
            }));
        }
        if next_nl >= text.len() { break; }
        scan_offset = next_nl + 1;
    }

    Some((rem.len(), TokenKind::StringLit { open_len, close_len: 0, closed: false }))
}

fn scan_markdown_backtick_span(rem: &[u8]) -> (usize, TokenKind) {
    let mut count = 0;
    while count < rem.len() && rem[count] == b'`' { count += 1; }
    let open_len = count;

    let mut pos = count;
    while pos < rem.len() {
        if rem[pos] == b'`' {
            let mut match_count = 0;
            while pos + match_count < rem.len() && rem[pos + match_count] == b'`' {
                match_count += 1;
            }
            if match_count == count {
                let total = pos + count;
                return (total, TokenKind::StringLit { open_len, close_len: count, closed: true });
            }
            pos += match_count;
        } else {
            pos += 1;
        }
    }
    (rem.len(), TokenKind::StringLit { open_len, close_len: 0, closed: false })
}

pub fn scan_xml_token(rem: &[u8], i: usize) -> Option<(usize, Vec<CodeToken>)> {
    if rem.starts_with(b"<![CDATA[") {
        let open_len = 9;
        let end_idx = rem[open_len..].windows(3).position(|w| w == b"]]>");
        let (len, close_len, closed) = match end_idx {
            Some(p) => (open_len + p + 3, 3, true),
            None => (rem.len(), 0, false),
        };
        return Some((len, vec![CodeToken {
            range: i..i + len,
            kind: TokenKind::StringLit { open_len, close_len, closed },
        }]));
    }

    if rem.starts_with(b"<?") {
        let open_len = 2;
        let end_idx = rem[open_len..].windows(2).position(|w| w == b"?>");
        let (len, close_len, closed) = match end_idx {
            Some(p) => (open_len + p + 2, 2, true),
            None => (rem.len(), 0, false),
        };
        return Some((len, vec![CodeToken {
            range: i..i + len,
            kind: TokenKind::StringLit { open_len, close_len, closed },
        }]));
    }

    None
}

pub fn scan_latex_token(text: &str, i: usize) -> Option<(usize, Vec<CodeToken>)> {
    let rem = text[i..].as_bytes();
    if rem.is_empty() { return None; }

    if rem.starts_with(b"\\{") || rem.starts_with(b"\\}") {
        return Some((2, vec![CodeToken { range: i..i + 2, kind: TokenKind::Other }]));
    }

    if rem.starts_with(b"\\[") {
        let open_len = 2;
        let end = rem[2..].windows(2).position(|w| w == b"\\]").map(|p| p + 4);
        let (len, close_len, closed) = match end {
            Some(e) => (e, 2, true),
            None => (rem.len(), 0, false),
        };
        return Some((len, vec![CodeToken {
            range: i..i + len,
            kind: TokenKind::StringLit { open_len, close_len, closed },
        }]));
    }

    if rem.starts_with(b"\\(") {
        let open_len = 2;
        let end = rem[2..].windows(2).position(|w| w == b"\\)").map(|p| p + 4);
        let (len, close_len, closed) = match end {
            Some(e) => (e, 2, true),
            None => (rem.len(), 0, false),
        };
        return Some((len, vec![CodeToken {
            range: i..i + len,
            kind: TokenKind::StringLit { open_len, close_len, closed },
        }]));
    }

    if rem.starts_with(b"$$") {
        let open_len = 2;
        let end = rem[2..].windows(2).position(|w| w == b"$$").map(|p| p + 4);
        let (len, close_len, closed) = match end {
            Some(e) => (e, 2, true),
            None => (rem.len(), 0, false),
        };
        return Some((len, vec![CodeToken {
            range: i..i + len,
            kind: TokenKind::StringLit { open_len, close_len, closed },
        }]));
    }

    if rem.starts_with(b"$") && !odd_backslash(text.as_bytes(), i) {
        let open_len = 1;
        let mut pos = 1;
        let mut closed = false;
        while pos < rem.len() {
            if rem[pos] == b'$' && !odd_backslash(rem, pos) {
                closed = true;
                pos += 1;
                break;
            }
            pos += 1;
        }
        return Some((pos, vec![CodeToken {
            range: i..i + pos,
            kind: TokenKind::StringLit { open_len, close_len: if closed { 1 } else { 0 }, closed },
        }]));
    }

    None
}

pub fn html_raw_body_at(text: &str, i: usize) -> Option<Range<usize>> {
    let bytes = text.as_bytes();
    if i >= bytes.len() || bytes[i] != b'<' { return None; }
    let rem = &bytes[i..];
    let name: &[u8] = if rem.len() >= 7 && rem[..7].eq_ignore_ascii_case(b"<script") {
        b"script"
    } else if rem.len() >= 6 && rem[..6].eq_ignore_ascii_case(b"<style") {
        b"style"
    } else {
        return None;
    };
    let after = i + 1 + name.len();
    if after < bytes.len() {
        let nch = bytes[after];
        if nch.is_ascii_alphanumeric() || nch == b'-' || nch == b'_' || nch == b':' { return None; }
    }
    let mut p = after;
    let mut in_quote = None;
    while p < bytes.len() && p - i < 8192 {
        let c = bytes[p];
        if let Some(q) = in_quote {
            if c == b'\\' { p += 1; }
            else if c == q { in_quote = None; }
        } else if c == b'"' || c == b'\'' {
            in_quote = Some(c);
        } else if c == b'/' && p + 1 < bytes.len() && bytes[p + 1] == b'>' {
            return None;
        } else if c == b'>' {
            p += 1;
            break;
        }
        p += 1;
        if p > bytes.len() { return None; }
    }
    if p == 0 || p > bytes.len() || bytes[p - 1] != b'>' { return None; }
    let body_start = p;
    let mut close = Vec::with_capacity(2 + name.len());
    close.extend_from_slice(b"</");
    close.extend_from_slice(name);
    let rest = &bytes[body_start..];
    let found = rest.windows(close.len()).position(|w| w.eq_ignore_ascii_case(&close))?;
    Some(body_start..body_start + found)
}

pub fn scan_lean_bracket(text: &str, i: usize) -> Option<(usize, CodeToken)> {
    let sub = &text[i..];
    if sub.starts_with('⟨') {
        let len = '⟨'.len_utf8();
        Some((len, CodeToken { range: i..i + len, kind: TokenKind::OpenBracket(BracketKind::Angle) }))
    } else if sub.starts_with('⟩') {
        let len = '⟩'.len_utf8();
        Some((len, CodeToken { range: i..i + len, kind: TokenKind::CloseBracket(BracketKind::Angle) }))
    } else if sub.starts_with('«') {
        let len = '«'.len_utf8();
        Some((len, CodeToken { range: i..i + len, kind: TokenKind::OpenBracket(BracketKind::Guillemet) }))
    } else if sub.starts_with('»') {
        let len = '»'.len_utf8();
        Some((len, CodeToken { range: i..i + len, kind: TokenKind::CloseBracket(BracketKind::Guillemet) }))
    } else {
        None
    }
}
