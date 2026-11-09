//! Shared sidebar edge: an 8px hit target with hover and drag feedback.
use gpui::{div, prelude::*, px, relative, CursorStyle, Div, SharedString, Stateful};
use crate::theme::Theme;

pub enum Edge { Left, Right }

pub fn resize_handle(id: &'static str, edge: Edge, active: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id(id).absolute().top_0().w(px(8.)).h_full()
        .when(matches!(edge, Edge::Left), |d| d.left(px(-4.)))
        .when(matches!(edge, Edge::Right), |d| d.right(px(-4.)))
        .cursor(CursorStyle::ResizeLeftRight).group(id)
        .child(div().absolute().top_0().left(px(3.)).w(px(2.)).h_full()
            .when(active, |line| line.bg(theme.accent))
            .when(!active, |line| line.group_hover(id, |style| style.bg(theme.menu_border))))
}

/// A centered side pill with a 20px grab zone that overlaps the resized surface's edge.
pub fn side_pill(id: (&'static str, usize), left: bool, height: Option<f32>, zoom: f32, group: SharedString, visible: bool) -> Stateful<Div> {
    let bar_h = height.filter(|&h| h < 64. * zoom)
        .map_or(48. * zoom, |h| (h * 0.6).round().max(16. * zoom));
    let hit_h = bar_h + 16. * zoom;
    let color = if crate::theme::is_dark() { 0x707070 } else { 0x9a9a9a };
    div().id(id).absolute().top(relative(0.5)).mt(px(-hit_h / 2.))
        .when(left, |s| s.left(px(-16.5 * zoom)))
        .when(!left, |s| s.right(px(-16.5 * zoom)))
        .w(px(20. * zoom)).h(px(hit_h)).flex().items_center().justify_center()
        .cursor(CursorStyle::ResizeLeftRight)
        .invisible().when(visible, |s| s.visible()).group_hover(group, |s| s.visible())
        .child(div().w(px((3. * zoom).max(2.))).h(px(bar_h)).rounded(px(3. * zoom)).bg(gpui::rgb(color)))
}
