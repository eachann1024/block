use std::rc::Rc;

use gpui::{AnyElement, App, CursorStyle, FontWeight, Hsla, IntoElement, MouseButton,
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
    let mut map = div().id(target.clone()).relative().children(smoke_overlay::probe(target)).w_full().h(px(212.)).flex_none()
        .overflow_hidden().rounded(radius::CONTROL).bg(theme.paper);
    if !items.is_empty() {
        map = map.child(partition(&items, total, total, id, selected, &on_select, true, 1., 1.));
    }
    map.into_any_element()
}

fn partition(
    items: &[TreemapItem], total: u128, root_total: u128, prefix: &str, selected: Option<&str>,
    on_select: &PerformanceSelect, horizontal: bool, width: f64, height: f64,
) -> AnyElement {
    if items.len() == 1 {
        return tile(&items[0], root_total, prefix, selected, on_select, width, height);
    }
    let mut left_total = 0_u128;
    let mut split = 1;
    for (index, item) in items.iter().enumerate().take(items.len() - 1) {
        left_total += item.bytes as u128;
        split = index + 1;
        if left_total * 2 >= total { break; }
    }
    let fraction = left_total as f64 / total as f64;
    let (left_width, left_height, right_width, right_height) = if horizontal {
        (width * fraction, height, width * (1. - fraction), height)
    } else {
        (width, height * fraction, width, height * (1. - fraction))
    };
    let first = partition(&items[..split], left_total, root_total, prefix, selected, on_select,
        !horizontal, left_width, left_height);
    let second = partition(&items[split..], total - left_total, root_total, prefix, selected, on_select,
        !horizontal, right_width, right_height);
    let mut left = div().overflow_hidden().flex_none();
    let mut right = div().overflow_hidden().flex_none();
    if horizontal {
        left = left.w(relative(fraction as f32)).h_full();
        right = right.w(relative((1. - fraction) as f32)).h_full();
    } else {
        left = left.h(relative(fraction as f32)).w_full();
        right = right.h(relative((1. - fraction) as f32)).w_full();
    }
    div().size_full().flex().when(!horizontal, |element| element.flex_col())
        .child(left.child(first)).child(right.child(second)).into_any_element()
}

fn tile(item: &TreemapItem, total: u128, prefix: &str, selected: Option<&str>, on_select: &PerformanceSelect,
    width: f64, height: f64) -> AnyElement {
    let theme = Theme::ocean();
    let target = SharedString::from(format!("{prefix}-tile-{}", item.id));
    let hover_id = item.id.clone();
    let click_id = item.id.clone();
    let on_hover = on_select.clone();
    let on_click = on_select.clone();
    let percent = item.bytes as f64 / total as f64 * 100.;
    let tip = format!("{} · {} B · {:.2}% · {}", item.label, item.bytes, percent, item.detail);
    div().id(target.clone()).relative().children(smoke_overlay::probe(target))
        .size_full().overflow_hidden().bg(item.color.opacity(0.25))
        .border_1().border_color(if selected == Some(item.id.as_str()) { theme.accent } else { theme.paper })
        .cursor(CursorStyle::PointingHand)
        .tooltip(tooltip::tooltip(tip))
        .on_hover(move |hovered, window, cx| {
            if *hovered { on_hover(&hover_id, window, cx); }
        })
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            on_click(&click_id, window, cx);
        })
        .when(width >= 0.16 && height >= 0.24, |tile| {
            tile.p(px(8.)).flex().flex_col().gap(px(3.)).text_color(theme.text)
                .child(div().overflow_hidden().whitespace_nowrap().text_ellipsis()
                    .text_size(px(14.)).font_weight(FontWeight(560.)).child(item.label.clone()))
                .child(div().overflow_hidden().whitespace_nowrap().text_ellipsis()
                    .text_size(px(14.)).child(format_bytes(item.bytes)))
        })
        .into_any_element()
}
