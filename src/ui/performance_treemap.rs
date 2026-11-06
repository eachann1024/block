use std::rc::Rc;

use gpui::{AnyElement, App, CursorStyle, Hsla, IntoElement, MouseButton,
    ParentElement, SharedString, Styled, Window, div, prelude::*, px, relative};

use crate::{theme::Theme, ui::{radius, smoke_overlay, tooltip}};

#[derive(Clone)]
pub struct TreemapItem {
    pub id: String,
    pub label: String,
    pub bytes: u64,
    pub color: Hsla,
    pub detail: String,
}

pub type PerformanceSelect = Rc<dyn Fn(&str, &mut Window, &mut App)>;

pub fn format_bytes(bytes: u64) -> String {
    const UNITS: [&str; 5] = ["B", "KiB", "MiB", "GiB", "TiB"];
    let mut value = bytes as f64;
    let mut unit = 0;
    while value >= 1024. && unit < UNITS.len() - 1 {
        value /= 1024.;
        unit += 1;
    }
    if unit == 0 { format!("{bytes} B") } else { format!("{value:.1} {}", UNITS[unit]) }
}

pub fn treemap(
    id: &str,
    items: &[TreemapItem],
    selected: Option<&str>,
    on_select: PerformanceSelect,
) -> AnyElement {
    let items: Vec<_> = items.iter().filter(|item| item.bytes > 0).cloned().collect();
    let total: u128 = items.iter().map(|item| item.bytes as u128).sum();
    let theme = Theme::ocean();
    let target = SharedString::from(id.to_owned());
    let mut map = div().id(target.clone()).relative().children(smoke_overlay::probe(target)).w_full().h(px(16.)).flex_none()
        .flex().overflow_hidden().rounded(radius::CONTROL).bg(theme.paper);
    for item in &items {
        map = map.child(tile(item, total, id, selected, &on_select));
    }
    map.into_any_element()
}

pub fn format_percent(bytes: u64, total: u128) -> String {
    if total == 0 { return "0.00%".into(); }
    let percent = bytes as f64 / total as f64 * 100.;
    if bytes > 0 && percent < 0.01 { "<0.01%".into() }
    else if (bytes as u128) < total && percent > 99.99 { ">99.99%".into() }
    else { format!("{percent:.2}%") }
}

fn tile(item: &TreemapItem, total: u128, prefix: &str, selected: Option<&str>, on_select: &PerformanceSelect) -> AnyElement {
    let target = SharedString::from(format!("{prefix}-tile-{}", item.id));
    let hover_id = item.id.clone();
    let click_id = item.id.clone();
    let on_hover = on_select.clone();
    let on_click = on_select.clone();
    let tip = format!("{} · {} B · {} · {}", item.label, item.bytes, format_percent(item.bytes, total), item.detail);
    div().id(target.clone()).relative().children(smoke_overlay::probe(target))
        .w(relative((item.bytes as f64 / total as f64) as f32)).h_full().flex_none()
        .overflow_hidden().bg(item.color.opacity(if selected == Some(item.id.as_str()) { 1. } else { 0.55 }))
        .cursor(CursorStyle::PointingHand)
        .tooltip(tooltip::tooltip(tip))
        .on_hover(move |hovered, window, cx| {
            if *hovered { on_hover(&hover_id, window, cx); }
        })
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            on_click(&click_id, window, cx);
        })
        .into_any_element()
}
