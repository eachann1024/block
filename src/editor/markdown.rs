use super::model::{Block, Doc, Kind, Numbering};
#[path = "markdown_inline.rs"] mod inline;
#[path = "markdown_blocks.rs"] mod blocks;
#[path = "markdown_html.rs"] mod html;
#[path = "markdown_table.rs"] mod table;
#[path = "markdown_table_html.rs"] mod table_html;
#[path = "markdown_paragraph.rs"] mod paragraph;
#[path = "markdown_containers.rs"] mod containers;

pub fn rebuild_table_source(table: &mut super::model::Table) { table::rebuild(table) }
pub(super) fn serialize_table(table: &super::model::Table, out: &mut String) { table::serialize(table, out) }

/// Opaque constructs retain their original bytes and an explicit explanation.
fn raw(id: u64, source: String, reason: &str) -> Block {
    let mut block = Block::new(id, Kind::Paragraph, source.clone());
    block.meta.raw_source = Some(source); block.meta.unsupported_reason = Some(reason.to_owned()); block
}
/// Line-oriented blocks retain stable identities; tables are semantic blocks with rich cells.
pub fn parse(source: &str) -> Doc {
    let lines: Vec<_> = source.split_inclusive('\n').collect();
    let mut result = Vec::new(); let mut index = 0; let mut offset = 0;
    while index < lines.len() {
        let raw_line = lines[index]; let line = raw_line.trim_end_matches(['\r','\n']);
        let id = result.len() as u64 + 1;
        if super::image_source::parse(line).is_some() {
            offset += raw_line.len(); index += 1;
            result.push(raw(id, raw_line.to_owned(), "Image")); continue;
        }
        if containers::starts_columns(line) {
            let end = index + containers::columns_line_count(&lines[index..]);
            let original = lines[index..end].concat();
            let group_id = result.iter().filter_map(|b: &Block| b.meta.column.map(|(g, _)| g)).max().unwrap_or(0) + 1;
            match containers::parse_columns(group_id, &original) {
                Ok(col_blocks) => for mut b in col_blocks {
                    b.id = result.len() as u64 + 1; result.push(b);
                },
                Err(reason) => result.push(raw(id, original.clone(), reason)),
            }
            offset += original.len(); index = end; continue;
        }
        if let Some((block, count, bytes)) = containers::parse_callout(id, &lines[index..]) {
            offset += bytes; index += count; result.push(block); continue;
        }
        if paragraph::starts(line) {
            let end = index + paragraph::line_count(&lines[index..]);
            let original = lines[index..end].concat();
            let block = match paragraph::parse(id, &original) {
                Ok(block) => block, Err(reason) => raw(id, original.clone(), reason),
            };
            offset += original.len(); index = end; result.push(block); continue;
        }
        if line.trim_start().to_ascii_lowercase().starts_with("<table") {
            let mut end = index;
            while end < lines.len() {
                let closing = lines[end].to_ascii_lowercase().contains("</table>"); end += 1;
                if closing { break; }
            }
            let original = lines[index..end].concat();
            let block = match table_html::parse(&original) {
                Ok(table) => Block::from_table(id, table), Err(reason) => raw(id, original.clone(), reason),
            };
            offset += original.len(); index = end; result.push(block); continue;
        }
        let pipe_candidate = line.contains('|') && lines.get(index+1).is_some_and(|next| {
            let next = next.trim(); next.contains('-') && next.chars().all(|c| matches!(c, ' '| '\t'|'-'|':'|'|'))
        });
        if pipe_candidate && let Some((parsed, consumed)) = table::parse_pipe(&source[offset..]) {
            let mut bytes = 0; let mut end = index;
            while end < lines.len() && bytes < consumed { bytes += lines[end].len(); end += 1; }
            result.push(Block::from_table(id, parsed)); offset += bytes; index = end; continue;
        }
        if pipe_candidate {
            let mut end = index + 2;
            while end < lines.len() && lines[end].contains('|') && !lines[end].trim().is_empty() { end += 1; }
            let original = lines[index..end].concat(); offset += original.len(); index = end;
            result.push(raw(id, original, "Unsupported table cell syntax is retained as original Markdown")); continue;
        }
        let setext = lines.get(index+1).and_then(|next| {
            let next = next.trim();
            if line.trim().is_empty() || next.is_empty() { return None; }
            if next.bytes().all(|b| b == b'=') { Some(Kind::H1) }
            else if next.bytes().all(|b| b == b'-') { Some(Kind::H2) } else { None }
        });
        if let Some((marker, width, info)) = blocks::fence(line) {
            let indent = line.len() - line.trim_start_matches(' ').len();
            let mut body = String::new(); let mut end = index + 1;
            while end < lines.len() {
                let next = lines[end]; end += 1;
                if blocks::closes_fence(next.trim_end_matches(['\r','\n']), marker, width) { break; }
                let strip = next.bytes().take(indent).take_while(|b| *b == b' ').count(); body.push_str(&next[strip..]);
            }
            let empty_line = body == "\n" || body == "\r\n";
            if body.ends_with('\n') { body.pop(); if body.ends_with('\r') { body.pop(); } }
            let mut block = Block::new(id, Kind::Paragraph, body);
            block.meta.indent = indent; block.meta.code_empty_line = empty_line;
            block.meta.code_language = Some(info.to_owned()); result.push(block);
            offset += lines[index..end].iter().map(|line| line.len()).sum::<usize>(); index = end; continue;
        }
        let (kind, body, checked, indent, start, opaque) = blocks::classify(line);
        let nested_fence = (body != line).then(|| blocks::fence(body)).flatten();
        let indented_code = indent >= 4 && !matches!(kind, Kind::Bullet|Kind::Numbered|Kind::Todo)
            && !result.last().is_some_and(|b|
            matches!(b.kind, Kind::Bullet|Kind::Numbered|Kind::Todo));
        if nested_fence.is_some() || indented_code {
            let mut end = index + 1;
            if let Some((marker,width,_)) = nested_fence {
                while end < lines.len() {
                    let closes = blocks::closes_fence(lines[end].trim(), marker, width); end += 1;
                    if closes { break; }
                }
            } else { while end < lines.len() && !lines[end].trim().is_empty() { end += 1; } }
            let original = lines[index..end].concat(); offset += original.len(); index = end;
            result.push(raw(id, original, "Nested or indented code is retained as source")); continue;
        }
        let mut block = Block::new(id, setext.unwrap_or(kind), String::new());
        block.checked = checked; block.meta.indent = indent; block.meta.list_start = start;
        let level = line.trim_start().bytes().take_while(|b| *b == b'#').count();
        if (4..=6).contains(&level) && kind == Kind::H3 { block.meta.heading_level = Some(level as u8); }
        if !opaque && let Some((text, spans, annotations)) = inline::parse(body) {
            block.text = text; block.spans = spans; block.meta.inline = annotations;
        } else {
            block = raw(id, raw_line.to_owned(), "Unsupported Markdown or HTML is retained as editable source");
        }
        offset += raw_line.len(); index += 1;
        if setext.is_some() && block.meta.raw_source.is_none() { offset += lines[index].len(); index += 1; }
        result.push(block);
    }
    swallow_separators(&mut result);
    Doc::from_blocks(result)
}

