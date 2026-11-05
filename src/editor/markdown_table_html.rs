use super::{html::{self, HtmlValue}, inline};
use super::super::model::{Block, Kind, Table, TableCell, TableRow, TableFormat};

pub(super) fn parse(source: &str) -> Result<Table, &'static str> {
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

pub(super) fn serialize(table: &Table, out: &mut String) {
    let mut cursor = 0;
    for row in &table.rows {
        for cell in &row.cells {
            if cell.content == *cell.original { continue; }
            let range = &cell.source_range;
            // Imported ranges are immutable; malformed drafts fail closed to their original HTML.
            if range.start < cursor || range.end > table.source.len()
                || !table.source.is_char_boundary(range.start) || !table.source.is_char_boundary(range.end) {
                out.push_str(&table.source[cursor..]); return;
            }
            out.push_str(&table.source[cursor..range.start]); inline::serialize_html(&cell.content, out);
            cursor = range.end;
        }
    }
    out.push_str(&table.source[cursor..]);
}
