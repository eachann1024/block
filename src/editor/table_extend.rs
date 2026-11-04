//! Dragging the add-column bar: the table grows or shrinks live while the button is held.
//! Every step is derived from the table as it was at mouse-down, so dragging back restores removed
//! columns with their content; the document only keeps the result, as one undo step, on release.
use gpui::{Context, Pixels, Point, px};
use crate::theme::tr;
use super::{input::transactions::{ListChange, Snapshot}, model::Table, table_actions::{TableCmd, TableSel},
    view::{Editor, selection::Position}};

const MAX_COLUMNS: usize = 64;

pub(super) struct TableExtend {
    pub block: u64, start_x: Pixels, unit: Pixels, original: Table, before: Snapshot,
    original_count: usize, count: usize, moved: bool,
}

impl Editor {
    pub(super) fn begin_extend(&mut self, id: u64, start: Point<Pixels>, cx: &mut Context<Self>) {
        self.commit_composition(cx);
        let Some(mut original) = self.doc.get(id).and_then(|b| b.meta.table.clone()) else { return };
        if !original.to_pipe() {
            self.status = tr("这个 HTML 表格含有合并单元格、样式、列宽或特殊表头，暂不支持直接增删行列", "This HTML table has merged cells or custom styles and cannot be edited structurally").into();
            cx.notify(); return;
        }
        let widths = self.table_chrome.as_ref().filter(|c| c.block == id).map(|c| c.columns.iter().map(|s| s.1 - s.0).collect::<Vec<_>>());
        let unit = widths.filter(|w| !w.is_empty()).map_or(px(120.), |w| w.iter().fold(px(0.), |a, b| a + *b) / w.len() as f32)
            .clamp(px(60.), px(240.));
        let (original_count, before) = (original.columns, self.snapshot());
        self.table_sel = None; self.table_menu = None;
        self.table_extend = Some(TableExtend { block: id, start_x: start.x, unit, original, before, original_count, count: original_count, moved: false });
        cx.notify();
    }
    pub(super) fn extend_move(&mut self, x: Pixels, cx: &mut Context<Self>) {
        let Some(ext) = self.table_extend.as_mut() else { return };
        let delta = (x - ext.start_x) / ext.unit;
        ext.moved |= (x - ext.start_x).abs() > px(4.);
        if !ext.moved { return; }
        let count = (ext.original_count as i64 + delta.round() as i64).clamp(1, MAX_COLUMNS as i64) as usize;
        if count == ext.count { return; }
        ext.count = count;
        let (id, mut table) = (ext.block, ext.original.clone());
        while table.columns > count { let at = table.columns - 1; table.delete_column(at); }
        while table.columns < count { let at = table.columns; table.insert_column(at); }
        table.finish_structure();
        if let Some(block) = self.doc.get_mut(id) { block.meta.table = Some(table); block.sync_table_text(); }
        self.doc.refresh_block(id);
        let at = Position { block: id, offset: 0 };
        self.caret.anchor = at; self.caret.head = at; self.caret.composing = None;
        self.layouts.clear(); cx.notify();
    }
    /// Release: a plain click appends one column; a drag keeps the previewed result as one undo step.
    pub(super) fn finish_extend(&mut self, cx: &mut Context<Self>) {
        let Some(ext) = self.table_extend.take() else { return };
        if !ext.moved { self.table_command(ext.block, TableSel::Whole, TableCmd::AppendColumn, cx); return; }
        if ext.count == ext.original_count { self.restore_extend(ext, cx); return; }
        let Some(index) = self.doc.index_of(ext.block) else { return };
        self.toolbar_anchor = None;
        self.record_changes(ext.before, vec![ListChange { start: index, before: 1, after: 1 }], true, cx);
        cx.notify();
    }
    /// Abort (focus lost, button released elsewhere): the document goes back to how it was.
    pub(super) fn cancel_extend(&mut self) {
        if let Some(ext) = self.table_extend.take() {
            if ext.moved { self.doc = ext.before.doc; self.caret = ext.before.caret; self.layouts.clear(); }
        }
    }
    fn restore_extend(&mut self, ext: TableExtend, cx: &mut Context<Self>) {
        self.doc = ext.before.doc; self.caret = ext.before.caret; self.layouts.clear(); cx.notify();
    }
}