pub fn serialize(doc: &Doc) -> String {
    let mut out = String::new(); let mut numbering = Numbering::default();
    let blocks: Vec<&Block> = doc.blocks.iter().collect();
    let nexts = next_non_blank(&blocks);
    let mut at = 0;
    while at < blocks.len() {
        let block = blocks[at];
        let number = numbering.next(block);
        if !out.is_empty() && !out.ends_with('\n') { out.push('\n'); }
        // Standard Markdown needs a blank line here (see `needs_separator`).
        if at > 0 && needs_separator(blocks[at - 1], nexts[at - 1]) && !out.ends_with("\n\n") { out.push('\n'); }
        if let Some((group_id, _)) = block.meta.column {
            let start = at;
            at += 1;
            while at < blocks.len() && blocks[at].meta.column.map(|(g, _)| g) == Some(group_id) {
                let _ = numbering.next(blocks[at]);
                at += 1;
            }
            containers::serialize_columns(&blocks[start..at], &mut out);
            continue;
        }
        at += 1;
        if let Some(table) = &block.meta.table { table::serialize(table, &mut out); continue; }
        if block.meta.raw_source.is_some() { out.push_str(&block.text); continue; }
        if let Some(callout) = block.meta.callout { containers::serialize_callout(block, callout, &mut out); continue; }
        if block.kind == Kind::Paragraph && block.meta.html_paragraph.is_some() {
            paragraph::serialize(block, &mut out); continue;
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
        if let Some(level) = block.meta.heading_level.filter(|level| (4..=6).contains(level)) {
            out.push_str(&"#".repeat(level as usize)); out.push(' ');
        } else {
            match block.kind {
                Kind::Paragraph => {}, Kind::H1 => out.push_str("# "), Kind::H2 => out.push_str("## "),
                Kind::H3 => out.push_str("### "), Kind::Bullet => out.push_str("- "),
                Kind::Numbered => { use std::fmt::Write; let _ = write!(out, "{}. ", number.unwrap_or(1)); },
                Kind::Quote => out.push_str("> "),
                Kind::Todo => out.push_str(if block.checked { "- [x] " } else { "- [ ] " }),
            }
        }
        inline::serialize(block, &mut out); out.push('\n');
    }
    out
}

// Blank source lines are empty paragraph blocks, so a separator blank line that is required by
// standard Markdown (paragraph/paragraph, paragraph/pipe table, pipe table/anything) must not
// become a block: serialize adds exactly one blank line and parse swallows exactly one.
fn is_plain(block: &Block) -> bool {
    block.meta.column.is_none() && block.meta.table.is_none() && block.meta.raw_source.is_none()
        && block.meta.html_paragraph.is_none() && block.meta.code_language.is_none()
}
fn is_blank(block: &Block) -> bool { is_plain(block) && block.kind == Kind::Paragraph && block.text.is_empty() }
fn is_text(block: &Block) -> bool { is_plain(block) && block.kind == Kind::Paragraph && !block.text.is_empty() }
fn is_pipe(block: &Block) -> bool {
    block.meta.table.as_ref().is_some_and(|table| table.format == super::model::TableFormat::Pipe)
}
fn is_image(block: &Block) -> bool {
    block.meta.unsupported_reason.as_deref() == Some("Image") || super::image_source::spec_of(block).is_some()
}
fn next_non_blank<'a>(blocks: &[&'a Block]) -> Vec<Option<&'a Block>> {
    let mut out = vec![None; blocks.len()]; let mut seen = None;
    for i in (0..blocks.len()).rev() {
        out[i] = seen;
        if !is_blank(blocks[i]) { seen = Some(blocks[i]); }
    }
    out
}
/// Whether the first non-blank block after `this` must be separated from `this` by a blank line.
fn needs_separator(this: &Block, next: Option<&Block>) -> bool {
    required_separator(this, next) || (optional_separator(this, next) && !this.meta.tight)
}
fn required_separator(this: &Block, next: Option<&Block>) -> bool {
    if this.meta.callout.is_some() && next.is_some_and(|n| (n.kind == Kind::Quote && n.meta.callout.is_none()) || is_text(n)) {
        return true;
    }
    let pipe = is_pipe(this);
    // Text glued under an HTML image block would be re-read as part of that HTML block.
    let image = is_image(this);
    if !pipe && !image && !is_text(this) { return false; }
    // A block glued to a pipe table would be re-read as one more table row.
    next.is_some_and(|next| pipe || is_text(next) || is_pipe(next) || (image && !is_image(next)))
}
fn is_list(block: &Block) -> bool { is_plain(block) && matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) }
/// Conventional (not required) blank line after text, headings, quotes, code, dividers/source and a list's last item,
/// so it is not read back as an empty paragraph; `meta.tight` keeps sources that omitted it.
fn optional_separator(this: &Block, next: Option<&Block>) -> bool {
    if is_blank(this) { return false; }
    let col_end = this.meta.column.is_some_and(|(g, _)| next.and_then(|n| n.meta.column).map(|(ng, _)| ng) != Some(g));
    let source = col_end || (this.meta.column.is_none() && this.meta.table.is_none()
        && (this.meta.code_language.is_some()
            || this.meta.raw_source.as_ref().is_some_and(|raw| raw.ends_with('\n'))));
    if !(is_plain(this) || source) { return false; }
    next.is_some_and(|next| !(is_list(this) && is_list(next)))
}
fn swallow_separators(blocks: &mut Vec<Block>) {
    let (tight, drop): (Vec<bool>, Vec<bool>) = {
        let refs: Vec<&Block> = blocks.iter().collect();
        let nexts = next_non_blank(&refs);
        let tight: Vec<bool> = (0..refs.len()).map(|at| at + 1 < refs.len() && !is_blank(refs[at + 1])
            && optional_separator(refs[at], nexts[at]) && !required_separator(refs[at], nexts[at])).collect();
        let mut drop = vec![false; refs.len()];
        for at in 0..refs.len().saturating_sub(1) {
            if is_blank(refs[at + 1]) && (required_separator(refs[at], nexts[at])
                || (optional_separator(refs[at], nexts[at]) && !tight[at])) {
                drop[at + 1] = true;
            }
        }
        (tight, drop)
    };
    for (block, tight) in blocks.iter_mut().zip(tight) { block.meta.tight = tight; }
    let mut flags = drop.into_iter();
    blocks.retain(|_| !flags.next().unwrap_or(false));
}
