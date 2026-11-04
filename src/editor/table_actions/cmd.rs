//! Table commands and the menu contents offered for each kind of selection.
use crate::{editor::model::CellAlignment, theme::tr};
use super::TableSel;

#[allow(dead_code)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub(in crate::editor) enum TableCmd {
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
pub(in crate::editor) fn menu_items(sel: TableSel, _rows: usize, _columns: usize) -> Vec<Option<TableCmd>> {
    use TableCmd::*;
    match sel {
        TableSel::Column(_) => vec![Some(InsertColLeft), Some(InsertColRight), Some(DeleteColumn)],
        TableSel::Row(_) => vec![Some(InsertRowAbove), Some(InsertRowBelow), Some(DeleteRow)],
        TableSel::Cell(..) => vec![Some(InsertRowAbove), Some(InsertRowBelow), Some(InsertColLeft), Some(InsertColRight),
            None, Some(DeleteRow), Some(DeleteColumn), None, Some(SelectTable), Some(CopyTable), Some(DeleteTable)],
        TableSel::Whole => vec![Some(SelectTable), Some(CopyTable), None, Some(DeleteTable)],
    }
}
