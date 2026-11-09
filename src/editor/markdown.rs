use super::model::{Block, Doc, Kind, Numbering};
#[path = "markdown_inline.rs"] mod inline;
#[path = "markdown_blocks.rs"] mod blocks;
#[path = "markdown_html.rs"] mod html;
#[path = "markdown_table.rs"] mod table;
#[path = "markdown_table_html.rs"] mod table_html;
#[path = "markdown_paragraph.rs"] mod paragraph;
#[path = "markdown_containers.rs"] mod containers;
#[path = "markdown_raw_guard.rs"] mod raw_guard;
#[path = "markdown_boundaries.rs"] mod boundaries;
#[path = "markdown_list.rs"] mod list;

/// Visible text runs, with their ranges in the editor block. Raw HTML scaffolding
/// is excluded; code remains literal, and semantic table text is already projected.
pub(crate) fn search_runs(block: &Block) -> Vec<(std::ops::Range<usize>, String)> {
    if block.meta.raw_source.is_some() && block.text.trim_start().starts_with('<') {
        if let Some(tokens) = html::tokens(&block.text) {
            let mut hidden = false;
            return tokens.into_iter().filter_map(|token| match token.value {
                html::HtmlValue::Tag { name, close, .. } => {
                    if matches!(name.as_str(), "script" | "style") { hidden = !close; }
                    None
                }
                html::HtmlValue::Text(text) if !hidden => Some((token.range, text)),
                _ => None,
            }).collect();
        }
        return Vec::new();
    }
    vec![(0..block.text.len(), block.text.clone())]
}

pub fn strip_frontmatter(source: &str) -> &str { blocks::strip_frontmatter(source) }
pub fn rebuild_table_source(table: &mut super::model::Table) { table::rebuild(table) }
pub(super) fn serialize_table(table: &super::model::Table, out: &mut String) { table::serialize(table, out) }

