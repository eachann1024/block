//! Table structure commands, row/column selection, and the geometry the table chrome needs.
use gpui::{Bounds, ClipboardItem, Context, Entity, Pixels, Point, Window, point, px};
use crate::theme::tr;
use super::{input::transactions::ListChange, model::{CellAlignment, Table}, table_layout, view::{Editor, layout::TextLayout, selection::Position}};

thread_local! {
    /// (table block, row, column) under the pointer; the chrome follows hover, not the caret.
    static HOVER: std::cell::Cell<Option<(u64, usize, usize)>> = const { std::cell::Cell::new(None) };
}
pub(super) fn hovered() -> Option<(u64, usize, usize)> { HOVER.with(|h| h.get()) }
/// Registered while a table paints. The reach covers the outside handles and the extend buttons,
/// so moving from a cell onto its handle keeps the chrome. Off-grid axes keep the last index.
pub(super) fn track_hover(editor: Entity<Editor>, block: u64, layout: &TextLayout, window: &mut Window) {
    let Some(grid) = table_layout::grid_rect(layout) else { return };
    let (rows, columns) = table_layout::spans(layout);
    let reach = Bounds::from_corners(grid.origin - point(px(36.), px(32.)), grid.bottom_right() + point(px(56.), px(36.)));
    window.on_mouse_event(move |event: &gpui::MouseMoveEvent, phase, _, cx| {
        if phase != gpui::DispatchPhase::Bubble || event.dragging() { return; }
        let at = event.position; let current = hovered();
        let next = reach.contains(&at).then(|| {
            let last = current.filter(|h| h.0 == block);
            let r = if at.y >= grid.top() && at.y < grid.bottom() { table_layout::span_index(&rows, at.y) }
                else { last.map(|h| h.1).or_else(|| table_layout::span_index(&rows, at.y)) };
            let c = if at.x >= grid.left() && at.x < grid.right() { table_layout::span_index(&columns, at.x) }
                else { last.map(|h| h.2).or_else(|| table_layout::span_index(&columns, at.x)) };
            Some((block, r?, c?))
        }).flatten();
        let changed = match (current, next) {
            (a, b) if a == b => false,
            (Some((owner, ..)), None) => owner == block,
            _ => true,
        };
        if changed {
            HOVER.with(|h| h.set(next)); editor.update(cx, |_, cx| cx.notify());
        }
    });
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub(super) enum TableSel { Row(usize), Column(usize), Whole, Cell(usize, usize) }
impl TableSel {
    pub fn covers(self, row: usize, column: usize) -> bool {
        match self { TableSel::Row(r) => r == row, TableSel::Column(c) => c == column,
            TableSel::Whole => true, TableSel::Cell(..) => false }
    }
}
#[derive(Clone, Copy)]
pub(super) struct TableMenu { pub block: u64, pub sel: TableSel, pub anchor: Point<Pixels> }
#[derive(Clone, Copy)]
pub(super) struct TableDrag {
    pub block: u64, pub sel: TableSel, pub start: Point<Pixels>, pub moved: bool,
    pub position: Option<Point<Pixels>>,
}
#[derive(Clone)]
pub(super) struct TableChrome {
    pub block: u64, pub bounds: Bounds<Pixels>,
    pub columns: Vec<(Pixels, Pixels)>, pub rows: Vec<(Pixels, Pixels)>, pub cell: (usize, usize),
    /// Visible editor area; controls that would leave it are clipped or hidden, never flipped.
    pub viewport: Bounds<Pixels>,
}
impl TableChrome {
    /// Keep the insertion edge and final destination tied to the same nearest-center rule.
    pub fn drop_target(&self, drag: TableDrag, position: Point<Pixels>) -> Option<(usize, Pixels)> {
        if self.block != drag.block { return None; }
        let (spans, from, at) = match drag.sel {
            TableSel::Row(from) => (&self.rows, from, position.y),
            TableSel::Column(from) => (&self.columns, from, position.x),
            _ => return None,
        };
        spans.get(from)?;
        let (to, &(start, end)) = spans.iter().enumerate().min_by(|a, b| {
            let da = (((a.1.0 + a.1.1) / 2.) - at).abs();
            let db = (((b.1.0 + b.1.1) / 2.) - at).abs();
            da.partial_cmp(&db).unwrap_or(std::cmp::Ordering::Equal)
        })?;
        (from != to).then_some((to, if to < from { start } else { end }))
    }
}
#[derive(Clone, Copy, PartialEq, Debug)]
pub(super) enum TableCmd {
    InsertRowAbove, InsertRowBelow, InsertColLeft, InsertColRight,
    MoveRowUp, MoveRowDown, MoveColLeft, MoveColRight,
    Align(CellAlignment), ClearRow, ClearColumn, DeleteRow, DeleteColumn,
    DeleteTable, SelectTable, CopyTable,
    DuplicateRow, DuplicateColumn, AppendRow, AppendColumn,
}
impl TableCmd {
    pub fn label(self) -> &'static str {
        match self {
            TableCmd::InsertRowAbove => tr("上方插入行", "Insert row above"),
            TableCmd::InsertRowBelow | TableCmd::AppendRow => tr("下方插入行", "Insert row below"),
            TableCmd::InsertColLeft => tr("左侧插入列", "Insert column left"),
            TableCmd::InsertColRight | TableCmd::AppendColumn => tr("右侧插入列", "Insert column right"),
            TableCmd::MoveRowUp => tr("上移行", "Move row up"), TableCmd::MoveRowDown => tr("下移行", "Move row down"),
            TableCmd::MoveColLeft => tr("左移列", "Move column left"), TableCmd::MoveColRight => tr("右移列", "Move column right"),
            TableCmd::Align(CellAlignment::Left) => tr("左对齐", "Align left"),
            TableCmd::Align(CellAlignment::Center) => tr("居中对齐", "Align center"),
            TableCmd::Align(CellAlignment::Right) => tr("右对齐", "Align right"),
            TableCmd::ClearRow | TableCmd::ClearColumn => tr("清空内容", "Clear contents"),
            TableCmd::DeleteRow => tr("删除行", "Delete row"), TableCmd::DeleteColumn => tr("删除列", "Delete column"),
            TableCmd::DeleteTable => tr("删除表格", "Delete table"), TableCmd::SelectTable => tr("选中表格", "Select table"),
            TableCmd::CopyTable => tr("复制表格", "Copy table"),
            TableCmd::DuplicateRow => tr("复制行", "Duplicate row"), TableCmd::DuplicateColumn => tr("复制列", "Duplicate column"),
        }
    }
    pub fn icon(self) -> crate::icons::Icon {
        use crate::icons::Icon;
        match self {
            TableCmd::InsertColLeft => Icon::ColInsertLeft,
            TableCmd::InsertColRight | TableCmd::AppendColumn => Icon::ColInsertRight,
            TableCmd::DeleteColumn => Icon::ColDelete, TableCmd::DeleteRow => Icon::RowDelete,
            TableCmd::InsertRowAbove => Icon::RowInsertAbove,
            TableCmd::InsertRowBelow | TableCmd::AppendRow => Icon::RowInsertBelow,
            TableCmd::MoveRowUp => Icon::ArrowUp, TableCmd::MoveRowDown => Icon::ArrowDown,
            TableCmd::MoveColLeft => Icon::ChevronLeft, TableCmd::MoveColRight => Icon::ChevronRight,
            TableCmd::Align(CellAlignment::Left) => Icon::AlignLeft, TableCmd::Align(CellAlignment::Center) => Icon::AlignCenter,
            TableCmd::Align(CellAlignment::Right) => Icon::AlignRight, TableCmd::ClearRow | TableCmd::ClearColumn => Icon::Eraser,
            TableCmd::DeleteTable => Icon::Trash, TableCmd::SelectTable => Icon::SelectAll,
            TableCmd::CopyTable => Icon::Copy, TableCmd::DuplicateRow | TableCmd::DuplicateColumn => Icon::Duplicate,
        }
    }
    pub fn danger(self) -> bool { matches!(self, TableCmd::DeleteTable) }
    pub fn disabled(self, rows: usize, columns: usize) -> bool {
        matches!((self, rows, columns), (TableCmd::DeleteRow, 0..=1, _) | (TableCmd::DeleteColumn, _, 0..=1))
    }
}
/// Menu contents for each kind of selection, in display order. `None` separates groups.
pub(super) fn menu_items(sel: TableSel, _rows: usize, _columns: usize) -> Vec<Option<TableCmd>> {
    use TableCmd::*;
    match sel {
        TableSel::Column(_) => vec![Some(InsertColLeft), Some(InsertColRight), Some(DeleteColumn)],
        TableSel::Row(_) => vec![Some(InsertRowAbove), Some(InsertRowBelow), Some(DeleteRow)],
        TableSel::Cell(..) => vec![Some(InsertRowAbove), Some(InsertRowBelow), Some(InsertColLeft), Some(InsertColRight),
            None, Some(DeleteRow), Some(DeleteColumn), None, Some(SelectTable), Some(CopyTable), Some(DeleteTable)],
        TableSel::Whole => vec![Some(SelectTable), Some(CopyTable), None, Some(DeleteTable)],
    }
}

