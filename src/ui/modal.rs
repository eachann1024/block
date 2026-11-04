use gpui::{
    AnimationExt, Div, ElementId, IntoElement, MouseButton, Pixels, Stateful, Styled, div,
    prelude::*, px, rgba,
};
use crate::{
    theme::Theme,
    ui::{dropdown::shadows, motion::modal_enter},
};

pub fn backdrop(
    id: impl Into<ElementId>,
    child: impl IntoElement + Styled + AnimationExt + 'static,
) -> Stateful<Div> {
    div()
        .id(id)
        .absolute()
        .top_0()
        .left_0()
        .size_full()
        .occlude()
        .bg(rgba(0x20242c2e))
        .flex()
        .flex_col()
        .items_center()
        .pt(px(96.))
        .px(px(24.))
        .child(modal_enter(child, "modal-card-enter"))
}

pub fn card(id: impl Into<ElementId>, width: Pixels) -> Stateful<Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .w(width)
        .max_w_full()
        .flex()
        .flex_col()
        .overflow_hidden()
        .rounded(px(12.))
        .bg(theme.menu)
        .border_1()
        .border_color(theme.menu_border)
        .shadow(shadows())
        .text_color(theme.text)
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
}
