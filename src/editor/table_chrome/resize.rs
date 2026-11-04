//! Column-boundary grab zones laid over a hovered table, plus the cursor lock while one is dragged.
use gpui::{AnyElement, CursorStyle, Entity, MouseButton, div, point, prelude::*, px};
use crate::{editor::{table_actions::TableChrome, view::Editor}, ui::table_controls::col_resize};
use super::floating;

/// Width of the grab zone; the closing boundary of the last column is narrower so it stays clear of the add-column bar.
const HIT: f32 = 8.;
const LAST_HIT: f32 = 6.;
/// Distance from the zone's left edge to the rule it covers.
const LINE_AT: f32 = 4.;

pub(super) fn zones(editor: &Entity<Editor>, chrome: &TableChrome, active: Option<usize>) -> Vec<AnyElement> {
    let (table, view) = (chrome.bounds, chrome.viewport);
    let (top, bottom) = (table.top().max(view.top()), table.bottom().min(view.bottom()));
    let mut out: Vec<AnyElement> = Vec::new();
    if bottom - top < px(24.) || chrome.columns.is_empty() { return out; }
    let last = chrome.columns.len() - 1;
    for (col, &(left, right)) in chrome.columns.iter().enumerate() {
        // An inner boundary's 1px rule is the next cell's left edge; the closing one sits inside the grid.
        let centre = if col == last { right - px(0.5) } else { right + px(0.5) };
        if centre < table.left() + px(8.) || centre > table.right() + px(1.) { continue; }
        let (editor, block, start_width) = (editor.clone(), chrome.block, f32::from(right - left));
        let zone = col_resize(("table-col-resize", col), if col == last { LAST_HIT } else { HIT }, bottom - top, LINE_AT, active == Some(col))
            .on_mouse_down(MouseButton::Left, move |event, _, cx| {
                let (x, double) = (event.position.x, event.click_count >= 2);
                editor.update(cx, |this, cx| if double { this.reset_column_width(block, col, cx) }
                    else { this.begin_column_resize(block, col, x, start_width, cx) });
                cx.stop_propagation();
            });
        out.push(floating(point(centre - px(LINE_AT), top), zone));
    }
    if active.is_some() {
        // Keeps the resize cursor while the pointer outruns the zone.
        out.push(floating(view.origin, div().w(view.size.width).h(view.size.height).cursor(CursorStyle::ResizeColumn)));
    }
    out
}
