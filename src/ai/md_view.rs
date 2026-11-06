//! Selectable Markdown replies use the shared UI renderer.
use gpui::{AnyElement, IntoElement};

pub(crate) fn render(source: &str, key: &str) -> AnyElement {
    let normalized = normalize_markdown(source);
    crate::ui::selectable_message::message(format!("ai-reply-{key}"), &normalized, true).into_any_element()
}

fn block_start(line: &str) -> bool {
    let line = line.trim_start();
    if ["- ", "* ", "+ "].iter().any(|prefix| line.starts_with(prefix)) { return true; }
    let hashes = line.bytes().take_while(|byte| *byte == b'#').count();
    if (1..=6).contains(&hashes) && line.as_bytes().get(hashes) == Some(&b' ') { return true; }
    let digits = line.bytes().take_while(u8::is_ascii_digit).count();
    digits > 0 && line[digits..].starts_with(". ")
}

fn closing_marker(text: &str, marker: &str) -> Option<usize> {
    let mut index = 0;
    while index < text.len() {
        let rest = &text[index..];
        if rest.starts_with('\\') {
            index += rest.chars().take(2).map(char::len_utf8).sum::<usize>();
        } else if rest.starts_with('`') {
            let count = rest.bytes().take_while(|byte| *byte == b'`').count();
            let mut cursor = count;
            let mut closing = None;
            while cursor < rest.len() {
                if rest.as_bytes()[cursor] == b'`' {
                    let run = rest[cursor..].bytes().take_while(|byte| *byte == b'`').count();
                    cursor += run;
                    if run == count { closing = Some(cursor); break; }
                } else {
                    cursor += rest[cursor..].chars().next().unwrap().len_utf8();
                }
            }
            index += closing.unwrap_or(count);
        } else if rest.starts_with(marker) {
            return Some(index);
        } else {
            index += rest.chars().next().unwrap().len_utf8();
        }
    }
    None
}

fn inline_markup(line: &str) -> String {
    let mut result = String::new();
    let mut index = 0;
    while index < line.len() {
        let rest = &line[index..];
        if rest.starts_with('\\') {
            let length = rest.chars().take(2).map(char::len_utf8).sum::<usize>();
            result.push_str(&rest[..length]);
            index += length;
            continue;
        }
        if rest.starts_with('`') {
            let count = rest.bytes().take_while(|byte| *byte == b'`').count();
            let mut end = count;
            let mut closed = false;
            while end < rest.len() {
                if rest.as_bytes()[end] == b'`' {
                    let run = rest[end..].bytes().take_while(|byte| *byte == b'`').count();
                    end += run;
                    if run == count { closed = true; break; }
                } else {
                    end += rest[end..].chars().next().unwrap().len_utf8();
                }
            }
            if !closed { end = count; }
            result.push_str(&rest[..end]);
            index += end;
            continue;
        }
        let marker = [("**", "strong"), ("~~", "del"), ("==", "mark")]
            .into_iter().find(|(marker, _)| rest.starts_with(marker));
        if let Some((marker, tag)) = marker {
            if let Some(end) = closing_marker(&rest[2..], marker).map(|offset| offset + 2) {
                let inner = &rest[2..end];
                if !inner.is_empty() && !inner.starts_with(char::is_whitespace) && !inner.ends_with(char::is_whitespace) {
                    result.push_str(&format!("<{tag}>{}</{tag}>", inline_markup(inner)));
                    index += end + 2;
                    continue;
                }
            }
        }
        let character = rest.chars().next().unwrap();
        result.push(character);
        index += character.len_utf8();
    }
    result
}

pub(crate) fn normalize_markdown(source: &str) -> String {
    let mut output = String::new();
    let mut fence: Option<(u8, usize)> = None;
    let mut previous_regular = false;
    let mut after_break = false;
    for segment in source.split_inclusive('\n') {
        let line = segment.strip_suffix('\n').unwrap_or(segment);
        let trimmed = line.trim();
        let bytes = trimmed.as_bytes();
        let fence_len = bytes.first().filter(|byte| **byte == b'`' || **byte == b'~')
            .map(|byte| bytes.iter().take_while(|item| *item == byte).count()).unwrap_or(0);
        let is_fence = fence_len >= 3;
        if let Some((marker, length)) = fence {
            output.push_str(segment);
            if is_fence && bytes[0] == marker && fence_len >= length && trimmed[fence_len..].trim().is_empty() { fence = None; }
            previous_regular = false;
            continue;
        }
        let thematic = matches!(trimmed, "---" | "***" | "___");
        if (thematic || (after_break && !trimmed.is_empty()) || (previous_regular && block_start(line))) && !output.is_empty() && !output.ends_with("\n\n") {
            if !output.ends_with('\n') { output.push('\n'); }
            output.push('\n');
        }
        if is_fence {
            fence = Some((bytes[0], fence_len));
            output.push_str(segment);
        } else {
            output.push_str(&inline_markup(line));
            if segment.ends_with('\n') { output.push('\n'); }
        }
        previous_regular = !trimmed.is_empty() && !thematic && !is_fence && !block_start(line);
        after_break = thematic;
    }
    if after_break && !output.ends_with("\n\n") {
        if !output.ends_with('\n') { output.push('\n'); }
        output.push('\n');
    }
    output
}
