use super::model::{Block, Doc, Kind, Numbering};
#[path = "markdown_inline.rs"] mod inline;
#[path = "markdown_blocks.rs"] mod blocks;

/// Line-oriented editor blocks with contextual fences; opaque syntax retains its bytes.
/// The parser scans source once and bulk-builds the persistent sequence once.
pub fn parse(source: &str) -> Doc {
    let mut result = Vec::new(); let mut lines = source.split_inclusive('\n').peekable();
    while let Some(raw) = lines.next() {
        let line = raw.trim_end_matches(['\r','\n']);
        let id = result.len() as u64 + 1;
        let html = line.trim_start().starts_with('<') && !["<strong>","<em>","<del>","<code>"].iter()
            .any(|tag| line.trim_start().starts_with(tag));
        let setext = lines.peek().is_some_and(|next| {
            let next = next.trim(); !line.is_empty() && !next.is_empty()
                && (next.bytes().all(|b| b == b'=') || next.bytes().all(|b| b == b'-'))
        });
        if html || setext {
            let mut original = raw.to_owned();
            if setext { original.push_str(lines.next().unwrap()); }
            else {
                while lines.peek().is_some_and(|next| !next.trim().is_empty()) {
                    original.push_str(lines.next().unwrap());
                }
            }
            let mut block = Block::new(id,Kind::Paragraph,original.clone());
            block.meta.raw_source = Some(original); result.push(block); continue;
        }
        if let Some((marker, width, info)) = blocks::fence(line) {
            let indent = line.len()-line.trim_start_matches(' ').len();
            let mut body = String::new();
            for next in lines.by_ref() {
                if blocks::closes_fence(next.trim_end_matches(['\r','\n']), marker, width) { break; }
                let strip = next.bytes().take(indent).take_while(|b| *b==b' ').count();
                body.push_str(&next[strip..]);
            }
            // The newline immediately before a closing fence is structural.
            let empty_line = body=="\n" || body=="\r\n";
            if body.ends_with('\n') { body.pop(); if body.ends_with('\r') { body.pop(); } }
            let mut block = Block::new(id, Kind::Paragraph, body);
            block.meta.indent = indent; block.meta.code_empty_line = empty_line;
            block.meta.code_language = Some(info.to_owned()); result.push(block); continue;
        }
        let (kind, body, checked, indent, start, opaque) = blocks::classify(line);
        let nested_fence = (body!=line).then(|| blocks::fence(body)).flatten();
        let indented_code = indent>=4 && !result.last().is_some_and(|b| matches!(b.kind,Kind::Bullet|Kind::Numbered|Kind::Todo));
        if nested_fence.is_some() || (!opaque && blocks::unclosed_inline(body)) || indented_code {
            let mut original = raw.to_owned();
            if let Some((marker,width,_)) = nested_fence {
                for next in lines.by_ref() { original.push_str(next);
                    if blocks::closes_fence(next.trim(),marker,width) { break; }
                }
            } else {
                while lines.peek().is_some_and(|next| !next.trim().is_empty()) { original.push_str(lines.next().unwrap()); }
            }
            let mut block=Block::new(id,Kind::Paragraph,original.clone()); block.meta.raw_source=Some(original);
            result.push(block); continue;
        }
        let mut block = Block::new(id, kind, String::new());
        block.checked = checked; block.meta.indent = indent; block.meta.list_start = start;
        if opaque {
            block.text = raw.to_owned(); block.meta.raw_source = Some(raw.to_owned());
        } else if let Some((text, spans)) = inline::parse(body) {
            block.text = text; block.spans = spans;
        } else {
            block.kind = Kind::Paragraph; block.meta = Default::default();
            block.text = raw.to_owned(); block.meta.raw_source = Some(raw.to_owned());
        }
        result.push(block);
    }
    Doc::from_blocks(result)
}

pub fn serialize(doc: &Doc) -> String {
    let mut out = String::new(); let mut numbering = Numbering::default();
    for block in &doc.blocks {
        let number = numbering.next(block);
        if !out.is_empty() && !out.ends_with('\n') { out.push('\n'); }
        if block.meta.raw_source.is_some() {
            out.push_str(&block.text);
            continue;
        }
        if let Some(language) = &block.meta.code_language {
            let marker = if language.contains('`') { '~' } else { '`' };
            let mut longest = 0; let mut run = 0;
            for ch in block.text.chars() {
                if ch == marker { run += 1; longest = longest.max(run); } else { run = 0; }
            }
            let fence = marker.to_string().repeat(3.max(longest+1));
            let padding = " ".repeat(block.meta.indent.min(3));
            out.push_str(&padding); out.push_str(&fence); out.push_str(language); out.push('\n');
            if !block.text.is_empty() {
                for line in block.text.split('\n') { out.push_str(&padding); out.push_str(line); out.push('\n'); }
            } else if block.meta.code_empty_line { out.push_str(&padding); out.push('\n'); }
            out.push_str(&padding); out.push_str(&fence); out.push('\n'); continue;
        }
        out.push_str(&" ".repeat(block.meta.indent));
        match block.kind {
            Kind::Paragraph => {}, Kind::H1 => out.push_str("# "), Kind::H2 => out.push_str("## "),
            Kind::H3 => out.push_str("### "), Kind::Bullet => out.push_str("- "),
            Kind::Numbered => { use std::fmt::Write; let _ = write!(out,"{}. ",number.unwrap_or(1)); },
            Kind::Quote => out.push_str("> "),
            Kind::Todo => out.push_str(if block.checked { "- [x] " } else { "- [ ] " }),
        }
        inline::serialize(block, &mut out); out.push('\n');
    }
    out
}
