//! Minimise / maximise / close buttons for windows whose native frame is hidden.
//! macOS keeps its traffic lights, so there this renders nothing.
use gpui::{CursorStyle, Div, ElementId, MouseButton, SharedString, WindowControlArea, div, prelude::*, px, rgb, transparent_black};
use crate::{icons::{Icon, IconColors, icon, stateful_icon}, theme::Theme};

pub const fn is_macos() -> bool {
    cfg!(any(target_os = "macos", target_family = "wasm"))
}

/// macOS traffic-light origin. `Y` centers a 14px button in the 40px titlebar; `Y_TALL` centers it in the 52px fullscreen session bar.
pub const TRAFFIC_X: f32 = 16.;
/// A 26px titlebar action contains an 18px glyph (4px inset). Match the
/// traffic lights' visible 16px edge clearance on the opposite side.
pub const TITLEBAR_ACTION_TRAILING: f32 = if is_macos() { TRAFFIC_X - 4. } else { 0. };
pub const TRAFFIC_Y: f32 = 13.;
pub const TRAFFIC_Y_TALL: f32 = 19.;
/// Left inset that keeps the session title clear of the traffic lights. Matches the titlebar's leading pad.
pub const TRAFFIC_LEADING: f32 = if cfg!(any(target_os = "macos", target_family = "wasm")) { 82. } else { 16. };

fn button(id: impl Into<ElementId>, area: WindowControlArea, glyph: Icon, size: f32, close: bool) -> gpui::Stateful<Div> {
    let theme = Theme::ocean();
    let (text, fill) = (theme.text_secondary, theme.text);
    let id: ElementId = id.into();
    let group = SharedString::from(format!("winctrl-{id:?}"));
    let glyph_el = if close {
        icon(glyph, px(size), IconColors { line: text, fill: transparent_black(), accent: text })
    } else {
        stateful_icon(group.clone(), glyph, px(size), false, theme.mono_icon_colors(text), theme.icon_colors(fill))
    };
    div()
        .id(id)
        .group(group)
        .window_control_area(area)
        .size(px(28.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::PointingHand)
        .child(glyph_el)
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
        .child(button((id_prefix, 3usize), WindowControlArea::Close, Icon::Close, 12., true)
            .on_click(|_, window, _| window.remove_window()))
}
