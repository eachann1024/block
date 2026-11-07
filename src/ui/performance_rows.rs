use gpui::{AnyElement, IntoElement, ParentElement, SharedString, Styled, div, prelude::*, px};
use crate::{theme::{Theme, tr}, ui::{performance_treemap::{TreemapItem, format_bytes, format_percent}, smoke_overlay}};

thread_local! { static DETAIL: std::cell::RefCell<String> = const { std::cell::RefCell::new(String::new()) }; }

pub(crate) fn smoke_detail() -> String { DETAIL.with(|detail| detail.borrow().clone()) }

pub fn metric(label: impl Into<SharedString>, value: impl Into<SharedString>) -> AnyElement {
    let theme = Theme::ocean();
    div().flex().items_center().gap(px(4.)).text_size(px(13.))
        .child(div().text_color(theme.text_secondary).child(label.into()))
        .child(div().text_color(theme.text).child(value.into())).into_any_element()
}

pub fn selection_detail(items: &[TreemapItem], selected: Option<&str>, total: u128) -> AnyElement {
    let theme = Theme::ocean();
    let text = items.iter().find(|item| selected == Some(item.id.as_str())).map(|item| {
        format!("{} · {} · {} · {}", item.label, format_bytes(item.bytes), format_percent(item.bytes, total), item.detail)
    }).unwrap_or_else(|| tr("悬停或点击方块查看详情", "Hover or click a tile for details").to_owned());
    if smoke_overlay::is_smoke_active() { DETAIL.with(|detail| *detail.borrow_mut() = text.clone()); }
    div().id("performance-selection-detail").relative().children(smoke_overlay::probe("performance-selection-detail"))
        .children(selected.and_then(|id| smoke_overlay::probe(format!("performance-detail-{id}"))))
        .h(px(24.)).w_full().overflow_hidden().whitespace_nowrap().text_ellipsis()
        .text_size(px(12.)).text_color(theme.text_secondary).child(text).into_any_element()
}
