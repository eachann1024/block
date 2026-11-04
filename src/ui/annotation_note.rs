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
        .rounded(px(6.))
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

/// Single-line micro strip (36px): `[dot] text [actions]`, no card, no shadow.
pub fn strip(id: impl Into<ElementId>, active: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id(id).w_full().h(px(36.)).px(px(8.)).rounded(px(6.)).flex().items_center().gap(px(8.))
        .when(active, |s| s.bg(theme.selected))
        .hover(move |s| s.bg(theme.hover))
}

/// Ellipsized single-line annotation text.
pub fn strip_text(text: impl Into<SharedString>) -> Div {
    div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
        .text_size(px(14.)).line_height(px(20.)).text_color(Theme::ocean().text_secondary).child(text.into())
}

/// Compact glyph action (`✓` / `↺`) inside a strip.
pub fn glyph_action(id: impl Into<ElementId>, glyph: &'static str, accent: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id(id).size(px(24.)).flex_none().rounded(px(4.)).flex().items_center().justify_center()
        .text_size(px(14.)).text_color(if accent { theme.accent } else { theme.text_secondary })
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover)).child(glyph)
}

/// Round numbered margin badge for narrow windows, tinted by status.
pub fn badge(id: impl Into<ElementId>, index: usize, status: u32, active: bool) -> Stateful<Div> {
    div().id(id).size(px(22.)).rounded_full().bg(rgb(status)).flex().items_center().justify_center()
        .text_size(px(14.)).line_height(px(16.)).font_weight(FontWeight(500.)).text_color(rgb(0x2b2b2b))
        .cursor(CursorStyle::PointingHand).when(active, |s| s.border_2().border_color(Theme::ocean().accent))
        .child(format!("{index}"))
}
