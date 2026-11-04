//! Minimise / maximise / close buttons for windows whose native frame is hidden.
//! macOS keeps its traffic lights, so there this renders nothing.
use gpui::{CursorStyle, Div, ElementId, MouseButton, WindowControlArea, div, prelude::*, px, rgb};
use crate::{icons::{Icon, icon}, theme::Theme};

pub const fn is_macos() -> bool {
    cfg!(target_os = "macos")
}

fn button(id: impl Into<ElementId>, area: WindowControlArea, glyph: Icon, size: f32, close: bool) -> gpui::Stateful<Div> {
    let theme = Theme::ocean();
    let (text, fill) = (theme.text_secondary, theme.text);
    div()
        .id(id)
        .window_control_area(area)
        .size(px(28.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(6.))
        .cursor(CursorStyle::PointingHand)
        .child(icon(glyph, px(size), theme.icon_colors(text)))
        .hover(move |s| if close { s.bg(rgb(0xe81123)).text_color(rgb(0xffffff)) } else { s.bg(theme.hover).text_color(fill) })
}

pub fn window_controls(id_prefix: &'static str, minimizable: bool, maximizable: bool) -> Div {
    if is_macos() {
        return div();
    }
    div()
        .flex()
        .items_center()
        .gap(px(2.))
        .pl(px(4.))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .when(minimizable, |row| {
            row.child(button((id_prefix, 1usize), WindowControlArea::Min, Icon::Minus, 13., false)
                .on_click(|_, window, _| window.minimize_window()))
        })
        .when(maximizable, |row| {
            row.child(button((id_prefix, 2usize), WindowControlArea::Max, Icon::WindowMax, 12., false)
                .on_click(|_, window, _| window.zoom_window()))
        })
        .child(button((id_prefix, 3usize), WindowControlArea::Close, Icon::Close, 13., true)
            .on_click(|_, window, _| window.remove_window()))
}
