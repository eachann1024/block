use super::{blocks, boundaries, inline, paragraph};
use super::super::model::{Block, Kind, ListCode};

pub(super) fn strip_columns(line: &str, cols: usize) -> String {
    let mut rem = cols;
    let mut idx = 0;
    for (i, ch) in line.char_indices() {
        if rem == 0 { idx = i; break; }
        match ch {
            ' ' => { rem -= 1; idx = i + 1; }
            '\t' => {
                if rem >= 4 { rem -= 4; idx = i + 1; }
                else {
                    let spaces = " ".repeat(4 - rem);
                    return format!("{spaces}{}", &line[i + 1..]);
                }
            }
            _ => { idx = i; break; }
        }
    }
    line[idx..].to_owned()
}

fn is_pipe_delimiter(line: &str) -> bool {
    let trimmed = line.trim();
    trimmed.contains('-') && trimmed.contains('|')
        && trimmed.chars().all(|c| matches!(c, ' ' | '\t' | '-' | ':' | '|'))
}

fn starts_block_in_list(line: &str, next_raw: Option<&str>, raw_tail: &[&str]) -> bool {
    if blocks::is_thematic_break(line) || blocks::classify_list_marker(line).is_some()
        || blocks::fence_any_indent(line).is_some() || blocks::inline_math_block(line).is_some()
    {
        return true;
    }
    let (_, trimmed) = blocks::leading_columns(line);
    if trimmed.starts_with('>') || (trimmed.starts_with(":::") && trimmed.trim() != ":::")
        || blocks::classify_heading(trimmed).is_some()
        || super::super::image_source::parse(trimmed).is_some()
        || paragraph::starts(trimmed)
        || trimmed.to_ascii_lowercase().starts_with("<table")
        || boundaries::raw_html_lines(raw_tail).is_some()
    {
        return true;
    }
    trimmed.contains('|') && next_raw.is_some_and(|n| is_pipe_delimiter(n.trim_end_matches(['\r', '\n'])))
}

