//! Compact text actions shared by banners, toasts, and confirm rows.
use crate::ui::tooltip::TooltipExt;
use gpui::{
    App, CursorStyle, ElementId, FontWeight, MouseButton, MouseDownEvent, SharedString, Stateful,
    Window, div, prelude::*, px,
};
use crate::theme::Theme;
use super::radius;

pub fn accent_action(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    enabled: bool,
    hint: impl Into<SharedString>,
    on_press: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let hint = hint.into();
    let id = id.into();
    div()
        .id(id.clone())
        .relative()
        .children(super::smoke_overlay::probe_for(&id))
        .h(px(30.))
        .px(px(12.))
        .rounded(radius::CONTROL)
        .flex()
        .items_center()
        .justify_center()
        .text_size(px(14.))
        .font_weight(FontWeight(460.))
        .tooltip_above(crate::theme::tooltip(hint))
        .when(!enabled, |button| {
            button.bg(theme.accent.opacity(0.48)).text_color(gpui::white()).cursor(CursorStyle::Arrow)
        })
        .when(enabled, |button| {
            button.bg(theme.accent).text_color(gpui::white()).cursor(CursorStyle::PointingHand)
                .hover(|button| button.opacity(0.9))
                .on_mouse_down(MouseButton::Left, on_press)
        })
        .child(label.into())
}

pub fn plain_action(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    height: f32,
    pad_x: f32,
    text: f32,
    on_press: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .h(px(height))
        .px(px(pad_x))
        .rounded(radius::CONTROL)
        .bg(theme.paper)
        .border_1()
        .border_color(theme.btn_border)
        .text_size(px(text))
        .text_color(theme.text)
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .hover(|action| action.bg(theme.hover))
        .on_mouse_down(MouseButton::Left, on_press)
        .child(label.into())
}
