//! Shell / Bash lexical scanner: single quotes without escapes, heredocs, and double-quote substitution protection.
use super::{CodeToken, TokenKind};

#[path = "code_selection_lex_shell_span.rs"]
mod span;

pub fn scan_shell_token(text: &str, i: usize) -> Option<(usize, Vec<CodeToken>)> {
    let rem = text[i..].as_bytes();
    if rem.is_empty() { return None; }

    if rem.starts_with(b"<<") && !rem.starts_with(b"<<<") {
        if let Some((len, ts)) = scan_bash_heredoc(text, i) {
            return Some((len, ts));
        }
    }

    if rem.starts_with(b"$'") {
        let (len, kind) = scan_bash_ansi_c_str(rem);
        return Some((len, vec![CodeToken { range: i..i + len, kind }]));
    }

    if rem.starts_with(b"'") {
        let (len, kind) = scan_bash_single_quote(rem);
        return Some((len, vec![CodeToken { range: i..i + len, kind }]));
    }

    if rem.starts_with(b"\"") {
        let (len, ts) = span::scan_double_quote(text, i);
        return Some((len, ts));
    }

    if rem.starts_with(b"`") {
        let (len, kind) = scan_bash_backtick(rem);
        return Some((len, vec![CodeToken { range: i..i + len, kind }]));
    }

    None
}

fn scan_bash_single_quote(rem: &[u8]) -> (usize, TokenKind) {
    if let Some(p) = rem[1..].iter().position(|&c| c == b'\'') {
        let len = 1 + p + 1;
        (len, TokenKind::StringLit { open_len: 1, close_len: 1, closed: true })
    } else {
        (rem.len(), TokenKind::StringLit { open_len: 1, close_len: 0, closed: false })
    }
}

fn scan_bash_ansi_c_str(rem: &[u8]) -> (usize, TokenKind) {
    let mut pos = 2;
    while pos < rem.len() {
        if rem[pos] == b'\\' {
            pos += 2;
        } else if rem[pos] == b'\'' {
            return (pos + 1, TokenKind::StringLit { open_len: 2, close_len: 1, closed: true });
        } else {
            pos += 1;
        }
    }
    (rem.len(), TokenKind::StringLit { open_len: 2, close_len: 0, closed: false })
}

fn scan_bash_backtick(rem: &[u8]) -> (usize, TokenKind) {
    let mut pos = 1;
    while pos < rem.len() {
        if rem[pos] == b'\\' {
            pos += 2;
        } else if rem[pos] == b'`' {
            return (pos + 1, TokenKind::StringLit { open_len: 1, close_len: 1, closed: true });
        } else {
            pos += 1;
        }
    }
    (rem.len(), TokenKind::StringLit { open_len: 1, close_len: 0, closed: false })
}

fn scan_bash_heredoc(text: &str, i: usize) -> Option<(usize, Vec<CodeToken>)> {
    let rem = text[i..].as_bytes();
    let mut pos = 2;
    let mut strip_tabs = false;
    if pos < rem.len() && rem[pos] == b'-' {
        strip_tabs = true;
        pos += 1;
    }
    while pos < rem.len() && (rem[pos] == b' ' || rem[pos] == b'\t') {
        pos += 1;
    }

    let expandable = !(pos < rem.len() && matches!(rem[pos], b'\'' | b'"' | b'\\'));
    let (delim, _delim_open_len) = if pos < rem.len() && (rem[pos] == b'\'' || rem[pos] == b'"') {
        let q = rem[pos];
        let d_start = pos + 1;
        let mut d_end = d_start;
        while d_end < rem.len() && rem[d_end] != q { d_end += 1; }
        let d = &rem[d_start..d_end];
        let next_pos = if d_end < rem.len() { d_end + 1 } else { d_end };
        (d, next_pos)
    } else if pos < rem.len() {
        let mut p = pos;
        if rem[p] == b'\\' { p += 1; }
        let d_start = p;
        while p < rem.len() && (rem[p].is_ascii_alphanumeric() || rem[p] == b'_') { p += 1; }
        (&rem[d_start..p], p)
    } else {
        return None;
    };

    if delim.is_empty() { return None; }

    let Some(nl) = text[i..].find('\n') else {
        return Some((rem.len(), vec![CodeToken {
            range: i..i + rem.len(),
            kind: TokenKind::StringLit { open_len: rem.len(), close_len: 0, closed: false },
        }]));
    };
    let open_len = nl + 1;

    let mut line_start = i + nl + 1;
    while line_start <= text.len() {
        let next_nl = text[line_start..].find('\n').map_or(text.len(), |p| line_start + p);
        let mut line = &text[line_start..next_nl];
        if line.ends_with('\r') { line = &line[..line.len() - 1]; }
        let stripped_line = if strip_tabs { line.trim_start_matches('\t') } else { line };

        if stripped_line.as_bytes() == delim {
            let total_len = next_nl.min(line_start + line.len()) - i;
            let close_len = total_len - (line_start - i);
            let mut tokens = vec![CodeToken {
                range: i..i + total_len,
                kind: TokenKind::StringLit { open_len, close_len, closed: true },
            }];
            if expandable {
                span::push_expansions(text, i + open_len, line_start, &mut tokens);
            }
            return Some((total_len, tokens));
        }
        if next_nl >= text.len() { break; }
        line_start = next_nl + 1;
    }

    Some((rem.len(), vec![CodeToken {
        range: i..text.len(),
        kind: TokenKind::StringLit { open_len, close_len: 0, closed: false },
    }]))
}
