use super::{inline, table_html, write};
use super::super::model::{Block, Kind, Table, TableCell, TableFormat, TableRow, CellAlignment};
use pulldown_cmark::{Alignment, Event, Options, Parser, Tag, TagEnd};

fn split_pipe_cells(line: &str) -> Vec<&str> {
    let line = line.trim();
    let bytes = line.as_bytes();
    let mut cuts = Vec::new();
    let mut escaped = false;
    for (idx, &byte) in bytes.iter().enumerate() {
        if byte == b'|' && !escaped { cuts.push(idx); }
        escaped = byte == b'\\' && !escaped;
    }
    if cuts.is_empty() { return vec![line]; }
    let start = if cuts.first() == Some(&0) { 1 } else { 0 };
    let has_trailing = cuts.last() == Some(&(bytes.len() - 1));
    let mut out = Vec::new();
    let mut prev = if start == 1 { 1 } else { 0 };
    for &cut in &cuts[start..cuts.len() - usize::from(has_trailing)] {
        out.push(line[prev..cut].trim());
        prev = cut + 1;
    }
    if !has_trailing {
        out.push(line[prev..].trim());
    }
    out
}

pub(super) fn parse_pipe(source: &str, defs: &str) -> Option<(Table, usize)> {
    let mut table = Table { format:TableFormat::Pipe, rows:Vec::new(), columns:0,
        alignments:Vec::new(), column_widths:Vec::new(), attrs:Vec::new(), source:String::new() };
    let mut started = false; let mut header = false;
    let mut cell_events = None;
    let combined;
    let parse_src = if defs.is_empty() || !source.contains('[') {
        source
    } else {
        combined = format!("{source}\n\n{defs}");
        combined.as_str()
    };
    let opts = Options::ENABLE_TABLES | Options::ENABLE_STRIKETHROUGH | Options::ENABLE_FOOTNOTES;
    for (event, range) in Parser::new_ext(parse_src, opts).into_offset_iter() {
        match event {
            Event::Start(Tag::Table(alignments)) if !started && range.start == 0 => {
                started = true; table.columns = alignments.len();
                table.alignments = alignments.into_iter().map(|alignment| match alignment {
                    Alignment::Center => CellAlignment::Center, Alignment::Right => CellAlignment::Right,
                    _ => CellAlignment::Left }).collect();
            },
            Event::Start(Tag::TableHead) if started => {
                header = true; table.rows.push(TableRow { cells:Vec::new() });
            },
            Event::End(TagEnd::TableHead) => header = false,
            Event::Start(Tag::TableRow) if started => table.rows.push(TableRow { cells:Vec::new() }),
            Event::Start(Tag::TableCell) if started => {
                cell_events = Some((range, Vec::new()));
            },
            Event::End(TagEnd::TableCell) if started => {
                let (range, events) = cell_events.take()?;
                let (text, spans, annotations) = inline::parse_events(events);
                let mut content = Block::new(0, Kind::Paragraph, text);
                content.spans = spans; content.meta.inline = annotations;
                let row = table.rows.len().checked_sub(1)?; let column = table.rows[row].cells.len();
                let clamped = range.start.min(source.len())..range.end.min(source.len());
                table.rows[row].cells.push(TableCell { original:Box::new(content.clone()), content,
                    row, column, colspan:1, rowspan:1, header, attrs:Vec::new(), source_range:clamped });
            },
            Event::End(TagEnd::Table) if started => {
                let end_byte = range.end.min(source.len());
                table.source = source[..end_byte].to_owned();
                // Preserve any extra trailing columns that CommonMark dropped past the header width.
                let raw_rows: Vec<&str> = table.source.lines().enumerate()
                    .filter_map(|(idx, line)| (idx != 1).then_some(line))
                    .collect();
                for (r, raw_line) in raw_rows.iter().enumerate() {
                    let raw_cells = split_pipe_cells(raw_line);
                    if raw_cells.len() > table.columns && let Some(row_obj) = table.rows.get_mut(r) {
                        for col in row_obj.cells.len()..raw_cells.len() {
                            let (text, spans, annotations) = inline::parse_with_defs(&raw_cells[col].replace("\\|", "|"), defs);
                            let mut content = Block::new(0, Kind::Paragraph, text);
                            content.spans = spans; content.meta.inline = annotations;
                            row_obj.cells.push(TableCell {
                                original: Box::new(content.clone()), content,
                                row: r, column: col, colspan: 1, rowspan: 1,
                                header: r == 0, attrs: Vec::new(), source_range: 0..0,
                            });
                        }
                        table.columns = table.columns.max(raw_cells.len());
                    }
                }
                while table.alignments.len() < table.columns {
                    table.alignments.push(CellAlignment::Left);
                }
                for (r, row_obj) in table.rows.iter_mut().enumerate() {
                    while row_obj.cells.len() < table.columns {
                        let col = row_obj.cells.len();
                        let empty = Block::new(0, Kind::Paragraph, String::new());
                        row_obj.cells.push(TableCell {
                            original: Box::new(empty.clone()), content: empty,
                            row: r, column: col, colspan: 1, rowspan: 1,
                            header: r == 0, attrs: Vec::new(), source_range: 0..0,
                        });
                    }
                }
                if !defs.is_empty() && source[..end_byte].contains('[') {
                    // Ensure reference-link expansion persists if saved later.
                    let mut fresh = String::new();
                    write_pipe(&table, &mut fresh);
                    table.source = fresh;
                }
                return Some((table, end_byte));
            },
            _ if !started => return None,
            event => if let Some((_, events)) = &mut cell_events { events.push(event); },
        }
    }
    None
}

