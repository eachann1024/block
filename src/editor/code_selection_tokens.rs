//! Lexical scanner for code selection: comments, strings, brackets, delimiters across 17 languages.
use std::ops::Range;

#[path = "code_selection_lex_common.rs"]
mod code_selection_lex_common;
#[path = "code_selection_lex_rust.rs"]
mod code_selection_lex_rust;
#[path = "code_selection_lex_python.rs"]
mod code_selection_lex_python;
#[path = "code_selection_lex_js.rs"]
mod code_selection_lex_js;
#[path = "code_selection_lex_shell.rs"]
mod code_selection_lex_shell;
#[path = "code_selection_lex_data.rs"]
mod code_selection_lex_data;
#[path = "code_selection_lex_markup.rs"]
mod code_selection_lex_markup;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BracketKind {
    Paren,    // ( )
    Bracket,  // [ ]
    Brace,    // { }
    Angle,    // ⟨ ⟩
    Guillemet,// « »
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TokenKind {
    Comment,
    StringLit { open_len: usize, close_len: usize, closed: bool },
    OpenBracket(BracketKind),
    CloseBracket(BracketKind),
    Comma,
    Semicolon,
    Colon,
    Word,
    Other,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CodeToken {
    pub range: Range<usize>,
    pub kind: TokenKind,
}

pub fn scan_tokens(text: &str, lang: &str) -> Vec<CodeToken> {
    let mut tokens = Vec::new();
    let bytes = text.as_bytes();
    let n = bytes.len();
    let mut i = 0;

    while i < n {
        if let Some((len, kind)) = code_selection_lex_common::scan_comment(text, bytes, i, lang) {
            tokens.push(CodeToken { range: i..i + len, kind });
            i += len;
            continue;
        }

        if lang == "rust" {
            if let Some((len, ts)) = code_selection_lex_rust::scan_rust_token(text, i) {
                tokens.extend(ts);
                i += len;
                continue;
            }
        } else if lang == "python" {
            if let Some((len, ts)) = code_selection_lex_python::scan_python_token(text, i) {
                tokens.extend(ts);
                i += len;
                continue;
            }
        } else if matches!(lang, "javascript" | "typescript") {
            let rem = &bytes[i..];
            if rem.starts_with(b"`") {
                let (len, ts) = code_selection_lex_js::scan_js_template(text, i);
                tokens.extend(ts);
                i += len;
                continue;
            } else if rem.starts_with(b"/") && code_selection_lex_js::can_be_regex(&tokens, text) {
                if let Some((len, kind)) = code_selection_lex_js::scan_js_regex(rem) {
                    tokens.push(CodeToken { range: i..i + len, kind });
                    i += len;
                    continue;
                }
            }
        } else if lang == "bash" {
            if let Some((len, ts)) = code_selection_lex_shell::scan_shell_token(text, i) {
                tokens.extend(ts);
                i += len;
                continue;
            }
        } else if lang == "sql" {
            if let Some((len, ts)) = code_selection_lex_data::scan_sql_token(&bytes[i..], i) {
                tokens.extend(ts);
                i += len;
                continue;
            }
        } else if lang == "yaml" {
            if let Some((len, ts)) = code_selection_lex_data::scan_yaml_token(text, i) {
                tokens.extend(ts);
                i += len;
                continue;
            }
        } else if lang == "markdown" {
            if let Some((len, ts)) = code_selection_lex_markup::scan_markdown_token(text, i) {
                tokens.extend(ts);
                i += len;
                continue;
            }
        } else if matches!(lang, "xml" | "html") {
            if let Some((len, ts)) = code_selection_lex_markup::scan_xml_token(&bytes[i..], i) {
                tokens.extend(ts);
                i += len;
                continue;
            }
        } else if lang == "latex" {
            if let Some((len, ts)) = code_selection_lex_markup::scan_latex_token(text, i) {
                tokens.extend(ts);
                i += len;
                continue;
            }
        }

        if lang == "lean" {
            if let Some((len, token)) = code_selection_lex_markup::scan_lean_bracket(text, i) {
                tokens.push(token);
                i += len;
                continue;
            }
        }

        if lang != "csv" && !matches!(lang, "rust" | "python" | "bash" | "sql") {
            if let Some((len, kind)) = code_selection_lex_common::scan_simple_string(&bytes[i..], lang) {
                tokens.push(CodeToken { range: i..i + len, kind });
                i += len;
                continue;
            }
        } else if lang == "csv" {
            if let Some((len, kind)) = code_selection_lex_common::scan_simple_string(&bytes[i..], lang) {
                tokens.push(CodeToken { range: i..i + len, kind });
                i += len;
                continue;
            }
        }

        let b = bytes[i];
        if lang == "csv" {
            match b {
                b',' => { tokens.push(CodeToken { range: i..i + 1, kind: TokenKind::Comma }); i += 1; }
                b'\n' | b'\r' => { i += 1; }
                b if code_selection_lex_common::is_ident_start(b) => {
                    let start = i;
                    while i < n && code_selection_lex_common::is_ident_continue(bytes[i]) { i += 1; }
                    tokens.push(CodeToken { range: start..i, kind: TokenKind::Word });
                }
                _ => {
                    let len = text[i..].chars().next().map_or(1, |c| c.len_utf8());
                    tokens.push(CodeToken { range: i..i + len, kind: TokenKind::Other });
                    i += len;
                }
            }
            continue;
        }

        match b {
            b'(' => { tokens.push(CodeToken { range: i..i + 1, kind: TokenKind::OpenBracket(BracketKind::Paren) }); i += 1; }
            b')' => { tokens.push(CodeToken { range: i..i + 1, kind: TokenKind::CloseBracket(BracketKind::Paren) }); i += 1; }
            b'[' => { tokens.push(CodeToken { range: i..i + 1, kind: TokenKind::OpenBracket(BracketKind::Bracket) }); i += 1; }
            b']' => { tokens.push(CodeToken { range: i..i + 1, kind: TokenKind::CloseBracket(BracketKind::Bracket) }); i += 1; }
            b'{' => { tokens.push(CodeToken { range: i..i + 1, kind: TokenKind::OpenBracket(BracketKind::Brace) }); i += 1; }
            b'}' => { tokens.push(CodeToken { range: i..i + 1, kind: TokenKind::CloseBracket(BracketKind::Brace) }); i += 1; }
            b',' => { tokens.push(CodeToken { range: i..i + 1, kind: TokenKind::Comma }); i += 1; }
            b';' => { tokens.push(CodeToken { range: i..i + 1, kind: TokenKind::Semicolon }); i += 1; }
            b':' => { tokens.push(CodeToken { range: i..i + 1, kind: TokenKind::Colon }); i += 1; }
            b if b.is_ascii_digit() => {
                let start = i;
                while i < n && (bytes[i].is_ascii_alphanumeric() || bytes[i] == b'.' || bytes[i] == b'_') {
                    i += 1;
                }
                tokens.push(CodeToken { range: start..i, kind: TokenKind::Word });
            }
            b if code_selection_lex_common::is_ident_start(b) => {
                let start = i;
                while i < n && code_selection_lex_common::is_ident_continue(bytes[i]) { i += 1; }
                tokens.push(CodeToken { range: start..i, kind: TokenKind::Word });
            }
            _ => {
                let len = text[i..].chars().next().map_or(1, |c| c.len_utf8());
                tokens.push(CodeToken { range: i..i + len, kind: TokenKind::Other });
                i += len;
            }
        }
    }

    tokens
}
