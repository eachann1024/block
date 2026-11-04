//! Small stateless widgets of the "生成图片" sidebar: section, segmented control, style swatches, button.
use std::rc::Rc;

use gpui::{
    App, ClickEvent, FontWeight, Hsla, IntoElement, ParentElement, Styled, Window, div, prelude::*,
    px,
};

use super::format::ExportFormat;
use super::palette::ExportTheme;
use crate::icons::{Icon, icon};
use crate::theme::Theme;

fn c(value: u32) -> Hsla {
    gpui::rgb(value).into()
}

pub(super) fn section(title: &'static str, content: impl IntoElement) -> impl IntoElement {
    let theme = Theme::ocean();
    let head = div().text_size(px(14.)).line_height(px(20.)).font_weight(FontWeight(560.)).text_color(theme.text).child(title);
    div().flex().flex_col().gap(px(8.)).child(head).child(content)
}

/// Five-segment format selector: PNG · PDF · Word · MD · HTML.
pub(super) fn format_bar(
    current: ExportFormat,
    on_pick: Rc<dyn Fn(&ExportFormat, &mut Window, &mut App)>,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let mut track = div().flex().p(px(3.)).bg(theme.shell).border_1().border_color(theme.hairline).rounded(px(8.)).gap(px(2.));
    for (index, fmt) in ExportFormat::ALL.into_iter().enumerate() {
        let on_pick = on_pick.clone();
        let selected = fmt == current;
        let mut seg = div()
            .id(("export-format", index))
            .flex_1()
            .h(px(32.))
            .flex()
            .items_center()
            .justify_center()
            .rounded(px(6.))
            .border_1()
            .border_color(gpui::transparent_black())
            .text_size(px(14.))
            .font_weight(FontWeight(if selected { 500. } else { 460. }))
            .text_color(theme.text_secondary)
            .cursor_pointer()
            .child(fmt.label());
        if selected {
            seg = seg
                .bg(theme.selected)
                .border_color(theme.accent)
                .text_color(theme.text);
        } else {
            seg = seg.hover(|h| h.text_color(theme.text));
        }
        seg = seg.on_click(move |_, window, cx| on_pick(&fmt, window, cx));
        track = track.child(seg);
    }
    track
}

