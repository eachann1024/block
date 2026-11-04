use super::{inline, table_html};
use super::super::model::{Block, Kind, Table, TableCell, TableFormat, TableRow, CellAlignment};
use pulldown_cmark::{Alignment, Event, Options, Parser, Tag, TagEnd};

pub(super) fn parse_pipe(source: &str) -> Option<(Table, usize)> {
    let mut table = Table { format:TableFormat::Pipe, rows:Vec::new(), columns:0,
        alignments:Vec::new(), column_widths:Vec::new(), attrs:Vec::new(), source:String::new() };
    let mut started = false; let mut header = false;
    for (event, range) in Parser::new_ext(source, Options::ENABLE_TABLES | Options::ENABLE_STRIKETHROUGH).into_offset_iter() {
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
                let raw = source[range.clone()].trim();
                let (text, spans, annotations) = inline::parse(raw)?;
                let mut content = Block::new(0, Kind::Paragraph, text);
                content.spans = spans; content.meta.inline = annotations;
                let row = table.rows.len().checked_sub(1)?; let column = table.rows[row].cells.len();
                table.rows[row].cells.push(TableCell { original:Box::new(content.clone()), content,
                    row, column, colspan:1, rowspan:1, header, attrs:Vec::new(), source_range:range });
            },
            Event::End(TagEnd::Table) if started => {
                table.source = source[..range.end].to_owned();
                if table.source.lines().enumerate().filter(|(index,_)| *index != 1)
                    .any(|(_,line)| cell_count(line) > table.columns) { return None; }
                return Some((table, range.end));
            },
            _ if !started => return None,
            _ => {},
        }
    }
    None
}

fn cell_count(line: &str) -> usize {
    let line = line.trim(); let bytes = line.as_bytes(); let mut escaped = false; let mut pipes = 0;
    for byte in bytes {
        if *byte == b'|' && !escaped { pipes += 1; }
        escaped = *byte == b'\\' && !escaped;
    }
    let leading = usize::from(line.starts_with('|'));
    let trailing = usize::from(line.ends_with('|') && !escaped
        && bytes.iter().rev().skip(1).take_while(|b| **b == b'\\').count() % 2 == 0);
    (pipes + 1usize).saturating_sub(leading + trailing)
}

pub(super) fn serialize(table: &Table, out: &mut String) {
    if table.format == TableFormat::Columns { super::containers::serialize_columns(table, out); return; }
    if table.format == TableFormat::Html { table_html::serialize(table, out); return; }
    // An untouched table is emitted exactly as imported, including escapes and whitespace.
    if table.rows.iter().flat_map(|row| &row.cells).all(|cell| cell.content == *cell.original) {
        out.push_str(&table.source); return;
    }
    write_pipe(table, out);
}

fn write_pipe(table: &Table, out: &mut String) {
    for (index, row) in table.rows.iter().enumerate() {
        out.push('|');
        for cell in &row.cells {
            out.push(' '); let mut value = String::new(); inline::serialize(&cell.content, &mut value);
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
