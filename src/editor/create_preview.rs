use gpui::{AnyElement, Context, IntoElement, Window, div, prelude::*};
use super::Editor;

impl Editor {
    pub(super) fn create_plan_preview(&mut self, window: &mut Window, cx: &mut Context<Self>) -> Option<AnyElement> {
        let Some(preview) = crate::editor::ai::plan_link::render_create_preview(cx.entity_id()) else {
            self.preview_was_open = false;
            return None;
        };
        if !self.preview_was_open {
            self.preview_was_open = true;
            cx.defer_in(window, |this, window, cx| {
                if crate::editor::ai::plan_link::create_preview_active(cx.entity_id()) { window.focus(&this.preview_focus, cx); }
            });
        }
        Some(div().size_full().relative().track_focus(&self.preview_focus)
            .on_key_down(|_, _, cx| cx.stop_propagation()).child(preview).into_any_element())
    }
}