impl Editor {
    pub(in crate::editor) fn table_dimensions(&self, id: u64) -> Option<(usize, usize)> {
        let table = self.doc.get(id)?.meta.table.as_ref()?; Some((table.rows.len(), table.columns))
    }
    pub(in crate::editor) fn cell_of_caret(&self) -> Option<(u64, usize, usize)> {
        let at = self.caret.head; let table = self.doc.get(at.block)?.meta.table.as_ref()?;
        table.cell_ranges().into_iter().find(|(_, _, body)| body.start <= at.offset && at.offset <= body.end)
            .map(|(r, c, _)| (at.block, r, c))
    }
    fn place_in_cell(&mut self, id: u64, row: usize, column: usize) {
        let Some(table) = self.doc.get(id).and_then(|b| b.meta.table.as_ref()) else { return };
        let Some((_, _, body)) = table.cell_ranges().into_iter().find(|(r, c, _)| *r == row && *c == column) else { return };
        let at = Position { block: id, offset: body.start };
        self.caret.anchor = at; self.caret.head = at; self.caret.composing = None;
        self.preferred_x = None; self.pending_vertical = None;
    }
    /// Applies one structural edit as a single undo step. `edit` returns the cell to place the caret in.
    pub(in crate::editor) fn table_edit(&mut self, id: u64, edit: impl FnOnce(&mut Table) -> Option<(usize, usize)>, cx: &mut Context<Self>) {
        self.commit_composition(cx);
        let Some(index) = self.doc.index_of(id) else { return };
        let Some(table) = self.doc.get(id).and_then(|b| b.meta.table.clone()) else { return };
        let mut next = table;
        if !next.to_pipe() {
            self.status = tr("这个 HTML 表格含有合并单元格、样式、列宽或特殊表头，暂不支持直接增删行列", "This HTML table has merged cells or custom styles and cannot be edited structurally").into(); cx.notify(); return;
        }
        let Some((row, column)) = edit(&mut next) else { return };
        next.finish_structure();
        let before = self.snapshot();
        if let Some(block) = self.doc.get_mut(id) { block.meta.table = Some(next); block.sync_table_text(); }
        self.doc.refresh_block(id);
        self.place_in_cell(id, row, column);
        self.table_sel = None; self.table_menu = None; self.toolbar_anchor = None;
        self.record_changes(before, vec![ListChange { start: index, before: 1, after: 1 }], true, cx);
    }
    pub(in crate::editor) fn select_whole_table(&mut self, id: u64, cx: &mut Context<Self>) {
        let Some(len) = self.doc.get(id).filter(|b| b.meta.table.is_some()).map(|b| b.text.len()) else { return };
        self.caret.anchor = Position { block: id, offset: 0 }; self.caret.head = Position { block: id, offset: len };
        self.table_sel = Some((id, TableSel::Whole)); self.table_menu = None; cx.notify();
    }
    pub(in crate::editor) fn table_command(&mut self, id: u64, sel: TableSel, cmd: TableCmd, cx: &mut Context<Self>) {
        let row = match sel { TableSel::Row(r) | TableSel::Cell(r, _) => Some(r), _ => None };
        let column = match sel { TableSel::Column(c) | TableSel::Cell(_, c) => Some(c), _ => None };
        self.table_menu = None;
        match cmd {
            TableCmd::SelectTable => self.select_whole_table(id, cx),
            TableCmd::CopyTable => {
                if let Some(table) = self.doc.get(id).and_then(|b| b.meta.table.as_ref()) {
                    let mut text = String::new();
                    super::markdown::serialize_table(table, &mut text);
                    cx.write_to_clipboard(ClipboardItem::new_string(text));
                    self.status = tr("已复制表格", "Table copied").into();
                }
                cx.notify();
            }
            TableCmd::DeleteTable => {
                let Some(index) = self.doc.index_of(id) else { return };
                self.commit_composition(cx); let before = self.snapshot();
                if let Some(block) = self.doc.get_mut(id) { block.meta.table = None; block.text.clear(); block.spans.clear(); }
                self.doc.refresh_block(id);
                let at = Position { block: id, offset: 0 };
                self.caret.anchor = at; self.caret.head = at; self.caret.composing = None;
                self.table_sel = None;
                self.record_changes(before, vec![ListChange { start: index, before: 1, after: 1 }], true, cx);
            }
            TableCmd::InsertRowAbove => if let Some(r) = row { let c = column.unwrap_or(0);
                self.table_edit(id, move |t| Some((t.insert_row(r), c.min(t.columns.saturating_sub(1)))), cx) },
            TableCmd::InsertRowBelow => if let Some(r) = row { let c = column.unwrap_or(0);
                self.table_edit(id, move |t| Some((t.insert_row(r + 1), c.min(t.columns.saturating_sub(1)))), cx) },
            TableCmd::InsertColLeft => if let Some(c) = column { let r = row.unwrap_or(0);
                self.table_edit(id, move |t| Some((r.min(t.rows.len() - 1), t.insert_column(c))), cx) },
            TableCmd::InsertColRight => if let Some(c) = column { let r = row.unwrap_or(0);
                self.table_edit(id, move |t| Some((r.min(t.rows.len() - 1), t.insert_column(c + 1))), cx) },
            TableCmd::DuplicateRow => if let Some(r) = row {
                self.table_edit(id, move |t| t.duplicate_row(r).map(|n| (n, 0)), cx) },
            TableCmd::DuplicateColumn => if let Some(c) = column {
                self.table_edit(id, move |t| t.duplicate_column(c).map(|n| (0, n)), cx) },
            TableCmd::AppendRow => self.table_edit(id, |t| { let at = t.rows.len(); Some((t.insert_row(at), 0)) }, cx),
            TableCmd::AppendColumn => self.table_edit(id, |t| { let at = t.columns; Some((0, t.insert_column(at))) }, cx),
            TableCmd::MoveRowUp => if let Some(r) = row.filter(|r| *r > 0) {
                self.table_edit(id, move |t| t.move_row(r, r - 1).then_some((r - 1, 0)), cx) },
            TableCmd::MoveRowDown => if let Some(r) = row {
                self.table_edit(id, move |t| t.move_row(r, r + 1).then_some((r + 1, 0)), cx) },
            TableCmd::MoveColLeft => if let Some(c) = column.filter(|c| *c > 0) {
                self.table_edit(id, move |t| t.move_column(c, c - 1).then_some((0, c - 1)), cx) },
            TableCmd::MoveColRight => if let Some(c) = column {
                self.table_edit(id, move |t| t.move_column(c, c + 1).then_some((0, c + 1)), cx) },
            TableCmd::Align(alignment) => if let Some(c) = column {
                self.table_edit(id, move |t| { t.set_column_alignment(c, alignment); Some((0, c)) }, cx) },
            TableCmd::ClearRow => if let Some(r) = row {
                self.table_edit(id, move |t| { t.clear_row(r); Some((r, 0)) }, cx) },
            TableCmd::ClearColumn => if let Some(c) = column {
                self.table_edit(id, move |t| { t.clear_column(c); Some((0, c)) }, cx) },
            TableCmd::DeleteRow => if let Some(r) = row {
                self.table_edit(id, move |t| t.delete_row(r).then(|| (r.min(t.rows.len() - 1), 0)), cx) },
            TableCmd::DeleteColumn => if let Some(c) = column {
                self.table_edit(id, move |t| t.delete_column(c).then(|| (0, c.min(t.columns - 1))), cx) },
        }
    }
    /// Moves a row or column to where a drag ended.
    pub(in crate::editor) fn table_drop(&mut self, drag: TableDrag, position: Point<Pixels>, cx: &mut Context<Self>) {
        if self.path.is_none() || !self.list.viewport_bounds().contains(&position) { return; }
        let Some((to, _)) = self.table_chrome.as_ref().and_then(|c| c.drop_target(drag, position)) else { return };
        match drag.sel {
            TableSel::Row(from) => {
                self.table_edit(drag.block, move |t| t.move_row(from, to).then_some((to, 0)), cx);
            },
            TableSel::Column(from) => {
                self.table_edit(drag.block, move |t| t.move_column(from, to).then_some((0, to)), cx);
            },
            _ => {}
        }
    }
    pub(in crate::editor) fn open_table_menu(&mut self, id: u64, sel: TableSel, anchor: Point<Pixels>, cx: &mut Context<Self>) {
        // Row menus open right of the row handle, column menus 4px below and left-aligned with the column handle.
        let chrome = self.table_chrome.as_ref().filter(|c| c.block == id);
        let anchor = match (sel, chrome) {
            (TableSel::Row(r), Some(c)) => c.rows.get(r)
                .map_or(anchor, |s| point(c.bounds.left() + px(11.), (s.0 + s.1) / 2. - px(13.))),
            (TableSel::Column(i), Some(c)) => c.columns.get(i)
                .map_or(anchor, |s| point((s.0 + s.1) / 2. - px(13.), c.bounds.top() + px(11.))),
            _ => anchor,
        };
        self.table_menu = Some(TableMenu { block: id, sel, anchor }); cx.notify();
    }
    /// Shift+F10: the row and column options for the cell holding the caret.
    pub(in crate::editor) fn open_cell_menu(&mut self, cx: &mut Context<Self>) {
        let Some((id, r, c)) = self.cell_of_caret() else { return };
        let Some(layout) = self.layouts.get(&id) else { return };
        let (rows, columns) = table_layout::spans(layout);
        let (Some(x), Some(y)) = (columns.get(c).map(|s| s.0), rows.get(r).map(|s| s.1)) else { return };
        self.open_table_menu(id, TableSel::Cell(r, c), point(x, y), cx);
    }
    /// Reads geometry published by the final paint, so overlays never use prepaint attempts.
    /// Shown for the hovered table; an open menu, drag or selection keeps the chrome visible.
    pub(in crate::editor) fn table_chrome_geometry(&self, window: &Window) -> Option<TableChrome> {
        if self.path.is_none() { return None; }
        let frozen = self.table_menu.map(|m| m.block)
            .or(self.table_drag.map(|d| d.block))
            .or(self.table_sel.map(|(id, _)| id));
        let (id, cell) = match frozen {
            Some(id) => (id, hovered().filter(|h| h.0 == id && self.table_menu.is_none() && self.table_drag.is_none())
                .map(|h| (h.1, h.2))
                .or_else(|| self.table_chrome.as_ref().filter(|c| c.block == id).map(|c| c.cell))
                .or_else(|| self.cell_of_caret().filter(|x| x.0 == id).map(|x| (x.1, x.2))).unwrap_or((0, 0))),
            None => { let (id, r, c) = hovered()?; (id, (r, c)) }
        };
        self.doc.get(id)?.meta.table.as_ref()?;
        let layout = self.layouts.get(&id)?;
        let bounds = table_layout::grid_rect(layout)?;
        // Hover is only refreshed on mouse move; after a scroll (Cmd+Up, caret jumps) re-check the pointer.
        let reach = Bounds::from_corners(bounds.origin - point(px(36.), px(32.)), bounds.bottom_right() + point(px(56.), px(36.)));
        if frozen.is_none() && !reach.contains(&window.mouse_position()) { return None; }
        let viewport = self.list.viewport_bounds();
        if bounds.bottom() < viewport.top() || bounds.top() > viewport.bottom() { return None; }
        let (rows, columns) = table_layout::spans(layout);
        if rows.is_empty() || columns.is_empty() { return None; }
        let cell = (cell.0.min(rows.len() - 1), cell.1.min(columns.len() - 1));
        Some(TableChrome { block: id, bounds, columns, rows, cell, viewport })
    }
}
