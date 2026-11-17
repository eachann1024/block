use super::model::{Block, Doc, Kind, Numbering};
#[path = "markdown_render.rs"] pub(crate) mod render;
#[path = "markdown_inline.rs"] mod inline;
#[path = "markdown_write.rs"] mod write;
#[path = "markdown_blocks.rs"] mod blocks;
#[path = "markdown_html.rs"] mod html;
#[path = "markdown_table.rs"] mod table;
#[path = "markdown_table_html.rs"] mod table_html;
#[path = "markdown_paragraph.rs"] mod paragraph;
#[path = "markdown_containers.rs"] mod containers;
#[path = "markdown_raw_guard.rs"] mod raw_guard;
#[path = "markdown_boundaries.rs"] mod boundaries;
#[path = "markdown_list.rs"] mod list;

/// Visible text runs, with their ranges in the editor block. Atomic dividers/images
/// have no inline text runs; code remains literal, and semantic table text is projected.
pub(crate) fn search_runs(block: &Block) -> Vec<(std::ops::Range<usize>, String)> {
    if block.is_atomic() { return Vec::new(); }
    vec![(0..block.text.len(), block.text.clone())]
}

pub fn strip_frontmatter(source: &str) -> &str { blocks::strip_frontmatter(source) }
pub fn rebuild_table_source(table: &mut super::model::Table) { table::rebuild(table) }

/// Total Notion-style Markdown normalization: every construct becomes a native editable
/// visual block; untouched blocks keep `clean_source` for lossless disk round-trip.
pub fn parse(source: &str) -> Doc {
    let decoded = raw_guard::decode(source);
    let source: &str = decoded.strip_prefix('\u{feff}').unwrap_or(&decoded);
    let (defs, used_labels) = blocks::collect_ref_defs(source);
    parse_inner(source, &defs, &used_labels)
}

pub(super) fn parse_with_defs(source: &str, defs: &str) -> Doc {
    parse_inner(source, defs, &std::collections::HashSet::new())
}

