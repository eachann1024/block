//! Shell / Bash lexical scanner: single quotes without escapes, heredocs, and double-quote substitution protection.
use super::{CodeToken, TokenKind};

pub fn scan_shell_token(text: &str, i: usize) -> Option<(usize, Vec<CodeToken>)> {
    let rem = text[i..].as_bytes();
    if rem.is_empty() { return None; }

    if rem.starts_with(b"<<") && !rem.starts_with(b"<<<") {
        if let Some((len, kind)) = scan_bash_heredoc(text, i) {
            return Some((len, vec![CodeToken { range: i..i + len, kind }]));
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
        let (len, kind) = scan_bash_double_quote(rem);
        return Some((len, vec![CodeToken { range: i..i + len, kind }]));
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

fn scan_bash_double_quote(rem: &[u8]) -> (usize, TokenKind) {
    let mut pos = 1;
    while pos < rem.len() {
        let b = rem[pos];
        if b == b'\\' {
            pos += 2;
        } else if b == b'"' {
            return (pos + 1, TokenKind::StringLit { open_len: 1, close_len: 1, closed: true });
        } else if b == b'$' && pos + 1 < rem.len() && rem[pos + 1] == b'(' {
            pos += 2;
            pos += skip_matching_delimiter(rem, pos, b'(', b')');
        } else if b == b'$' && pos + 1 < rem.len() && rem[pos + 1] == b'{' {
            pos += 2;
            pos += skip_matching_delimiter(rem, pos, b'{', b'}');
        } else if b == b'`' {
            pos += 1;
            while pos < rem.len() && rem[pos] != b'`' {
                if rem[pos] == b'\\' { pos += 2; } else { pos += 1; }
            }
            if pos < rem.len() { pos += 1; }
        } else {
            pos += 1;
        }
    }
    (rem.len(), TokenKind::StringLit { open_len: 1, close_len: 0, closed: false })
}

fn skip_matching_delimiter(rem: &[u8], start: usize, open_b: u8, close_b: u8) -> usize {
    let mut pos = start;
    let mut depth = 1;
    while pos < rem.len() && depth > 0 {
        let b = rem[pos];
        if b == b'\\' {
            pos += 2;
        } else if b == b'"' {
            pos += 1;
            while pos < rem.len() && rem[pos] != b'"' {
                if rem[pos] == b'\\' { pos += 2; } else { pos += 1; }
            }
            if pos < rem.len() { pos += 1; }
        } else if b == b'\'' {
            pos += 1;
            while pos < rem.len() && rem[pos] != b'\'' {
                pos += 1;
            }
            if pos < rem.len() { pos += 1; }
        } else if b == open_b {
            depth += 1;
            pos += 1;
        } else if b == close_b {
            depth -= 1;
            pos += 1;
        } else {
            pos += 1;
        }
    }
    pos - start
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

fn scan_bash_heredoc(text: &str, i: usize) -> Option<(usize, TokenKind)> {
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

    let (delim, delim_open_len) = if pos < rem.len() && (rem[pos] == b'\'' || rem[pos] == b'"') {
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
    let open_len = delim_open_len;

    let Some(nl) = text[i..].find('\n') else {
        return Some((rem.len(), TokenKind::StringLit { open_len, close_len: 0, closed: false }));
    };

    let mut line_start = i + nl + 1;
    while line_start <= text.len() {
        let next_nl = text[line_start..].find('\n').map_or(text.len(), |p| line_start + p);
        let mut line = &text[line_start..next_nl];
        if line.ends_with('\r') { line = &line[..line.len() - 1]; }
        let stripped_line = if strip_tabs { line.trim_start_matches('\t') } else { line };

        if stripped_line.as_bytes() == delim {
            let delim_offset = line_start + (line.len() - stripped_line.len());
            let total_len = (delim_offset + delim.len()) - i;
            return Some((total_len, TokenKind::StringLit {
                open_len,
                close_len: delim.len(),
                closed: true,
            }));
        }
        if next_nl >= text.len() { break; }
        line_start = next_nl + 1;
    }

    Some((rem.len(), TokenKind::StringLit { open_len, close_len: 0, closed: false }))
}
