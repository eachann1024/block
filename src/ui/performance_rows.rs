use gpui::{AnyElement, CursorStyle, FontWeight, IntoElement, MouseButton, ParentElement,
    SharedString, Styled, div, prelude::*, px, relative};

use crate::{theme::Theme, ui::{performance_treemap::{PerformanceSelect, TreemapItem, format_bytes},
    radius, smoke_overlay, tooltip}};

pub fn metric(label: impl Into<SharedString>, value: impl Into<SharedString>) -> AnyElement {
    let theme = Theme::ocean();
    div().flex().items_center().gap(px(5.)).text_size(px(14.))
        .child(div().text_color(theme.text_secondary).child(label.into()))
        .child(div().text_color(theme.text).child(value.into())).into_any_element()
}

pub fn detail_row(id: &str, item: &TreemapItem, total_bytes: u64, selected: bool,
    on_select: PerformanceSelect) -> AnyElement {
    let theme = Theme::ocean();
    let target = SharedString::from(format!("{id}-row-{}", item.id));
    let hover_id = item.id.clone();
    let click_id = item.id.clone();
    let on_hover = on_select.clone();
    let fraction = if total_bytes == 0 { 0. } else {
        (item.bytes as f64 / total_bytes as f64).clamp(0., 1.) as f32
    };
    let percent = if total_bytes == 0 { 0. } else { item.bytes as f64 / total_bytes as f64 * 100. };
    let tip = format!("{} · {} B · {:.2}% · {}", item.label, item.bytes, percent, item.detail);
    div().id(target.clone()).relative().children(smoke_overlay::probe(target))
        .w_full().h(px(34.)).flex_none().px(px(8.)).rounded(radius::CONTROL)
        .flex().items_center().gap(px(10.)).text_size(px(14.)).text_color(theme.text)
        .bg(if selected { theme.selected } else { gpui::transparent_black() })
        .cursor(CursorStyle::PointingHand).hover(move |row| row.bg(theme.hover))
        .tooltip(tooltip::tooltip(tip))
        .on_hover(move |hovered, window, cx| {
            if *hovered { on_hover(&hover_id, window, cx); }
        })
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            on_select(&click_id, window, cx);
        })
        .child(div().size(px(8.)).flex_none().rounded(px(2.)).bg(item.color))
        .child(div().w(px(96.)).flex_none().overflow_hidden().whitespace_nowrap().text_ellipsis()
            .font_weight(FontWeight(460.)).child(item.label.clone()))
        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
            .text_color(theme.text_secondary).child(item.detail.clone()))
        .child(div().w(px(76.)).h(px(5.)).flex_none().overflow_hidden().rounded_full().bg(theme.shell)
            .child(div().w(relative(fraction)).h_full().bg(item.color)))
        .child(div().w(px(76.)).flex_none().text_right().child(format_bytes(item.bytes)))
        .into_any_element()
}
