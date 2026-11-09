use super::{Block, Doc, Kind};
use serde::{Deserialize, Serialize};
use std::ops::Range;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum TableFormat { Pipe, Html }
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub enum CellAlignment { #[default] Left, Center, Right }
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct TableCell {
    pub content: Block,
    pub row: usize, pub column: usize,
    pub colspan: usize, pub rowspan: usize, pub header: bool,
    pub attrs: Vec<(String, String)>,
    /// Byte range of the original cell body, excluding its opening and closing tags.
    pub source_range: Range<usize>,
    pub original: Box<Block>,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct TableRow { pub cells: Vec<TableCell> }
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Table {
    pub format: TableFormat,
    pub rows: Vec<TableRow>,
    pub columns: usize,
    pub alignments: Vec<CellAlignment>,
    pub column_widths: Vec<Option<String>>,
    pub attrs: Vec<(String, String)>,
    /// Keep all structural HTML and unedited cells byte-for-byte.
    pub source: String,
}
impl TableCell {
    pub fn attr(&self, name: &str) -> Option<&str> {
        self.attrs.iter().find(|(key, _)| key == name).map(|(_, value)| value.as_str())
    }
    pub fn alignment(&self) -> CellAlignment {
        let alignment = self.attr("data-thinkly-text-alignment")
            .or_else(|| self.attr("data-goose-text-alignment"));
        let css = self.attr("style").unwrap_or("");
        let alignment = alignment.or_else(|| css.split(';').find_map(|part| {
            let (name, value) = part.split_once(':')?;
            (name.trim() == "text-align").then_some(value.trim())
        })).or_else(|| self.attr("align"));
        match alignment { Some("center") => CellAlignment::Center,
            Some("right") => CellAlignment::Right, _ => CellAlignment::Left }
    }
}
impl Table {
    pub fn cell(&self, row: usize, cell: usize) -> Option<&TableCell> {
        self.rows.get(row)?.cells.get(cell)
    }
    pub fn cell_mut(&mut self, row: usize, cell: usize) -> Option<&mut TableCell> {
        self.rows.get_mut(row)?.cells.get_mut(cell)
    }
    pub fn projection(&self) -> String {
        self.rows.iter().map(|row| row.cells.iter().map(|cell| cell.content.text.as_str())
            .collect::<Vec<_>>().join("\t")).collect::<Vec<_>>().join("\n")
    }
    pub fn cell_ranges(&self) -> Vec<(usize, usize, Range<usize>)> {
        let mut result = Vec::new(); let mut at = 0;
        for (r, row) in self.rows.iter().enumerate() {
            if r > 0 { at += 1; }
            for (c, cell) in row.cells.iter().enumerate() {
                if c > 0 { at += 1; }
                let end = at + cell.content.text.len(); result.push((r, c, at..end)); at = end;
            }
        }
        result
    }
    pub fn cell_at_range(&self, range: &Range<usize>) -> Option<(usize, usize, Range<usize>)> {
        self.cell_ranges().into_iter().find_map(|(row, cell, body)| {
            (body.start <= range.start && range.end <= body.end).then(||
                (row, cell, range.start - body.start..range.end - body.start))
        })
    }
    pub fn replace_range(&mut self, range: Range<usize>, text: &str) -> bool {
        let Some((row, cell, local)) = self.cell_at_range(&range) else { return false; };
        self.rows[row].cells[cell].content.replace_range(local, text)
    }
    /// Preserve the table structure for clipboard slices and cross-block transactions.
    pub fn slice(&self, range: Range<usize>) -> Self {
        let mut table = self.clone();
        for (r, c, cell_range) in self.cell_ranges() {
            let start = range.start.max(cell_range.start).min(cell_range.end) - cell_range.start;
            let end = range.end.max(cell_range.start).min(cell_range.end) - cell_range.start;
            let cell = &mut table.rows[r].cells[c];
            cell.content = cell.content.slice(start..end.max(start));
        }
        table
    }
}
impl Block {
    pub fn from_table(id: u64, table: Table) -> Self {
        let mut block = Self::new(id, Kind::Paragraph, table.projection());
        block.meta.table = Some(table); block
    }
    pub fn sync_table_text(&mut self) {
        if let Some(table) = &self.meta.table { self.text = table.projection(); self.spans.clear(); }
    }
}
impl Doc {
    /// Returns false without changing a document when the target is missing or unchanged.
    #[allow(dead_code)]
    pub fn set_table_cell_text(&mut self, id: u64, row: usize, cell: usize, text: String) -> bool {
        let Some(block) = self.get_mut(id) else { return false; };
        let Some(target) = block.meta.table.as_mut().and_then(|t| t.cell_mut(row, cell)) else { return false; };
        if target.content.text == text { return false; }
        let old = &target.content.text;
        let mut prefix = old.bytes().zip(text.bytes()).take_while(|(a,b)| a == b).count();
        while !old.is_char_boundary(prefix) || !text.is_char_boundary(prefix) { prefix -= 1; }
        let mut suffix = old[prefix..].bytes().rev().zip(text[prefix..].bytes().rev())
            .take_while(|(a,b)| a == b).count();
        while !old.is_char_boundary(old.len()-suffix) || !text.is_char_boundary(text.len()-suffix) { suffix -= 1; }
        target.content.replace_range(prefix..old.len()-suffix, &text[prefix..text.len()-suffix]);
        block.sync_table_text(); self.refresh_block(id); true
    }
}

/// Structural edits. Every edit works on a Markdown pipe table: the first row is the header.
impl TableCell {
    fn blank(row: usize, column: usize) -> Self {
        let content = Block::new(0, Kind::Paragraph, String::new());
        Self { original: Box::new(content.clone()), content, row, column, colspan: 1, rowspan: 1,
            header: row == 0, attrs: Vec::new(), source_range: 0..0 }
    }
}
impl Table {
    /// Pipe tables require one header row and cannot retain widths, merges or non-alignment cell attributes.
    pub fn is_simple(&self) -> bool {
        let benign_table = self.attrs.iter().all(|(k, v)| matches!(
            (k.as_str(), v.as_str()),
            ("data-thinkly-table" | "data-goose-table", _) | ("data-header-rows", "1") | ("data-header-cols", "0")
        ));
        self.columns > 0 && !self.rows.is_empty() && benign_table && self.column_widths.iter().all(Option::is_none)
            && self.rows.iter().enumerate().all(|(index, row)| row.cells.len() == self.columns
                && row.cells.iter().all(|cell| cell.colspan == 1 && cell.rowspan == 1 && cell.header == (index == 0)
                    && cell.attrs.iter().all(|(k, v)| match k.as_str() {
                        "colspan" | "rowspan" | "data-thinkly-text-alignment" | "data-goose-text-alignment" | "align" => true,
                        // Exported tables repeat alignment in CSS; it is preserved by to_pipe.
                        "style" => v.split(';').filter(|part| !part.trim().is_empty()).all(|part| {
                            part.split_once(':').is_some_and(|(name, value)| name.trim() == "text-align"
                                && matches!(value.trim(), "left" | "center" | "right"))
                        }),
                        _ => false,
                    })))
    }
    /// Converts a simple HTML table into a pipe table. Returns false when that would lose data.
    pub fn to_pipe(&mut self) -> bool {
        if self.format == TableFormat::Pipe { return true; }
        if !self.is_simple() { return false; }
        self.alignments = (0..self.columns)
            .map(|c| self.rows[0].cells.get(c).map_or(CellAlignment::Left, TableCell::alignment))
            .collect();
        self.attrs.clear(); self.column_widths.clear();
        for row in &mut self.rows { for cell in &mut row.cells { cell.attrs.clear(); } }
        self.format = TableFormat::Pipe; true
    }
    pub fn insert_row(&mut self, at: usize) -> usize {
        let at = at.min(self.rows.len());
        let cells = (0..self.columns).map(|c| TableCell::blank(at, c)).collect();
        self.rows.insert(at, TableRow { cells }); at
    }
    /// Clones row `at` directly below itself and returns the new row index.
    pub fn duplicate_row(&mut self, at: usize) -> Option<usize> {
        let row = self.rows.get(at)?.clone(); self.rows.insert(at + 1, row); Some(at + 1)
    }
    /// Clones column `at` (cells and alignment) directly to its right and returns the new index.
    pub fn duplicate_column(&mut self, at: usize) -> Option<usize> {
        if at >= self.columns { return None; }
        for row in &mut self.rows { if let Some(cell) = row.cells.get(at).cloned() { row.cells.insert(at + 1, cell); } }
        self.alignments.resize(self.columns, CellAlignment::Left);
        let alignment = self.alignments[at]; self.alignments.insert(at + 1, alignment);
        self.columns += 1; Some(at + 1)
    }
    pub fn delete_row(&mut self, at: usize) -> bool {
        if self.rows.len() <= 1 || at >= self.rows.len() { return false; }
        self.rows.remove(at); true
    }
    pub fn insert_column(&mut self, at: usize) -> usize {
        let at = at.min(self.columns);
        for (r, row) in self.rows.iter_mut().enumerate() { row.cells.insert(at, TableCell::blank(r, at)); }
        self.alignments.insert(at.min(self.alignments.len()), CellAlignment::Left);
        self.columns += 1; at
    }
    pub fn delete_column(&mut self, at: usize) -> bool {
        if self.columns <= 1 || at >= self.columns { return false; }
        for row in &mut self.rows { row.cells.remove(at); }
        if at < self.alignments.len() { self.alignments.remove(at); }
        self.columns -= 1; true
    }
    pub fn move_row(&mut self, from: usize, to: usize) -> bool {
        if from >= self.rows.len() || to >= self.rows.len() || from == to { return false; }
        let row = self.rows.remove(from); self.rows.insert(to, row); true
    }
    pub fn move_column(&mut self, from: usize, to: usize) -> bool {
        if from >= self.columns || to >= self.columns || from == to { return false; }
        for row in &mut self.rows { let cell = row.cells.remove(from); row.cells.insert(to, cell); }
        if from < self.alignments.len() && to < self.alignments.len() {
            let alignment = self.alignments.remove(from); self.alignments.insert(to, alignment);
        }
        true
    }
    pub fn set_column_alignment(&mut self, column: usize, alignment: CellAlignment) {
        self.alignments.resize(self.columns, CellAlignment::Left);
        if let Some(slot) = self.alignments.get_mut(column) { *slot = alignment; }
    }
    pub fn clear_row(&mut self, row: usize) {
        if let Some(row) = self.rows.get_mut(row) { for cell in &mut row.cells { cell.content = Block::new(0, Kind::Paragraph, String::new()); } }
    }
    pub fn clear_column(&mut self, column: usize) {
        for row in &mut self.rows { if let Some(cell) = row.cells.get_mut(column) { cell.content = Block::new(0, Kind::Paragraph, String::new()); } }
    }
    /// Renumbers cells after a structural edit and refreshes the canonical Markdown source.
    pub fn finish_structure(&mut self) {
        for (r, row) in self.rows.iter_mut().enumerate() {
            for (c, cell) in row.cells.iter_mut().enumerate() { cell.row = r; cell.column = c; cell.header = r == 0; }
        }
        self.columns = self.rows.first().map_or(0, |row| row.cells.len());
        self.alignments.resize(self.columns, CellAlignment::Left);
        crate::editor::markdown::rebuild_table_source(self);
    }
    pub fn blank(rows: usize, columns: usize) -> Self {
        let rows_vec = (0..rows).map(|r| TableRow { cells: (0..columns).map(|c| TableCell::blank(r, c)).collect() }).collect();
        let mut table = Self { format: TableFormat::Pipe, rows: rows_vec, columns,
            alignments: vec![CellAlignment::Left; columns], column_widths: Vec::new(), attrs: Vec::new(), source: String::new() };
        table.finish_structure(); table
    }
}