/// Two-state segmented track with accent selection ring.
pub(super) fn segmented(
    id: &'static str,
    items: [(&'static str, bool, bool); 2],
    on_pick: Rc<dyn Fn(&usize, &mut Window, &mut App)>,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let mut track = div()
        .flex()
        .p(px(3.))
        .bg(theme.shell)
        .border_1()
        .border_color(theme.hairline)
        .rounded(px(8.))
        .gap(px(2.));
    for (index, (label, selected, enabled)) in items.into_iter().enumerate() {
        let on_pick = on_pick.clone();
        let mut seg = div()
            .id((id, index))
            .flex_1()
            .h(px(32.))
            .flex()
            .items_center()
            .justify_center()
            .rounded(px(6.))
            .border_1()
            .border_color(gpui::transparent_black())
            .text_size(px(14.))
            .font_weight(FontWeight(if selected { 500. } else { 460. }))
            .text_color(theme.text_secondary)
            .child(label);
        if selected {
            seg = seg
                .bg(theme.selected)
                .border_color(theme.accent)
                .text_color(theme.text);
        }
        seg = if enabled {
            seg.cursor_pointer()
                .when(!selected, |s| s.hover(|h| h.text_color(theme.text)))
                .on_click(move |_, window, cx| on_pick(&index, window, cx))
        } else {
            seg.opacity(0.4)
        };
        track = track.child(seg);
    }
    track
}

/// 2×2 grid of paper style swatch buttons for the active light or dark group.
pub(super) fn style_grid(
    current: ExportTheme,
    on_pick: Rc<dyn Fn(&ExportTheme, &mut Window, &mut App)>,
) -> impl IntoElement {
    let items = ExportTheme::group(current.is_dark());
    let row0 = div()
        .flex()
        .gap(px(8.))
        .child(swatch_card(0, items[0], current == items[0], on_pick.clone()))
        .child(swatch_card(1, items[1], current == items[1], on_pick.clone()));
    let row1 = div()
        .flex()
        .gap(px(8.))
        .child(swatch_card(2, items[2], current == items[2], on_pick.clone()))
        .child(swatch_card(3, items[3], current == items[3], on_pick));
    div().flex().flex_col().gap(px(8.)).child(row0).child(row1)
}

fn swatch_card(
    index: usize,
    style: ExportTheme,
    selected: bool,
    on_pick: Rc<dyn Fn(&ExportTheme, &mut Window, &mut App)>,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let (paper_hex, accent_hex, inner_hex) = style.swatch();
    let inner = div()
        .w(px(18.))
        .h(px(14.))
        .bg(c(inner_hex))
        .border_t(px(2.))
        .border_color(c(accent_hex));
    let chip = div()
        .w(px(28.))
        .h(px(22.))
        .flex_none()
        .rounded(px(4.))
        .bg(c(paper_hex))
        .border_1()
        .border_color(c(if style.is_dark() { 0x3a3a3a } else { 0xd8d2c5 }))
        .overflow_hidden()
        .flex()
        .items_end()
        .justify_center()
        .child(inner);
    let label = div().flex_1().min_w_0().truncate().child(style.label());
    let base = div()
        .id(("export-style", index))
        .flex_1()
        .min_w_0()
        .h(px(52.))
        .px(px(10.))
        .rounded(px(10.))
        .border_1()
        .flex()
        .items_center()
        .gap(px(10.))
        .cursor_pointer()
        .text_size(px(14.))
        .font_weight(FontWeight(if selected { 500. } else { 460. }))
        .child(chip)
        .child(label)
        .when(selected, |d| {
            d.child(icon(
                Icon::Check,
                px(14.),
                theme.icon_colors(theme.accent),
            ))
        });
    let base = if selected {
        base.bg(theme.selected)
            .border_color(theme.accent)
            .text_color(theme.text)
    } else {
        base.bg(theme.paper)
            .border_color(theme.btn_border)
            .text_color(theme.text_secondary)
            .hover(move |s| s.bg(theme.btn_hover).text_color(theme.text))
    };
    base.on_click(move |_, window, cx| on_pick(&style, window, cx))
}

pub(super) fn switch_row(
    id: &'static str,
    label: &'static str,
    on: bool,
    on_toggle: Rc<dyn Fn(&ClickEvent, &mut Window, &mut App)>,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let knob = div()
        .absolute()
        .top(px(2.))
        .left(px(if on { 16. } else { 2. }))
        .size(px(16.))
        .rounded(px(8.))
        .bg(c(0xffffff));
    div()
        .h(px(40.))
        .flex()
        .items_center()
        .justify_between()
        .child(div().text_size(px(14.)).text_color(theme.text).child(label))
        .child(
            div()
                .id(id)
                .relative()
                .w(px(34.))
                .h(px(20.))
                .rounded(px(10.))
                .cursor_pointer()
                .bg(if on { theme.accent } else { theme.btn_border })
                .on_click(move |ev, window, cx| on_toggle(ev, window, cx))
                .child(knob),
        )
}

pub(super) fn fmt_int(n: u32) -> String {
    let s = n.to_string();
    let mut out = String::new();
    for (i, ch) in s.chars().rev().enumerate() {
        if i > 0 && i % 3 == 0 {
            out.push(',');
        }
        out.push(ch);
    }
    out.chars().rev().collect()
}

pub(super) fn button(
    id: &'static str,
    label: &'static str,
    glyph: impl IntoElement,
    primary: bool,
    enabled: bool,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let base = div()
        .id(id)
        .h(px(38.))
        .px(px(16.))
        .flex()
        .items_center()
        .justify_center()
        .gap(px(6.))
        .rounded(px(8.))
        .text_size(px(14.))
        .font_weight(FontWeight(500.))
        .child(glyph)
        .child(label);
    let base = if primary {
        base.flex_1().bg(theme.accent).text_color(c(0xffffff))
    } else {
        base.flex_none()
            .bg(theme.shell)
            .border_1()
            .border_color(theme.btn_border)
            .text_color(theme.text)
    };
    if enabled {
        let hover = if primary { c(0x466ec0) } else { theme.btn_hover };
        base.cursor_pointer().hover(move |s| s.bg(hover))
    } else {
        base.opacity(0.5)
    }
}

/// Pixel size of a PNG from its IHDR chunk (width/height are big-endian at bytes 16..24).
pub(super) fn png_size(bytes: &[u8]) -> Option<(f32, f32)> {
    let field =
        |at: usize| Some(u32::from_be_bytes(bytes.get(at..at + 4)?.try_into().ok()?) as f32);
    bytes.starts_with(&[0x89, b'P', b'N', b'G']).then_some(())?;
    Some((field(16)?, field(20)?)).filter(|(w, h)| *w > 0. && *h > 0.)
}
