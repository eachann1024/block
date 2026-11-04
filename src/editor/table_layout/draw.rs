//! Table painting: the pixel-snapped collapsed grid, cell fills and the accent selection tint.
use std::{collections::HashSet, ops::Range};
use gpui::{Bounds, Hsla, Pixels, Window, fill, point, px, size};
use crate::{editor::{table_actions::TableSel, view::layout::TextLayout}, theme::Theme};
use super::grid_rect;

fn snap(value: Pixels, scale: f32) -> Pixels { px((f32::from(value) * scale).round() / scale) }
/// Edges rounded to whole device pixels, so adjoining fills never leave seams and rules stay sharp.
pub(super) fn snapped(bounds: Bounds<Pixels>, scale: f32) -> Bounds<Pixels> {
    Bounds::from_corners(
        point(snap(bounds.left(), scale), snap(bounds.top(), scale)),
        point(snap(bounds.right(), scale), snap(bounds.bottom(), scale)))
}
/// Selected cells are the accent colour at 10% over whatever the cell shows.
fn select_tint() -> Hsla {
    if crate::theme::is_dark() { gpui::rgba(0x4c8bf52e).into() } else { gpui::rgba(0x2a78d61a).into() }
}

pub(in crate::editor) fn paint_background(layout: &TextLayout, window: &mut Window) {
    let scale = window.scale_factor();
    for cell in &layout.cells {
        if let Some(color) = cell.background { window.paint_quad(fill(snapped(cell.frame, scale), color)); }
    }
    paint_grid_lines(layout, window);
}
/// Collapsed 1px grid: every cell contributes its top and left edge, the outer frame closes it,
/// and a rule shared by several cells is painted once.
pub(in crate::editor) fn paint_grid_lines(layout: &TextLayout, window: &mut Window) {
    let border: Hsla = if crate::theme::is_dark() { Theme::ocean().menu_border } else { gpui::rgb(0xe8e5dc).into() };
    let Some(grid) = grid_rect(layout) else { return };
    let scale = window.scale_factor();
    let (grid, line) = (snapped(grid, scale), px(1.));
    let mut seen = HashSet::new();
    let mut rule = |window: &mut Window, rect: Bounds<Pixels>| {
        let device = |value: Pixels| (f32::from(value) * scale).round() as i32;
        let key = (device(rect.left()), device(rect.top()), device(rect.size.width), device(rect.size.height));
        if seen.insert(key) { window.paint_quad(fill(rect, border)); }
    };
    for cell in &layout.cells {
        let frame = snapped(cell.frame, scale);
        rule(window, Bounds::new(frame.origin, size(frame.size.width, line)));
        rule(window, Bounds::new(frame.origin, size(line, frame.size.height)));
    }
    rule(window, Bounds::new(point(grid.right() - line, grid.top()), size(line, grid.size.height)));
    rule(window, Bounds::new(point(grid.left(), grid.bottom() - line), size(grid.size.width, line)));
}
/// Row, column and whole-table selection: the covered cells get the accent tint, rules stay on top.
pub(in crate::editor) fn paint_selection(layout: &TextLayout, sel: TableSel, columns: usize, window: &mut Window) {
    let (scale, tint) = (window.scale_factor(), select_tint());
    for (i, cell) in layout.cells.iter().enumerate() {
        if sel.covers(i / columns, i % columns) { window.paint_quad(fill(snapped(cell.frame, scale), tint)); }
    }
    paint_grid_lines(layout, window);
}
/// A text selection crossing cells selects a rectangle of whole cells, like a spreadsheet: the
/// first and last touched cells are opposite corners, so dragging down a column selects that
/// column rather than the row-major text run. Returns false when it stays in one cell.
pub(in crate::editor) fn paint_cell_selection(layout: &TextLayout, range: &Range<usize>, window: &mut Window) -> bool {
    // Corners are the cells holding the two ends of the selection (ends inclusive, so a caret at the very
    // start or end of a cell still belongs to that cell instead of leaking into its neighbour).
    let holding = |at: usize| layout.cells.iter().find(|cell| cell.range.start <= at && at <= cell.range.end);
    let (Some(first), Some(last)) = (holding(range.start), holding(range.end)) else { return false };
    if std::ptr::eq(first, last) { return false; }
    let corners = Bounds::from_corners(
        point(first.frame.left().min(last.frame.left()), first.frame.top().min(last.frame.top())),
        point(first.frame.right().max(last.frame.right()), first.frame.bottom().max(last.frame.bottom())));
    let (scale, tint) = (window.scale_factor(), select_tint());
    for cell in layout.cells.iter().filter(|c| corners.contains(&c.frame.center())) {
        window.paint_quad(fill(snapped(cell.frame, scale), tint));
    }
    paint_grid_lines(layout, window);
    true
}
