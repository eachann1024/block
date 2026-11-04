//! Session width changes of a table (fit to page, dragged columns) and the re-measuring they need.
//! Dragging a column boundary sets that column's width live; double-clicking it restores auto width.
use std::cell::Cell;
use gpui::{Context, Pixels, px};
use super::{table_layout::{self, MAX_MANUAL, MIN_COL}, view::Editor};

#[derive(Clone, Copy)]
pub(super) struct TableResize { pub block: u64, pub col: usize, start_x: Pixels, start_width: f32, moved: bool }
thread_local! {
    static ACTIVE: Cell<Option<TableResize>> = const { Cell::new(None) };
}
/// The boundary drag in progress, if any.
pub(super) fn active() -> Option<TableResize> { ACTIVE.with(Cell::get) }
/// Forgets a drag in progress; widths already applied stay.
pub(super) fn end() { ACTIVE.with(|a| a.set(None)); }

impl Editor {
    /// Re-measures one table after its session widths changed: the list caches row heights and
    /// wrapping depends on the column widths.
    pub(super) fn relayout_table(&mut self, id: u64, cx: &mut Context<Self>) {
        if let Some(index) = self.doc.index_of(id) { self.list.splice(index..index + 1, 1); }
        self.layouts.remove(&id);
        cx.notify();
    }
    pub(super) fn begin_column_resize(&mut self, block: u64, col: usize, x: Pixels, width: f32, cx: &mut Context<Self>) {
        self.table_menu = None;
        ACTIVE.with(|a| a.set(Some(TableResize { block, col, start_x: x, start_width: width, moved: false })));
        cx.notify();
    }
    /// Double-click on a boundary: the column goes back to its automatic width.
    pub(super) fn reset_column_width(&mut self, block: u64, col: usize, cx: &mut Context<Self>) {
        let Some((_, columns)) = self.table_dimensions(block) else { return };
        table_layout::set_manual(cx.entity_id(), block, col, columns, None);
        self.relayout_table(block, cx);
    }
    pub(super) fn resize_move(&mut self, x: Pixels, cx: &mut Context<Self>) {
        let Some(mut drag) = active() else { return };
        drag.moved |= (x - drag.start_x).abs() > px(2.);
        ACTIVE.with(|a| a.set(Some(drag)));
        if !drag.moved { return; }
        let Some((_, columns)) = self.table_dimensions(drag.block) else { return };
        let mut width = (drag.start_width + f32::from(x - drag.start_x)).clamp(MIN_COL, MAX_MANUAL);
        let id = cx.entity_id();
        if table_layout::widths_of(id, drag.block).fit && columns > 1 {
            // Fitted tables keep the other columns on the page: leave each at least its minimum.
            if let Some(layout) = self.layouts.get(&drag.block) {
                let page = f32::from(table_layout::visible(layout.bounds).size.width);
                width = width.min((page - MIN_COL * (columns - 1) as f32).max(MIN_COL));
            }
        }
        table_layout::set_manual(id, drag.block, drag.col, columns, Some(width));
        self.relayout_table(drag.block, cx);
    }
    /// Mouse release: the widths were applied while dragging, so only the drag state ends. True if one was running.
    pub(super) fn finish_resize(&mut self, cx: &mut Context<Self>) -> bool {
        let running = active().is_some();
        end();
        if running { cx.notify(); }
        running
    }
}
