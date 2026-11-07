use crate::editor::model::{Block, InlineTag, Kind, MarkKind, Span, TableCell, TableRow};
use html5ever::{tendril::StrTendril, tokenizer::{BufferQueue, TagKind, Token, TokenSink, TokenSinkResult, Tokenizer, TokenizerOpts}};
use pulldown_cmark::{Event, Options, Parser, Tag, TagEnd};
use std::{cell::RefCell, rc::Rc};

pub(super) fn is_table_scaffolding(trimmed: &str) -> bool {
    if trimmed.starts_with('|') && trimmed.chars().all(|ch| matches!(ch, '|' | ' ' | '-' | ':')) {
        return true;
    }
    let lower = trimmed.to_ascii_lowercase();
    lower.starts_with("<table") || lower.starts_with("</table") || lower.starts_with("<colgroup")
        || lower.starts_with("</colgroup") || lower.starts_with("<col ") || lower.starts_with("<col>")
        || lower.starts_with("<thead") || lower.starts_with("</thead") || lower.starts_with("<tbody")
        || lower.starts_with("</tbody") || lower.starts_with("<tfoot") || lower.starts_with("</tfoot")
        || lower == "<tr>" || lower == "</tr>"
}

pub(super) fn is_table_row_line(trimmed: &str) -> bool {
    if is_table_scaffolding(trimmed) { return false; }
    trimmed.starts_with('|') || trimmed.to_ascii_lowercase().starts_with("<tr")
}

pub(super) fn is_meaningful_line(source: &str) -> bool {
    let trimmed = source.trim();
    if trimmed.is_empty() || trimmed.starts_with("```") || trimmed.starts_with("~~~") || is_table_scaffolding(trimmed) {
        return false;
    }
    if trimmed.starts_with('|') { return true; }
    if trimmed.to_ascii_lowercase().starts_with("<tr") {
        return parse_table_row(trimmed, false).is_some_and(|row| row.cells.iter().any(|c| !c.content.text.trim().is_empty()));
    }
    let block = parse_line_block(trimmed);
    !block.text.trim().is_empty() || block.meta.table.is_some()
}

pub(super) fn parse_table_row(trimmed: &str, default_header: bool) -> Option<TableRow> {
    if trimmed.starts_with('|') {
        let count = trimmed.chars().filter(|ch| *ch == '|').count().saturating_sub(1).max(1);
        let separator = format!("|{}|", vec![" --- "; count].join("|"));
        let doc = crate::editor::markdown::parse(&format!("{trimmed}\n{separator}\n"));
        if let Some(mut row) = doc.blocks.iter().find_map(|b| b.meta.table.as_ref()).and_then(|t| t.rows.first()).cloned() {
            for cell in &mut row.cells {
                if default_header { cell.header = true; }
                sanitize_block(&mut cell.content);
            }
            return Some(row);
        }
        let inner = trimmed.strip_prefix('|').unwrap_or(trimmed);
        let inner = inner.strip_suffix('|').unwrap_or(inner);
        let cells: Vec<TableCell> = inner.split('|').enumerate().map(|(col, raw)| {
            let content = parse_inline_fragment(raw.trim());
            TableCell {
                original: Box::new(content.clone()), content, row: 0, column: col,
                colspan: 1, rowspan: 1, header: default_header, attrs: Vec::new(), source_range: 0..0,
            }
        }).collect();
        return (!cells.is_empty()).then_some(TableRow { cells });
    }
    if trimmed.to_ascii_lowercase().starts_with("<tr") {
        let doc = crate::editor::markdown::parse(&format!("<table><tbody>{trimmed}</tbody></table>"));
        if let Some(mut row) = doc.blocks.iter().find_map(|b| b.meta.table.as_ref()).and_then(|t| t.rows.first()).cloned() {
            for cell in &mut row.cells {
                if default_header { cell.header = true; }
                sanitize_block(&mut cell.content);
            }
            return Some(row);
        }
        let cells = extract_html_cells(trimmed, default_header);
        if !cells.is_empty() { return Some(TableRow { cells }); }
    }
    None
}

fn extract_html_cells(source: &str, default_header: bool) -> Vec<TableCell> {
    let mut cells = Vec::new();
    let mut rest = source;
    while let Some(open_at) = rest.find('<') {
        let after = &rest[open_at + 1..];
        let lower = after.to_ascii_lowercase();
        let (is_th, tag_len) = if lower.starts_with("th") && lower.as_bytes().get(2).is_some_and(|b| b.is_ascii_whitespace() || *b == b'>') {
            (true, 2)
        } else if lower.starts_with("td") && lower.as_bytes().get(2).is_some_and(|b| b.is_ascii_whitespace() || *b == b'>') {
            (false, 2)
        } else {
            rest = &after[after.chars().next().map_or(0, char::len_utf8)..];
            continue;
        };
        let Some(gt) = after[tag_len..].find('>') else { break };
        let inner_start = open_at + 1 + tag_len + gt + 1;
        let body = &rest[inner_start..];
        let close_pat = if is_th { "</th>" } else { "</td>" };
        let close_at = body.to_ascii_lowercase().find(close_pat).unwrap_or(body.len());
        let content = parse_inline_fragment(&body[..close_at]);
        let col = cells.len();
        cells.push(TableCell {
            original: Box::new(content.clone()), content, row: 0, column: col,
            colspan: 1, rowspan: 1, header: is_th || default_header, attrs: Vec::new(), source_range: 0..0,
        });
        rest = &body[(close_at + close_pat.len()).min(body.len())..];
    }
    cells
}

