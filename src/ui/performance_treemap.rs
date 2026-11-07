use std::rc::Rc;
use gpui::{AnyElement, App, CursorStyle, Hsla, IntoElement, MouseButton, ParentElement,
    SharedString, Styled, Window, div, prelude::*, px, relative};
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
    while value >= 1024. && unit < UNITS.len() - 1 { value /= 1024.; unit += 1; }
    if unit == 0 { format!("{bytes} B") } else { format!("{value:.1} {}", UNITS[unit]) }
}

pub fn format_percent(bytes: u64, total: u128) -> String {
    if total == 0 { return "0.00%".into(); }
    let percent = bytes as f64 / total as f64 * 100.;
    if bytes > 0 && percent < 0.01 { "<0.01%".into() }
    else if (bytes as u128) < total && percent > 99.99 { ">99.99%".into() }
    else { format!("{percent:.2}%") }
}

pub fn treemap(id: &str, items: &[TreemapItem], columns: usize, selected: Option<&str>, on_select: PerformanceSelect) -> AnyElement {
    let total = items.iter().map(|item| item.bytes as u128).sum();
    let target = SharedString::from(id.to_owned());
    let rows: Vec<&[TreemapItem]> = if items.len() == 7 && columns == 3 {
        vec![&items[..3], &items[3..5], &items[5..]]
    } else { items.chunks(columns).collect() };
    div().id(target.clone()).relative().children(smoke_overlay::probe(target)).w_full().flex_none()
        .flex().flex_col().gap(px(6.))
        .children(rows.into_iter().map(|row| {
            let weights: Vec<f32> = row.iter().map(|item| (1. + ((item.bytes as f64 + 1.).log10() / 14.).clamp(0., 0.8)) as f32).collect();
            let sum: f32 = weights.iter().sum::<f32>().max(1.);
            div().w_full().flex().gap(px(6.)).children(row.iter().zip(weights).map(|(item, weight)| {
                tile(item, weight / sum, total, id, selected, &on_select)
            }))
        }))
        .into_any_element()
}

fn tile(item: &TreemapItem, share: f32, total: u128, prefix: &str, selected: Option<&str>, on_select: &PerformanceSelect) -> AnyElement {
    let theme = Theme::ocean();
    let active = selected == Some(item.id.as_str());
    let target = SharedString::from(format!("{prefix}-tile-{}", item.id));
    let hover_id = item.id.clone();
    let click_id = item.id.clone();
    let on_hover = on_select.clone();
    let on_click = on_select.clone();
    let percent = format_percent(item.bytes, total);
    let tip = format!("{} · {} B · {} · {}", item.label, item.bytes, percent, item.detail);
    div().id(target.clone()).relative().children(smoke_overlay::probe(target))
        .w(relative(share)).flex_1().min_w_0().h(px(58.)).px(px(9.)).py(px(5.)).rounded(radius::CONTROL)
        .border_1().border_color(if active { theme.accent } else { item.color.opacity(0.42) })
        .bg(item.color.opacity(if active { 0.28 } else { 0.18 }))
        .flex().flex_col().justify_center().gap(px(2.)).cursor(CursorStyle::PointingHand)
        .tooltip(tooltip::tooltip(tip))
        .on_hover(move |hovered, window, cx| { if *hovered { on_hover(&hover_id, window, cx); } })
        .on_mouse_down(MouseButton::Left, move |_, window, cx| on_click(&click_id, window, cx))
        .child(div().w_full().flex().items_center().gap(px(5.)).text_size(px(13.)).text_color(theme.text)
            .child(div().size(px(7.)).flex_none().rounded_full().bg(item.color))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(item.label.clone()))
            .child(div().flex_none().child(format_bytes(item.bytes))))
        .child(div().w_full().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
            .text_size(px(12.)).text_color(theme.text_secondary).child(format!("{percent} · {}", item.detail)))
        .into_any_element()
}
