use super::{html::{self, HtmlValue}, inline};
use super::super::model::{Block, Kind, Table, TableCell, TableRow, TableFormat};
use html5ever::{tendril::StrTendril, tokenizer::{BufferQueue, TagKind, Token, TokenSink, TokenSinkResult, Tokenizer, TokenizerOpts}};
use std::{cell::RefCell, rc::Rc};

const SYNTHETIC_RANGE: std::ops::Range<usize> = usize::MAX..usize::MAX;

fn parse_strict(source: &str) -> Result<Table, &'static str> {
    let tokens = html::tokens(source).ok_or("HTML contains unsupported or malformed tokens")?;
    let mut table = Table { format:TableFormat::Html, rows:Vec::new(), columns:0,
        alignments:Vec::new(), column_widths:Vec::new(), attrs:Vec::new(), source:source.to_owned() };
    let mut stack: Vec<String> = Vec::new(); let mut i = 0; let mut seen_table = false;
    while i < tokens.len() {
        let token = &tokens[i];
        let HtmlValue::Tag { name, attrs, close, empty } = &token.value else {
            if let HtmlValue::Text(text) = &token.value && !text.trim().is_empty() {
                return Err("Text outside a table cell cannot be represented visually");
            }
            i += 1; continue;
        };
        if *close {
            if stack.pop().as_ref() != Some(name) { return Err("Table tags are not explicitly balanced"); }
            i += 1; continue;
        }
        match name.as_str() {
            "table" if stack.is_empty() && !seen_table => { seen_table = true; table.attrs = attrs.clone(); },
            "thead"|"tbody"|"tfoot"|"colgroup" if stack.last().map(String::as_str) == Some("table") => {},
            "tr" if matches!(stack.last().map(String::as_str), Some("table"|"thead"|"tbody"|"tfoot")) => {
                table.rows.push(TableRow { cells:Vec::new() });
            },
            "col" if stack.last().map(String::as_str) == Some("colgroup") => {
                let width = attrs.iter().find(|(key,_)| key == "style").and_then(|(_,css)|
                    css.split(';').find_map(|part| { let (name,value) = part.split_once(':')?;
                        (name.trim() == "width").then(|| value.trim().to_owned()) }));
                table.column_widths.push(width); i += 1; continue;
            },
            "td"|"th" if stack.last().map(String::as_str) == Some("tr") => {
                let mut end = i + 1;
                while end < tokens.len() {
                    if let HtmlValue::Tag { name:n, close:true, .. } = &tokens[end].value && n == name { break; }
                    if let HtmlValue::Tag { name:n, .. } = &tokens[end].value
                        && matches!(n.as_str(), "table"|"tr"|"td"|"th") {
                        return Err("Nested or unbalanced cells are retained as original HTML");
                    }
                    end += 1;
                }
                let close = tokens.get(end).ok_or("A table cell has no explicit closing tag")?;
                let range = token.range.end..close.range.start;
                let (text, spans, annotations) = inline::parse_html(&source[range.clone()])
                    .ok_or("Cell contains unsupported HTML; the original table is retained")?;
                let mut content = Block::new(0, Kind::Paragraph, text);
                content.spans = spans; content.meta.inline = annotations;
                let span = |key: &str| -> Result<usize, &'static str> {
                    let value = attrs.iter().find(|(name,_)| name == key).map(|(_,value)| value.as_str()).unwrap_or("1");
                    let number = value.parse::<usize>().map_err(|_| "Invalid table cell span")?;
                    if !(1..=256).contains(&number) { return Err("Table cell span exceeds the visual editor limit"); }
                    Ok(number)
                };
                let row = table.rows.len() - 1;
                table.rows[row].cells.push(TableCell { original:Box::new(content.clone()), content,
                    row, column:0, colspan:span("colspan")?, rowspan:span("rowspan")?,
                    header:name == "th", attrs:attrs.clone(), source_range:range });
                if table.rows.len() > 4096 || table.rows[row].cells.len() > 256 {
                    return Err("Table exceeds the visual editor limit; original HTML is retained");
                }
                i = end + 1; continue;
            },
            _ => return Err("Unsupported table structure is retained as original HTML"),
        }
        if *empty { return Err("Non-void table elements cannot be self-closing"); }
        stack.push(name.clone()); i += 1;
    }
    if !seen_table || !stack.is_empty() || table.rows.is_empty() { return Err("Incomplete HTML table"); }
    let mut occupied = vec![vec![false;256]; table.rows.len()]; let mut width = 0;
    for (r, row) in table.rows.iter_mut().enumerate() {
        let mut column = 0;
        for cell in &mut row.cells {
            while column < 256 && occupied[r][column] { column += 1; }
            if column + cell.colspan > 256 || r + cell.rowspan > occupied.len() {
                return Err("Table spans extend outside the supported grid");
            }
            cell.column = column;
            for line in occupied.iter_mut().skip(r).take(cell.rowspan) {
                for slot in line.iter_mut().skip(column).take(cell.colspan) {
                    if *slot { return Err("Overlapping merged cells are retained as original HTML"); }
                    *slot = true;
                }
            }
            column += cell.colspan; width = width.max(column);
        }
    }
    if width == 0 || occupied.iter().any(|row| row[..width].iter().any(|slot| !slot)) {
        return Err("Irregular table grid is retained as original HTML");
    }
    if !table.column_widths.is_empty() && table.column_widths.len() != width {
        return Err("Column widths do not match the table grid");
    }
    table.columns = width; Ok(table)
}

