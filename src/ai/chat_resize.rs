//! AI panel sizing, with the same edge gesture and persistence as navigation.
use gpui::{div, prelude::*, px, AnyElement, App, Context, CursorStyle, MouseButton, Pixels};
use super::{AiChat, panel};

const DEFAULT_WIDTH: f32 = 396.;
fn clamp_width(width: Pixels) -> Pixels { width.clamp(px(320.), px(720.)) }

pub(super) fn load_width() -> Pixels {
    let width = crate::files::load_state().ok().and_then(|s| s.ai_panel_width)
        .filter(|w| w.is_finite()).unwrap_or(DEFAULT_WIDTH);
    clamp_width(px(width))
}

impl AiChat {
    pub(crate) fn resize_handle(&self, cx: &mut Context<Self>) -> impl IntoElement {
        crate::ui::resize_handle::resize_handle("ai-panel-resize", crate::ui::resize_handle::Edge::Left, self.resizing.is_some())
            .on_mouse_down(MouseButton::Left, cx.listener(|this, event: &gpui::MouseDownEvent, window, cx| {
                cx.stop_propagation();
                if event.click_count >= 2 {
                    this.resizing = None;
                    this.panel_width = px(DEFAULT_WIDTH);
                    this.persist_width(cx);
                } else {
                    this.resizing = Some((event.position.x, this.panel_width));
                }
                window.refresh();
                cx.notify();
            }))
    }

    fn persist_width(&self, cx: &mut Context<Self>) {
        let width = f32::from(self.panel_width);
        cx.background_executor().spawn(async move {
            let _ = crate::files::update_state(|state| state.ai_panel_width = Some(width));
        }).detach();
    }

    fn finish_resize(&mut self, cx: &mut Context<Self>) {
        if self.resizing.take().is_none() { return; }
        self.persist_width(cx);
        cx.notify();
    }
}

/// Capture across the entire window so dragging beyond the panel never loses input.
pub(crate) fn resize_capture(cx: &App) -> Option<AnyElement> {
    let entity = panel(cx)?;
    if entity.read(cx).fullscreen { return None; }
    entity.read(cx).resizing?;
    let end = entity.clone();
    Some(div().id("ai-resize-capture").absolute().top_0().left_0().size_full().occlude()
        .cursor(CursorStyle::ResizeLeftRight)
        .on_mouse_move(move |event: &gpui::MouseMoveEvent, window, cx| {
            entity.update(cx, |this, cx| {
                let Some((start, width)) = this.resizing else { return; };
                if event.pressed_button != Some(MouseButton::Left) { this.finish_resize(cx); }
                else { this.panel_width = clamp_width(width + start - event.position.x); cx.notify(); }
            });
            window.refresh();
        })
        .on_mouse_up(MouseButton::Left, move |_, window, cx| {
            end.update(cx, |this, cx| this.finish_resize(cx));
            window.refresh();
        }).into_any_element())
}
