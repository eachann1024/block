//! Shared sidebar edge: an 8px hit target with hover and drag feedback.
use gpui::{div, prelude::*, px, CursorStyle, Div, Stateful};
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
