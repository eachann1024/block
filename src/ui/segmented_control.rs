//! Stateless segmented control. Ocean Paper: shell track, selected fill, 1px accent outline.
//! Segments are non-focusable radio buttons so a click does not take the editor's input method.
use gpui::{
    App, Axis, CursorStyle, ElementId, FontWeight, IntoElement, MouseButton, MouseDownEvent,
    ParentElement, Role, SharedString, Styled, Toggled, Window, prelude::*, px,
};
use gpui_base::{Button, RadioGroup};
use crate::{theme::Theme, ui::radius};

pub struct SegmentSpec {
    pub label: SharedString,
    pub selected: bool,
    pub on_select: Box<dyn Fn(&MouseDownEvent, &mut Window, &mut App) + 'static>,
}

pub fn segmented_control(id: impl Into<ElementId>, segments: Vec<SegmentSpec>) -> impl IntoElement {
    let id = id.into();
    let theme = Theme::ocean();
    let count = segments.len();
    let probe = super::smoke_overlay::probe_for(&id);
    RadioGroup::new(id.clone())
        .axis(Axis::Horizontal)
        .relative()
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
        .children(probe)
        .children(segments.into_iter().enumerate().map(|(index, segment)| {
            let child = ElementId::from((id.clone(), index.to_string()));
            let name = segment.label.clone();
            Button::new(child.clone())
                .focusable(false)
                .role(Role::RadioButton)
                .selected(segment.selected)
                .accessibility_label(name)
                .aria_selected(segment.selected)
                .aria_toggled(if segment.selected { Toggled::True } else { Toggled::False })
                .aria_position_in_set(index + 1)
                .aria_size_of_set(count)
                .relative()
                .children(super::smoke_overlay::probe_for(&child))
                .h_full()
                .px(px(10.))
                .rounded(px(6.))
                .flex()
                .items_center()
                .justify_center()
                .text_size(px(14.))
                .line_height(px(20.))
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