pub(super) fn serialize(table: &Table, out: &mut String) {
    if table.format == TableFormat::Html { table_html::serialize(table, out); return; }
    // An untouched table is emitted exactly as imported, including escapes and whitespace.
    if table.rows.iter().flat_map(|row| &row.cells).all(|cell| cell.content == *cell.original) {
        out.push_str(&table.source); return;
    }
    write_pipe(table, out);
}

fn write_pipe(table: &Table, out: &mut String) {
    let alignments = table.alignments.iter().map(|alignment| match alignment {
        CellAlignment::Left => Alignment::None, CellAlignment::Center => Alignment::Center,
        CellAlignment::Right => Alignment::Right,
    }).collect();
    let mut events = vec![Event::Start(Tag::Table(alignments))];
    for (index, row) in table.rows.iter().enumerate() {
        let tag = if index == 0 { Tag::TableHead } else { Tag::TableRow };
        events.push(Event::Start(tag.clone()));
        for cell in &row.cells {
            events.push(Event::Start(Tag::TableCell)); events.extend(write::events(&cell.content));
            events.push(Event::End(TagEnd::TableCell));
        }
        events.push(Event::End(tag.to_end()));
    }
    events.push(Event::End(TagEnd::Table));
    if let Some(source) = write::render(events) {
        if parse_pipe(&source, "").is_some_and(|(parsed, _)| parsed.columns == table.columns
            && parsed.rows.len() == table.rows.len()
            && parsed.rows.iter().zip(&table.rows).all(|(a, b)| a.cells.len() == b.cells.len())
            && parsed.rows.iter().flat_map(|row| &row.cells)
            .zip(table.rows.iter().flat_map(|row| &row.cells)).all(|(a, b)|
                a.content.text == b.content.text && a.content.spans == b.content.spans
                    && a.content.meta.inline == b.content.meta.inline)) {
            out.push_str(&source); return;
        }
    }
    // GFM permits inline HTML for content that native delimiters cannot express.
    for (index, row) in table.rows.iter().enumerate() {
        out.push('|');
        for cell in &row.cells {
            out.push(' '); let mut value = String::new(); write::markdown_html(&cell.content, &mut value);
            // Attribute pipes also need escaping at the Markdown table layer.
            out.push_str(&value.replace('|', "&#124;")); out.push_str(" |");
        }
        out.push('\n');
        if index == 0 {
            out.push('|');
            for alignment in &table.alignments {
                out.push_str(match alignment { CellAlignment::Left => " --- |",
                    CellAlignment::Center => " :---: |", CellAlignment::Right => " ---: |" });
            }
            out.push('\n');
        }
    }
}

/// Makes the current rows the canonical source, so serialization emits them instead of a stale import.
pub(super) fn rebuild(table: &mut Table) {
    table.format = TableFormat::Pipe;
    let mut out = String::new(); write_pipe(table, &mut out);
    table.source = out.trim_end_matches('\n').to_owned();
    for row in &mut table.rows { for cell in &mut row.cells { cell.original = Box::new(cell.content.clone()); } }
}
