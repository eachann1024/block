use crate::ui::tooltip::TooltipExt;
use gpui::{
    App, CursorStyle, Div, Hsla, MouseButton, MouseDownEvent, Pixels, Stateful,
    Window, div, prelude::*, px, rgb, rgba,
};
use crate::{
    icons::{Icon, icon, stateful_icon},
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

pub fn table_add_bar(id: &'static str, w: Pixels, h: Pixels, zoom: f32, tip: &'static str) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let bg: Hsla = if dark { rgb(0x262626).into() } else { rgba(0x1f1e1d0d).into() };
    let hover_bg: Hsla = if dark { rgb(0x303030).into() } else { rgba(0x1f1e1d1a).into() };
    let active_bg: Hsla = if dark { rgb(0x3a3a3a).into() } else { rgba(0x1f1e1d33).into() };
    let fg: Hsla = if dark { theme.text_secondary } else { rgb(0x73726c).into() };
    div()
        .id(id)
        .group(id)
        .w(w)
        .h(h)
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(4. * zoom))
        .bg(bg)
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .active(move |s| s.bg(active_bg))
        .child(stateful_icon(id, Icon::Plus, px(12. * zoom), false, theme.mono_icon_colors(fg), theme.icon_colors(theme.text)))
        .tooltip_above(move |window, cx| build_tooltip(tip, window, cx))
}

pub fn grip_button(
    id: (&'static str, usize),
    active: bool,
    zoom: f32,
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
        .size(px(26. * zoom))
        .flex_none()
        .rounded(px(6. * zoom))
        .flex()
        .items_center()
        .justify_center()
        .when(active, move |s| s.bg(bg_hover))
        .hover(move |s| s.bg(bg_hover))
        .cursor(if active { CursorStyle::ClosedHand } else { CursorStyle::OpenHand })
        .child(
            div()
                .relative()
                .flex_none()
                .w(px(8. * zoom))
                .h(px(12. * zoom))
                .child(div().group_hover("hover-icon", |s| s.invisible()).child(super::table_controls::dots(true, idle_fg, zoom)))
                .child(
                    div()
                        .absolute()
                        .top_0()
                        .left_0()
                        .invisible()
                        .group_hover("hover-icon", |s| s.visible())
                        .child(super::table_controls::dots(true, hover_fg, zoom)),
                ),
        )
        .on_mouse_down(MouseButton::Left, listener)
}

pub fn handle_button(
    glyph: Icon,
    id: (&'static str, usize),
    zoom: f32,
    listener: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let bg_hover: Hsla = if is_dark() { rgb(0x2c2c2a).into() } else { rgba(0x0b0b0b0d).into() };
    div()
        .id(id)
        .group("hover-icon")
        .size(px(24. * zoom))
        .flex_none()
        .rounded(px(6. * zoom))
        .flex()
        .items_center()
        .justify_center()
        .hover(move |s| s.bg(bg_hover))
        .cursor(CursorStyle::PointingHand)
        .child(hover_icon(glyph, 14. * zoom, theme.text_secondary, theme.text))
        .on_mouse_down(MouseButton::Left, listener)
}

pub fn delete_arm_caret_color() -> Hsla {
    if is_dark() { rgb(0xff857d).into() } else { rgb(0xb92d29).into() }
}

pub fn delete_arm_placeholder_text() -> &'static str { tr("再次按删除键删除此行", "Press Backspace again to delete this line") }
pub fn delete_arm_placeholder_color() -> Hsla { if is_dark() { rgba(0xff857db8).into() } else { rgba(0xb92d29b8).into() } }

pub fn delete_arm_button(id: (&'static str, usize), zoom: f32, listener: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let bg: Hsla = if dark { rgba(0xff73692e).into() } else { rgba(0xd44c471f).into() };
    let hover_bg: Hsla = if dark { rgba(0xff736942).into() } else { rgba(0xd44c4733).into() };
    let border: Hsla = if dark { rgba(0xff73697a).into() } else { rgba(0xb92d2961).into() };
    let fg = delete_arm_caret_color();
    div().id(id).size(px(24. * zoom)).flex_none().rounded(px(6. * zoom)).flex().items_center().justify_center()
        .bg(bg).border_1().border_color(border).hover(move |s| s.bg(hover_bg)).cursor(CursorStyle::PointingHand)
        .children(crate::ui::smoke_overlay::probe("first-line-delete-badge"))
        .child(icon(Icon::KeyBackspace, px(15. * zoom), theme.mono_icon_colors(fg)))
        .tooltip_above(move |window, cx| build_tooltip(delete_arm_placeholder_text(), window, cx))
        .on_mouse_down(MouseButton::Left, listener)
}

pub fn fold_button(
    folded: bool,
    id: (&'static str, usize),
    zoom: f32,
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
        .size(px(24. * zoom))
        .flex_none()
        .rounded(px(6. * zoom))
        .flex()
        .items_center()
        .justify_center()
        .hover(move |s| s.bg(bg_hover))
        .cursor(CursorStyle::PointingHand)
        .tooltip_above(move |window, cx| build_tooltip(tip, window, cx))
        .child(hover_icon(glyph, 14. * zoom, base_fg, hover_fg))
        .on_mouse_down(MouseButton::Left, listener)
}

/// Overlay-only drop feedback: keeps row geometry stable throughout the drag.
pub fn block_drop_marker(left: Pixels, right: Pixels, after: bool, nested: bool, zoom: f32) -> Div {
    let theme = Theme::ocean();
    div().absolute().left(left).right(right).h(px(3. * zoom)).bg(theme.accent)
        .when(after, |line| line.bottom_0()).when(!after, |line| line.top_0())
        .child(div().absolute().left(px(-3. * zoom)).top(px(-2. * zoom))
            .size(px(7. * zoom)).rounded_full().bg(theme.accent))
        .when(nested, |line| line.child(div().absolute().right_0().bottom(px(4. * zoom))
            .px(px(8. * zoom)).py(px(2. * zoom)).rounded(px(4. * zoom))
            .bg(theme.paper).text_color(theme.accent).text_size(px(14. * zoom))
            .child(tr("放入子节点", "Drop as child"))))
}
