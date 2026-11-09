//! Agent configuration navigation and explicit opt-in are separate hit targets.
use gpui::{App, ClickEvent, CursorStyle, Div, ElementId, FontWeight, MouseButton,
    MouseDownEvent, SharedString, Stateful, Window, div, prelude::*, px, svg};
use gpui_base::{Switch, SwitchThumb};
use crate::theme::{Theme, is_dark};
use super::{card_bg, mono_font, selected_bg};

pub fn brand_glyph(logo: &'static str, size: f32, color: gpui::Hsla) -> Div {
    div().size(px(size)).flex_none().flex().items_center().justify_center()
        .child(svg().path(logo).size(px(size)).text_color(color))
}

pub fn pill_switch(id: impl Into<ElementId>, checked: bool, enabled: bool) -> Switch {
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    let track_off = if is_dark() { gpui::rgb(0x3a3a3a) } else { gpui::rgb(0xd4d7dd) };
    Switch::new(id).checked(checked).disabled(!enabled)
        .relative().children(probe)
        .w(px(34.)).h(px(20.)).p(px(2.)).rounded(px(999.)).flex_none()
        .flex().items_center()
        .when(checked, |d| d.justify_end().bg(theme.accent))
        .when(!checked, |d| d.justify_start().bg(track_off))
        .when(enabled, |d| d.cursor(CursorStyle::PointingHand))
        .when(!enabled, |d| d.opacity(0.5))
        .child(SwitchThumb::new(checked).size(px(16.)).rounded(px(999.)).bg(gpui::rgb(0xffffff)))
}

pub fn agent_card(
    id: ElementId, toggle_id: ElementId, label: &'static str, subtitle: impl Into<SharedString>,
    logo: &'static str, focused: bool, enabled: bool, is_last: bool,
    on_select: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_toggle: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let sub: SharedString = subtitle.into();
    let well = div().size(px(26.)).rounded(px(6.)).bg(card_bg()).border_1().border_color(theme.hairline)
        .flex().items_center().justify_center().flex_none()
        .child(brand_glyph(logo, 16., theme.text));
    let toggle = pill_switch(toggle_id, enabled, true)
        .accessibility_label(label)
        .on_change(move |_next, ev, window, cx| on_toggle(ev, window, cx));
    div().id(id.clone()).relative().children(crate::ui::smoke_overlay::probe_for(&id))
        .w_full().min_w_0().py(px(11.)).px(px(14.)).flex().items_center().justify_between().gap(px(10.))
        .when(!is_last, |d| d.border_b_1().border_color(theme.hairline))
        .when(focused, |d| d.bg(selected_bg()))
        .when(!focused, |d| d.hover(move |s| s.bg(theme.hover)))
        .cursor(CursorStyle::PointingHand)
        .on_mouse_down(MouseButton::Left, on_select)
        .child(div().flex().items_center().gap(px(9.)).min_w_0().flex_1()
            .child(well)
            .child(div().flex(). items_baseline().gap(px(6.)).min_w_0().flex_1()
                .child(div().text_size(px(14.)).line_height(px(20.)).font_weight(FontWeight(560.)).text_color(theme.text)
                    .whitespace_nowrap().child(label))
                .when(!sub.is_empty(), |d| d.child(div().text_size(px(12.)).line_height(px(16.)).font(mono_font())
                    .text_color(theme.text_secondary).overflow_hidden().whitespace_nowrap().text_ellipsis().child(sub)))))
        .child(toggle)
}

