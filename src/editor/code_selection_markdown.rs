//! Markdown heading and list marker spans for double-click selection.
use std::ops::Range;

pub fn match_markdown_span(text: &str, offset: usize, include_delimiters: bool) -> Option<Range<usize>> {
    if offset > text.len() { return None; }
    let line_start = text[..offset].rfind('\n').map_or(0, |p| p + 1);
    let line_end = text[offset..].find('\n').map_or(text.len(), |p| offset + p);
    let line = &text[line_start..line_end];
    let rel = offset - line_start;
    if let Some(local) = atx(line, rel, include_delimiters) {
        return Some(line_start + local.start..line_start + local.end);
    }
    let (mark, content_on_line) = list_marker(line, rel)?;
    let item_end = list_item_end(text, line_start, line_end);
    let local_end = item_end - line_start;
    let end = if include_delimiters {
        local_end.max(mark.end)
    } else if content_on_line < local_end {
        local_end
    } else {
        mark.end
    };
    let start = if include_delimiters || content_on_line >= local_end { mark.start } else { content_on_line };
    Some(line_start + start..line_start + end)
}

fn atx(line: &str, rel: usize, alt: bool) -> Option<Range<usize>> {
    let bytes = line.as_bytes();
    let mut i = 0;
    while i < bytes.len() && i < 3 && bytes[i] == b' ' { i += 1; }
    let hash_start = i;
    while i < bytes.len() && bytes[i] == b'#' { i += 1; }
    let hashes = i - hash_start;
    if hashes == 0 || hashes > 6 { return None; }
    if i < bytes.len() && bytes[i] != b' ' && bytes[i] != b'\t' { return None; }
    let after = i;
    let trimmed_end = line.trim_end().len();
    let mut close_at = trimmed_end;
    if trimmed_end > after {
        let mut k = trimmed_end;
        while k > after && bytes[k - 1] == b'#' { k -= 1; }
        if k < trimmed_end && k > after && (bytes[k - 1] == b' ' || bytes[k - 1] == b'\t') {
            close_at = k;
        }
    }
    let on_open = rel >= hash_start && rel <= hash_start + hashes;
    let on_close = close_at < trimmed_end && rel >= close_at && rel <= trimmed_end;
    if !on_open && !on_close { return None; }
    let mid = &line[after..close_at];
    let lead = mid.len() - mid.trim_start().len();
    let content_start = after + lead;
    let content_end = after + mid.trim_end().len();
    if alt {
        Some(hash_start..trimmed_end)
    } else if content_start < content_end {
        Some(content_start..content_end)
    } else {
        Some(hash_start..hash_start + hashes)
    }
}

fn list_marker(line: &str, rel: usize) -> Option<(Range<usize>, usize)> {
    let bytes = line.as_bytes();
    let mut i = 0;
    while i < bytes.len() && (bytes[i] == b' ' || bytes[i] == b'\t') { i += 1; }
    if i >= bytes.len() { return None; }
    let mark_start = i;
    if matches!(bytes[i], b'-' | b'*' | b'+') {
        i += 1;
    } else if bytes[i].is_ascii_digit() {
        while i < bytes.len() && bytes[i].is_ascii_digit() { i += 1; }
        if i >= bytes.len() || (bytes[i] != b'.' && bytes[i] != b')') { return None; }
        i += 1;
    } else {
        return None;
    }
    if i < bytes.len() && bytes[i] != b' ' && bytes[i] != b'\t' { return None; }
    if rel < mark_start || rel > i { return None; }
    let rest = &line[i..];
    let content = i + rest.len() - rest.trim_start().len();
    Some((mark_start..i, content.min(line.trim_end().len())))
}

fn marker_indent(line: &str) -> Option<usize> {
    list_marker(line, 0).map(|(mark, _)| {
        line[..mark.start].chars().map(|c| if c == '\t' { 4 } else { 1 }).sum()
    })
}

/// Continue through indented wrapped lines. Stop at a blank line or a marker
/// at the same or a smaller indent. Nested deeper markers stay inside.
fn list_item_end(text: &str, line_start: usize, line_end: usize) -> usize {
    let base = marker_indent(&text[line_start..line_end]).unwrap_or(0);
    let mut end = line_end;
    let mut pos = line_end;
    while pos < text.len() {
        if text.as_bytes().get(pos) == Some(&b'\n') { pos += 1; }
        if pos >= text.len() { break; }
        let next_end = text[pos..].find('\n').map_or(text.len(), |p| pos + p);
        let next = &text[pos..next_end];
        if next.trim().is_empty() { break; }
        if let Some(indent) = marker_indent(next) {
            if indent <= base { break; }
        }
        end = next_end;
        pos = next_end;
    }
    end
}
