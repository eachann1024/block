use gpui::Context;
use super::view::{Editor, selection::Position};

impl Editor {
    pub(in crate::editor) fn table_edit_rejected(&mut self, cx: &mut Context<Self>) {
        const MESSAGE: &str = "请在单个单元格内编辑，或选中整个表格后整体替换";
        self.status = MESSAGE.into();
        // A hint, not a state: it clears by itself unless something newer replaced it.
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(std::time::Duration::from_millis(2500)).await;
            this.update(cx, |this, cx| if this.status == MESSAGE { this.status.clear(); cx.notify(); }).ok();
        }).detach();
        cx.notify();
    }
    /// Enter in a cell moves to the cell below in the same column; on the last row it appends a row.
    pub(in crate::editor) fn table_enter(&mut self, cx: &mut Context<Self>) -> bool {
        let Some((id, row, column)) = self.cell_of_caret() else { return false; };
        let Some(table) = self.doc.get(id).and_then(|b| b.meta.table.as_ref()) else { return false; };
        let target_column = table.rows[row].cells[column].column;
        let below = table.cell_ranges().into_iter().find(|(r, c, _)| *r > row
            && table.rows[*r].cells[*c].column <= target_column
            && target_column < table.rows[*r].cells[*c].column + table.rows[*r].cells[*c].colspan)
            .map(|(_, _, body)| body.start);
        if let Some(offset) = below {
            self.set_head(Position { block: id, offset }, false, cx);
        } else {
            let column = target_column;
            self.table_edit(id, move |t| { let at = t.rows.len(); Some((t.insert_row(at), column.min(t.columns.saturating_sub(1)))) }, cx);
        }
        true
    }
    /// Tab-separated lines with one shared column count (copied from a spreadsheet) become a table,
    /// or fill the current table from the caret cell, growing rows and columns as needed.
    pub(in crate::editor) fn paste_tsv(&mut self, text: &str, cx: &mut Context<Self>) -> bool {
        use super::model::{Block, Kind, Table};
        let lines: Vec<&str> = text.trim_end_matches(['\n', '\r']).split('\n').map(|l| l.trim_end_matches('\r')).collect();
        if lines.len() < 2 || !lines.iter().all(|l| l.contains('\t')) { return false; }
        let grid: Vec<Vec<String>> = lines.iter().map(|l| l.split('\t').map(|c| c.trim().to_owned()).collect()).collect();
        let columns = grid[0].len();
        if columns < 2 || grid.iter().any(|r| r.len() != columns) { return false; }
        let fill = |t: &mut Table, row0: usize, col0: usize| {
            while t.rows.len() < row0 + grid.len() { let at = t.rows.len(); t.insert_row(at); }
            while t.columns < col0 + columns { let at = t.columns; t.insert_column(at); }
            for (r, cells) in grid.iter().enumerate() { for (c, value) in cells.iter().enumerate() {
                t.rows[row0 + r].cells[col0 + c].content = Block::new(0, Kind::Paragraph, value.clone());
            } }
        };
        if let Some((id, row, cell)) = self.cell_of_caret() {
            let column = self.doc.get(id).and_then(|b| b.meta.table.as_ref()).map_or(cell, |t| t.rows[row].cells[cell].column);
            let (rows, cols) = (grid.len(), columns);
            self.table_edit(id, move |t| { fill(t, row, column); Some((row + rows - 1, column + cols - 1)) }, cx);
            return true;
        }
        if self.path.is_none() { return false; }
        self.commit_composition(cx);
        let id = self.caret.head.block;
        let Some(index) = self.doc.index_of(id) else { return false; };
        let mut table = Table::blank(grid.len(), columns);
        fill(&mut table, 0, 0); table.finish_structure();
        let replace = self.doc.get(id).is_some_and(|b| b.text.is_empty() && b.kind == Kind::Paragraph
            && b.meta.table.is_none() && b.meta.raw_source.is_none() && b.meta.code_language.is_none());
        let mut blocks = vec![Block::from_table(0, table)];
        if index + 1 >= self.doc.blocks.len() { blocks.push(Block::new(0, Kind::Paragraph, String::new())); }
        let before = self.snapshot();
        let (start, remove) = if replace { (index, 1) } else { (index + 1, 0) };
        let count = blocks.len();
        self.doc.splice_blocks(start..start + remove, blocks);
        let focus = self.doc.blocks[start].id;
        let len = self.doc.blocks[start].text.len();
        let at = Position { block: focus, offset: len };
        self.caret.anchor = at; self.caret.head = at;
        self.record_changes(before, vec![super::input::transactions::ListChange { start, before: remove, after: count }], true, cx);
        true
    }
    /// Tab follows source row/cell order. At table edges it enters an adjacent block,
    /// without inventing rows or changing spans; arrows continue using painted cell geometry.
    pub(in crate::editor) fn table_tab(&mut self, forward: bool, cx: &mut Context<Self>) {
        let at = self.caret.head;
        let Some(block) = self.doc.get(at.block) else { return; };
        let Some(table) = &block.meta.table else { self.tab_outside_table(forward, cx); return; };
        let ranges = table.cell_ranges();
        let current = ranges.iter().position(|(_,_,body)| body.start <= at.offset && at.offset <= body.end)
            .unwrap_or(0);
        let next = if forward { current.checked_add(1).filter(|index| *index < ranges.len()) }
            else { current.checked_sub(1) };
        if let Some(next) = next {
            self.set_head(Position { block:at.block, offset:ranges[next].2.start }, false, cx); return;
        }
        // Tab in the last cell appends a row, when the table can be edited structurally.
        if forward && (table.format == super::model::TableFormat::Pipe || table.is_simple()) {
            self.table_command(at.block, super::table_actions::TableSel::Whole,
                super::table_actions::TableCmd::AppendRow, cx);
            return;
        }
        let index = self.doc.index_of(at.block).unwrap_or(0);
        let adjacent = if forward { index.checked_add(1).filter(|index| *index < self.doc.blocks.len()) }
            else { index.checked_sub(1) };
        if let Some(index) = adjacent {
            let block = &self.doc.blocks[index];
            self.set_head(Position { block:block.id, offset:if forward {0} else {block.text.len()} }, false, cx);
        } else {
            self.status = if forward { "已是最后一个单元格，可用 + 在下方添加内容" }
                else { "已是第一个单元格" }.into();
            cx.notify();
        }
    }
}
