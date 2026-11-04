//! Shared menu look for the block, slash and table menus (design contract: white shell, 1px border,
//! radius 8, padding 6, row gap 2, no shadow; items min 32px, 14/20 text, radius 4, hover tint).
use gpui::{CursorStyle, ElementId, Hsla, Stateful, Div, div, hsla, prelude::*, px};
use crate::theme::Theme;

/// Destructive entries (delete) use red text.
pub(super) fn danger_color() -> Hsla { hsla(0.0, 0.62, 0.45, 1.) }

/// Menu shell: callers add the width, scrolling limits and mouse handlers.
pub(super) fn shell(id: impl Into<ElementId>, theme: &Theme) -> Stateful<Div> {
    div().id(id).p(px(6.)).rounded(px(8.)).border_1().border_color(theme.menu_border).bg(theme.menu)
        .flex().flex_col().gap(px(2.)).text_size(theme.ui_font).line_height(px(20.)).text_color(theme.text)
}

/// Blank space between groups: 2px row gap on both sides plus this makes the 6px group gap.
pub(super) fn group_gap() -> Div { div().h(px(2.)).flex_none() }

/// Hairline divider between groups in the slash command menu.
pub(super) fn separator(theme: &Theme) -> Div {
    div().mx(px(4.)).my(px(3.)).h(px(1.)).flex_none().bg(theme.hairline)
}

/// One menu row; `active` is the keyboard highlight and `disabled` dims it and drops hover and clicks.
pub(super) fn item(id: impl Into<ElementId>, label: &'static str, danger: bool, active: bool, disabled: bool,
    theme: &Theme) -> Stateful<Div> {
    let hover = theme.hover; let selected = theme.selected;
    div().id(id).w_full().min_h(theme.menu_item_height).flex_none().px(px(8.)).rounded(px(4.))
        .flex().items_center().gap(px(8.))
        .text_color(if danger { danger_color() } else { theme.text })
        .when(active, move |s| s.bg(selected))
        .when(disabled, |s| s.opacity(0.4))
        .when(!disabled, move |s| s.cursor(CursorStyle::PointingHand).hover(move |s| s.bg(hover)))
        .child(div().flex_1().min_w_0().child(label))
}
