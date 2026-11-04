//! Markdown round-trip for GitHub alert callouts (`> [!NOTE]`) and multi-column HTML containers.
use super::{html::{self, HtmlValue}, inline};
use super::super::model::{Block, CalloutKind, Doc, Kind};

pub(super) fn parse_callout(id: u64, lines: &[&str]) -> Option<(Block, usize, usize)> {
    let first = lines.first()?.trim_end_matches(['\r', '\n']);
    let trimmed = first.trim_start_matches(' ');
    if first.len() - trimmed.len() > 3 { return None; }
    let kind = CalloutKind::from_alert(trimmed.strip_prefix('>')?)?;
    let mut bodies: Vec<&str> = Vec::new();
    let mut end = 1;
    while end < lines.len() {
        let line = lines[end].trim_end_matches(['\r', '\n']);
        let rest = line.trim_start_matches(' ');
        if line.len() - rest.len() > 3 { break; }
        let Some(after) = rest.strip_prefix('>') else { break };
        let body = after.strip_prefix(' ').unwrap_or(after);
        if CalloutKind::from_alert(body).is_some() { break; }
        bodies.push(body);
        end += 1;
    }
    let mut block = Block::new(id, Kind::Quote, String::new());
    block.meta.callout = Some(kind);
    for (i, body) in bodies.iter().enumerate() {
        if i > 0 { block.replace_range(block.text.len()..block.text.len(), "\n"); }
        if body.is_empty() { continue; }
        let (text, spans, tags) = inline::parse(body)?;
        let mut part = Block::new(0, Kind::Paragraph, text);
        part.spans = spans; part.meta.inline = tags;
        block.append(&part);
    }
    let bytes = lines[..end].iter().map(|l| l.len()).sum();
    Some((block, end, bytes))
}

pub(super) fn serialize_callout(block: &Block, kind: CalloutKind, out: &mut String) {
    out.push_str("> [!"); out.push_str(kind.tag()); out.push_str("]\n");
    if block.text.is_empty() && block.spans.is_empty() && block.meta.inline.is_empty() { return; }
    let mut start = 0;
    for part in block.text.split('\n') {
        let end = start + part.len();
        let slice = block.slice(start..end);
        out.push_str("> ");
        inline::serialize(&slice, out);
        out.push('\n');
        start = end + 1;
    }
}

fn is_div_class(name: &str, attrs: &[(String, String)], target: &str) -> bool {
    name.eq_ignore_ascii_case("div")
        && attrs.iter().any(|(k, v)| k.eq_ignore_ascii_case("class") && v.split_whitespace().any(|c| c == target))
}

fn update_fence(fence: &mut Option<(u8, usize)>, line: &str) -> bool {
    let body = line.trim_end_matches(['\r', '\n']);
    if let Some((ch, min_len)) = *fence {
        if super::blocks::closes_fence(body, ch, min_len) { *fence = None; }
        true
    } else if let Some((ch, len, _)) = super::blocks::fence(body) {
        *fence = Some((ch, len)); true
    } else { false }
}

pub(super) fn starts_columns(line: &str) -> bool {
    let Some(tokens) = html::tokens(line.trim()) else { return false };
    matches!(tokens.first().map(|t| &t.value),
        Some(HtmlValue::Tag { name, attrs, close: false, empty: false }) if is_div_class(name, attrs, "columns"))
}

pub(super) fn columns_line_count(lines: &[&str]) -> usize {
    let mut depth = 0usize;
    let mut fence = None;
    for (i, line) in lines.iter().enumerate() {
        if i > 0 && update_fence(&mut fence, line) { continue; }
        if let Some(tokens) = html::tokens(line.trim()) {
            for token in tokens {
                if let HtmlValue::Tag { name, close, empty, .. } = token.value
                    && name.eq_ignore_ascii_case("div") && !empty
                {
                    if close { depth = depth.saturating_sub(1); } else { depth += 1; }
                }
            }
        }
        if i > 0 && depth == 0 { return i + 1; }
        if i == 0 && depth == 0 { return 1; }
    }
    lines.len()
}

