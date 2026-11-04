//! Keeps scrolling while a table row or column handle is dragged near an edge, even when the
//! pointer rests there, and refreshes the drop target against the geometry the scroll produces.
use std::time::Duration;
use gpui::{Context, px};
use super::{table_actions::TableSel, table_layout, view::Editor};

thread_local! {
    /// True while the timer of a table row/column drag runs.
    static RUNNING: std::cell::Cell<bool> = const { std::cell::Cell::new(false) };
}

/// Distance from the edge where scrolling starts, and the slowest and fastest step per tick.
const ZONE: f32 = 32.;
const SLOW: f32 = 4.;
const FAST: f32 = 22.;

/// Signed step for a pointer at `at` inside `lo..hi`: negative near `lo`, positive near `hi`, zero in between.
fn edge_speed(at: f32, lo: f32, hi: f32) -> f32 {
    let ramp = |gap: f32| SLOW + (FAST - SLOW) * ((ZONE - gap) / ZONE).clamp(0., 1.);
    if at < lo + ZONE { -ramp(at - lo) } else if at > hi - ZONE { ramp(hi - at) } else { 0. }
}

impl Editor {
    /// Starts the timer once per drag; it stops itself when the drag ends.
    pub(super) fn ensure_drag_ticker(&mut self, cx: &mut Context<Self>) {
        if RUNNING.with(|r| r.replace(true)) { return; }
        cx.spawn(async move |this, cx| {
            loop {
                cx.background_executor().timer(Duration::from_millis(16)).await;
                if !this.update(cx, |this, cx| this.tick_table_drag(cx)).unwrap_or(false) { break; }
            }
            RUNNING.with(|r| r.set(false));
        }).detach();
    }
    /// One step: scroll toward the nearest edge, then re-pick the drop target on the new geometry.
    fn tick_table_drag(&mut self, cx: &mut Context<Self>) -> bool {
        let Some(mut drag) = self.table_drag.filter(|d| d.moved) else { return false };
        let (Some(at), Some(chrome)) = (drag.position, self.table_chrome.as_ref().filter(|c| c.block == drag.block)) else { return true };
        let step = match drag.sel {
            TableSel::Column(_) => edge_speed(f32::from(at.x), f32::from(chrome.bounds.left()), f32::from(chrome.bounds.right())),
            TableSel::Row(_) => edge_speed(f32::from(at.y), f32::from(chrome.viewport.top()), f32::from(chrome.viewport.bottom())),
            _ => 0.,
        };
        let target = chrome.target_at(drag, at);
        let scrolled = step != 0. && match drag.sel {
            TableSel::Column(_) => self.layouts.get(&drag.block)
                .is_some_and(|layout| table_layout::scroll_by(cx.entity_id(), drag.block, layout, step)),
            _ => { self.list.scroll_by(px(step)); true }
        };
        if scrolled || target != drag.target {
            drag.target = target;
            self.table_drag = Some(drag);
            cx.notify();
        }
        true
    }
}
