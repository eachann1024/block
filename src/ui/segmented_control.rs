//! Stateless segmented control. Ocean Paper: shell track, selected fill, 1px accent outline.
use gpui::{
    App, CursorStyle, ElementId, FontWeight, IntoElement, MouseButton, MouseDownEvent,
    ParentElement, SharedString, Styled, Window, div, prelude::*, px,
};
use crate::{theme::Theme, ui::radius};

pub struct SegmentSpec {
    pub label: SharedString,
    pub selected: bool,
    pub on_select: Box<dyn Fn(&MouseDownEvent, &mut Window, &mut App) + 'static>,
}

pub fn segmented_control(id: impl Into<ElementId>, segments: Vec<SegmentSpec>) -> impl IntoElement {
    let id = id.into();
    let theme = Theme::ocean();
    div()
        .id(id.clone())
        .relative()
        .children(super::smoke_overlay::probe_for(&id))
        .flex_none()
        .h(px(30.))
        .p(px(2.))
        .bg(theme.paper)
        .border_1()
        .border_color(theme.btn_border)
        .rounded(radius::CONTROL)
        .flex()
        .items_center()
        .gap(px(2.))
        .children(segments.into_iter().enumerate().map(|(index, segment)| {
            let child = gpui::ElementId::from((id.clone(), index.to_string()));
            div()
                .id(child.clone())
                .relative()
                .children(super::smoke_overlay::probe_for(&child))
                .h_full()
                .px(px(10.))
                .rounded(px(6.))
                .flex()
                .items_center()
                .justify_center()
                .text_size(px(14.))
                .font_weight(FontWeight(460.))
                .cursor(CursorStyle::PointingHand)
                .when(segment.selected, |item| {
                    item.bg(theme.selected)
                        .border_1()
                        .border_color(theme.accent)
                        .text_color(theme.text)
                })
                .when(!segment.selected, |item| {
                    item.text_color(theme.text_secondary)
                        .hover(|item| item.bg(theme.hover).text_color(theme.text))
                })
                .on_mouse_down(MouseButton::Left, segment.on_select)
                .child(segment.label)
        }))
}