pub(super) fn parse_line_block(source: &str) -> Block {
    let line = source.trim_end_matches(['\r', '\n']);
    let doc = crate::editor::markdown::parse(line);
    let mut block = doc.blocks.iter().next().cloned().unwrap_or_else(|| Block::new(1, Kind::Paragraph, String::new()));
    sanitize_block(&mut block);
    block
}

pub(super) fn sanitize_block(block: &mut Block) {
    if block.meta.code_language.is_some() { return; }
    if let Some(table) = &mut block.meta.table {
        for row in &mut table.rows {
            for cell in &mut row.cells { sanitize_block(&mut cell.content); }
        }
        return;
    }
    if block.meta.raw_source.is_none() && !has_html_tag(&block.text) { return; }
    let raw = block.text.trim_end_matches(['\r', '\n']).trim_start_matches([' ', '\t']);
    let (kind, body, checked, level) = if block.meta.raw_source.is_some() {
        classify_prefix(raw)
    } else {
        (block.kind, block.text.as_str(), block.checked, block.meta.heading_level)
    };
    let parsed = parse_inline_fragment(body);
    block.kind = kind;
    block.checked = checked;
    block.meta.heading_level = level;
    block.text = parsed.text;
    block.spans = parsed.spans;
    block.meta.inline = parsed.meta.inline;
    block.meta.raw_source = None;
    block.meta.unsupported_reason = None;
}

fn has_html_tag(text: &str) -> bool {
    let bytes = text.as_bytes();
    for i in 0..bytes.len().saturating_sub(2) {
        if bytes[i] == b'<' && (bytes[i + 1].is_ascii_alphabetic() || bytes[i + 1] == b'/') && text[i + 2..].contains('>') {
            return true;
        }
    }
    false
}

fn classify_prefix(body: &str) -> (Kind, &str, bool, Option<u8>) {
    for (prefix, checked) in [("- [ ] ", false), ("- [x] ", true), ("- [X] ", true)] {
        if let Some(rest) = body.strip_prefix(prefix) { return (Kind::Todo, rest, checked, None); }
    }
    for prefix in ["- ", "+ ", "* "] {
        if let Some(rest) = body.strip_prefix(prefix) { return (Kind::Bullet, rest, false, None); }
    }
    let digits = body.bytes().take_while(u8::is_ascii_digit).count();
    if digits > 0 && (body[digits..].starts_with(". ") || body[digits..].starts_with(") ")) {
        return (Kind::Numbered, &body[digits + 2..], false, None);
    }
    for (prefix, kind, lvl) in [
        ("###### ", Kind::H3, Some(6)), ("##### ", Kind::H3, Some(5)), ("#### ", Kind::H3, Some(4)),
        ("### ", Kind::H3, None), ("## ", Kind::H2, None), ("# ", Kind::H1, None), ("> ", Kind::Quote, None),
    ] {
        if let Some(rest) = body.strip_prefix(prefix) { return (kind, rest, false, lvl); }
    }
    (Kind::Paragraph, body, false, None)
}

#[derive(Clone)]
enum Tok { Text(String), Tag { name: String, attrs: Vec<(String, String)>, close: bool, empty: bool } }

#[derive(Clone)]
struct Sink(Rc<RefCell<Vec<Tok>>>);
impl TokenSink for Sink {
    type Handle = ();
    fn process_token(&self, token: Token, _: u64) -> TokenSinkResult<()> {
        match token {
            Token::CharacterTokens(t) => self.0.borrow_mut().push(Tok::Text(t.to_string())),
            Token::TagToken(tag) => self.0.borrow_mut().push(Tok::Tag {
                name: tag.name.to_string().to_ascii_lowercase(),
                close: tag.kind == TagKind::EndTag,
                empty: tag.self_closing,
                attrs: tag.attrs.into_iter().map(|a| (a.name.local.to_string(), a.value.to_string())).collect(),
            }),
            _ => {}
        }
        TokenSinkResult::Continue
    }
}

