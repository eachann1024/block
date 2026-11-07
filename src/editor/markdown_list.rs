use super::{blocks, inline, raw};
use super::super::model::{Block, Kind, ListCode};
use pulldown_cmark::{CodeBlockKind, Event, Options, Parser, Tag, TagEnd};

pub(super) fn parse(id: u64, lines: &[&str]) -> Option<(Vec<Block>, usize)> {
    let line = lines.first()?.trim_end_matches(['\r', '\n']);
    let (kind, body, checked, indent, start, opaque) = blocks::classify(line);
    if !matches!(kind, Kind::Bullet | Kind::Numbered | Kind::Todo) { return None; }
    let next = lines.iter().skip(1).find(|line| !line.trim().is_empty())?;
    let (next_kind, _, _, next_indent, _, _) = blocks::classify(next.trim_end_matches(['\r', '\n']));
    if next_indent <= indent { return None; }
    if matches!(next_kind, Kind::Bullet | Kind::Numbered | Kind::Todo) { return None; }
    let limit = lines.iter().enumerate().skip(1).find_map(|(index, line)| {
        if line.trim().is_empty() { return None; }
        let (_, _, _, child_indent, _, _) = blocks::classify(line.trim_end_matches(['\r', '\n']));
        (child_indent <= indent).then_some(index)
    }).unwrap_or(lines.len());
    let source = lines[..limit].concat();
    if indent >= 4 {
        let mut block = raw(id, source, "Unsupported list item is retained as original Markdown");
        block.kind = kind; block.checked = checked; block.meta.indent = indent; block.meta.list_start = start;
        return Some((vec![block], limit));
    }
    let mut item_range = None;
    let mut depth = 0usize;
    let mut code = None;
    let mut code_text = String::new();
    let mut in_code = false;
    let mut nested = false;
    for (event, range) in Parser::new_ext(&source, Options::ENABLE_TASKLISTS).into_offset_iter() {
        match event {
            Event::Start(Tag::Item) => {
                depth += 1;
                if depth == 1 { item_range = Some(range); } else { nested = true; }
            }
            Event::End(TagEnd::Item) => {
                depth = depth.saturating_sub(1);
                if depth == 0 && item_range.is_some() { break; }
            }
            Event::Start(Tag::CodeBlock(CodeBlockKind::Fenced(info))) if depth == 1 => {
                if code.is_some() { nested = true; }
                code = Some((range, info.into_string())); in_code = true;
            }
            Event::End(TagEnd::CodeBlock) => in_code = false,
            Event::Text(text) if in_code => code_text.push_str(&text),
            _ => {}
        }
    }
    let item = item_range?;
    if item.start > line.len() { return None; }
    let count = lines.iter().scan(0usize, |bytes, line| {
        if *bytes >= item.end { return None; }
        *bytes += line.len(); Some(())
    }).count();
    if count == 0 { return None; }
    let original = lines[..count].concat();
    if let Some((range, language)) = code && !nested && !opaque {
        let open_line = source[..range.start].rfind('\n').map_or(0, |at| at + 1);
        let before = source.get(lines[0].len()..open_line).unwrap_or("not a continuation");
        let after = &source[range.end..original.len().min(source.len())];
        let marker_width = if kind == Kind::Numbered {
            line.trim_start().bytes().take_while(u8::is_ascii_digit).count() + 2
        } else { 2 };
        let same_line = open_line == 0 && blocks::fence(body).is_some();
        let opening = &source[open_line..range.start];
        let padding = opening.len();
        if (same_line || (open_line >= lines[0].len() && before.trim().is_empty()
            && opening.bytes().all(|byte| byte == b' ') && padding >= indent + marker_width
            && padding <= indent + marker_width + 3)) && after.trim().is_empty()
            && let Some((text, spans, annotations)) = inline::parse(if same_line { "" } else { body })
        {
            let mut parent = Block::new(id, kind, text);
            parent.checked = checked; parent.meta.indent = indent; parent.meta.list_start = start;
            parent.spans = spans; parent.meta.inline = annotations;
            let empty_line = code_text == "\n" || code_text == "\r\n";
            if code_text.ends_with('\n') { code_text.pop(); if code_text.ends_with('\r') { code_text.pop(); } }
            let mut child = Block::new(id + 1, Kind::Paragraph, code_text);
            child.meta.code_language = Some(language); child.meta.code_empty_line = empty_line;
            child.meta.list_code = Some(ListCode { parent_indent: indent,
                extra_indent: if same_line { 0 } else { padding - indent - marker_width },
                blank_before: !same_line && !before.is_empty(), same_line });
            return Some((vec![parent, child], count));
        }
    }
    let mut block = raw(id, original, "Unsupported list item is retained as original Markdown");
    block.kind = kind; block.checked = checked; block.meta.indent = indent; block.meta.list_start = start;
    Some((vec![block], count))
}
