use std::time::Duration;
use gpui::{
    Animation, AnimationExt, App, CursorStyle, Div, Hsla, IntoElement, MouseButton,
    MouseDownEvent, Pixels, Stateful, Window, div, prelude::*, px, rgb, rgba, transparent_black,
};
use crate::{
    icons::{Icon, IconColors, icon},
    theme::{Theme, build_tooltip, is_dark, tr},
    ui::motion::ease_out_expo,
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

pub fn table_capsule(
    id: (&'static str, usize),
    vertical: bool,
    expanded: bool,
    dragging: bool,
    tip: &'static str,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let (w, h) = match (vertical, expanded) {
        (false, false) => (px(24.), px(8.)),
        (false, true) => (px(24.), px(14.)),
        (true, false) => (px(8.), px(24.)),
        (true, true) => (px(14.), px(24.)),
    };
    let bg: Hsla = if dark {
        if expanded { rgb(0x262626).into() } else { rgb(0x1e1e1e).into() }
    } else {
        rgb(0xffffff).into()
    };
    let border: Hsla = if dark {
        if expanded { rgb(0x6b6b6b).into() } else { rgb(0x555555).into() }
    } else {
        rgba(0x1f1e1d52).into()
    };
    let dot_color: Hsla = if dark { theme.text } else { rgba(0x1f1e1d8c).into() };
    let radius = if expanded { px(7.) } else { px(999.) };
    div()
        .id(id)
        .w(w)
        .h(h)
        .flex_none()
        .rounded(radius)
        .flex()
        .items_center()
        .justify_center()
        .bg(bg)
        .border_1()
        .border_color(border)
        .cursor(CursorStyle::OpenHand)
        .when(expanded, |el| {
            let grip = div().flex().items_center().justify_center().child(icon(
                if vertical { Icon::Drag } else { Icon::GripHorizontal },
                px(12.),
                theme.icon_colors(dot_color),
            ));
            el.child(grip.with_animation(
                ("cap-grip-anim", id.1 * 2 + usize::from(vertical)),
                Animation::new(Duration::from_millis(150)).with_easing(ease_out_expo()),
                |el, delta| el.opacity(delta),
            ))
        })
        .when(!dragging, |el| el.tooltip(move |window, cx| build_tooltip(tip, window, cx)))
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

pub fn table_more_button(open: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let (bg, hover_bg, border, fg, hover_fg): (Hsla, Hsla, Hsla, Hsla, Hsla) = if dark {
        let b = if open { theme.selected } else { theme.paper };
        let br = if open { theme.accent } else { theme.btn_border };
        let f = if open { theme.text } else { theme.text_secondary };
        (b, theme.hover, br, f, theme.text)
    } else {
        let b = if open { rgb(0xf7f7f7).into() } else { rgb(0xffffff).into() };
        let f = if open { rgb(0x0b0b0b).into() } else { rgb(0x52514e).into() };
        (b, rgb(0xf7f7f7).into(), rgba(0x0b0b0b1a).into(), f, rgb(0x0b0b0b).into())
    };
    div()
        .id("table-more")
        .group("hover-icon")
        .w(px(28.))
        .h(px(28.))
        .flex_none()
        .rounded(px(8.))
        .flex()
        .items_center()
        .justify_center()
        .bg(bg)
        .border_1()
        .border_color(border)
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .child(hover_icon(Icon::More, 16., fg, hover_fg))
        .tooltip(|window, cx| build_tooltip(tr("表格选项", "Table options"), window, cx))
}

pub fn grip_button(
    id: (&'static str, usize),
    active: bool,
    listener: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let bg_hover: Hsla = if dark { rgb(0x2c2c2a).into() } else { rgb(0xf5f4ed).into() };
    let base_fg: Hsla = if dark { theme.text_secondary } else { rgba(0x1f1e1d8c).into() };
    let hover_fg: Hsla = if dark { theme.text } else { rgb(0x1f1e1d).into() };
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
        .child(hover_icon(Icon::Drag, 14., if active { hover_fg } else { base_fg }, hover_fg))
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
