use gpui::{
    AnimationExt, App, BoxShadow, CursorStyle, Div, ElementId, Entity, FontWeight, Hsla,
    IntoElement, MouseButton, MouseDownEvent, Pixels, SharedString, Stateful, Styled, Window,
    div, prelude::*, px, rgb,
};
use gpui_component::input::{Input, InputState};
use crate::{
    theme::{Theme, is_dark},
    ui::dropdown::{palette, shadows as dropdown_shadows},
};

fn accent_hover() -> Hsla {
    if is_dark() { rgb(0x6389d6).into() } else { rgb(0x3d63b0).into() }
}

pub fn shadows() -> Vec<BoxShadow> {
    dropdown_shadows()
}

pub fn card(id: impl Into<ElementId>, width: Option<Pixels>) -> Stateful<Div> {
    let p = palette();
    let id = id.into();
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    div()
        .id(id).relative().children(probe)
        .when_some(width, |d, w| d.w(w))
        .p(px(6.))
        .rounded(crate::ui::radius::SURFACE)
        .bg(p.bg)
        .shadow(shadows())
        .occlude()
        .text_size(px(14.))
        .line_height(px(20.))
        .text_color(p.text)
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
}

/// Compact inline text input slot (`height: 30px`, `border-radius: 8px`, `font-size: 14px`) for popovers.
pub fn inline_input_slot(input: &Entity<InputState>, width: Option<Pixels>) -> Div {
    let theme = Theme::ocean();
    let input_bg = if is_dark() { rgb(0x2d2f34).into() } else { theme.shell };
    div()
        .when_some(width, |d, w| d.w(w).flex_none())
        .when(width.is_none(), |d| d.flex_1().min_w_0())
        .h(px(30.))
        .px(px(10.))
        .rounded(crate::ui::radius::CONTROL)
        .bg(input_bg)
        .border_1()
        .border_color(theme.accent)
        .text_size(px(14.))
        .line_height(px(20.))
        .text_color(theme.text)
        .flex()
        .items_center()
        .overflow_hidden()
        .child(
            div().flex_1().min_w_0().child(
                Input::new(input)
                    .appearance(false)
                    .bordered(false)
                    .focus_bordered(false)
                    .h(px(28.))
                    .px(px(0.))
                    .py(px(0.))
                    .text_size(px(14.))
                    .line_height(px(20.)),
            ),
        )
}

/// Compact popover action button (`height: 30px`, `border-radius: 8px`, `font-size: 14px`).
pub fn inline_action_button(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    accent: bool,
    on_press: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui_base::Button {
    let theme = Theme::ocean();
    let label = label.into();
    gpui_base::Button::new(id)
        .focusable(false)
        .accessibility_label(label.clone())
        .h(px(30.))
        .px(px(12.))
        .flex_none()
        .rounded(crate::ui::radius::CONTROL)
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .text_size(px(14.))
        .line_height(px(20.))
        .font_weight(FontWeight(500.))
        .when(accent, |b| {
            b.bg(theme.accent)
                .text_color(rgb(0xffffff))
                .hover(move |s| s.bg(accent_hover()))
        })
        .when(!accent, |b| {
            b.text_color(theme.text)
                .hover(move |s| s.bg(theme.hover))
        })
        .child(label)
        .on_mouse_down(MouseButton::Left, on_press)
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
        .rounded(crate::ui::radius::SURFACE)
        .bg(theme.menu)
        .border_1()
        .border_color(theme.menu_border)
        .shadow(shadows())
        .text_size(px(12.))
        .text_color(theme.text)
        .occlude()
}

pub fn animate<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    crate::ui::motion::popup_enter(el, id)
}
