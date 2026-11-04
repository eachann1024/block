use gpui::{
    App, CursorStyle, Div, Hsla, IntoElement, MouseButton, MouseDownEvent, Pixels, Stateful,
    Window, div, prelude::*, px, rgb, rgba, transparent_black,
};
use crate::{
    icons::{Icon, IconColors, icon},
    theme::{Theme, build_tooltip, is_dark, tr},
};

pub fn hover_icon(glyph: Icon, size: f32, base: Hsla, hover: Hsla) -> impl IntoElement {
    let theme = Theme::ocean();
    let tint = |line| IconColors { line, fill: transparent_black(), accent: theme.accent };
    div()
        .relative()
        .flex_none()
        .size(px(size))
        .child(div().group_hover("hover-icon", |s| s.invisible()).child(icon(glyph, px(size), tint(base))))
        .child(
            div().absolute().top_0().left_0().invisible()
                .group_hover("hover-icon", |s| s.visible())
                .child(icon(glyph, px(size), tint(hover))),
        )
}

pub fn table_add_bar(id: &'static str, w: Pixels, h: Pixels, tip: &'static str) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let bg: Hsla = if dark { rgb(0x262626).into() } else { rgba(0x1f1e1d0d).into() };
    let hover_bg: Hsla = if dark { rgb(0x303030).into() } else { rgba(0x1f1e1d1a).into() };
    let active_bg: Hsla = if dark { rgb(0x3a3a3a).into() } else { rgba(0x1f1e1d33).into() };
    let fg: Hsla = if dark { theme.text_secondary } else { rgb(0x73726c).into() };
    div()
        .id(id)
        .w(w)
        .h(h)
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(4.))
        .bg(bg)
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .active(move |s| s.bg(active_bg))
        .child(icon(Icon::Plus, px(12.), theme.icon_colors(fg)))
        .tooltip(move |window, cx| build_tooltip(tip, window, cx))
}

pub fn grip_button(
    id: (&'static str, usize),
    active: bool,
    listener: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let bg_hover: Hsla = if dark { rgb(0x2c2c2a).into() } else { rgb(0xf5f4ed).into() };
    let base_fg: Hsla = if dark { theme.text.opacity(0.55) } else { rgba(0x1f1e1d8c).into() };
    let hover_fg: Hsla = if dark { theme.text } else { rgb(0x1f1e1d).into() };
    let idle_fg = if active { hover_fg } else { base_fg };
    div()
        .id(id)
        .group("hover-icon")
        .size(px(26.))
        .flex_none()
        .rounded(px(8.))
        .flex()
        .items_center()
        .justify_center()
        .when(active, move |s| s.bg(bg_hover))
        .hover(move |s| s.bg(bg_hover))
        .cursor(CursorStyle::PointingHand)
        .child(
            div()
                .relative()
                .flex_none()
                .w(px(8.))
                .h(px(12.))
                .child(div().group_hover("hover-icon", |s| s.invisible()).child(super::table_controls::dots(true, idle_fg)))
                .child(
                    div()
                        .absolute()
                        .top_0()
                        .left_0()
                        .invisible()
                        .group_hover("hover-icon", |s| s.visible())
                        .child(super::table_controls::dots(true, hover_fg)),
                ),
        )
        .on_mouse_down(MouseButton::Left, listener)
}

pub fn handle_button(
    glyph: Icon,
    id: (&'static str, usize),
    listener: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let bg_hover: Hsla = if is_dark() { rgb(0x2c2c2a).into() } else { rgba(0x0b0b0b0d).into() };
    div()
        .id(id)
        .group("hover-icon")
        .size(px(24.))
        .flex_none()
        .rounded(px(6.))
        .flex()
        .items_center()
        .justify_center()
        .hover(move |s| s.bg(bg_hover))
        .cursor(CursorStyle::PointingHand)
        .child(hover_icon(glyph, 14., theme.text_secondary, theme.text))
        .on_mouse_down(MouseButton::Left, listener)
}

pub fn fold_button(
    folded: bool,
    id: (&'static str, usize),
    listener: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let bg_hover: Hsla = if dark { rgb(0x2c2c2a).into() } else { rgba(0x0b0b0b0d).into() };
    let base_fg: Hsla = if dark { theme.text_secondary } else { rgb(0x52514e).into() };
    let hover_fg: Hsla = if dark { theme.text } else { rgb(0x1f1e1d).into() };
    let glyph = if folded { Icon::TbChevronRight } else { Icon::TbChevron };
    let tip = if folded { tr("展开章节", "Expand section") } else { tr("折叠章节", "Collapse section") };
    div()
        .id(id)
        .group("hover-icon")
        .size(px(24.))
        .flex_none()
        .rounded(px(6.))
        .flex()
        .items_center()
        .justify_center()
        .hover(move |s| s.bg(bg_hover))
        .cursor(CursorStyle::PointingHand)
        .tooltip(move |window, cx| build_tooltip(tip, window, cx))
        .child(hover_icon(glyph, 14., base_fg, hover_fg))
        .on_mouse_down(MouseButton::Left, listener)
}
