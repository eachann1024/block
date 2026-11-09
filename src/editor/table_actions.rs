//! Table structure commands, row/column selection, and the geometry the table chrome needs.
use gpui::{Bounds, Context, Entity, Pixels, Point, Window, point, px};
use crate::theme::tr;
use super::{input::transactions::ListChange, model::Table, table_layout, view::{Editor, layout::TextLayout, selection::Position}};

mod cmd;
pub(super) use cmd::{TableCmd, menu_items};

fn table_slots(items: &[Option<TableCmd>], dims: (usize, usize)) -> Vec<crate::ui::menu_nav::Slot> {
    items.iter().map(|item| match item {
        Some(cmd) if !cmd.disabled(dims.0, dims.1) => crate::ui::menu_nav::Slot::Item,
        _ => crate::ui::menu_nav::Slot::Skip,
    }).collect()
}

thread_local! {
    /// (table block, row, column) under the pointer; the chrome follows hover, not the caret.
    static HOVER: std::cell::Cell<Option<(u64, usize, usize)>> = const { std::cell::Cell::new(None) };
    static HITBOXES: std::cell::RefCell<std::collections::HashMap<(gpui::EntityId, u64), gpui::Hitbox>> = Default::default();
}
pub(super) fn hovered() -> Option<(u64, usize, usize)> { HOVER.with(|h| h.get()) }
pub(super) fn reach_rect(layout: &TextLayout) -> Option<Bounds<Pixels>> {
    let zoom = table_layout::zoom_of(layout);
    let grid = table_layout::grid_rect(layout)?.intersect(&table_layout::visible(layout));
    Some(Bounds::from_corners(grid.origin - point(px(16. * zoom), px(14. * zoom)), grid.bottom_right() + point(px(50. * zoom), px(24. * zoom))))
}
pub(super) fn unoccluded(editor: gpui::EntityId, block: u64, rect: Bounds<Pixels>, window: &Window) -> bool {
    HITBOXES.with(|h| {
        let map = h.borrow();
        let Some(hb) = map.get(&(editor, block)) else { return true };
        let mask = hb.bounds.intersect(&hb.content_mask.bounds);
        let pts = [rect.origin, point(rect.right() - px(1.), rect.top()), point(rect.left(), rect.bottom() - px(1.)),
            point(rect.right() - px(1.), rect.bottom() - px(1.)), rect.center()];
        pts.into_iter().filter(|p| mask.contains(p)).all(|p| hb.is_hovered_at(p, window))
    })
}
/// Registered while a table paints. The reach covers the outside handles and the extend buttons,
/// so moving from a cell onto its handle keeps the chrome. Off-grid axes keep the last index.
pub(super) fn track_hover(editor: Entity<Editor>, block: u64, layout: &TextLayout, hitbox: Option<gpui::Hitbox>, window: &mut Window) {
    // Only the part inside the horizontal scroll window counts as the table.
    let Some(grid) = table_layout::grid_rect(layout).map(|g| g.intersect(&table_layout::visible(layout))) else { return };
    let Some(reach) = reach_rect(layout) else { return };
    let (rows, columns) = table_layout::spans(layout);
    if let Some(hb) = &hitbox {
        HITBOXES.with(|h| { h.borrow_mut().insert((editor.entity_id(), block), hb.clone()); });
        if hovered().is_some_and(|h| h.0 == block) && !hb.should_handle_scroll(window) {
            HOVER.with(|h| h.set(None));
        }
    }
    window.on_mouse_event(move |event: &gpui::MouseMoveEvent, phase, window, cx| {
        if phase != gpui::DispatchPhase::Bubble || event.dragging() { return; }
        let at = event.position; let current = hovered();
        let unoccluded = hitbox.as_ref().is_none_or(|hb| hb.should_handle_scroll(window));
        let next = (unoccluded && reach.contains(&at)).then(|| {
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
pub(super) struct TableMenu { pub block: u64, pub sel: TableSel, pub spec: crate::dropdown::PlacementSpec }
#[derive(Clone, Copy)]
pub(super) struct TableDrag {
    pub block: u64, pub sel: TableSel, pub start: Point<Pixels>, pub moved: bool,
    pub position: Option<Point<Pixels>>,
    /// Row or column the drop would land on; the indicator line and the final move both read this.
    pub target: Option<usize>,
}
#[derive(Clone)]
pub(super) struct TableChrome {
    /// `bounds` is the grid clipped to the horizontal scroll window; spans keep their full extent.
    pub block: u64, pub bounds: Bounds<Pixels>,
    pub columns: Vec<(Pixels, Pixels)>, pub rows: Vec<(Pixels, Pixels)>, pub cell: (usize, usize),
    /// Visible editor area; controls that would leave it are clipped or hidden, never flipped.
    pub viewport: Bounds<Pixels>,
}
impl TableChrome {
    fn axis(&self, sel: TableSel) -> Option<(&[(Pixels, Pixels)], usize)> {
        match sel { TableSel::Row(from) => Some((&self.rows, from)), TableSel::Column(from) => Some((&self.columns, from)), _ => None }
    }
    /// The row or column whose centre is nearest the pointer on the current geometry; `None` when
    /// that is the dragged one itself.
    pub fn target_at(&self, drag: TableDrag, position: Point<Pixels>) -> Option<usize> {
        if self.block != drag.block { return None; }
        let (spans, from) = self.axis(drag.sel)?;
        let at = if matches!(drag.sel, TableSel::Row(_)) { position.y } else { position.x };
        spans.get(from)?;
        let (to, _) = spans.iter().enumerate().min_by(|a, b| {
            let da = (((a.1.0 + a.1.1) / 2.) - at).abs();
            let db = (((b.1.0 + b.1.1) / 2.) - at).abs();
            da.partial_cmp(&db).unwrap_or(std::cmp::Ordering::Equal)
        })?;
        (from != to).then_some(to)
    }
    /// Where the insertion line sits for a move to `to`, on the current geometry.
    pub fn edge(&self, sel: TableSel, to: usize) -> Option<Pixels> {
        let (spans, from) = self.axis(sel)?;
        let &(start, end) = spans.get(to)?;
        Some(if to < from { start } else { end })
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
        if matches!(cmd, TableCmd::InsertColLeft | TableCmd::InsertColRight | TableCmd::DuplicateColumn | TableCmd::AppendColumn
            | TableCmd::MoveColLeft | TableCmd::MoveColRight | TableCmd::DeleteColumn) {
            table_layout::reset_manual(cx.entity_id(), id);
        }
        match cmd {
            TableCmd::SelectTable => self.select_whole_table(id, cx),
            TableCmd::CopyTable => {
                if let Some(block) = self.doc.get(id).filter(|b| b.meta.table.is_some()) {
                    cx.write_to_clipboard(super::paste_clipboard::blocks_item(vec![block.clone()]));
                    self.status = tr("已复制表格", "Table copied").into();
                }
                cx.notify();
            }
            TableCmd::FitWidth => { table_layout::toggle_fit(cx.entity_id(), id); self.relayout_table(id, cx); }
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
        if self.path.is_none() { return; }
        let Some(to) = drag.target.or_else(|| self.table_chrome.as_ref().and_then(|c| c.target_at(drag, position))) else { return };
        match drag.sel {
            TableSel::Row(from) => {
                self.table_edit(drag.block, move |t| t.move_row(from, to).then_some((to, 0)), cx);
            },
            TableSel::Column(from) => {
                table_layout::reset_manual(cx.entity_id(), drag.block);
                self.table_edit(drag.block, move |t| t.move_column(from, to).then_some((0, to)), cx);
            },
            _ => {}
        }
    }
    /// Recomputes the open menu's trigger from the current chrome so a resize does not
    /// leave it on the rectangle captured when it opened. A point anchor only refreshes
    /// its viewport. A right-aligned cell menu is the grid's more button.
    pub(in crate::editor) fn live_table_menu_spec(&self, menu: &TableMenu) -> crate::dropdown::PlacementSpec {
        let viewport = self.list.viewport_bounds();
        let frozen = menu.spec;
        let point_anchor = frozen.trigger.size.width <= px(0.) && frozen.trigger.size.height <= px(0.);
        if point_anchor {
            return frozen.viewport(viewport);
        }
        let Some(chrome) = self.table_chrome.as_ref().filter(|chrome| chrome.block == menu.block) else {
            return frozen.viewport(viewport);
        };
        let zoom = super::zoom::zoom_factor(self.minimal);
        let rect = match menu.sel {
            TableSel::Row(row) => {
                let Some(span) = chrome.rows.get(row) else { return frozen.viewport(viewport); };
                let mid_y = (span.0 + span.1 + if row + 1 < chrome.rows.len() { px(1.) } else { px(0.) }) / 2.;
                Bounds::new(point(chrome.bounds.left() - px(6.5 * zoom), mid_y - px(12. * zoom)), gpui::size(px(13. * zoom), px(24. * zoom)))
            }
            TableSel::Column(col) => {
                let Some(span) = chrome.columns.get(col) else { return frozen.viewport(viewport); };
                let (seen_l, seen_r) = (span.0.max(chrome.bounds.left()), span.1.min(chrome.bounds.right()));
                let x = ((seen_l + seen_r + if col + 1 < chrome.columns.len() && seen_r == span.1 { px(1.) } else { px(0.) }) / 2. - px(12. * zoom)).max(chrome.bounds.left());
                Bounds::new(point(x, chrome.bounds.top() - px(6.5 * zoom)), gpui::size(px(24. * zoom), px(14. * zoom)))
            }
            TableSel::Cell(_, _) if frozen.align_x == crate::dropdown::AlignX::Right => {
                let btn_sz = px(26. * zoom);
                Bounds::new(point(chrome.bounds.right() + px(22. * zoom), chrome.bounds.top()), gpui::size(btn_sz, btn_sz))
            }
            TableSel::Cell(row, col) => {
                let (Some(column), Some(row_span)) = (chrome.columns.get(col), chrome.rows.get(row)) else {
                    return frozen.viewport(viewport);
                };
                Bounds::from_corners(point(column.0, row_span.0), point(column.1, row_span.1))
            }
            TableSel::Whole => return frozen.viewport(viewport),
        };
        crate::dropdown::PlacementSpec::below(rect).viewport(viewport).gap(frozen.gap).margin(frozen.margin).align_x(frozen.align_x)
    }
    pub(in crate::editor) fn open_table_menu_spec(&mut self, id: u64, sel: TableSel, spec: crate::dropdown::PlacementSpec, cx: &mut Context<Self>) {
        let dims = self.table_dimensions(id).unwrap_or((1, 1));
        super::menu_keys::arm_table(&table_slots(&menu_items(sel, dims.0, dims.1), dims));
        self.table_menu = Some(TableMenu { block: id, sel, spec }); cx.notify();
    }
    pub(super) fn step_table_menu(&mut self, down: bool, cx: &mut Context<Self>) -> bool {
        let Some(menu) = self.table_menu else { return false };
        let dims = self.table_dimensions(menu.block).unwrap_or((1, 1));
        let items = menu_items(menu.sel, dims.0, dims.1);
        let slots = table_slots(&items, dims);
        if !crate::ui::menu_nav::has_item(&slots) { return false; }
        let cur = super::menu_keys::table_index();
        let next = crate::ui::menu_nav::step(cur, &slots, down);
        let changed = next != crate::ui::menu_nav::clamp_index(cur, &slots);
        super::menu_keys::set_table_index(next);
        if changed { crate::ui::menu_nav::reveal_or_defer(&crate::ui::menu_nav::scroll("table"), next, slots.len()); }
        cx.notify();
        true
    }
    pub(super) fn activate_table_menu(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(menu) = self.table_menu else { return false };
        let dims = self.table_dimensions(menu.block).unwrap_or((1, 1));
        let items = menu_items(menu.sel, dims.0, dims.1);
        let slots = table_slots(&items, dims);
        if !crate::ui::menu_nav::has_item(&slots) { return false; }
        let index = crate::ui::menu_nav::clamp_index(super::menu_keys::table_index(), &slots);
        super::menu_keys::set_table_index(index);
        let Some(Some(cmd)) = items.get(index).copied() else { return true; };
        if cmd.disabled(dims.0, dims.1) { return true; }
        let (id, sel) = (menu.block, menu.sel);
        self.table_command(id, sel, cmd, cx);
        window.focus(&self.focus, cx);
        true
    }
    pub(in crate::editor) fn open_table_menu(&mut self, id: u64, sel: TableSel, anchor: Point<Pixels>, cx: &mut Context<Self>) {
        let zoom = super::zoom::zoom_factor(self.minimal);
        let vp = self.list.viewport_bounds();
        let chrome = self.table_chrome.as_ref().filter(|c| c.block == id);
        let fallback = crate::dropdown::PlacementSpec::point(anchor).viewport(vp);
        let spec = match (sel, chrome) {
            (TableSel::Row(r), Some(c)) => c.rows.get(r).map_or(fallback, |s| {
                let mid_y = (s.0 + s.1 + if r + 1 < c.rows.len() { px(1.) } else { px(0.) }) / 2.;
                let rect = Bounds::new(point(c.bounds.left() - px(6.5 * zoom), mid_y - px(12. * zoom)), gpui::size(px(13. * zoom), px(24. * zoom)));
                crate::dropdown::PlacementSpec::below(rect).viewport(vp).gap(px(4.))
            }),
            (TableSel::Column(i), Some(c)) => c.columns.get(i).map_or(fallback, |s| {
                let (seen_l, seen_r) = (s.0.max(c.bounds.left()), s.1.min(c.bounds.right()));
                let x = ((seen_l + seen_r + if i + 1 < c.columns.len() && seen_r == s.1 { px(1.) } else { px(0.) }) / 2. - px(12. * zoom)).max(c.bounds.left());
                let rect = Bounds::new(point(x, c.bounds.top() - px(6.5 * zoom)), gpui::size(px(24. * zoom), px(14. * zoom)));
                crate::dropdown::PlacementSpec::below(rect).viewport(vp).gap(px(4.))
            }),
            _ => fallback,
        };
        self.open_table_menu_spec(id, sel, spec, cx);
    }
    /// Shift+F10: the row and column options for the cell holding the caret.
    pub(in crate::editor) fn open_cell_menu(&mut self, cx: &mut Context<Self>) {
        let Some((id, r, c)) = self.cell_of_caret() else { return };
        let Some(layout) = self.layouts.get(&id) else { return };
        let (rows, columns) = table_layout::spans(layout);
        let (Some(cs), Some(rs)) = (columns.get(c), rows.get(r)) else { return };
        let rect = Bounds::from_corners(point(cs.0, rs.0), point(cs.1, rs.1));
        let spec = crate::dropdown::PlacementSpec::below(rect).viewport(self.list.viewport_bounds()).gap(px(4.));
        self.open_table_menu_spec(id, TableSel::Cell(r, c), spec, cx);
    }
    /// Reads geometry published by the final paint, so overlays never use prepaint attempts.
    /// Shown for the hovered table; an open menu, drag or selection keeps the chrome visible.
    pub(in crate::editor) fn table_chrome_geometry(&self, editor_id: gpui::EntityId, window: &Window) -> Option<TableChrome> {
        if self.path.is_none() || self.dragging.is_some() { return None; }
        let frozen = self.table_menu.map(|m| m.block)
            .or(self.table_drag.map(|d| d.block)).or(self.table_extend.as_ref().map(|e| e.block))
            .or(super::table_resize::active().map(|r| r.block))
            .or(self.table_sel.map(|(id, _)| id));
        let (id, cell) = match frozen {
            Some(id) => (id, hovered().filter(|h| h.0 == id && self.table_menu.is_none() && self.table_drag.is_none() && self.table_extend.is_none() && super::table_resize::active().is_none())
                .map(|h| (h.1, h.2))
                .or_else(|| self.table_chrome.as_ref().filter(|c| c.block == id).map(|c| c.cell))
                .or_else(|| self.cell_of_caret().filter(|x| x.0 == id).map(|x| (x.1, x.2))).unwrap_or((0, 0))),
            None => { let (id, r, c) = hovered()?; (id, (r, c)) }
        };
        self.doc.get(id)?.meta.table.as_ref()?;
        let layout = self.layouts.get(&id)?;
        let bounds = table_layout::grid_rect(layout)?.intersect(&table_layout::visible(layout));
        // Hover is only refreshed on mouse move; after a scroll (Cmd+Up, caret jumps) re-check the pointer.
        let reach = reach_rect(layout)?;
        let pointer_ok = HITBOXES.with(|h| h.borrow().get(&(editor_id, id)).map(|hb| hb.should_handle_scroll(window))).unwrap_or(true);
        if frozen.is_none() && (!pointer_ok || !reach.contains(&window.mouse_position())) { return None; }
        let viewport = self.list.viewport_bounds();
        if bounds.bottom() < viewport.top() || bounds.top() > viewport.bottom() { return None; }
        let (rows, columns) = table_layout::spans(layout);
        if rows.is_empty() || columns.is_empty() { return None; }
        let cell = (cell.0.min(rows.len() - 1), cell.1.min(columns.len() - 1));
        Some(TableChrome { block: id, bounds, columns, rows, cell, viewport })
    }
}