/// Opaque constructs retain their original bytes and an explicit explanation.
fn raw(id: u64, source: String, reason: &str) -> Block {
    let mut block = Block::new(id, Kind::Paragraph, source.clone());
    block.meta.raw_source = Some(source); block.meta.unsupported_reason = Some(reason.to_owned()); block
}
/// Line-oriented blocks retain stable identities; tables are semantic blocks with rich cells.
pub fn parse(source: &str) -> Doc {
    let decoded = raw_guard::decode(source);
    let source: &str = decoded.strip_prefix('\u{feff}').unwrap_or(&decoded);
    let lines: Vec<_> = source.split_inclusive('\n').collect();
    let mut result = Vec::new(); let mut index = 0; let mut offset = 0;
    while index < lines.len() {
        let raw_line = lines[index]; let line = raw_line.trim_end_matches(['\r','\n']);
        let id = result.len() as u64 + 1;
        if index == 0 && let Some((block, count, bytes)) = blocks::parse_frontmatter(id, &lines) {
            offset += bytes; index += count; result.push(block); continue;
        }
        if super::image_source::parse(line).is_some() {
            offset += raw_line.len(); index += 1;
            result.push(raw(id, raw_line.to_owned(), "Image")); continue;
        }
        if let Some((block, count, bytes)) = containers::parse_callout(id, &lines[index..]) {
            offset += bytes; index += count; result.push(block); continue;
        }
        if let Some((items, count)) = list::children(id, &lines[index..]).or_else(|| list::parse(id, &lines[index..])) {
            offset += lines[index..index + count].iter().map(|line| line.len()).sum::<usize>();
            index += count; result.extend(items); continue;
        }
        if let Some(count) = boundaries::unsupported(&lines[index..]) {
            let original = lines[index..index + count].concat();
            offset += original.len(); index += count;
            result.push(raw(id, original, "Unsupported construct is retained as original Markdown")); continue;
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
            let end = index + boundaries::table_lines(&lines[index..]);
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
        if let Some((indent, math)) = blocks::inline_math_block(line) {
            let mut block = Block::new(id, Kind::Paragraph, math.to_owned());
            block.meta.indent = indent; block.meta.code_language = Some("math".to_owned());
            result.push(block); offset += raw_line.len(); index += 1; continue;
        }
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
    let mut list_padding = None;
    let mut at = 0;
    while at < blocks.len() {
        let block = blocks[at];
        // Serialize descendants at local depth, then indent every line, including
        // opaque images, tables and fences. This keeps standard Markdown nesting.
        if at > 0 && is_list(blocks[at - 1]) && block.meta.indent > blocks[at - 1].meta.indent {
            let parent = blocks[at - 1];
            let padding = parent.meta.indent + if parent.kind == Kind::Numbered {
                doc.list_number(parent.id).unwrap_or(1).to_string().len() + 2
            } else { 2 };
            let end = (at..blocks.len()).find(|&i| blocks[i].meta.indent < padding).unwrap_or(blocks.len());
            if end > at {
                let children = blocks[at..end].iter().map(|block| {
                    let mut child = (*block).clone();
                    child.meta.indent -= padding;
                    if let Some(code) = &mut child.meta.list_code { code.parent_indent = code.parent_indent.saturating_sub(padding); }
                    child
                }).collect();
                if !out.ends_with("\n\n") { out.push('\n'); }
                let source = serialize(&Doc::from_blocks(children));
                for line in source.split_inclusive('\n') {
                    out.push_str(&" ".repeat(padding)); out.push_str(line);
                }
                at = end;
                continue;
            }
        }
        let number = numbering.next(block);
        let same_line = at > 0 && same_line_list_code(blocks[at - 1], block);
        if !same_line && !out.is_empty() && !out.ends_with('\n') { out.push('\n'); }
        // Standard Markdown needs a blank line here (see `needs_separator`).
        if at > 0 && (if let Some(code) = &block.meta.list_code { code.blank_before }
            else if blocks[at - 1].meta.list_code.is_some() && is_list(block) { false }
            else { needs_separator(blocks[at - 1], nexts[at - 1]) }) && !out.ends_with("\n\n") { out.push('\n'); }
        let is_first = at == 0;
        at += 1;
        if let Some(table) = &block.meta.table { table::serialize(table, &mut out); continue; }
        if block.meta.raw_source.is_some() { out.push_str(&block.text); continue; }
        if let Some(callout) = block.meta.callout { containers::serialize_callout(block, callout, &mut out); continue; }
        if let Some(language) = &block.meta.code_language {
            if blocks::serialize_frontmatter(block, is_first, &mut out) { continue; }
            let language = if language == "yaml-frontmatter" { "yaml" } else { language.as_str() };
            let marker = if language.contains('`') { '~' } else { '`' };
            let mut longest = 0; let mut run = 0;
            for ch in block.text.chars() {
                if ch == marker { run += 1; longest = longest.max(run); } else { run = 0; }
            }
            let fence = marker.to_string().repeat(3.max(longest+1));
            let padding = if let Some(code) = &block.meta.list_code {
                " ".repeat(list_padding.filter(|(indent, _)| *indent == code.parent_indent)
                    .map_or(code.parent_indent + 2, |(_, width)| width) + code.extra_indent)
            } else { " ".repeat(block.meta.indent.min(3)) };
            if !same_line { out.push_str(&padding); }
            out.push_str(&fence); out.push_str(language); out.push('\n');
            if !block.text.is_empty() {
                for line in block.text.split('\n') { out.push_str(&padding); out.push_str(line); out.push('\n'); }
            } else if block.meta.code_empty_line { out.push_str(&padding); out.push('\n'); }
            out.push_str(&padding); out.push_str(&fence); out.push('\n'); continue;
        }
        if block.meta.html_paragraph.is_some() || (block.kind == Kind::Paragraph && block.meta.indent > 0) {
            paragraph::serialize(block, &mut out); continue;
        }
        list_padding = match block.kind {
            Kind::Bullet | Kind::Todo => Some((block.meta.indent, block.meta.indent + 2)),
            Kind::Numbered => Some((block.meta.indent, block.meta.indent + number.unwrap_or(1).to_string().len() + 2)),
            _ => None,
        };
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
        inline::serialize(block, &mut out);
        if !blocks.get(at).is_some_and(|next| same_line_list_code(block, next)) {
            out.push('\n');
        }
    }
    out
}

// Blank source lines are empty paragraph blocks, so a separator blank line that is required by
// standard Markdown (paragraph/paragraph, paragraph/pipe table, pipe table/anything) must not
// become a block: serialize adds exactly one blank line and parse swallows exactly one.
fn is_plain(block: &Block) -> bool {
    block.meta.table.is_none() && block.meta.raw_source.is_none()
        && block.meta.html_paragraph.is_none() && block.meta.code_language.is_none()
}
fn is_blank(block: &Block) -> bool { is_plain(block) && block.kind == Kind::Paragraph && block.meta.indent == 0 && block.text.is_empty() }
fn is_text(block: &Block) -> bool { is_plain(block) && block.kind == Kind::Paragraph && block.meta.indent == 0 && !block.text.is_empty() }
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
fn same_line_list_code(parent: &Block, child: &Block) -> bool {
    is_list(parent) && parent.text.is_empty() && child.meta.code_language.is_some()
        && child.meta.list_code.as_ref().is_some_and(|code| code.same_line && code.parent_indent == parent.meta.indent)
}
/// Conventional (not required) blank line after text, headings, quotes, code, dividers/source and a list's last item,
/// so it is not read back as an empty paragraph; `meta.tight` keeps sources that omitted it.
fn optional_separator(this: &Block, next: Option<&Block>) -> bool {
    if is_blank(this) { return false; }
    let source = this.meta.table.is_none()
        && (this.meta.code_language.is_some()
            || this.meta.raw_source.as_ref().is_some_and(|raw| raw.ends_with('\n')));
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