pub(super) fn parse_item(id: u64, lines: &[&str], defs: &str) -> Option<(Vec<Block>, usize)> {
    let raw0 = *lines.first()?;
    let line0 = raw0.trim_end_matches(['\r', '\n']);
    let marker = blocks::classify_list_marker(line0)?;
    let content_col = marker.indent + marker.marker_width;

    // 1. Same-line fenced code block (`- ```rust`).
    if let Some((_, _, f_char, f_width, f_info)) = blocks::fence_any_indent(marker.body) {
        let mut body = String::new();
        let mut end = 1;
        while end < lines.len() {
            let next_raw = lines[end];
            end += 1;
            let next_line = next_raw.trim_end_matches(['\r', '\n']);
            if blocks::closes_fence_any_indent(next_line, f_char, f_width) { break; }
            body.push_str(&strip_columns(next_raw, content_col));
        }
        let empty_line = body == "\n" || body == "\r\n";
        if body.ends_with('\n') { body.pop(); if body.ends_with('\r') { body.pop(); } }
        let mut parent = Block::new(id, marker.kind, String::new());
        parent.checked = marker.checked;
        parent.meta.indent = marker.indent;
        parent.meta.list_start = marker.start;
        let mut child = Block::new(id + 1, Kind::Paragraph, body);
        child.meta.code_language = Some(f_info.to_owned());
        child.meta.code_empty_line = empty_line;
        child.meta.list_code = Some(ListCode {
            parent_indent: marker.indent,
            extra_indent: 0,
            blank_before: false,
            same_line: true,
        });
        return Some((vec![parent, child], end));
    }

    // 2. Collect soft-wrapped / lazy continuation lines of the item's first paragraph.
    let mut para_lines: Vec<&str> = Vec::new();
    let mut para_end = 1usize;
    if !marker.body.trim().is_empty() {
        para_lines.push(marker.body);
        while para_end < lines.len() {
            let next_line = lines[para_end].trim_end_matches(['\r', '\n']);
            if next_line.trim().is_empty() { break; }
            let (next_indent, trimmed) = blocks::leading_columns(next_line);
            if marker.indent > 0 && next_indent < marker.indent { break; }
            if starts_block_in_list(next_line, lines.get(para_end + 1).copied(), &lines[para_end..]) {
                break;
            }
            para_lines.push(trimmed);
            para_end += 1;
        }
    }

    // 3. Check whether indented child blocks belong to this item.
    let first_child = (para_end..lines.len()).find(|&i| !lines[i].trim().is_empty()).filter(|&fc| {
        let (child_indent, _) = blocks::leading_columns(lines[fc].trim_end_matches(['\r', '\n']));
        if fc == para_end { child_indent > marker.indent } else { child_indent >= content_col }
    });

    let joined_body = para_lines.join("\n");
    let (text, spans, inline_tags) = inline::parse_with_defs(&joined_body, defs);
    let mut parent = Block::new(id, marker.kind, text);
    parent.checked = marker.checked;
    parent.meta.indent = marker.indent;
    parent.meta.list_start = marker.start;
    parent.spans = spans;
    parent.meta.inline = inline_tags;

    let Some(first_child) = first_child else {
        if defs.is_empty() || !joined_body.contains('[') {
            parent.seal_clean_source(lines[..para_end].concat(), None);
        }
        return Some((vec![parent], para_end));
    };

    let (first_child_indent, _) = blocks::leading_columns(lines[first_child].trim_end_matches(['\r', '\n']));
    let min_after_blank = content_col.min(first_child_indent);
    let mut region_end = first_child + 1;
    let mut in_fence: Option<(u8, usize)> = blocks::fence_any_indent(lines[first_child].trim_end_matches(['\r', '\n']))
        .map(|(_, _, ch, w, _)| (ch, w));
    while region_end < lines.len() {
        let raw = lines[region_end];
        let line = raw.trim_end_matches(['\r', '\n']);
        if let Some((f_ch, f_w)) = in_fence {
            if blocks::closes_fence_any_indent(line, f_ch, f_w) { in_fence = None; }
            region_end += 1;
            continue;
        }
        if line.trim().is_empty() {
            region_end += 1;
            continue;
        }
        let (col, _) = blocks::leading_columns(line);
        let after_blank = lines[region_end - 1].trim().is_empty();
        let belongs = if after_blank { col >= min_after_blank } else { col > marker.indent };
        if !belongs { break; }
        in_fence = blocks::fence_any_indent(line).map(|(_, _, ch, w, _)| (ch, w));
        region_end += 1;
    }

    let content_end = (first_child..region_end)
        .rfind(|&i| !lines[i].trim().is_empty())
        .map_or(first_child + 1, |i| i + 1);

    // Preserve `ListCode` metadata when the only child is a single fenced code block.
    if para_end == 1 && let Some((f_indent, _, f_char, f_width, f_info)) =
        blocks::fence_any_indent(lines[first_child].trim_end_matches(['\r', '\n']))
    {
        let mut close_at = None;
        for idx in (first_child + 1)..content_end {
            if blocks::closes_fence_any_indent(lines[idx].trim_end_matches(['\r', '\n']), f_char, f_width) {
                close_at = Some(idx);
                break;
            }
        }
        if close_at == Some(content_end - 1) && f_indent >= content_col && f_indent <= content_col + 3 {
            let mut body = String::new();
            for raw in &lines[first_child + 1..content_end - 1] {
                body.push_str(&strip_columns(raw, f_indent));
            }
            let empty_line = body == "\n" || body == "\r\n";
            if body.ends_with('\n') { body.pop(); if body.ends_with('\r') { body.pop(); } }
            let mut child = Block::new(id + 1, Kind::Paragraph, body);
            child.meta.code_language = Some(f_info.to_owned());
            child.meta.code_empty_line = empty_line;
            child.meta.list_code = Some(ListCode {
                parent_indent: marker.indent,
                extra_indent: f_indent - content_col,
                blank_before: first_child > para_end,
                same_line: false,
            });
            return Some((vec![parent, child], content_end));
        }
    }

    let min_child_col = (first_child..content_end)
        .filter(|&i| !lines[i].trim().is_empty())
        .map(|i| blocks::leading_columns(lines[i].trim_end_matches(['\r', '\n'])).0)
        .min()
        .unwrap_or(content_col);
    let strip_cols = content_col.min(min_child_col).max(marker.indent + 1);
    let mut child_source = String::new();
    for raw in &lines[first_child..content_end] {
        if raw.trim().is_empty() { child_source.push('\n'); }
        else { child_source.push_str(&strip_columns(raw, strip_cols)); }
    }
    parent.meta.tight = first_child == para_end;
    let children = super::parse_with_defs(&child_source, defs);
    let mut result = vec![parent];
    for mut child in children.blocks.iter().cloned() {
        child.id = id + result.len() as u64;
        child.meta.indent += strip_cols;
        child.meta.clean_source = None;
        if let Some(code) = &mut child.meta.list_code { code.parent_indent += strip_cols; }
        result.push(child);
    }
    Some((result, content_end))
}
