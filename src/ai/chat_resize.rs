//! AI panel sizing, with the same edge gesture and persistence as navigation.
use gpui::{div, prelude::*, px, AnyElement, App, Context, CursorStyle, MouseButton, Pixels, Window};
use super::{AiChat, panel};

pub(super) const DEFAULT_WIDTH: f32 = 396.;
fn clamp_width(width: Pixels) -> Pixels {
    width.clamp(px(crate::ui::shell_layout::CHAT_MIN), px(crate::ui::shell_layout::CHAT_MAX))
}

pub(super) fn load_width() -> Pixels {
    let width = crate::files::load_state().ok().and_then(|s| s.ai_panel_width)
        .filter(|w| w.is_finite()).unwrap_or(DEFAULT_WIDTH);
    clamp_width(px(width))
}

pub(super) fn load_fullscreen() -> bool {
    crate::files::load_state().ok().is_some_and(|state| state.ai_fullscreen)
}

impl AiChat {
    pub(crate) fn displayed_split(&self, window: &Window) -> crate::ui::shell_layout::ShellSplit {
        if let Some(split) = crate::ui::shell_layout::current_split() {
            return split;
        }
        let frame = crate::ui::shell_layout::frame();
        crate::ui::shell_layout::resolve(crate::ui::shell_layout::Input {
            window_width: f32::from(window.viewport_size().width),
            sidebar_width: frame.sidebar_width,
            sidebar_collapsed: frame.sidebar_collapsed,
            sidebar_pinned: frame.sidebar_pinned,
            chat_open: true,
            chat_fullscreen: self.fullscreen,
            chat_width: f32::from(self.panel_width),
            chat_min: crate::ui::shell_layout::CHAT_MIN,
            chat_max: crate::ui::shell_layout::CHAT_MAX,
            content_floor: crate::ui::shell_layout::NOTE_FLOOR,
        })
    }

    pub(crate) fn resize_handle(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let entity = cx.entity();
        crate::ui::resize_handle::resize_handle("ai-panel-resize", crate::ui::resize_handle::Edge::Left, self.resizing.is_some())
            .on_mouse_down(MouseButton::Left, move |event, window, cx| {
                cx.stop_propagation();
                entity.update(cx, |this, cx| {
                    if event.click_count >= 2 {
                        this.resizing = None;
                        this.panel_width = px(DEFAULT_WIDTH);
                        this.persist_width(cx);
                    } else {
                        let shown = this.displayed_split(window).chat_width;
                        this.resizing = Some((event.position.x, px(shown)));
                    }
                    cx.notify();
                });
                window.refresh();
            })
    }

    fn persist_width(&self, cx: &mut Context<Self>) {
        let width = f32::from(self.panel_width);
        cx.background_executor().spawn(async move {
            let _ = crate::files::update_state(|state| state.ai_panel_width = Some(width));
        }).detach();
    }

    pub(crate) fn set_fullscreen(&mut self, fullscreen: bool, cx: &mut Context<Self>) {
        if self.fullscreen == fullscreen { return; }
        self.fullscreen = fullscreen;
        cx.background_executor().spawn(async move {
            let _ = crate::files::update_state(|state| state.ai_fullscreen = fullscreen);
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
            let frame = crate::ui::shell_layout::frame();
            let limit = crate::ui::shell_layout::chat_limit(
                f32::from(window.viewport_size().width), frame.sidebar_width, frame.sidebar_collapsed,
                crate::ui::shell_layout::CHAT_MIN, crate::ui::shell_layout::CHAT_MAX,
            );
            entity.update(cx, |this, cx| {
                let Some((start, shown)) = this.resizing else { return; };
                if event.pressed_button != Some(MouseButton::Left) { this.finish_resize(cx); return; }
                let target = f32::from(shown) + f32::from(start) - f32::from(event.position.x);
                let saved = f32::from(this.panel_width);
                let next = target.clamp(crate::ui::shell_layout::CHAT_MIN, limit);
                // Pushing outward into the note's floor keeps the wider saved width.
                if target > f32::from(shown) + 0.5 && next <= saved + 0.5 { return; }
                if (next - saved).abs() < 0.5 { return; }
                this.panel_width = px(next);
                cx.notify();
            });
            window.refresh();
        })
        .on_mouse_up(MouseButton::Left, move |_, window, cx| {
            end.update(cx, |this, cx| this.finish_resize(cx));
            window.refresh();
        }).into_any_element())
}
