//! Explicit HTML paragraphs reuse the same editable inline model as HTML table cells.
use super::{html::{self, HtmlValue}, inline};
use super::super::model::{Block, HtmlParagraph, Kind};

pub(super) fn starts(line: &str) -> bool {
    let body = line.trim_start_matches(' ');
    if line.len() - body.len() > 3 { return false; }
    let bytes = body.as_bytes();
    bytes.first() == Some(&b'<') && bytes.get(1).is_some_and(|b| b.eq_ignore_ascii_case(&b'p'))
        && bytes.get(2).is_some_and(|b| b.is_ascii_whitespace() || matches!(b, b'>'|b'/'))
}

/// Stop at the explicit close, or a Markdown paragraph boundary for malformed HTML.
pub(super) fn line_count(lines: &[&str]) -> usize {
    let mut source = String::new();
    for (index, line) in lines.iter().enumerate() {
        if index > 0 && line.trim().is_empty() { return index; }
        source.push_str(line);
        if html::tokens(&source).is_some_and(|tokens| tokens.iter().any(|token|
            matches!(&token.value, HtmlValue::Tag { name, close:true, .. } if name == "p"))) {
            return index + 1;
        }
    }
    lines.len()
}

pub(super) fn parse(id: u64, source: &str) -> Result<Block, &'static str> {
    let body = source.trim();
    let tokens = html::tokens(body).ok_or("Malformed or unsupported HTML paragraph is retained as source")?;
    let first = tokens.first().ok_or("Empty HTML paragraph source")?;
    let last = tokens.last().ok_or("Incomplete HTML paragraph")?;
    // Paragraph-level attributes need a block-style model, so retain them as raw HTML.
    if !matches!(&first.value, HtmlValue::Tag { name, attrs, close:false, empty:false }
        if name == "p" && attrs.is_empty())
        || !matches!(&last.value, HtmlValue::Tag { name, attrs, close:true, empty:false }
            if name == "p" && attrs.is_empty()) || first.range.end > last.range.start {
        return Err("Unsupported paragraph wrapper is retained as original HTML");
    }
    let leading = source.len() - source.trim_start().len();
    let content = leading + first.range.end..leading + last.range.start;
    let (text, spans, annotations) = inline::parse_html(&source[content.clone()])
        .ok_or("Paragraph contains unsupported HTML; the original paragraph is retained")?;
    let mut block = Block::new(id, Kind::Paragraph, text);
    block.spans = spans; block.meta.inline = annotations;
    block.meta.html_paragraph = Some(HtmlParagraph { source:source.to_owned(), content });
    Ok(block)
}

pub(super) fn serialize(block: &Block, out: &mut String) {
    let paragraph = block.meta.html_paragraph.as_ref().expect("HTML paragraph metadata");
    let original = paragraph.source.get(paragraph.content.clone());
    if let Some(content) = original {
        let unchanged = inline::parse_html(content).is_some_and(|(text, spans, annotations)|
            text == block.text && spans == block.spans && annotations == block.meta.inline);
        if unchanged { out.push_str(&paragraph.source); return; }
        out.push_str(&paragraph.source[..paragraph.content.start]);
        inline::serialize(block, out);
        out.push_str(&paragraph.source[paragraph.content.end..]);
    } else {
        out.push_str("<p>"); inline::serialize(block, out); out.push_str("</p>\n");
    }
}
