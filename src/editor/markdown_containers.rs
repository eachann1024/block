//! Markdown round-trip and Notion-style normalization for callouts, blockquotes, and `:::` containers.
use super::{blocks, boundaries, inline, paragraph};
use super::super::model::{Block, CalloutKind, Kind, MarkKind, Span};

pub(super) fn parse_callout(id: u64, lines: &[&str], defs: &str) -> Option<(Block, usize, usize)> {
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
    let mut has_bracket = false;
    for (i, body) in bodies.iter().enumerate() {
        if i > 0 { block.replace_range(block.text.len()..block.text.len(), "\n"); }
        if body.is_empty() { continue; }
        has_bracket |= body.contains('[');
        let (text, spans, tags) = inline::parse_with_defs(body, defs);
        let mut part = Block::new(0, Kind::Paragraph, text);
        part.spans = spans; part.meta.inline = tags;
        block.append(&part);
    }
    let original = lines[..end].concat();
    let bytes = original.len();
    if defs.is_empty() || !has_bracket {
        block.seal_clean_source(original, None);
    }
    Some((block, end, bytes))
}

fn is_structural_quote_inner(line: &str) -> bool {
    if line.trim().is_empty() || blocks::is_thematic_break(line) || blocks::classify_list_marker(line).is_some()
        || blocks::fence_any_indent(line).is_some() || blocks::inline_math_block(line).is_some()
    {
        return true;
    }
    let (_, trimmed) = blocks::leading_columns(line);
    trimmed.starts_with('>') || trimmed.starts_with("[!") || trimmed.starts_with(":::")
        || blocks::classify_heading(trimmed).is_some()
        || super::super::image_source::parse(trimmed).is_some()
        || paragraph::starts(trimmed)
        || trimmed.to_ascii_lowercase().starts_with("<table")
        || boundaries::raw_html_lines(&[line]).is_some()
}

pub(super) fn parse_blockquote(id: u64, lines: &[&str], defs: &str) -> Option<(Vec<Block>, usize)> {
    let first = lines.first()?.trim_end_matches(['\r', '\n']);
    let trimmed = first.trim_start_matches(' ');
    if first.len() - trimmed.len() > 3 || !trimmed.starts_with('>') { return None; }
    let mut inner_lines: Vec<String> = Vec::new();
    let mut end = 0usize;
    while end < lines.len() {
        let raw = lines[end];
        let line = raw.trim_end_matches(['\r', '\n']);
        let rest = line.trim_start_matches(' ');
        if line.len() - rest.len() > 3 { break; }
        let Some(after) = rest.strip_prefix('>') else { break };
        let body = after.strip_prefix(' ').unwrap_or(after);
        if end > 0 && CalloutKind::from_alert(body).is_some() { break; }
        inner_lines.push(body.to_owned());
        end += 1;
    }
    if end == 0 { return None; }
    let simple_prose = inner_lines.iter().all(|l| !is_structural_quote_inner(l))
        && !inner_lines.iter().enumerate().any(|(i, l)| {
            l.contains('|') && inner_lines.get(i + 1).is_some_and(|n| {
                let n = n.trim();
                n.contains('-') && n.contains('|') && n.chars().all(|c| matches!(c, ' ' | '\t' | '-' | ':' | '|'))
            })
        });
    if simple_prose {
        let joined = inner_lines.join("\n");
        let (text, spans, tags) = inline::parse_with_defs(&joined, defs);
        let mut block = Block::new(id, Kind::Quote, text);
        block.spans = spans;
        block.meta.inline = tags;
        if defs.is_empty() || !joined.contains('[') {
            block.seal_clean_source(lines[..end].concat(), None);
        }
        return Some((vec![block], end));
    }
    let mut inner_source = String::new();
    for line in &inner_lines {
        inner_source.push_str(line);
        inner_source.push('\n');
    }
    let parsed = super::parse_with_defs(&inner_source, defs);
    let mut out = Vec::new();
    for mut block in parsed.blocks.iter().cloned() {
        block.id = id + out.len() as u64;
        block.meta.clean_source = None;
        if block.kind == Kind::Paragraph && block.meta.table.is_none()
            && block.meta.code_language.is_none() && !block.is_atomic()
        {
            block.kind = Kind::Quote;
        }
        out.push(block);
    }
    Some((out, end))
}