fn parse_inner(source: &str, defs: &str, used_labels: &std::collections::HashSet<String>) -> Doc {
    let lines: Vec<_> = source.split_inclusive('\n').collect();
    let mut result: Vec<Block> = Vec::new();
    let mut index = 0usize;
    let mut offset = 0usize;
    while index < lines.len() {
        let raw_line = lines[index];
        let line = raw_line.trim_end_matches(['\r', '\n']);
        let id = result.len() as u64 + 1;

        if index == 0 && let Some((block, count, bytes)) = blocks::parse_frontmatter(id, &lines) {
            offset += bytes; index += count; result.push(block); continue;
        }
        if blocks::is_empty_block_comment(line) {
            let mut block = Block::new(id, Kind::Paragraph, String::new());
            block.seal_clean_source(raw_line.to_owned(), None);
            offset += raw_line.len(); index += 1; result.push(block); continue;
        }
        if line.trim().is_empty() {
            offset += raw_line.len(); index += 1;
            result.push(Block::new(id, Kind::Paragraph, String::new())); continue;
        }
        if blocks::is_thematic_break(line) {
            let mut block = Block::divider_with_source(id, raw_line);
            block.seal_clean_source(raw_line.to_owned(), None);
            offset += raw_line.len(); index += 1; result.push(block); continue;
        }
        if super::image_source::parse(line).is_some() {
            let mut block = Block::image(id, raw_line.to_owned());
            block.seal_clean_source(raw_line.to_owned(), None);
            offset += raw_line.len(); index += 1; result.push(block); continue;
        }
        if let Some(count) = blocks::consumed_ref_def_lines(&lines[index..], used_labels) {
            offset += lines[index..index + count].iter().map(|l| l.len()).sum::<usize>();
            index += count; continue;
        }
        if let Some((block, count, bytes)) = containers::parse_callout(id, &lines[index..], defs) {
            offset += bytes; index += count; result.push(block); continue;
        }
        if let Some((items, count)) = containers::parse_blockquote(id, &lines[index..], defs) {
            offset += lines[index..index + count].iter().map(|l| l.len()).sum::<usize>();
            index += count; result.extend(items); continue;
        }
        if let Some((items, count)) = list::parse_item(id, &lines[index..], defs) {
            offset += lines[index..index + count].iter().map(|l| l.len()).sum::<usize>();
            index += count; result.extend(items); continue;
        }
        if let Some((block, count)) = containers::parse_custom_container(id, &lines[index..], defs) {
            offset += lines[index..index + count].iter().map(|l| l.len()).sum::<usize>();
            index += count; result.push(block); continue;
        }
        if line.trim_start().to_ascii_lowercase().starts_with("<table") {
            let end = index + boundaries::table_lines(&lines[index..]);
            let original = lines[index..end].concat();
            let block = match table_html::parse(&original) {
                Some(parsed) => Block::from_table(id, parsed),
                None => paragraph::parse_raw_html_block(id, &original),
            };
            offset += original.len(); index = end; result.push(block); continue;
        }
        if let Some(count) = boundaries::raw_html_lines(&lines[index..]) {
            let original = lines[index..index + count].concat();
            let block = paragraph::parse_raw_html_block(id, &original);
            offset += original.len(); index += count; result.push(block); continue;
        }
        if paragraph::starts(line) {
            let end = index + paragraph::line_count(&lines[index..]);
            let original = lines[index..end].concat();
            let block = paragraph::parse(id, &original);
            offset += original.len(); index = end; result.push(block); continue;
        }
        let pipe_candidate = line.contains('|') && lines.get(index + 1).is_some_and(|next| {
            let next = next.trim();
            next.contains('-') && next.chars().all(|c| matches!(c, ' ' | '\t' | '-' | ':' | '|'))
        });
        if pipe_candidate && let Some((parsed, consumed)) = table::parse_pipe(&source[offset..], defs) {
            let mut bytes = 0; let mut end = index;
            while end < lines.len() && bytes < consumed { bytes += lines[end].len(); end += 1; }
            result.push(Block::from_table(id, parsed)); offset += bytes; index = end; continue;
        }
        if let Some((indent, math)) = blocks::inline_math_block(line) {
            let mut block = Block::new(id, Kind::Paragraph, math.to_owned());
            block.meta.indent = indent.min(3);
            block.meta.code_language = Some("math".to_owned());
            block.seal_clean_source(raw_line.to_owned(), None);
            result.push(block); offset += raw_line.len(); index += 1; continue;
        }
        if let Some((indent_cols, _, marker, width, info)) = blocks::fence_any_indent(line) {
            let mut body = String::new();
            let mut end = index + 1;
            while end < lines.len() {
                let next = lines[end]; end += 1;
                if blocks::closes_fence_any_indent(next.trim_end_matches(['\r', '\n']), marker, width) { break; }
                body.push_str(&list::strip_columns(next, indent_cols));
            }
            let original = lines[index..end].concat();
            let empty_line = body == "\n" || body == "\r\n";
            if body.ends_with('\n') { body.pop(); if body.ends_with('\r') { body.pop(); } }
            let mut block = Block::new(id, Kind::Paragraph, body);
            block.meta.indent = indent_cols.min(3);
            block.meta.code_empty_line = empty_line;
            block.meta.code_language = Some(info.to_owned());
            block.seal_clean_source(original.clone(), None);
            result.push(block); offset += original.len(); index = end; continue;
        }
        let (indent_cols, body_trimmed) = blocks::leading_columns(line);
        let prev_is_prose = result.last().is_some_and(|b|
            !is_blank(b) && matches!(b.kind, Kind::Paragraph | Kind::Bullet | Kind::Numbered | Kind::Todo | Kind::Quote)
            && b.meta.code_language.is_none() && b.meta.table.is_none() && !b.is_atomic());
        if indent_cols >= 4 && !prev_is_prose {
            let mut end = index + 1;
            while end < lines.len() {
                let l = lines[end].trim_end_matches(['\r', '\n']);
                if !l.trim().is_empty() && blocks::leading_columns(l).0 < 4 { break; }
                end += 1;
            }
            let content_end = (index..end).rfind(|&i| !lines[i].trim().is_empty()).map_or(index + 1, |i| i + 1);
            let original = lines[index..content_end].concat();
            let mut body = String::new();
            for raw in &lines[index..content_end] {
                if raw.trim().is_empty() { body.push('\n'); }
                else { body.push_str(&list::strip_columns(raw, 4)); }
            }
            let empty_line = body == "\n" || body == "\r\n";
            if body.ends_with('\n') { body.pop(); if body.ends_with('\r') { body.pop(); } }
            let mut block = Block::new(id, Kind::Paragraph, body);
            block.meta.code_empty_line = empty_line;
            block.meta.code_language = Some(String::new());
            block.seal_clean_source(original.clone(), None);
            result.push(block); offset += original.len(); index = content_end; continue;
        }
        if let Some((h_kind, h_level, h_text)) = blocks::classify_heading(line) {
            let (text, spans, annotations) = inline::parse_with_defs(h_text, defs);
            let mut block = Block::new(id, h_kind, text);
            block.meta.heading_level = h_level;
            block.spans = spans; block.meta.inline = annotations;
            if defs.is_empty() || !h_text.contains('[') {
                block.seal_clean_source(raw_line.to_owned(), None);
            }
            offset += raw_line.len(); index += 1; result.push(block); continue;
        }
        let setext = (indent_cols <= 3).then(|| lines.get(index + 1)).flatten().and_then(|next| {
            let next_line = next.trim_end_matches(['\r', '\n']);
            let next_body = next_line.trim_start_matches(' ');
            if next_line.len() - next_body.len() > 3 { return None; }
            let next_trimmed = next_body.trim_end();
            if next_trimmed.is_empty() { return None; }
            if next_trimmed.bytes().all(|b| b == b'=') { Some(Kind::H1) }
            else if next_trimmed.bytes().all(|b| b == b'-') { Some(Kind::H2) }
            else { None }
        });
        let (text, spans, annotations) = inline::parse_with_defs(body_trimmed, defs);
        let mut block = Block::new(id, setext.unwrap_or(Kind::Paragraph), text);
        block.spans = spans;
        block.meta.inline = annotations;
        if setext.is_some() {
            let slice = lines[index..index + 2].concat();
            if defs.is_empty() || !body_trimmed.contains('[') {
                block.seal_clean_source(slice.clone(), None);
            }
            offset += slice.len(); index += 2;
        } else {
            if indent_cols > 0 { block.meta.indent = indent_cols.min(12); }
            if defs.is_empty() || !body_trimmed.contains('[') {
                block.seal_clean_source(raw_line.to_owned(), None);
            }
            offset += raw_line.len(); index += 1;
        }
        result.push(block);
    }

    swallow_separators(&mut result);
    let mut numbering = Numbering::default();
    let mut after_blank = false;
    for block in &mut result {
        if is_blank(block) { after_blank = true; continue; }
        numbering.normalize_imported_start(block, after_blank);
        after_blank = false;
    }
    // Stamp final computed list numbers onto `clean_source` so renumbering invalidates stale prefixes.
    let mut check_num = Numbering::default();
    for block in &mut result {
        let num = check_num.next(block);
        if let Some(clean) = block.meta.clean_source.take() {
            block.seal_clean_source(clean.source, num);
        }
    }
    Doc::from_blocks(result)
}

