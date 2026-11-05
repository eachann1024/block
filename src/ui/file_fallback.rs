//! Centered fallback when a file cannot be previewed or edited.
use gpui::{App, FontWeight, IntoElement, MouseButton, MouseDownEvent, ParentElement, Styled, Window, div, prelude::*, px};
use crate::theme::{Theme, tr};

pub fn unsupported_file(
    reason: impl Into<String>,
    on_open: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let reason = reason.into();
    div().size_full().flex().flex_col().items_center().justify_center().gap(px(8.)).px(px(32.)).bg(theme.paper)
        .child(div().text_size(px(16.)).line_height(px(24.)).font_weight(FontWeight(560.)).text_color(theme.text)
            .child(tr("无法预览此文件", "Cannot preview this file")))
        .child(div().text_size(px(14.)).line_height(px(20.)).text_color(theme.text_secondary).text_center()
            .child(reason))
        .child(div().mt(px(8.)).child(open_button("unsupported-open", tr("使用默认应用打开", "Open with Default App"), on_open)))
}

pub fn open_button(
    id: &'static str,
    label: &'static str,
    on_open: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    div().id(id).h(px(32.)).px(px(14.)).flex_none().flex().items_center()
        .rounded(crate::ui::radius::CONTROL).bg(theme.paper).border_1().border_color(theme.btn_border)
        .text_size(px(14.)).text_color(theme.text).cursor(gpui::CursorStyle::PointingHand)
        .hover(|s| s.bg(theme.hover))
        .on_mouse_down(MouseButton::Left, move |event, window, cx| {
            cx.stop_propagation();
            on_open(event, window, cx);
        })
        .child(label)
}
