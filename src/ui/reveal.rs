use gpui::{
    App, CursorStyle, Div, Hsla, MouseButton, MouseDownEvent, Pixels, Stateful,
    Window, div, prelude::*, px, rgb, rgba, transparent_black,
};
use crate::{
    icons::{Icon, IconColors, icon},
    theme::{Theme, build_tooltip, is_dark, tr},
};

pub fn hover_icon(glyph: Icon, size: f32, base: Hsla, hover: Hsla) -> Div {
    let theme = Theme::ocean();
    div()
        .relative()
        .flex_none()
        .size(px(size))
        .child(div().group_hover("hover-icon", |s| s.invisible()).child(icon(glyph, px(size), theme.mono_icon_colors(base))))
        .child(
            div().absolute().top_0().left_0().invisible()
                .group_hover("hover-icon", |s| s.visible())
                .child(icon(glyph, px(size), theme.icon_colors(hover))),
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
        .rounded(crate::ui::radius::MICRO_SM)
        .bg(bg)
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .active(move |s| s.bg(active_bg))
        .child(icon(Icon::Plus, px(12.), IconColors { line: fg, fill: transparent_black(), accent: fg }))
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
        .rounded(crate::ui::radius::CONTROL)
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
        .rounded(crate::ui::radius::CONTROL)
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
        .rounded(crate::ui::radius::CONTROL)
        .flex()
        .items_center()
        .justify_center()
        .hover(move |s| s.bg(bg_hover))
        .cursor(CursorStyle::PointingHand)
        .tooltip(move |window, cx| build_tooltip(tip, window, cx))
        .child(hover_icon(glyph, 14., base_fg, hover_fg))
        .on_mouse_down(MouseButton::Left, listener)
}

pub fn top_insert_slot(
    id: (&'static str, usize),
    handle_w: f32,
    row_gap: f32,
    right_inset: f32,
    visible: bool,
    listener: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let pill_bg: Hsla = if dark { rgba(0x2d426b80).into() } else { rgba(0xdbeafecc).into() };
    let pill_border: Hsla = theme.accent.opacity(if dark { 0.55 } else { 0.45 });
    let bar_bg: Hsla = if dark { rgba(0x2d426b38).into() } else { rgba(0xdbeafe66).into() };
    let bar_hover_bg: Hsla = if dark { rgba(0x2d426b66).into() } else { rgba(0xdbeafeb3).into() };
    div()
        .id(id)
        .group("top-slot")
        .w_full()
        .min_w_0()
        .pr(px(right_inset))
        .pb(px(6.))
        .flex()
        .items_center()
        .cursor(CursorStyle::PointingHand)
        .opacity(if visible { 1. } else { 0. })
        .group_hover("top-slot", |s| s.opacity(1.))
        .on_mouse_down(MouseButton::Left, listener)
        .child(
            div().w(px(handle_w)).mr(px(row_gap)).flex_none().flex().justify_end().items_center().child(
                div().size(px(24.)).rounded(crate::ui::radius::CONTROL).bg(pill_bg).border_1().border_color(pill_border)
                    .flex().items_center().justify_center()
                    .child(icon(Icon::Plus, px(13.), IconColors { line: theme.accent, fill: transparent_black(), accent: theme.accent })),
            ),
        )
        .child(
            div().flex_1().min_w_0().max_w(theme.content_width).h(px(28.)).px(px(12.)).rounded(crate::ui::radius::CONTROL)
                .bg(bar_bg).border_1().border_color(pill_border)
                .group_hover("top-slot", move |s| s.bg(bar_hover_bg).border_color(theme.accent))
                .flex().items_center().justify_between()
                .child(div().flex().items_center().gap(px(6.)).text_size(px(13.)).text_color(theme.accent)
                    .child(tr("点击在文档最上方插入新行", "Click to insert a new line at the top")))
                .child(div().flex().items_center().gap(px(4.)).text_size(px(12.)).text_color(theme.text_secondary)
                    .child(tr("首字符 ↑ / ⌫", "↑ / ⌫ at start"))),
        )
}