fn can_multiline_list(block: &Block) -> bool {
    if !block.text.contains('\n') { return false; }
    let mut lines = block.text.split('\n');
    let Some(first) = lines.next() else { return false };
    if first.trim().is_empty() { return false; }
    lines.all(|l| {
        !l.trim().is_empty()
            && !blocks::is_thematic_break(l)
            && blocks::classify_list_marker(l).is_none()
            && blocks::fence_any_indent(l).is_none()
            && blocks::inline_math_block(l).is_none()
            && blocks::classify_heading(l.trim_start()).is_none()
            && !l.trim_start().starts_with(['>', '<', '|'])
            && !l.trim_start().starts_with(":::")
            && super::image_source::parse(l.trim_start()).is_none()
    })
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
        // images, dividers, tables and fences. This keeps standard Markdown nesting.
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
                if !out.is_empty() && !out.ends_with('\n') { out.push('\n'); }
                if needs_separator(parent, nexts[at - 1]) && !out.ends_with("\n\n") { out.push('\n'); }
                let source = serialize(&Doc::from_blocks(children));
                for line in source.split_inclusive('\n') {
                    if line.trim().is_empty() { out.push('\n'); }
                    else { out.push_str(&" ".repeat(padding)); out.push_str(line); }
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

        if is_list(block) {
            list_padding = Some((block.meta.indent, block.meta.indent + match block.kind {
                Kind::Numbered => number.unwrap_or(1).to_string().len() + 2,
                _ => 2,
            }));
        } else if block.meta.list_code.is_none() {
            list_padding = None;
        }

        if let Some(table) = &block.meta.table { table::serialize(table, &mut out); continue; }
        if !same_line && !blocks.get(at).is_some_and(|next| same_line_list_code(block, next))
            && let Some(clean) = block.clean_source_for(number)
        {
            out.push_str(clean);
            continue;
        }
        if block.meta.divider {
            out.push_str(&" ".repeat(block.meta.indent));
            out.push_str("***\n");
            continue;
        }
        if let Some(image_src) = &block.meta.image {
            out.push_str(image_src);
            if !image_src.ends_with('\n') { out.push('\n'); }
            continue;
        }
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
        if block.kind == Kind::Quote {
            containers::serialize_quote(block, &mut out); continue;
        }
        if is_list(block) && can_multiline_list(block) {
            let cont_pad = list_padding.map_or(block.meta.indent + 2, |(_, w)| w);
            let mut start = 0usize;
            for (idx, part) in block.text.split('\n').enumerate() {
                let end = start + part.len();
                let mut slice = block.slice(start..end);
                slice.kind = Kind::Paragraph;
                if idx == 0 {
                    out.push_str(&" ".repeat(block.meta.indent));
                    match block.kind {
                        Kind::Bullet => out.push_str("- "),
                        Kind::Numbered => { use std::fmt::Write; let _ = write!(out, "{}. ", number.unwrap_or(1)); }
                        Kind::Todo => out.push_str(if block.checked { "- [x] " } else { "- [ ] " }),
                        _ => {}
                    }
                } else {
                    out.push_str(&" ".repeat(cont_pad));
                }
                inline::serialize(&slice, &mut out);
                out.push('\n');
                start = end + 1;
            }
            continue;
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
        inline::serialize(block, &mut out);
        if !blocks.get(at).is_some_and(|next| same_line_list_code(block, next)) {
            out.push('\n');
        }
    }
    out
}

fn is_plain(block: &Block) -> bool {
    block.meta.table.is_none() && !block.is_atomic()
        && block.meta.html_paragraph.is_none() && block.meta.code_language.is_none()
}
fn is_blank(block: &Block) -> bool {
    is_plain(block) && block.kind == Kind::Paragraph && block.meta.indent == 0 && block.text.is_empty()
        && block.meta.clean_source.is_none()
}
fn is_text(block: &Block) -> bool { is_plain(block) && block.kind == Kind::Paragraph && block.meta.indent == 0 && !block.text.is_empty() }
fn is_pipe(block: &Block) -> bool {
    block.meta.table.as_ref().is_some_and(|table| table.format == super::model::TableFormat::Pipe)
}
fn is_image(block: &Block) -> bool {
    block.meta.image.is_some() || super::image_source::spec_of(block).is_some()
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
/// Conventional (not required) blank line after text, headings, quotes, code, dividers/images and a list's last item,
/// so it is not read back as an empty paragraph; `meta.tight` keeps sources that omitted it.
fn optional_separator(this: &Block, next: Option<&Block>) -> bool {
    if is_blank(this) { return false; }
    let source = this.meta.table.is_none() && (this.meta.code_language.is_some() || this.is_atomic());
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
