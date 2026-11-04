use gpui::{
    AnimationExt, BoxShadow, Div, ElementId, IntoElement, MouseButton, Pixels, Stateful, Styled,
    div, prelude::*, px,
};
use crate::{
    theme::Theme,
    ui::dropdown::{palette, shadows as dropdown_shadows},
};

pub fn shadows() -> Vec<BoxShadow> {
    dropdown_shadows()
}

pub fn card(id: impl Into<ElementId>, width: Option<Pixels>) -> Stateful<Div> {
    let p = palette();
    div()
        .id(id)
        .when_some(width, |d, w| d.w(w))
        .p(px(4.))
        .rounded(px(12.))
        .bg(p.bg)
        .shadow(shadows())
        .occlude()
        .text_size(px(12.))
        .text_color(p.text)
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
}

pub fn find_card(id: impl Into<ElementId>) -> Stateful<Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .py(px(4.))
        .pr(px(4.))
        .pl(px(10.))
        .flex()
        .items_center()
        .gap(px(4.))
        .rounded(px(12.))
        .bg(theme.menu)
        .border_1()
        .border_color(theme.menu_border)
        .shadow(shadows())
        .text_size(px(12.))
        .text_color(theme.text)
        .occlude()
}

/// In-place annotation popover card (`border-radius: 10px`, `1px` accent border, `12px 14px` padding).
pub fn annotation_card(id: impl Into<ElementId>, width: Pixels) -> Stateful<Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .w(width)
        .px(px(14.))
        .py(px(12.))
        .rounded(px(10.))
        .bg(theme.menu)
        .border_1()
        .border_color(theme.accent)
        .shadow(shadows())
        .occlude()
        .overflow_y_scroll()
        .text_size(px(14.))
        .line_height(px(20.))
        .text_color(theme.text)
        .flex()
        .flex_col()
        .gap(px(8.))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_scroll(|_, _, cx| cx.stop_propagation())
}

pub fn animate<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    crate::ui::motion::popup_enter(el, id)
}
