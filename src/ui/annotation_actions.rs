use gpui::{App, CursorStyle, Div, ElementId, MouseDownEvent, MouseButton, SharedString, Stateful, Styled, Window, div, prelude::*, px};
use crate::theme::Theme;

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum BatchTone { Accent, Neutral }

pub fn batch_button(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    tone: BatchTone,
    enabled: bool,
    on_press: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id(id).h(px(28.)).px(px(9.)).flex_none().rounded(px(6.))
        .flex().items_center().justify_center().text_size(px(14.))
        .when(tone == BatchTone::Accent, |button| button.bg(theme.selected).text_color(theme.accent))
        .when(tone == BatchTone::Neutral, |button| button.text_color(theme.text_secondary))
        .when(enabled, |button| button.cursor(CursorStyle::PointingHand)
            .hover(|style| style.bg(theme.hover).text_color(theme.text)))
        .when(!enabled, |button| button.cursor(CursorStyle::Arrow).opacity(0.6))
        .when(enabled, |button| button.on_mouse_down(MouseButton::Left, on_press))
        .child(label.into())
}