pub(super) fn parse_inline_fragment(input: &str) -> Block {
    let mut text = String::new();
    let mut spans: Vec<Span> = Vec::new();
    let mut inline: Vec<InlineTag> = Vec::new();
    let mut marks = [0u32; 4];
    let mut open_tags: Vec<(String, usize, Vec<(String, String)>, Option<usize>)> = Vec::new();
    let mut open_links: Vec<(usize, Vec<(String, String)>)> = Vec::new();
    let push_text = |value: &str, text: &mut String, spans: &mut Vec<Span>, marks: &[u32; 4]| {
        if value.is_empty() { return; }
        let start = text.len();
        text.push_str(value);
        let active: Vec<_> = [MarkKind::Bold, MarkKind::Italic, MarkKind::Strike, MarkKind::Code]
            .into_iter().filter(|m| marks[*m as usize] > 0).collect();
        if active.is_empty() { return; }
        if let Some(last) = spans.last_mut() && last.end == start && last.marks == active {
            last.end = text.len();
        } else {
            spans.push(Span { start, end: text.len(), marks: active });
        }
    };
    let handle_html = |raw_html: &str, text: &mut String, spans: &mut Vec<Span>, inline: &mut Vec<InlineTag>,
        marks: &mut [u32; 4], open_tags: &mut Vec<(String, usize, Vec<(String, String)>, Option<usize>)>| {
        let out = Rc::new(RefCell::new(Vec::new()));
        let tokenizer = Tokenizer::new(Sink(out.clone()), TokenizerOpts::default());
        let queue = BufferQueue::default();
        queue.push_back(StrTendril::from(raw_html));
        let _ = tokenizer.feed(&queue);
        tokenizer.end();
        for tok in out.borrow().iter() {
            match tok {
                Tok::Text(t) => push_text(t, text, spans, marks),
                Tok::Tag { name, attrs, close, empty } => {
                    if name == "br" { if !*close { push_text("\n", text, spans, marks); } continue; }
                    if name == "wbr" { continue; }
                    let mark = match name.as_str() {
                        "strong" | "b" => Some(0),
                        "em" | "i" | "cite" | "dfn" | "var" => Some(1),
                        "del" | "s" | "strike" => Some(2),
                        "code" | "kbd" | "samp" | "tt" => Some(3),
                        _ => None,
                    };
                    if *close {
                        if let Some(pos) = open_tags.iter().rposition(|(n, _, _, _)| n == name) {
                            let (opened, start, attrs, m) = open_tags.remove(pos);
                            if let Some(m) = m { marks[m] = marks[m].saturating_sub(1); }
                            if start < text.len() && (m.is_none() || !attrs.is_empty()) {
                                inline.push(InlineTag { start, end: text.len(), name: if m.is_some() { "span".into() } else { opened }, attrs });
                            }
                        }
                    } else if !*empty {
                        if let Some(m) = mark { marks[m] += 1; }
                        open_tags.push((name.clone(), text.len(), attrs.clone(), mark));
                    }
                }
            }
        }
    };
    let source = format!("x {input}");
    let mut skip = 2usize;
    for event in Parser::new_ext(&source, Options::ENABLE_STRIKETHROUGH) {
        match event {
            Event::Start(Tag::Strong) => marks[0] += 1,
            Event::End(TagEnd::Strong) => marks[0] = marks[0].saturating_sub(1),
            Event::Start(Tag::Emphasis) => marks[1] += 1,
            Event::End(TagEnd::Emphasis) => marks[1] = marks[1].saturating_sub(1),
            Event::Start(Tag::Strikethrough) => marks[2] += 1,
            Event::End(TagEnd::Strikethrough) => marks[2] = marks[2].saturating_sub(1),
            Event::Start(Tag::Link { dest_url, title, .. }) => {
                let mut attrs = vec![("href".into(), dest_url.to_string())];
                if !title.is_empty() { attrs.push(("title".into(), title.to_string())); }
                open_links.push((text.len(), attrs));
            }
            Event::End(TagEnd::Link) => if let Some((start, attrs)) = open_links.pop() && start < text.len() {
                inline.push(InlineTag { start, end: text.len(), name: "a".into(), attrs });
            },
            Event::Text(val) => {
                let n = skip.min(val.len());
                skip -= n;
                push_text(&val[n..], &mut text, &mut spans, &marks);
            }
            Event::Code(val) => {
                marks[3] += 1;
                push_text(&val, &mut text, &mut spans, &marks);
                marks[3] = marks[3].saturating_sub(1);
            }
            Event::InlineHtml(val) | Event::Html(val) => {
                handle_html(&val, &mut text, &mut spans, &mut inline, &mut marks, &mut open_tags);
            }
            Event::SoftBreak | Event::HardBreak => push_text("\n", &mut text, &mut spans, &marks),
            _ => {}
        }
    }
    for (opened, start, attrs, m) in open_tags {
        if start < text.len() && (m.is_none() || !attrs.is_empty()) {
            inline.push(InlineTag { start, end: text.len(), name: if m.is_some() { "span".into() } else { opened }, attrs });
        }
    }
    let mut block = Block::new(0, Kind::Paragraph, text);
    block.spans = spans;
    block.meta.inline = inline;
    block
}
