//! Explicit HTML block wrappers (`<p>`, `<div>`, `<center>`, `<h1>`–`<h6>`, `<blockquote>`)
//! reuse the same editable inline model as HTML table cells.
use super::{html::{self, HtmlValue}, inline};
use super::super::model::{Block, HtmlParagraph, InlineTag, Kind};

fn block_tag_kind(name: &str) -> Option<(Kind, Option<u8>)> {
    match name.to_ascii_lowercase().as_str() {
        "p" | "div" | "center" | "section" | "article" | "header" | "footer" => Some((Kind::Paragraph, None)),
        "blockquote" => Some((Kind::Quote, None)),
        "h1" => Some((Kind::H1, None)),
        "h2" => Some((Kind::H2, None)),
        "h3" => Some((Kind::H3, None)),
        "h4" => Some((Kind::H3, Some(4))),
        "h5" => Some((Kind::H3, Some(5))),
        "h6" => Some((Kind::H3, Some(6))),
        _ => None,
    }
}

fn open_block_tag_name(line: &str) -> Option<String> {
    let body = line.trim_start_matches(' ');
    if line.len() - body.len() > 3 || !body.starts_with('<') { return None; }
    let rest = &body[1..];
    let len = rest.bytes().take_while(|b| b.is_ascii_alphanumeric()).count();
    if len == 0 { return None; }
    let tag = rest[..len].to_ascii_lowercase();
    let after = rest.as_bytes().get(len).copied()?;
    if !(after.is_ascii_whitespace() || matches!(after, b'>' | b'/')) { return None; }
    block_tag_kind(&tag).map(|_| tag)
}

pub(super) fn starts(line: &str) -> bool {
    open_block_tag_name(line).is_some()
}

/// Stop at the explicit close, or a Markdown paragraph boundary for malformed HTML.
pub(super) fn line_count(lines: &[&str]) -> usize {
    let Some(target) = lines.first().and_then(|l| open_block_tag_name(l)) else { return 1; };
    let mut source = String::new();
    for (index, line) in lines.iter().enumerate() {
        if index > 0 && line.trim().is_empty() { return index; }
        source.push_str(line);
        if html::tokens(&source).is_some_and(|tokens| {
            let mut depth = 0i32;
            for token in tokens {
                if let HtmlValue::Tag { name, close, empty: false, .. } = token.value
                    && name.eq_ignore_ascii_case(&target)
                {
                    depth += if close { -1 } else { 1 };
                }
            }
            depth <= 0
        }) {
            return index + 1;
        }
    }
    lines.len()
}

pub(super) fn parse(id: u64, source: &str) -> Result<Block, &'static str> {
    let body = source.trim();
    let tokens = html::tokens(body).ok_or("Malformed or unsupported HTML block is retained as source")?;
    let first = tokens.first().ok_or("Empty HTML block source")?;
    let last = tokens.last().ok_or("Incomplete HTML block")?;
    let (tag_name, open_attrs) = match &first.value {
        HtmlValue::Tag { name, attrs, close: false, empty: false } => (name.to_ascii_lowercase(), attrs.clone()),
        _ => return Err("Unsupported HTML wrapper is retained as original HTML"),
    };
    let (kind, heading_level) = block_tag_kind(&tag_name)
        .ok_or("Unsupported HTML wrapper is retained as original HTML")?;
    if !matches!(&last.value, HtmlValue::Tag { name, attrs, close: true, empty: false }
        if name.eq_ignore_ascii_case(&tag_name) && attrs.is_empty()) || first.range.end > last.range.start
    {
        return Err("Unsupported HTML wrapper is retained as original HTML");
    }
    let indent = if tag_name == "p" && open_attrs.len() == 1 && open_attrs[0].0 == "data-indent" {
        Some(open_attrs[0].1.trim().parse::<usize>().ok().filter(|&n| n > 0).map(|n| n.min(12))
            .ok_or("Unsupported paragraph wrapper is retained as original HTML")?)
    } else {
        None
    };
    let leading = source.len() - source.trim_start().len();
    let content = leading + first.range.end..leading + last.range.start;
    let inner = &source[content.clone()];
    let (text, spans, mut annotations) = inline::parse(inner).or_else(|| inline::parse_html(inner))
        .ok_or("Block contains unsupported HTML; the original HTML is retained")?;
    if indent.is_none() && !open_attrs.is_empty() && !text.is_empty() {
        annotations.insert(0, InlineTag { start: 0, end: text.len(), name: "span".into(), attrs: open_attrs });
    }
    let mut block = Block::new(id, kind, text);
    block.spans = spans; block.meta.inline = annotations; block.meta.heading_level = heading_level;
    if let Some(indent) = indent {
        block.meta.indent = indent;
    } else {
        block.meta.html_paragraph = Some(HtmlParagraph { source: source.to_owned(), content });
    }
    Ok(block)
}

pub(super) fn serialize(block: &Block, out: &mut String) {
    if block.meta.indent > 0 {
        use std::fmt::Write;
        let _ = write!(out, "<p data-indent=\"{}\">", block.meta.indent.min(12));
        inline::serialize_html(block, out);
        out.push_str("</p>\n");
        return;
    }
    let paragraph = block.meta.html_paragraph.as_ref().expect("HTML paragraph metadata");
    let original = paragraph.source.get(paragraph.content.clone());
    if original.is_some() {
        let unchanged = parse(block.id, &paragraph.source).is_ok_and(|original|
            original.text == block.text && original.spans == block.spans && original.meta.inline == block.meta.inline);
        if unchanged { out.push_str(&paragraph.source); return; }
        out.push_str(&paragraph.source[..paragraph.content.start]);
        inline::serialize_html(block, out);
        out.push_str(&paragraph.source[paragraph.content.end..]);
    } else {
        out.push_str("<p>"); inline::serialize_html(block, out); out.push_str("</p>\n");
    }
}
