//! Indentation-based block selection for Python and YAML.
use std::ops::Range;
use super::code_selection_tokens::{CodeToken, TokenKind};

pub fn match_indent_block(
    text: &str,
    tokens: &[CodeToken],
    offset: usize,
    lang: &str,
    include_delimiters: bool,
) -> Option<Range<usize>> {
    if !matches!(lang, "python" | "yaml") {
        return None;
    }
    let line_start = text[..offset].rfind('\n').map_or(0, |p| p + 1);
    let line_end = text[offset..].find('\n').map_or(text.len(), |p| offset + p);
    let line = &text[line_start..line_end];
    let trimmed = line.trim();
    if trimmed.is_empty() { return None; }

    let base_indent = line.len() - line.trim_start().len();
    let in_indent_area = base_indent > 0 && offset >= line_start && offset < line_start + base_indent;

    let is_keyword_hit = tokens.iter().any(|t| {
        t.kind == TokenKind::Word
            && offset >= t.range.start
            && offset <= t.range.end
            && is_block_keyword(&text[t.range.clone()], lang)
    });

    let is_colon_hit = tokens.iter().any(|t| {
        t.kind == TokenKind::Colon
            && (offset == t.range.start || offset == t.range.end)
            && t.range.start >= line_start
            && t.range.end <= line_end
    });

    if !in_indent_area && !is_keyword_hit && !is_colon_hit {
        return None;
    }

    if is_keyword_hit || is_colon_hit {
        let mut scan_pos = if line_end < text.len() { line_end + 1 } else { line_end };
        let mut block_end = line_end;

        while scan_pos < text.len() {
            let next_nl = text[scan_pos..].find('\n').map_or(text.len(), |p| scan_pos + p);
            let next_line = &text[scan_pos..next_nl];
            if next_line.trim().is_empty() {
                block_end = next_nl;
                scan_pos = if next_nl < text.len() { next_nl + 1 } else { next_nl };
                continue;
            }
            let indent = next_line.len() - next_line.trim_start().len();
            if indent > base_indent {
                block_end = next_nl;
                scan_pos = if next_nl < text.len() { next_nl + 1 } else { next_nl };
            } else {
                break;
            }
        }

        if block_end > line_end {
            if include_delimiters {
                Some(line_start..block_end)
            } else {
                let inner_start = if line_end < text.len() { line_end + 1 } else { line_end };
                Some(inner_start..block_end)
            }
        } else {
            None
        }
    } else {
        let mut block_start = line_start;
        let mut block_end = line_end;

        let mut prev_scan = line_start;
        while prev_scan > 0 {
            let prev_nl = text[..prev_scan - 1].rfind('\n').map_or(0, |p| p + 1);
            let prev_line = &text[prev_nl..prev_scan - 1];
            if prev_line.trim().is_empty() { break; }
            let indent = prev_line.len() - prev_line.trim_start().len();
            if indent >= base_indent {
                block_start = prev_nl;
                prev_scan = prev_nl;
            } else {
                break;
            }
        }

        let mut scan_pos = if line_end < text.len() { line_end + 1 } else { line_end };
        while scan_pos < text.len() {
            let next_nl = text[scan_pos..].find('\n').map_or(text.len(), |p| scan_pos + p);
            let next_line = &text[scan_pos..next_nl];
            if next_line.trim().is_empty() {
                block_end = next_nl;
                scan_pos = if next_nl < text.len() { next_nl + 1 } else { next_nl };
                continue;
            }
            let indent = next_line.len() - next_line.trim_start().len();
            if indent >= base_indent {
                block_end = next_nl;
                scan_pos = if next_nl < text.len() { next_nl + 1 } else { next_nl };
            } else {
                break;
            }
        }

        Some(block_start..block_end)
    }
}

fn is_block_keyword(word: &str, lang: &str) -> bool {
    if lang == "python" {
        matches!(
            word,
            "def" | "class" | "if" | "elif" | "else" | "for" | "while"
                | "try" | "except" | "finally" | "with" | "async" | "match" | "case"
        )
    } else {
        false
    }
}