#[derive(Clone)]
enum FlatTok {
    Text(String),
    Tag { name: String, attrs: Vec<(String, String)>, close: bool, empty: bool },
}

#[derive(Clone)]
struct FlatSink(Rc<RefCell<Vec<FlatTok>>>);
impl TokenSink for FlatSink {
    type Handle = ();
    fn process_token(&self, token: Token, _: u64) -> TokenSinkResult<()> {
        match token {
            Token::CharacterTokens(t) => self.0.borrow_mut().push(FlatTok::Text(t.to_string())),
            Token::TagToken(tag) => self.0.borrow_mut().push(FlatTok::Tag {
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

fn reconstruct_fragment(toks: &[FlatTok]) -> String {
    let mut out = String::new();
    for tok in toks {
        match tok {
            FlatTok::Text(t) => {
                for ch in t.chars() {
                    match ch { '&' => out.push_str("&amp;"), '<' => out.push_str("&lt;"), '>' => out.push_str("&gt;"), _ => out.push(ch) }
                }
            }
            FlatTok::Tag { name, attrs, close, empty } => {
                if *close {
                    out.push_str("</"); out.push_str(name); out.push('>');
                } else {
                    html::open_tag(name, attrs, &mut out);
                    if *empty && !matches!(name.as_str(), "br" | "wbr" | "hr" | "img" | "col" | "input") {
                        out.push_str("</"); out.push_str(name); out.push('>');
                    }
                }
            }
        }
    }
    out
}

/// Notion-style permissive HTML table normalizer: accepts `<p>`/`<div>`/`<li>` in cells,
/// irregular row widths, unclosed cells, and nested wrappers while preserving `table.source` until edited.
pub(super) fn parse(source: &str) -> Option<Table> {
    if let Ok(table) = parse_strict(source) { return Some(table); }
    let sink = Rc::new(RefCell::new(Vec::new()));
    let tokenizer = Tokenizer::new(FlatSink(sink.clone()), TokenizerOpts::default());
    let queue = BufferQueue::default();
    queue.push_back(StrTendril::from(source));
    let _ = tokenizer.feed(&queue);
    tokenizer.end();
    let toks = sink.borrow();
    let mut table = Table {
        format: TableFormat::Html, rows: Vec::new(), columns: 0,
        alignments: Vec::new(), column_widths: Vec::new(), attrs: Vec::new(), source: source.to_owned(),
    };
    let mut table_depth = 0usize;
    let mut in_head = false;
    let mut i = 0usize;
    while i < toks.len() {
        let FlatTok::Tag { name, attrs, close, .. } = &toks[i] else { i += 1; continue; };
        match name.as_str() {
            "table" => {
                if *close {
                    table_depth = table_depth.saturating_sub(1);
                    if table_depth == 0 { break; }
                } else {
                    if table_depth == 0 { table.attrs = attrs.clone(); }
                    table_depth += 1;
                }
                i += 1;
            }
            "thead" if table_depth == 1 => { in_head = !*close; i += 1; }
            "tr" if table_depth == 1 && !*close => {
                table.rows.push(TableRow { cells: Vec::new() });
                i += 1;
            }
            "td" | "th" if table_depth == 1 && !*close => {
                if table.rows.is_empty() { table.rows.push(TableRow { cells: Vec::new() }); }
                let cell_tag = name.clone();
                let cell_attrs = attrs.clone();
                let mut inner_depth = 0usize;
                let mut end = i + 1;
                while end < toks.len() {
                    if let FlatTok::Tag { name: n, close: c, .. } = &toks[end] {
                        if n == "table" {
                            if *c { inner_depth = inner_depth.saturating_sub(1); } else { inner_depth += 1; }
                        } else if inner_depth == 0 {
                            if *c && (n == &cell_tag || matches!(n.as_str(), "tr" | "thead" | "tbody" | "tfoot" | "table")) {
                                break;
                            }
                            if !*c && matches!(n.as_str(), "td" | "th" | "tr") {
                                break;
                            }
                        }
                    }
                    end += 1;
                }
                let fragment = reconstruct_fragment(&toks[i + 1..end]);
                let (text, spans, annotations) = inline::parse_html_permissive(&fragment, true);
                let mut content = Block::new(0, Kind::Paragraph, text);
                content.spans = spans; content.meta.inline = annotations;
                let parse_span = |key: &str| -> usize {
                    cell_attrs.iter().find(|(k, _)| k == key)
                        .and_then(|(_, v)| v.trim().parse::<usize>().ok())
                        .unwrap_or(1).clamp(1, 64)
                };
                let row = table.rows.len() - 1;
                if table.rows[row].cells.len() < 256 && table.rows.len() <= 4096 {
                    table.rows[row].cells.push(TableCell {
                        original: Box::new(content.clone()), content,
                        row, column: 0, colspan: parse_span("colspan"), rowspan: parse_span("rowspan"),
                        header: cell_tag == "th" || in_head, attrs: cell_attrs, source_range: SYNTHETIC_RANGE,
                    });
                }
                i = if toks.get(end).is_some_and(|t| matches!(t, FlatTok::Tag { name: n, close: true, .. } if n == &cell_tag)) {
                    end + 1
                } else { end };
            }
            _ => { i += 1; }
        }
    }
    table.rows.retain(|r| !r.cells.is_empty());
    if table.rows.is_empty() { return None; }
    let row_count = table.rows.len();
    let mut occupied = vec![vec![false; 256]; row_count];
    let mut width = 0usize;
    for (r, row) in table.rows.iter_mut().enumerate() {
        let mut col = 0usize;
        for cell in &mut row.cells {
            while col < 256 && occupied[r][col] { col += 1; }
            if col >= 256 { col = 255; }
            cell.colspan = cell.colspan.min(256 - col).max(1);
            cell.rowspan = cell.rowspan.min(row_count - r).max(1);
            // Clamp colspan if an earlier rowspan already occupies part of the target range.
            let mut max_c = 0usize;
            while max_c < cell.colspan && !occupied[r][col + max_c] { max_c += 1; }
            cell.colspan = max_c.max(1);
            cell.row = r;
            cell.column = col;
            for rr in r..r + cell.rowspan {
                for cc in col..col + cell.colspan { occupied[rr][cc] = true; }
            }
            col += cell.colspan;
            width = width.max(col);
        }
    }
    if width == 0 { return None; }
    // Fill any missing slots in ragged HTML rows so the visual grid invariant holds.
    for (r, row) in table.rows.iter_mut().enumerate() {
        let default_hdr = row.cells.iter().all(|c| c.header) && !row.cells.is_empty();
        for c in 0..width {
            if !occupied[r][c] {
                occupied[r][c] = true;
                let empty = Block::new(0, Kind::Paragraph, String::new());
                let pos = row.cells.partition_point(|cell| cell.column < c);
                row.cells.insert(pos, TableCell {
                    original: Box::new(empty.clone()), content: empty,
                    row: r, column: c, colspan: 1, rowspan: 1,
                    header: default_hdr, attrs: Vec::new(), source_range: SYNTHETIC_RANGE,
                });
            }
        }
    }
    table.columns = width;
    table.alignments = vec![super::super::model::CellAlignment::Left; width];
    Some(table)
}

fn write_canonical_html(table: &Table, out: &mut String) {
    html::open_tag("table", &table.attrs, out);
    out.push('\n');
    for row in &table.rows {
        out.push_str("<tr>");
        for cell in &row.cells {
            let tag = if cell.header { "th" } else { "td" };
            let mut attrs: Vec<(String, String)> = cell.attrs.iter()
                .filter(|(k, _)| !matches!(k.as_str(), "colspan" | "rowspan"))
                .cloned()
                .collect();
            if cell.colspan > 1 { attrs.push(("colspan".into(), cell.colspan.to_string())); }
            if cell.rowspan > 1 { attrs.push(("rowspan".into(), cell.rowspan.to_string())); }
            html::open_tag(tag, &attrs, out);
            inline::serialize_html(&cell.content, out);
            out.push_str("</"); out.push_str(tag); out.push('>');
        }
        out.push_str("</tr>\n");
    }
    out.push_str("</table>\n");
}

pub(super) fn serialize(table: &Table, out: &mut String) {
    if table.rows.iter().flat_map(|row| &row.cells).all(|cell| cell.content == *cell.original) {
        out.push_str(&table.source);
        return;
    }
    if table.rows.iter().flat_map(|row| &row.cells).any(|cell| cell.source_range == SYNTHETIC_RANGE) {
        write_canonical_html(table, out);
        return;
    }
    let mut cursor = 0;
    for row in &table.rows {
        for cell in &row.cells {
            if cell.content == *cell.original { continue; }
            let range = &cell.source_range;
            if range.start < cursor || range.end > table.source.len()
                || !table.source.is_char_boundary(range.start) || !table.source.is_char_boundary(range.end) {
                write_canonical_html(table, out);
                return;
            }
            out.push_str(&table.source[cursor..range.start]); inline::serialize_html(&cell.content, out);
            cursor = range.end;
        }
    }
    out.push_str(&table.source[cursor..]);
}
