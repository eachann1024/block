use gpui::{CursorStyle, Div, ElementId, FontWeight, IntoElement, SharedString, Stateful, div, prelude::*, px, rgb, rgba};
use crate::theme::Theme;

/// Cardless margin note with a single semantic status rule.
pub fn rail_note(id: impl Into<ElementId>, status: u32, content: impl IntoElement) -> Stateful<Div> {
    div().id(id).w_full().border_l_2().border_color(rgb(status)).pl(px(10.)).child(content)
}

/// Emoji reaction pill in the right rail (`❤️ 1`, `👀 1`), matching the Tiptap/Claude artifact spec.
pub fn reaction_pill(id: impl Into<ElementId>, emoji: &str, active: bool) -> Stateful<Div> {
    let dark = crate::theme::is_dark();
    let (fg, bg, hover_bg, border) = if dark {
        (
            rgb(0x8ab4f8),
            if active { rgba(0x4c8bf540) } else { rgba(0x4c8bf526) },
            rgba(0x4c8bf540),
            rgba(0x8ab4f866),
        )
    } else {
        (
            rgb(0x1757ac),
            if active { rgba(0x257de02e) } else { rgba(0x257de01a) },
            rgba(0x257de02e),
            rgba(0x1757ac66),
        )
    };
    div()
        .id(id)
        .h(px(24.))
        .px(px(8.))
        .gap(px(4.))
        .rounded_full()
        .bg(bg)
        .text_color(fg)
        .when(active, |s| s.border_1().border_color(border))
        .hover(move |s| s.bg(hover_bg))
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .child(div().text_size(px(14.)).line_height(px(16.)).child(emoji.to_owned()))
        .child(div().text_size(px(12.)).line_height(px(16.)).font_weight(FontWeight(500.)).child("1"))
}

/// Emoji picker button inside the reaction popover, highlighting already-selected reactions.
pub fn reaction_picker_item(id: impl Into<ElementId>, emoji: &str, selected: bool) -> Stateful<Div> {
    let dark = crate::theme::is_dark();
    let (sel_bg, sel_border, sel_fg) = if dark {
        (rgba(0x4c8bf526), rgba(0x8ab4f84d), rgb(0x8ab4f8))
    } else {
        (rgba(0x257de01a), rgba(0x1757ac4d), rgb(0x1757ac))
    };
    let hover_bg = if selected { gpui::Hsla::from(sel_bg) } else { Theme::ocean().hover };
    div()
        .id(id)
        .size(px(30.))
        .rounded(crate::ui::radius::CONTROL)
        .flex()
        .items_center()
        .justify_center()
        .text_size(px(16.))
        .cursor(CursorStyle::PointingHand)
        .when(selected, |s| s.bg(sel_bg).border_1().border_color(sel_border).text_color(sel_fg))
        .hover(move |s| s.bg(hover_bg))
        .child(emoji.to_owned())
}

/// 6px status dot (pending / generated).
pub fn status_dot(status: u32) -> Div {
    div().size(px(6.)).flex_none().rounded_full().bg(rgb(status))
}

/// 8px status dot for the in-place narrow popover header.
pub fn popover_dot(status: u32) -> Div {
    div().size(px(8.)).flex_none().rounded_full().bg(rgb(status))
}

/// Wide-rail micro strip container (`border-radius: 8px`, `padding: 8px 10px`, 1px border).
pub fn strip(id: impl Into<ElementId>, active: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id(id).w_full().px(px(10.)).py(px(8.)).rounded(crate::ui::radius::CONTROL).bg(theme.menu).border_1()
        .border_color(if active { theme.accent } else { theme.sidebar_border })
        .flex().flex_col().gap(px(6.))
        .when(!active, |s| s.hover(move |h| h.bg(theme.hover)))
}

/// Annotation text inside a micro strip: single-line ellipsized at rest, full multi-line when expanded.
pub fn strip_text(text: impl Into<SharedString>, expanded: bool) -> Div {
    div().flex_1().min_w_0().text_size(px(14.)).line_height(px(20.)).text_color(Theme::ocean().text)
        .when(!expanded, |d| d.overflow_hidden().whitespace_nowrap().text_ellipsis())
        .child(text.into())
}

/// Compact 22×22 glyph action (`✓` / `↺`) inside a wide-rail micro strip.
pub fn glyph_action(id: impl Into<ElementId>, glyph: &'static str, accent: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id(id).size(px(22.)).flex_none().rounded(crate::ui::radius::MICRO_SM).flex().items_center().justify_center()
        .text_size(px(14.)).line_height(px(16.))
        .when(accent, |b| b.bg(theme.selected).text_color(theme.accent))
        .when(!accent, |b| b.text_color(theme.text_secondary))
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover)).child(glyph)
}

/// Follow-up submit button (`↵` in wide rail, `发送 ↵` in narrow popover).
pub fn send_button(id: impl Into<ElementId>, label: impl Into<SharedString>, compact: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    let label = label.into();
    div().id(id).flex_none().border_1().border_color(theme.accent).bg(theme.selected).text_color(theme.text)
        .when(compact, |b| b.size(px(28.)).rounded(crate::ui::radius::CONTROL))
        .when(!compact, |b| b.h(px(30.)).px(px(12.)).rounded(crate::ui::radius::CONTROL).font_weight(FontWeight(500.)))
        .flex().items_center().justify_center().text_size(px(14.)).line_height(px(20.))
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
        .child(crate::ui::shortcut_label::shortcut_label(
            label,
            crate::ui::shortcut_label::ShortcutStyle::ui(px(11.), px(16.), FontWeight::MEDIUM, theme.text),
        ))
}

/// Dismiss pill (`收起`) in the narrow in-place popover header.
pub fn close_pill(id: impl Into<ElementId>, label: impl Into<SharedString>) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id(id).h(px(26.)).px(px(8.)).flex_none().rounded(crate::ui::radius::CONTROL).bg(theme.shell).text_color(theme.text_secondary)
        .flex().items_center().justify_center().text_size(px(14.)).line_height(px(20.))
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover).text_color(theme.text))
        .child(label.into())
}

/// 24×24 round numbered margin badge for narrow windows, styled per Scheme A spec.
pub fn badge(id: impl Into<ElementId>, index: usize, generated: bool, active: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = crate::theme::is_dark();
    let bg = if generated {
        theme.selected
    } else if dark {
        rgb(0x4a3c21).into()
    } else {
        rgb(0xfef0d1).into()
    };
    let border = if active || generated { theme.accent } else { theme.btn_border };
    div().id(id).size(px(24.)).rounded_full().bg(bg).border_1().border_color(border)
        .flex().items_center().justify_center()
        .text_size(px(14.)).line_height(px(16.)).font_weight(FontWeight(600.)).text_color(theme.text)
        .cursor(CursorStyle::PointingHand).hover(move |s| s.border_color(theme.accent))
        .child(format!("{index}"))
}