pub(super) fn parse_custom_container(id: u64, lines: &[&str], defs: &str) -> Option<(Block, usize)> {
    let first = lines.first()?.trim_end_matches(['\r', '\n']).trim_start();
    let header = first.strip_prefix(":::")?.trim();
    if header.is_empty() && first.trim() == ":::" { return None; }
    let (closed, end) = lines.iter().enumerate().skip(1)
        .find(|(_, l)| l.trim() == ":::")
        .map_or_else(|| {
            let stop = lines.iter().skip(1).position(|l| l.trim().is_empty()).map_or(lines.len(), |i| i + 1);
            (false, stop)
        }, |(i, _)| (true, i + 1));
    let body_end = if closed { end - 1 } else { end };
    let mut words = header.split_whitespace();
    let tag = words.next().unwrap_or("").to_ascii_lowercase();
    let rest_title = header[tag.len()..].trim();
    let callout = match tag.as_str() {
        "note" | "info" => Some(CalloutKind::Note),
        "tip" | "success" => Some(CalloutKind::Tip),
        "important" => Some(CalloutKind::Important),
        "warning" | "warn" => Some(CalloutKind::Warning),
        "caution" | "danger" | "error" => Some(CalloutKind::Caution),
        _ => None,
    };
    let mut block = Block::new(id, Kind::Quote, String::new());
    block.meta.callout = callout;
    let title = if callout.is_some() { rest_title } else { header };
    if !title.is_empty() {
        let (t, mut spans, tags) = inline::parse_with_defs(title, defs);
        if !t.is_empty() {
            spans.push(Span { start: 0, end: t.len(), marks: vec![MarkKind::Bold] });
        }
        let mut head = Block::new(0, Kind::Paragraph, t);
        head.spans = spans; head.meta.inline = tags;
        head.normalize_spans();
        block.append(&head);
    }
    for raw in &lines[1..body_end] {
        let body = raw.trim_end_matches(['\r', '\n']).trim();
        if body.is_empty() { continue; }
        if !block.text.is_empty() { block.replace_range(block.text.len()..block.text.len(), "\n"); }
        let (t, spans, tags) = inline::parse_with_defs(body, defs);
        let mut part = Block::new(0, Kind::Paragraph, t);
        part.spans = spans; part.meta.inline = tags;
        block.append(&part);
    }
    if defs.is_empty() || !lines[..end].iter().any(|l| l.contains('[')) {
        block.seal_clean_source(lines[..end].concat(), None);
    }
    Some((block, end))
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

pub(super) fn serialize_quote(block: &Block, out: &mut String) {
    let indent_str = " ".repeat(block.meta.indent);
    if !block.text.contains('\n') {
        out.push_str(&indent_str);
        out.push_str("> ");
        inline::serialize(block, out);
        out.push('\n');
        return;
    }
    let parts: Vec<&str> = block.text.split('\n').collect();
    if parts.iter().any(|p| p.is_empty() || is_structural_quote_inner(p)) {
        out.push_str(&indent_str);
        out.push_str("> ");
        inline::serialize(block, out);
        out.push('\n');
        return;
    }
    let mut start = 0;
    for part in parts {
        let end = start + part.len();
        let mut slice = block.slice(start..end);
        slice.kind = Kind::Paragraph;
        out.push_str(&indent_str);
        out.push_str("> ");
        inline::serialize(&slice, out);
        out.push('\n');
        start = end + 1;
    }
}
