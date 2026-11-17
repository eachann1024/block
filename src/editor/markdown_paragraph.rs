//! HTML block wrappers (`<p>`, `<div>`, `<center>`, `<details>`, `<h1>`–`<h6>`, `<blockquote>`)
//! and raw HTML blocks (`<pre>`, `<!-- ... -->`, `<svg>`, `<style>`, `<script>`).
use super::{html::{self, HtmlValue}, inline};
use super::super::model::{Block, HtmlParagraph, InlineTag, Kind};

fn block_tag_kind(name: &str) -> Option<(Kind, Option<u8>)> {
    match name.to_ascii_lowercase().as_str() {
        "p" | "div" | "center" | "section" | "article" | "header" | "footer"
        | "main" | "aside" | "nav" | "figure" | "figcaption" | "dl" | "ul" | "ol" => Some((Kind::Paragraph, None)),
        "blockquote" | "details" => Some((Kind::Quote, None)),
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
    let close_needle = format!("</{target}>");
    let open_needle = format!("<{target}");
    let mut depth = 0i32;
    for (index, line) in lines.iter().enumerate() {
        if index > 0 && line.trim().is_empty() && target != "details" { return index; }
        let lower = line.to_ascii_lowercase();
        let mut rest = lower.as_str();
        while let Some(at) = rest.find('<') {
            rest = &rest[at..];
            if rest.starts_with(&close_needle) {
                depth -= 1;
                rest = &rest[close_needle.len()..];
                if depth <= 0 { return index + 1; }
            } else if let Some(after) = rest.strip_prefix(&open_needle)
                && after.starts_with(['>', '/', ' ', '\t', '\r', '\n'])
            {
                if !after.split('>').next().unwrap_or("").ends_with('/') { depth += 1; }
                rest = &rest[open_needle.len()..];
            } else {
                rest = &rest[1..];
            }
        }
        if depth <= 0 { return index + 1; }
    }
    lines.len()
}

fn parse_strict(id: u64, source: &str) -> Option<Block> {
    let body = source.trim();
    let tokens = html::tokens(body)?;
    let first = tokens.first()?;
    let last = tokens.last()?;
    let (tag_name, open_attrs) = match &first.value {
        HtmlValue::Tag { name, attrs, close: false, empty: false } => (name.to_ascii_lowercase(), attrs.clone()),
        _ => return None,
    };
    if matches!(tag_name.as_str(), "details" | "ul" | "ol" | "dl") { return None; }
    let (kind, heading_level) = block_tag_kind(&tag_name)?;
    if !matches!(&last.value, HtmlValue::Tag { name, attrs, close: true, empty: false }
        if name.eq_ignore_ascii_case(&tag_name) && attrs.is_empty()) || first.range.end > last.range.start
    {
        return None;
    }
    let indent = if tag_name == "p" && open_attrs.len() == 1 && open_attrs[0].0 == "data-indent" {
        Some(open_attrs[0].1.trim().parse::<usize>().ok().filter(|&n| n > 0)?.min(12))
    } else {
        None
    };
    let leading = source.len() - source.trim_start().len();
    let content = leading + first.range.end..leading + last.range.start;
    let inner = &source[content.clone()];
    let (text, spans, mut annotations) = inline::parse_html(inner)?;
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
    Some(block)
}

pub(super) fn parse(id: u64, source: &str) -> Block {
    if let Some(block) = parse_strict(id, source) {
        return block;
    }
    let (kind, heading_level) = open_block_tag_name(source)
        .and_then(|tag| block_tag_kind(&tag))
        .unwrap_or((Kind::Paragraph, None));
    let (text, spans, tags) = inline::parse_html_permissive(source, true);
    let mut block = Block::new(id, kind, text);
    block.spans = spans;
    block.meta.inline = tags;
    block.meta.heading_level = heading_level;
    block.seal_clean_source(source.to_owned(), None);
    block
}

pub(super) fn parse_raw_html_block(id: u64, source: &str) -> Block {
    let trimmed = source.trim();
    let lower = trimmed.to_ascii_lowercase();
    if lower.starts_with("<pre") {
        let (mut text, _, _) = inline::parse_html_permissive(trimmed, false);
        if text.ends_with('\n') { text.pop(); if text.ends_with('\r') { text.pop(); } }
        let mut block = Block::new(id, Kind::Paragraph, text);
        block.meta.code_language = Some(String::new());
        block.seal_clean_source(source.to_owned(), None);
        return block;
    }
    let mut body = source.to_owned();
    if body.ends_with('\n') { body.pop(); if body.ends_with('\r') { body.pop(); } }
    let mut block = Block::new(id, Kind::Paragraph, body);
    block.meta.code_language = Some("html".to_owned());
    block.seal_clean_source(source.to_owned(), None);
    block
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
        let unchanged = parse_strict(block.id, &paragraph.source).is_some_and(|original|
            original.text == block.text && original.spans == block.spans && original.meta.inline == block.meta.inline);
        if unchanged { out.push_str(&paragraph.source); return; }
        out.push_str(&paragraph.source[..paragraph.content.start]);
        inline::serialize_html(block, out);
        out.push_str(&paragraph.source[paragraph.content.end..]);
    } else {
        out.push_str("<p>"); inline::serialize_html(block, out); out.push_str("</p>\n");
    }
}
