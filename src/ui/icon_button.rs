use gpui::{
    App, CursorStyle, Div, ElementId, Hsla, MouseButton, MouseDownEvent, SharedString, Stateful,
    Window, div, prelude::*, px, rgb,
};
use crate::{
    icons::{Icon, icon, stateful_icon},
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
    let group = SharedString::from(format!("ib-{id:?}"));
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    let fg = theme.text_secondary;
    let lead = if enabled {
        stateful_icon(group.clone(), glyph, px(14.), false, theme.mono_icon_colors(fg), theme.icon_colors(theme.text))
    } else {
        icon(glyph, px(14.), theme.mono_icon_colors(fg))
    };
    div()
        .id(id)
        .group(group)
        .relative()
        .children(probe)
        .size(px(24.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(crate::ui::radius::CONTROL)
        .when(enabled, |b| {
            b.cursor(CursorStyle::PointingHand)
                .hover(|style| style.bg(theme.hover))
                .on_mouse_down(MouseButton::Left, on_down)
        })
        .when(!enabled, |b| b.opacity(0.5))
        .child(lead)
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
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let group = SharedString::from(format!("ib-{id:?}"));
    let icon_fg = if copied { hover_fg } else { base_fg };
    div()
        .id(id)
        .group(group.clone())
        .size(px(24.))
        .rounded(crate::ui::radius::CONTROL)
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .child(stateful_icon(
            group,
            glyph,
            px(13.),
            copied,
            theme.mono_icon_colors(icon_fg),
            theme.icon_colors(hover_fg),
        ))
        .on_mouse_down(MouseButton::Left, on_down)
}

pub fn back_button(
    id: impl Into<ElementId>,
    label: &'static str,
    on_click: impl Fn(&gpui::ClickEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let group = SharedString::from(format!("ib-{id:?}"));
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    div()
        .id(id)
        .group(group.clone())
        .relative()
        .children(probe)
        .h(px(28.))
        .px(px(10.))
        .flex_none()
        .flex()
        .items_center()
        .gap(px(6.))
        .rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::PointingHand)
        .text_size(px(14.))
        .font_weight(gpui::FontWeight(500.))
        .text_color(theme.text)
        .hover(move |s| s.bg(theme.hover))
        .on_click(on_click)
        .child(stateful_icon(
            group,
            Icon::ArrowLeft,
            px(15.),
            false,
            theme.mono_icon_colors(theme.text),
            theme.icon_colors(theme.text),
        ))
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
    let group = SharedString::from(format!("ib-{id:?}"));
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    div()
        .id(id)
        .group(group.clone())
        .relative()
        .children(probe)
        .size(px(30.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::PointingHand)
        .when(active, |b| b.bg(theme.hover))
        .hover(move |s| s.bg(theme.hover))
        .when(!active, |b| b.tooltip(crate::ui::tooltip::tooltip(tip)))
        .on_mouse_down(MouseButton::Left, on_down)
        .child(stateful_icon(
            group,
            glyph,
            px(16.),
            active,
            theme.mono_icon_colors(fg),
            theme.icon_colors(if active { fg } else { theme.text }),
        ))
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
    let group = SharedString::from(format!("ib-{id:?}"));
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    let fg = theme.text;
    let lead = if enabled {
        stateful_icon(group.clone(), glyph, px(15.), false, theme.mono_icon_colors(fg), theme.icon_colors(theme.text))
    } else {
        icon(glyph, px(15.), theme.mono_icon_colors(fg))
    };
    div()
        .id(id)
        .group(group)
        .relative()
        .children(probe)
        .size(px(32.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(crate::ui::radius::CONTROL)
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
        .child(lead)
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
    let group = SharedString::from(format!("ib-{id:?}"));
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    div()
        .id(id)
        .group(group.clone())
        .relative()
        .children(probe)
        .size(px(30.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::PointingHand)
        .when(active, |b| b.bg(theme.selected).border_1().border_color(theme.accent))
        .when(!active, |b| b.hover(move |s| s.bg(theme.hover)).tooltip(crate::ui::tooltip::tooltip(tip)))
        .on_mouse_down(MouseButton::Left, on_down)
        .child(stateful_icon(
            group,
            glyph,
            px(16.),
            active,
            theme.mono_icon_colors(fg),
            theme.icon_colors(if active { theme.accent } else { theme.text }),
        ))
}


