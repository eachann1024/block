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
    let id: ElementId = id.into();
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    div()
        .id(id)
        .relative()
        .children(probe)
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

pub fn back_button(
    id: impl Into<ElementId>,
    label: &'static str,
    on_click: impl Fn(&gpui::ClickEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    div()
        .id(id)
        .relative()
        .children(probe)
        .h(px(28.))
        .px(px(10.))
        .flex_none()
        .flex()
        .items_center()
        .gap(px(6.))
        .rounded(px(6.))
        .cursor(CursorStyle::PointingHand)
        .text_size(px(14.))
        .font_weight(gpui::FontWeight(500.))
        .text_color(theme.text)
        .hover(move |s| s.bg(theme.hover))
        .on_click(on_click)
        .child(icon(Icon::ArrowLeft, px(15.), theme.icon_colors(theme.text)))
        .child(label)
}

pub fn header_action_button(
    id: impl Into<ElementId>,
    glyph: Icon,
    active: bool,
    tip: impl Into<gpui::SharedString>,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let fg = if active { theme.accent } else { theme.text_secondary };
    let id: ElementId = id.into();
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    div()
        .id(id)
        .relative()
        .children(probe)
        .size(px(30.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(8.))
        .cursor(CursorStyle::PointingHand)
        .when(active, |b| b.bg(theme.hover))
        .hover(move |s| s.bg(theme.hover))
        .tooltip(crate::ui::tooltip::tooltip(tip))
        .on_mouse_down(MouseButton::Left, on_down)
        .child(icon(glyph, px(16.), theme.icon_colors(fg)))
}

pub fn toolbar_icon_button(
    id: impl Into<ElementId>,
    glyph: Icon,
    enabled: bool,
    tip: impl Into<gpui::SharedString>,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    div()
        .id(id)
        .relative()
        .children(probe)
        .size(px(32.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(8.))
        .border_1()
        .border_color(theme.btn_border)
        .bg(theme.paper)
        .when(enabled, |b| {
            b.cursor(CursorStyle::PointingHand)
                .hover(move |s| s.bg(theme.hover).border_color(theme.accent))
                .tooltip(crate::ui::tooltip::tooltip(tip))
                .on_mouse_down(MouseButton::Left, on_down)
        })
        .when(!enabled, |b| b.opacity(0.5))
        .child(icon(glyph, px(15.), theme.icon_colors(theme.text)))
}

pub fn dock_icon_button(
    id: impl Into<ElementId>,
    glyph: Icon,
    active: bool,
    tip: impl Into<gpui::SharedString>,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let fg = if active { theme.accent } else { theme.text_secondary };
    let id: ElementId = id.into();
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    div()
        .id(id)
        .relative()
        .children(probe)
        .size(px(30.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(6.))
        .cursor(CursorStyle::PointingHand)
        .when(active, |b| b.bg(theme.selected).border_1().border_color(theme.accent))
        .when(!active, |b| b.hover(move |s| s.bg(theme.hover)))
        .tooltip(crate::ui::tooltip::tooltip(tip))
        .on_mouse_down(MouseButton::Left, on_down)
        .child(icon(glyph, px(16.), theme.icon_colors(fg)))
}


