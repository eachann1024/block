//! Markdown round-trip for GitHub alert callouts (`> [!NOTE]`) and multi-column HTML containers.
use super::{html::{self, HtmlValue}, inline};
use super::super::model::{Block, CalloutKind, CellAlignment, Kind, Table, TableCell, TableFormat, TableRow};

pub(super) fn parse_callout(id: u64, lines: &[&str]) -> Option<(Block, usize, usize)> {
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
    for (i, body) in bodies.iter().enumerate() {
        if i > 0 { block.replace_range(block.text.len()..block.text.len(), "\n"); }
        if body.is_empty() { continue; }
        let (text, spans, tags) = inline::parse(body)?;
        let mut part = Block::new(0, Kind::Paragraph, text);
        part.spans = spans; part.meta.inline = tags;
        block.append(&part);
    }
    let bytes = lines[..end].iter().map(|l| l.len()).sum();
    Some((block, end, bytes))
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

fn is_div_class(name: &str, attrs: &[(String, String)], target: &str) -> bool {
    name == "div" && attrs.iter().any(|(k, v)| k == "class" && v.split_whitespace().any(|c| c == target))
}

pub(super) fn starts_columns(line: &str) -> bool {
    let Some(tokens) = html::tokens(line.trim()) else { return false };
    matches!(tokens.first().map(|t| &t.value),
        Some(HtmlValue::Tag { name, attrs, close: false, empty: false }) if is_div_class(name, attrs, "columns"))
}

pub(super) fn columns_line_count(lines: &[&str]) -> usize {
    let mut depth = 0usize;
    for (i, line) in lines.iter().enumerate() {
        if let Some(tokens) = html::tokens(line) {
            for token in tokens {
                if let HtmlValue::Tag { name, close, empty, .. } = token.value && name == "div" && !empty {
                    if close { depth = depth.saturating_sub(1); } else { depth += 1; }
                }
            }
        }
        if i > 0 && depth == 0 { return i + 1; }
        if i == 0 && depth == 0 { return 1; }
    }
    lines.len()
}

pub(super) fn parse_columns(source: &str) -> Result<Table, &'static str> {
    let tokens = html::tokens(source).ok_or("Columns HTML contains malformed tokens")?;
    let Some(first) = tokens.first() else { return Err("Empty columns block") };
    let HtmlValue::Tag { name, attrs: outer_attrs, close: false, empty: false } = &first.value else {
        return Err("Columns block must start with an opening div");
    };
    if !is_div_class(name, outer_attrs, "columns") { return Err("Missing columns wrapper class"); }
    let Some(last) = tokens.last() else { return Err("Incomplete columns block") };
    if !matches!(&last.value, HtmlValue::Tag { name, close: true, .. } if name == "div") {
        return Err("Columns block has no closing div");
    }
    let mut cells = Vec::new();
    let mut i = 1;
    while i + 1 < tokens.len() {
        match &tokens[i].value {
            HtmlValue::Text(t) if t.trim().is_empty() => { i += 1; continue; }
            HtmlValue::Tag { name, attrs, close: false, empty: false } if is_div_class(name, attrs, "column") => {
                let mut depth = 1usize;
                let mut end = i + 1;
                while end + 1 < tokens.len() {
                    if let HtmlValue::Tag { name: n, close, empty: false, .. } = &tokens[end].value && n == "div" {
                        if *close {
                            depth -= 1;
                            if depth == 0 { break; }
                        } else { depth += 1; }
                    }
                    end += 1;
                }
                if depth != 0 { return Err("Unclosed column div"); }
                let range = tokens[i].range.end..tokens[end].range.start;
                let raw = &source[range.clone()];
                let body = raw.strip_prefix("\r\n").or_else(|| raw.strip_prefix('\n')).unwrap_or(raw);
                let body = body.strip_suffix("\r\n").or_else(|| body.strip_suffix('\n')).unwrap_or(body);
                let mut content = Block::new(0, Kind::Paragraph, String::new());
                if !body.is_empty() {
                    for (line_idx, line) in body.split('\n').enumerate() {
                        if line_idx > 0 { content.replace_range(content.text.len()..content.text.len(), "\n"); }
                        let line = line.strip_suffix('\r').unwrap_or(line);
                        if line.is_empty() { continue; }
                        let (text, spans, tags) = inline::parse(line)
                            .ok_or("Column contains unsupported Markdown or HTML")?;
                        let mut part = Block::new(0, Kind::Paragraph, text);
                        part.spans = spans; part.meta.inline = tags;
                        content.append(&part);
                    }
                }
                let column = cells.len();
                cells.push(TableCell {
                    original: Box::new(content.clone()), content, row: 0, column,
                    colspan: 1, rowspan: 1, header: false, attrs: attrs.clone(), source_range: range,
                });
                i = end + 1;
            }
            _ => return Err("Unsupported element inside columns container"),
        }
    }
    if cells.len() < 2 { return Err("Columns container requires at least two columns"); }
    let columns = cells.len();
    Ok(Table {
        format: TableFormat::Columns, rows: vec![TableRow { cells }], columns,
        alignments: vec![CellAlignment::Left; columns], column_widths: Vec::new(),
        attrs: outer_attrs.clone(), source: source.to_owned(),
    })
}

pub(super) fn serialize_columns(table: &Table, out: &mut String) {
    out.push_str("<div class=\"columns\">\n");
    if let Some(row) = table.rows.first() {
        for cell in &row.cells {
            out.push_str("<div class=\"column\">\n");
            let b = &cell.content;
            if !b.text.is_empty() || !b.spans.is_empty() || !b.meta.inline.is_empty() {
                let mut start = 0;
                for part in b.text.split('\n') {
                    let end = start + part.len();
                    let slice = b.slice(start..end);
                    inline::serialize(&slice, out);
                    out.push('\n');
                    start = end + 1;
                }
            }
            out.push_str("</div>\n");
        }
    }
    out.push_str("</div>\n");
}
