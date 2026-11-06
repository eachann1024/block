use super::blocks::{closes_fence, fence};
use std::borrow::Cow;

fn is_marker(line: &str) -> bool {
    let rest = match line.trim_start().strip_prefix("<!--") { Some(rest) => rest.trim_start(), None => return false };
    let rest = match rest.strip_prefix("thinkly").or_else(|| rest.strip_prefix("goose-note")) { Some(rest) => rest, None => return false };
    match rest.strip_prefix(":raw-block") { Some(rest) => rest.trim_start() == "-->", None => false }
}

fn content(line: &str) -> &str { line.trim_end_matches(['\r', '\n']) }

/// Unescape numeric ASCII entities (`&#35;`, `&#42;`, `&#60;`, `&#124;`, etc.) on lines that were
/// entity-escaped during a previous plain-text copy/paste round-trip.
fn unescape_entity_line(line: &str) -> Cow<'_, str> {
    let body = content(line);
    let suffix = &line[body.len()..];
    let trimmed = body.trim_start();
    let looks_escaped = trimmed.starts_with("&#35;") || trimmed.starts_with("&#62;")
        || trimmed.starts_with("&#45;") || trimmed.starts_with("&#42;") || trimmed.starts_with("&#124;")
        || trimmed.starts_with("&#60;") || body.contains("&#42;&#42;") || body.contains("&#60;strong&#62;")
        || body.contains("&#60;span") || body.contains("&#60;table") || body.contains("&#60;tr")
        || body.contains("&#60;td") || body.contains("&#60;th") || body.contains("&#60;br&#62;")
        || body.contains("&#91;") && body.contains("&#93;&#40;");
    if !looks_escaped { return Cow::Borrowed(line); }
    let mut out = String::with_capacity(line.len());
    let bytes = body.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'&' && i + 3 < bytes.len() && bytes[i + 1] == b'#' {
            let mut j = i + 2;
            let mut code: u32 = 0;
            while j < bytes.len() && bytes[j].is_ascii_digit() && j - (i + 2) <= 3 {
                code = code * 10 + (bytes[j] - b'0') as u32;
                j += 1;
            }
            if j > i + 2 && j < bytes.len() && bytes[j] == b';' && (32..=126).contains(&code)
                && let Some(ch) = char::from_u32(code) {
                out.push(ch); i = j + 1; continue;
            }
        }
        let ch = body[i..].chars().next().unwrap();
        out.push(ch); i += ch.len_utf8();
    }
    out.push_str(suffix);
    Cow::Owned(out)
}

/// Unwrap `thinkly-raw-block` fences so the original HTML is what the editor sees and saves.
pub(super) fn decode(source: &str) -> Cow<'_, str> {
    if !source.contains("raw-block") && !source.contains("&#") { return Cow::Borrowed(source); }
    let lines: Vec<&str> = source.split_inclusive('\n').collect();
    let mut out = String::with_capacity(source.len());
    let mut index = 0;
    while index < lines.len() {
        let line = lines[index];
        if content(line).trim() == "$$" {
            let mut end = index + 1;
            while end < lines.len() && content(lines[end]).trim() != "$$" { end += 1; }
            let stop = if end < lines.len() { end + 1 } else { end };
            lines[index..stop].iter().for_each(|l| out.push_str(l));
            index = end + 1; continue;
        }
        let Some((marker, width, info)) = fence(content(line)) else {
            out.push_str(&unescape_entity_line(line)); index += 1; continue;
        };
        let Some(end) = (index + 1..lines.len()).find(|&i| closes_fence(content(lines[i]), marker, width)) else {
            lines[index..].iter().for_each(|l| out.push_str(l)); break;
        };
        if matches!(info.split_whitespace().next(), Some("thinkly-raw-block" | "goose-raw-block")) {
            let skip = usize::from(lines.get(index + 1).is_some_and(|l| index + 1 < end && is_marker(content(l))));
            lines[index + 1 + skip..end].iter().for_each(|l| out.push_str(l));
        } else {
            lines[index..=end].iter().for_each(|l| out.push_str(l));
        }
        index = end + 1;
    }
    Cow::Owned(out)
}
