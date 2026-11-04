use gpui::{
    App, CursorStyle, Div, ElementId, Hsla, MouseButton, MouseDownEvent, Stateful, Window, div,
    prelude::*, px, rgb, transparent_black,
};
use crate::{
    icons::{Icon, IconColors, icon},
    theme::{Theme, is_dark},
};

pub fn icon_button(
    id: impl Into<ElementId>,
    glyph: Icon,
    enabled: bool,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .size(px(24.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(6.))
        .when(enabled, |b| {
            b.cursor(CursorStyle::PointingHand)
                .hover(|style| style.bg(theme.hover))
                .on_mouse_down(MouseButton::Left, on_down)
        })
        .when(!enabled, |b| b.opacity(0.5))
        .child(icon(glyph, px(14.), theme.icon_colors(theme.text_secondary)))
}

pub fn copy_button(
    id: impl Into<ElementId>,
    copied: bool,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let dark = is_dark();
    let base_fg: Hsla = if dark { rgb(0x9aa3b2).into() } else { rgb(0x526071).into() };
    let hover_fg: Hsla = if dark { rgb(0xe6e9ef).into() } else { rgb(0x16181c).into() };
    let hover_bg = if dark { rgb(0x252a34) } else { rgb(0xe8eaee) };
    let glyph = if copied { Icon::Check } else { Icon::Copy };
    let tint = |line| IconColors { line, fill: transparent_black(), accent: transparent_black() };
    div()
        .id(id)
        .group("ui-copy-btn")
        .size(px(24.))
        .rounded(px(6.))
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .child(
            div()
                .relative()
                .flex_none()
                .size(px(13.))
                .child(div().group_hover("ui-copy-btn", |s| s.invisible()).child(icon(glyph, px(13.), tint(base_fg))))
                .child(
                    div().absolute().top_0().left_0().invisible()
                        .group_hover("ui-copy-btn", |s| s.visible())
                        .child(icon(glyph, px(13.), tint(hover_fg))),
                ),
        )
        .on_mouse_down(MouseButton::Left, on_down)
}
