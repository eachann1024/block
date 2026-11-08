use std::rc::Rc;
use gpui::{
    AnyElement, App, CursorStyle, FontWeight, Hsla, IntoElement, MouseButton, ParentElement,
    Pixels, SharedString, Styled, Window, div, prelude::*, px, relative,
};
use crate::{theme::Theme, ui::{performance_rows::{numeric_slot, tabular_nums}, radius, smoke_overlay}};

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
    // Roll over at 999.95 so `{value:.1}` never prints "1000.0" and the readout
    // stays within the "999.9 MiB" slot.
    while value >= 999.95 && unit < UNITS.len() - 1 { value /= 1024.; unit += 1; }
    if unit == 0 { format!("{bytes} B") } else { format!("{value:.1} {}", UNITS[unit]) }
}

pub fn format_percent(bytes: u64, total: u128) -> String {
    if total == 0 { return "0.00%".into(); }
    let percent = bytes as f64 / total as f64 * 100.;
    if bytes > 0 && percent < 0.01 { "<0.01%".into() }
    else if (bytes as u128) < total && percent > 99.99 { ">99.99%".into() }
    else { format!("{percent:.2}%") }
}

fn pad_figures(text: &str, width: usize) -> String {
    let extra = width.saturating_sub(text.chars().count());
    let mut out = String::with_capacity(text.len() + extra * 3);
    out.extend(std::iter::repeat('\u{2007}').take(extra));
    out.push_str(text);
    out
}

fn cpu_body(value: f64) -> String {
    if !value.is_finite() || value < 0. {
        "0.0".into()
    } else if value >= 999.95 {
        format!("{:.0}", value.min(9999.))
    } else {
        format!("{value:.1}")
    }
}

pub fn format_cpu(value: f64) -> String {
    format!("{}%", cpu_body(value))
}

/// CPU text padded to `999.9%`, so 5.2% and 100.0% take the same width inside one line.
pub fn format_cpu_field(value: Option<f64>) -> String {
    let body = value.filter(|value| value.is_finite()).map(|value| cpu_body(value)).unwrap_or_else(|| "—".into());
    format!("{}%", pad_figures(&body, 5))
}

/// Thread count padded to four digits.
pub fn format_count_field(value: Option<u32>) -> String {
    match value {
        Some(value) => pad_figures(&value.min(9_999).to_string(), 4),
        None => pad_figures("—", 4),
    }
}

pub fn treemap(
    id: &str,
    items: &[TreemapItem],
    columns: usize,
    selected: Option<&str>,
    on_select: PerformanceSelect,
    bytes_slot: Pixels,
    percent_slot: Pixels,
) -> AnyElement {
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
                tile(item, weight / sum, total, id, selected, &on_select, bytes_slot, percent_slot)
            }))
        }))
        .into_any_element()
}

fn tile(
    item: &TreemapItem,
    share: f32,
    total: u128,
    prefix: &str,
    selected: Option<&str>,
    on_select: &PerformanceSelect,
    bytes_slot: Pixels,
    percent_slot: Pixels,
) -> AnyElement {
    let theme = Theme::ocean();
    let active = selected == Some(item.id.as_str());
    let target = SharedString::from(format!("{prefix}-tile-{}", item.id));
    let hover_id = item.id.clone();
    let click_id = item.id.clone();
    let on_hover = on_select.clone();
    let on_click = on_select.clone();
    let percent = format_percent(item.bytes, total);
    let ratio = if total == 0 || item.bytes == 0 {
        0.
    } else {
        (item.bytes as f64 / total as f64).clamp(0.03, 1.0) as f32
    };
    div().id(target.clone()).relative().children(smoke_overlay::probe(target))
        .w(relative(share)).flex_1().min_w_0().h(px(62.)).px(px(11.)).py(px(7.)).rounded(radius::CONTROL)
        .border_1().border_color(if active { theme.accent } else { theme.hairline })
        .bg(if active { theme.selected } else { theme.paper })
        .hover(|s| s.bg(if active { theme.selected } else { theme.hover }).border_color(theme.accent))
        .flex().flex_col().justify_between().cursor(CursorStyle::PointingHand)
        .on_hover(move |hovered, window, cx| { if *hovered { on_hover(&hover_id, window, cx); } })
        .on_mouse_down(MouseButton::Left, move |_, window, cx| on_click(&click_id, window, cx))
        .child(div().w_full().flex().items_center().gap(px(5.)).text_size(px(13.)).text_color(theme.text)
            .child(div().size(px(7.)).flex_none().rounded_full().bg(item.color))
            .child(div().flex_1().min_w_0().font_weight(FontWeight(500.)).overflow_hidden().whitespace_nowrap().text_ellipsis().child(item.label.clone()))
            .child(numeric_slot(format_bytes(item.bytes), bytes_slot, px(13.), FontWeight(560.))))
        .child(div().w_full().flex().items_center().gap(px(8.)).text_size(px(12.)).text_color(theme.text_secondary)
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().font_features(tabular_nums()).child(item.detail.clone()))
            .child(numeric_slot(percent, percent_slot, px(12.), FontWeight::NORMAL)))
        .child(div().w_full().h(px(3.)).rounded(radius::PILL).bg(theme.shell).overflow_hidden()
            .when(ratio > 0., |bar| bar.child(div().h_full().w(relative(ratio)).rounded(radius::PILL).bg(item.color))))
        .into_any_element()
}
