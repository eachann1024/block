//! SQL and YAML lexical scanners: SQL dollar quotes, YAML single-quote doubling, and block scalars.
use super::{CodeToken, TokenKind};
use super::code_selection_lex_common::{is_ident_continue, is_ident_start};

pub fn scan_sql_token(rem: &[u8], i: usize) -> Option<(usize, Vec<CodeToken>)> {
    if rem.is_empty() { return None; }
    if rem[0] == b'$' {
        if let Some((len, kind)) = scan_sql_dollar_str(rem) {
            return Some((len, vec![CodeToken { range: i..i + len, kind }]));
        }
    }
    if rem[0] == b'\'' {
        if let Some((len, kind)) = scan_sql_string(rem) {
            return Some((len, vec![CodeToken { range: i..i + len, kind }]));
        }
    }
    None
}

fn scan_sql_dollar_str(rem: &[u8]) -> Option<(usize, TokenKind)> {
    if rem.is_empty() || rem[0] != b'$' { return None; }
    let tag_end = rem[1..].iter().position(|&c| c == b'$').map(|p| p + 1)?;
    let inner = &rem[1..tag_end];
    if !inner.is_empty() {
        if !is_ident_start(inner[0]) || inner.iter().any(|&b| !is_ident_continue(b)) {
            return None;
        }
    }
    let tag_len = tag_end + 1;
    let tag = &rem[..tag_len];
    let body = &rem[tag_len..];
    if let Some(p) = body.windows(tag_len).position(|w| w == tag) {
        let total = tag_len + p + tag_len;
        Some((total, TokenKind::StringLit { open_len: tag_len, close_len: tag_len, closed: true }))
    } else {
        Some((rem.len(), TokenKind::StringLit { open_len: tag_len, close_len: 0, closed: false }))
    }
}

fn scan_sql_string(rem: &[u8]) -> Option<(usize, TokenKind)> {
    if rem.is_empty() || rem[0] != b'\'' { return None; }
    let mut pos = 1;
    while pos < rem.len() {
        if rem[pos] == b'\'' {
            if pos + 1 < rem.len() && rem[pos + 1] == b'\'' {
                pos += 2;
            } else {
                return Some((pos + 1, TokenKind::StringLit { open_len: 1, close_len: 1, closed: true }));
            }
        } else {
            pos += 1;
        }
    }
    Some((rem.len(), TokenKind::StringLit { open_len: 1, close_len: 0, closed: false }))
}

pub fn scan_yaml_token(text: &str, i: usize) -> Option<(usize, Vec<CodeToken>)> {
    let rem = text[i..].as_bytes();
    if rem.is_empty() { return None; }

    if rem[0] == b'\'' {
        if let Some((len, kind)) = scan_sql_string(rem) {
            return Some((len, vec![CodeToken { range: i..i + len, kind }]));
        }
    }

    if rem[0] == b'|' || rem[0] == b'>' {
        if let Some((len, kind)) = scan_yaml_block_scalar(text, i) {
            return Some((len, vec![CodeToken { range: i..i + len, kind }]));
        }
    }

    None
}

fn scan_yaml_block_scalar(text: &str, i: usize) -> Option<(usize, TokenKind)> {
    let rem = text[i..].as_bytes();
    let mut p = 1;
    if p < rem.len() && (rem[p] == b'+' || rem[p] == b'-') { p += 1; }
    if p < rem.len() && rem[p].is_ascii_digit() { p += 1; }
    while p < rem.len() && (rem[p] == b' ' || rem[p] == b'\t') { p += 1; }
    if p < rem.len() && rem[p] == b'#' {
        while p < rem.len() && rem[p] != b'\n' { p += 1; }
    }
    if p >= rem.len() || rem[p] != b'\n' {
        return None;
    }
    let open_len = p + 1;

    let line_start = text[..i].rfind('\n').map_or(0, |pos| pos + 1);
    let parent_line = &text[line_start..i];
    let parent_indent = parent_line.len() - parent_line.trim_start().len();

    let mut scan_offset = i + open_len;
    let mut block_indent = None;
    let mut last_valid_end = scan_offset;

    while scan_offset < text.len() {
        let nl = text[scan_offset..].find('\n').map_or(text.len(), |pos| scan_offset + pos);
        let line = &text[scan_offset..nl];
        let trimmed = line.trim();

        if trimmed.is_empty() {
            last_valid_end = if nl < text.len() { nl + 1 } else { nl };
            scan_offset = last_valid_end;
            continue;
        }

        let indent = line.len() - line.trim_start().len();
        match block_indent {
            None => {
                if indent > parent_indent {
                    block_indent = Some(indent);
                    last_valid_end = if nl < text.len() { nl + 1 } else { nl };
                } else {
                    break;
                }
            }
            Some(target) => {
                if indent >= target {
                    last_valid_end = if nl < text.len() { nl + 1 } else { nl };
                } else {
                    break;
                }
            }
        }
        if nl >= text.len() { break; }
        scan_offset = nl + 1;
    }

    let total = last_valid_end.saturating_sub(i);
    if total >= open_len {
        Some((total, TokenKind::StringLit { open_len, close_len: 0, closed: true }))
    } else {
        None
    }
}
