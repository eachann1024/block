//! Small, lossless lexical highlighter. Unsupported languages keep plain text.
//! Offsets always refer to UTF-8 boundaries in the original source.
use gpui::{FontWeight, TextRun};
use crate::theme::Theme;
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(in crate::editor) enum TokenKind { Keyword, String, Comment, Number }
#[derive(Clone, Debug, PartialEq, Eq)]
pub(in crate::editor) struct Token { pub start: usize, pub end: usize, pub kind: TokenKind }

pub(in crate::editor) fn tokens(source: &str, language: &str) -> Vec<Token> {
    let language = language.split_whitespace().next().unwrap_or("").to_ascii_lowercase();
    let python = matches!(language.as_str(), "python" | "py");
    let rust = matches!(language.as_str(), "rust" | "rs");
    let javascript = matches!(language.as_str(), "javascript" | "js" | "typescript" | "ts" | "jsx" | "tsx");
    let json = language == "json";
    if !python && !rust && !javascript && !json { return Vec::new(); }
    let bytes = source.as_bytes(); let mut at = 0; let mut out = Vec::new();
    while at < bytes.len() {
        let start = at;
        let comment = (python && bytes[at] == b'#') || (!python && !json && source[at..].starts_with("//"));
        if comment {
            at = source[at..].find('\n').map(|n| at+n).unwrap_or(bytes.len());
            out.push(Token { start, end: at, kind: TokenKind::Comment }); continue;
        }
        if !python && !json && source[at..].starts_with("/*") {
            at += 2; let mut depth = 1usize;
            while at < bytes.len() && depth > 0 {
                if rust && source[at..].starts_with("/*") { depth += 1; at += 2; }
                else if source[at..].starts_with("*/") { depth -= 1; at += 2; }
                else { at += source[at..].chars().next().unwrap().len_utf8(); }
            }
            out.push(Token { start, end: at, kind: TokenKind::Comment }); continue;
        }
        let quote = bytes[at];
        if quote == b'"' || (!json && quote == b'\'') || (javascript && quote == b'`') {
            // Rust lifetimes are identifiers, not unterminated character strings.
            if rust && quote == b'\'' && source[at+1..].chars().next().is_some_and(|c| c.is_alphabetic()) {
                let end = at + 1 + source[at+1..].chars().next().unwrap().len_utf8();
                if bytes.get(end) != Some(&b'\'') { at += 1; continue; }
            }
            let triple = python && bytes.get(at..at+3).is_some_and(|s| s == [quote, quote, quote]);
            let delimiter = if triple { 3 } else { 1 }; at += delimiter;
            while at < bytes.len() {
                if bytes[at] == b'\\' {
                    at += 1;
                    if at < bytes.len() { at += source[at..].chars().next().unwrap().len_utf8(); }
                } else if bytes.get(at..at+delimiter).is_some_and(|s| s.iter().all(|b| *b == quote)) { at += delimiter; break; }
                else { at += source[at..].chars().next().unwrap().len_utf8(); }
            }
            out.push(Token { start, end: at, kind: TokenKind::String }); continue;
        }
        if quote.is_ascii_digit() {
            at += 1;
            while at < bytes.len() && (bytes[at].is_ascii_alphanumeric() || matches!(bytes[at], b'.' | b'_')) { at += 1; }
            out.push(Token { start, end: at, kind: TokenKind::Number }); continue;
        }
        if quote.is_ascii_alphabetic() || quote == b'_' {
            at += 1;
            while at < bytes.len() && (bytes[at].is_ascii_alphanumeric() || bytes[at] == b'_') { at += 1; }
            let word = &source[start..at];
            if matches!(word, "fn"|"let"|"mut"|"pub"|"struct"|"enum"|"impl"|"use"|"mod"|"match"|"self"|"Self"|"trait"|"where"|"move"|"unsafe"|"const"|"static"|"async"|"await"|"return"|"if"|"else"|"for"|"while"|"loop"|"in"|"break"|"continue"|"true"|"false"|"null"|"None"|"Some"|"True"|"False"|"def"|"class"|"import"|"from"|"as"|"with"|"try"|"except"|"finally"|"raise"|"yield"|"lambda"|"pass"|"and"|"or"|"not"|"is"|"function"|"var"|"new"|"this"|"throw"|"catch"|"switch"|"case"|"default"|"export"|"interface"|"type"|"extends"|"implements"|"typeof"|"undefined") {
                out.push(Token { start, end: at, kind: TokenKind::Keyword });
            }
            continue;
        }
        at += source[at..].chars().next().unwrap().len_utf8();
    }
    out
}
pub(in crate::editor) fn runs(source: &str, language: &str, base: TextRun) -> Vec<TextRun> {
    let theme = Theme::ocean(); let mut cursor = 0; let mut out = Vec::new();
    for token in tokens(source, language) {
        if cursor < token.start { let mut run = base.clone(); run.len = token.start-cursor; out.push(run); }
        let mut run = base.clone(); run.len = token.end-token.start;
        match token.kind {
            TokenKind::Keyword => { run.color = theme.accent; run.font.weight = FontWeight::BOLD; }
            TokenKind::String => run.color = theme.accent,
            TokenKind::Comment | TokenKind::Number => run.color = theme.text_secondary,
        }
        out.push(run); cursor = token.end;
    }
    if cursor < source.len() { let mut run = base; run.len = source.len()-cursor; out.push(run); }
    out
}
