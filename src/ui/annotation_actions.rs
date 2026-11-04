use gpui::{App, CursorStyle, Div, ElementId, FontWeight, MouseButton, MouseDownEvent, SharedString, Stateful, Styled, Window, div, prelude::*, px};
use crate::theme::Theme;

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum BatchTone { Accent, Neutral }

fn action_btn(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    height: f32,
    pad_x: f32,
    tone: BatchTone,
    enabled: bool,
    on_press: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id(id).h(px(height)).px(px(pad_x)).flex_none().rounded(crate::ui::radius::CONTROL).border_1()
        .flex().items_center().justify_center().text_size(px(14.)).line_height(px(20.))
        .when(tone == BatchTone::Accent, |button| {
            button.bg(theme.selected).border_color(theme.accent).text_color(theme.text).font_weight(FontWeight(500.))
        })
        .when(tone == BatchTone::Neutral, |button| {
            button.bg(theme.paper).border_color(theme.btn_border).text_color(theme.text_secondary)
        })
        .when(enabled, |button| button.cursor(CursorStyle::PointingHand)
            .hover(move |style| style.bg(theme.hover).text_color(theme.text)))
        .when(!enabled, |button| button.cursor(CursorStyle::Arrow).opacity(0.6))
        .when(enabled, |button| button.on_mouse_down(MouseButton::Left, on_press))
        .child(label.into())
}

pub fn batch_button(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    tone: BatchTone,
    enabled: bool,
    on_press: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    action_btn(id, label, 28., 10., tone, enabled, on_press)
}

pub fn compact_action_button(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    tone: BatchTone,
    enabled: bool,
    on_press: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    action_btn(id, label, 26., 10., tone, enabled, on_press)
}