fn parse_column_body(group_id: u64, col_idx: usize, raw: &str) -> Result<Vec<Block>, &'static str> {
    let mut blocks = if raw.trim_matches(['\r', '\n']).is_empty() {
        vec![Block::new(0, Kind::Paragraph, String::new())]
    } else if let Some(after_lead) = raw.strip_prefix("\r\n").or_else(|| raw.strip_prefix('\n')) {
        let inner = after_lead.strip_suffix("\r\n\r\n").map(|s| format!("{s}\n"))
            .or_else(|| after_lead.strip_suffix("\n\n").map(|s| format!("{s}\n")))
            .unwrap_or_else(|| after_lead.to_owned());
        super::parse(&inner).blocks.iter().cloned().collect()
    } else {
        let body = raw.strip_suffix("\r\n").or_else(|| raw.strip_suffix('\n')).unwrap_or(raw);
        let mut out = Vec::new();
        for line in body.split('\n') {
            let line = line.strip_suffix('\r').unwrap_or(line);
            if line.is_empty() {
                out.push(Block::new(0, Kind::Paragraph, String::new()));
            } else {
                let (text, spans, tags) = inline::parse(line)
                    .ok_or("Column contains unsupported Markdown or HTML")?;
                let mut b = Block::new(0, Kind::Paragraph, text);
                b.spans = spans; b.meta.inline = tags; out.push(b);
            }
        }
        out
    };
    if blocks.is_empty() { blocks.push(Block::new(0, Kind::Paragraph, String::new())); }
    for b in &mut blocks { b.meta.column = Some((group_id, col_idx)); }
    Ok(blocks)
}

pub(super) fn parse_columns(group_id: u64, source: &str) -> Result<Vec<Block>, &'static str> {
    let mut depth = 0usize;
    let mut fence = None;
    let mut col_start: Option<usize> = None;
    let mut col_idx = 0usize;
    let mut result = Vec::new();
    let mut offset = 0usize;
    for line in source.split_inclusive('\n') {
        let line_start = offset;
        offset += line.len();
        if depth > 0 && update_fence(&mut fence, line) { continue; }
        let trimmed = line.trim();
        if !trimmed.starts_with('<') {
            if depth < 2 && !trimmed.is_empty() { return Err("Text outside column"); }
            continue;
        }
        let Some(tokens) = html::tokens(trimmed) else {
            if depth < 2 { return Err("Columns HTML contains malformed tokens"); }
            continue;
        };
        for token in &tokens {
            let HtmlValue::Tag { name, attrs, close, empty } = &token.value else { continue };
            if !name.eq_ignore_ascii_case("div") || *empty { continue; }
            if !*close {
                depth += 1;
                if depth == 1 && !is_div_class(name, attrs, "columns") {
                    return Err("Missing columns wrapper class");
                }
                if depth == 2 {
                    if !is_div_class(name, attrs, "column") {
                        return Err("Unsupported element inside columns container");
                    }
                    let tag_end_in_line = line.find(trimmed).unwrap_or(0) + token.range.end;
                    col_start = Some(
                        if tag_end_in_line >= line.trim_end_matches(['\r', '\n']).len() { offset }
                        else { line_start + tag_end_in_line },
                    );
                }
            } else {
                if depth == 2 && let Some(start) = col_start.take() {
                    let close_pos = line_start + line.find(trimmed).unwrap_or(0) + token.range.start;
                    let end_offset = if close_pos == line_start + line.len() - trimmed.len() { line_start } else { close_pos };
                    let raw = &source[start..end_offset];
                    result.extend(parse_column_body(group_id, col_idx, raw)?);
                    col_idx += 1;
                }
                depth = depth.checked_sub(1).ok_or("Unclosed column div")?;
            }
        }
    }
    if depth != 0 || col_idx < 2 { return Err("Columns container requires at least two columns"); }
    Ok(result)
}

fn is_empty_plain_paragraph(b: &Block) -> bool {
    b.kind == Kind::Paragraph && b.text.is_empty() && b.spans.is_empty() && b.meta.inline.is_empty()
        && b.meta.table.is_none() && b.meta.raw_source.is_none()
        && b.meta.html_paragraph.is_none() && b.meta.code_language.is_none()
}

pub(super) fn serialize_columns(blocks: &[&Block], out: &mut String) {
    out.push_str("<div class=\"columns\">\n");
    let max_col = blocks.iter().filter_map(|b| b.meta.column.map(|(_, c)| c)).max().unwrap_or(0);
    let col_count = (max_col + 1).max(2);
    for col in 0..col_count {
        let mut col_blocks: Vec<Block> = blocks.iter()
            .filter(|b| b.meta.column.map(|(_, c)| c) == Some(col))
            .map(|b| { let mut cloned = (*b).clone(); cloned.meta.column = None; cloned })
            .collect();
        if col_blocks.is_empty() {
            col_blocks.push(Block::new(0, Kind::Paragraph, String::new()));
        }
        if col_blocks.len() == 1 && is_empty_plain_paragraph(&col_blocks[0]) {
            out.push_str("<div class=\"column\">\n\n</div>\n");
        } else {
            out.push_str("<div class=\"column\">\n\n");
            let inner = super::serialize(&Doc::from_blocks(col_blocks));
            out.push_str(&inner);
            if !inner.ends_with("\n\n") { out.push('\n'); }
            out.push_str("</div>\n");
        }
    }
    out.push_str("</div>\n");
}
