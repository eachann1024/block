//! Horizontal scrolling for tables wider than the text column (the reference `overflow-x: auto`):
//! per-table offset, wheel input, caret reveal and the cues drawn on a clipped right edge.
use std::{cell::RefCell, collections::HashMap};
use gpui::{Bounds, DispatchPhase, Entity, EntityId, Pixels, ScrollEvent, Window,
    fill, linear_color_stop, linear_gradient, point, px, size};
use crate::{editor::view::{Editor, layout::TextLayout}, theme::Theme};
use super::{EXTEND_ROOM, draw::snapped, grid_rect};

#[derive(Clone, Copy, Default)]
struct Scroll {
    /// Pixels the grid is scrolled to the left.
    offset: f32,
    /// Offset already baked into the stored layout's geometry.
    applied: f32,
}
thread_local! {
    static STATE: RefCell<HashMap<(EntityId, u64), Scroll>> = RefCell::default();
}

/// The window the grid scrolls inside: the block minus the room kept for the extend buttons.
pub(in crate::editor) fn visible(bounds: Bounds<Pixels>) -> Bounds<Pixels> {
    Bounds::new(bounds.origin, size((bounds.size.width - px(EXTEND_ROOM)).max(px(1.)), bounds.size.height))
}
fn max_offset(layout: &TextLayout) -> f32 {
    let Some(grid) = grid_rect(layout) else { return 0. };
    (f32::from(grid.size.width) - f32::from(visible(layout.bounds).size.width)).max(0.)
}
fn apply(layout: &mut TextLayout, state: &mut Scroll) {
    let delta = state.applied - state.offset;
    if delta == 0. { return; }
    let bounds = layout.bounds;
    layout.translate(point(px(delta), px(0.)));
    layout.bounds = bounds;
    state.applied = state.offset;
}

/// Shifts a freshly positioned layout by the table's scroll offset. `fresh` means the layout
/// was just measured (unscrolled); `caret` is a caret offset to bring into view.
pub(in crate::editor) fn settle(editor: EntityId, block: u64, layout: &mut TextLayout, fresh: bool, caret: Option<usize>) {
    let key = (editor, block);
    let max = max_offset(layout);
    let mut state = STATE.with(|s| s.borrow().get(&key).copied().unwrap_or_default());
    if fresh { state.applied = 0.; }
    state.offset = state.offset.clamp(0., max);
    apply(layout, &mut state);
    if let Some(offset) = caret {
        let (view, x, pad) = (visible(layout.bounds), layout.point(offset).x, px(12.));
        let want = if x < view.left() + pad { state.offset - f32::from(view.left() + pad - x) }
            else if x > view.right() - pad { state.offset + f32::from(x - (view.right() - pad)) }
            else { state.offset };
        state.offset = want.clamp(0., max);
        apply(layout, &mut state);
    }
    STATE.with(|s| { s.borrow_mut().insert(key, state); });
}

/// Scrolls one table by `delta` pixels (positive moves toward the right edge); false when already at an end.
pub(in crate::editor) fn scroll_by(editor: EntityId, block: u64, layout: &TextLayout, delta: f32) -> bool {
    let max = max_offset(layout);
    STATE.with(|s| {
        let mut s = s.borrow_mut();
        let state = s.entry((editor, block)).or_default();
        let next = (state.offset + delta).clamp(0., max);
        let moved = next != state.offset;
        state.offset = next;
        moved
    })
}

/// Registered while a scrollable table paints: sideways wheel or trackpad motion over the grid scrolls it.
pub(in crate::editor) fn track_wheel(editor: Entity<Editor>, block: u64, layout: &TextLayout, hitbox: Option<gpui::Hitbox>, window: &mut Window) {
    let max = max_offset(layout);
    if max <= 0. { return; }
    let (area, key) = (visible(layout.bounds), (editor.entity_id(), block));
    window.on_mouse_event(move |event: &ScrollEvent, phase, window, cx| {
        let unoccluded = hitbox.as_ref().is_none_or(|hb| hb.should_handle_scroll(window));
        if phase != DispatchPhase::Bubble || !unoccluded || !area.contains(&event.position) { return; }
        let delta = event.delta.pixel_delta(px(20.));
        if delta.x.abs() <= delta.y.abs() { return; }
        let moved = STATE.with(|s| {
            let mut s = s.borrow_mut();
            let state = s.entry(key).or_default();
            let next = (state.offset - f32::from(delta.x)).clamp(0., max);
            let moved = next != state.offset;
            state.offset = next;
            moved
        });
        if moved { editor.update(cx, |_, cx| cx.notify()); }
        cx.stop_propagation();
    });
}

/// When the grid runs past the right edge: a fade into the page and the 1px cut rule, as in the reference.
pub(in crate::editor) fn paint_edge(layout: &TextLayout, window: &mut Window) {
    let Some(grid) = grid_rect(layout) else { return };
    let view = visible(layout.bounds);
    if grid.right() <= view.right() + px(0.5) { return; }
    let theme = Theme::ocean();
    let rule = if crate::theme::is_dark() { theme.menu_border } else { gpui::rgb(0xe8e5dc).into() };
    let (scale, fade) = (window.scale_factor(), px(32.));
    let band = snapped(Bounds::new(point(view.right() - fade, grid.top()), size(fade, grid.size.height)), scale);
    window.paint_quad(fill(band, linear_gradient(90.,
        linear_color_stop(theme.paper.opacity(0.), 0.), linear_color_stop(theme.paper, 1.))));
    let (x, cap, wide) = (view.right() - px(1.), px(16.), px(1.));
    let top = Bounds::new(point(x, grid.top() - cap), size(wide, cap));
    let mid = Bounds::new(point(x, grid.top()), size(wide, grid.size.height));
    let bottom = Bounds::new(point(x, grid.bottom()), size(wide, cap));
    window.paint_quad(fill(snapped(top, scale), linear_gradient(180.,
        linear_color_stop(rule.opacity(0.), 0.), linear_color_stop(rule, 1.))));
    window.paint_quad(fill(snapped(mid, scale), rule));
    window.paint_quad(fill(snapped(bottom, scale), linear_gradient(180.,
        linear_color_stop(rule, 0.), linear_color_stop(rule.opacity(0.), 1.))));
}
