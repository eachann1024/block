use crate::ui::tooltip::TooltipExt;
use gpui::{
    App, CursorStyle, ElementId, Hsla, MouseButton, MouseDownEvent, SharedString, Window,
    prelude::*, px, rgb,
};
use gpui_base::Button;
use crate::{
    icons::{Icon, icon, stateful_icon},
    theme::{Theme, is_dark, tr},
    ui::icon_size,
};

/// Sidebar footer bar height; footer buttons fill it so hover matches the notebook switcher.
const FOOTER_BOX: f32 = 30.;

/// Pointer hit target that does not take keyboard focus, so an editor caret keeps its input method.
fn quiet(id: impl Into<ElementId>) -> Button {
    Button::new(id).focusable(false)
}

pub fn icon_button(
    id: impl Into<ElementId>,
    glyph: Icon,
    enabled: bool,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Button {
    icon_button_zoomed(id, glyph, enabled, 1., on_down)
}

pub fn icon_button_zoomed(
    id: impl Into<ElementId>,
    glyph: Icon,
    enabled: bool,
    zoom: f32,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Button {
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let group = SharedString::from(format!("ib-{id:?}"));
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    let fg = theme.text_secondary;
    let lead = if enabled {
        stateful_icon(group.clone(), glyph, px(icon_size::INLINE * zoom), false, theme.mono_icon_colors(fg), theme.icon_colors(theme.text))
    } else {
        icon(glyph, px(icon_size::INLINE * zoom), theme.mono_icon_colors(fg))
    };
    quiet(id)
        .disabled(!enabled)
        .group(group)
        .relative()
        .children(probe)
        .size(px(24. * zoom))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(6. * zoom))
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
    zoom: f32,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Button {
    let dark = is_dark();
    let base_fg: Hsla = if dark { rgb(0x9aa3b2).into() } else { rgb(0x526071).into() };
    let hover_fg: Hsla = if dark { rgb(0xe6e9ef).into() } else { rgb(0x16181c).into() };
    let hover_bg = if dark { rgb(0x252a34) } else { rgb(0xe8eaee) };
    let glyph = if copied { Icon::Check } else { Icon::Copy };
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let group = SharedString::from(format!("ib-{id:?}"));
    let icon_fg = if copied { hover_fg } else { base_fg };
    quiet(id)
        .group(group.clone())
        .accessibility_label(if copied { tr("已复制", "Copied") } else { tr("复制", "Copy") })
        .size(px(24. * zoom))
        .rounded(px(6. * zoom))
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .child(stateful_icon(
            group,
            glyph,
            px(icon_size::INLINE * zoom),
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
) -> Button {
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let group = SharedString::from(format!("ib-{id:?}"));
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    quiet(id)
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
        .line_height(px(20.))
        .font_weight(crate::ui::type_scale::CONTROL)
        .text_color(theme.text)
        .hover(move |s| s.bg(theme.hover))
        .on_click(on_click)
        .child(stateful_icon(
            group,
            Icon::ArrowLeft,
            px(icon_size::ROW),
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
    tip: Option<&'static str>,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Button {
    let theme = Theme::ocean();
    let fg = if active { theme.accent } else { theme.text_secondary };
    let id: ElementId = id.into();
    let group = SharedString::from(format!("ib-{id:?}"));
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    quiet(id)
        .selected(active)
        .group(group.clone())
        .relative()
        .children(probe)
        .size(px(icon_size::CHROME_BOX))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::PointingHand)
        .when(active, |b| b.bg(theme.selected).border_1().border_color(theme.accent))
        .when(!active, |b| b.hover(move |s| s.bg(theme.hover)))
        .when_some(tip.filter(|_| !active), |b, tip| {
            b.accessibility_label(tip).tooltip_above(crate::ui::tooltip::tooltip(tip))
        })
        .on_mouse_down(MouseButton::Left, on_down)
        .child(stateful_icon(
            group,
            glyph,
            px(icon_size::CHROME),
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
) -> Button {
    let theme = Theme::ocean();
    let id: ElementId = id.into();
    let group = SharedString::from(format!("ib-{id:?}"));
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    let fg = theme.text;
    let tip = tip.into();
    let lead = if enabled {
        stateful_icon(group.clone(), glyph, px(icon_size::ROW), false, theme.mono_icon_colors(fg), theme.icon_colors(theme.text))
    } else {
        icon(glyph, px(icon_size::ROW), theme.mono_icon_colors(fg))
    };
    quiet(id)
        .disabled(!enabled)
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
                .hover(move |s| s.bg(theme.hover))
                .accessibility_label(tip.clone())
                .tooltip_above(crate::ui::tooltip::tooltip(tip))
                .on_mouse_down(MouseButton::Left, on_down)
        })
        .when(!enabled, |b| b.opacity(0.5))
        .child(lead)
}

/// Sidebar footer button; its box fills the footer bar like the notebook switcher.
pub fn dock_icon_button(
    id: impl Into<ElementId>,
    glyph: Icon,
    active: bool,
    tip: impl Into<gpui::SharedString>,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Button {
    chrome_icon_button(id, glyph, active, true, tip, FOOTER_BOX, on_down)
}

/// Title-bar button at the chrome tier (18px glyph, 26px box).
pub fn titlebar_icon_button(
    id: impl Into<ElementId>,
    glyph: Icon,
    active: bool,
    enabled: bool,
    tip: impl Into<gpui::SharedString>,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Button {
    chrome_icon_button(id, glyph, active, enabled, tip, icon_size::CHROME_BOX, on_down)
}

fn chrome_icon_button(
    id: impl Into<ElementId>,
    glyph: Icon,
    active: bool,
    enabled: bool,
    tip: impl Into<gpui::SharedString>,
    box_px: f32,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Button {
    let theme = Theme::ocean();
    let fg = if active { theme.accent } else { theme.text_secondary };
    let id: ElementId = id.into();
    let group = SharedString::from(format!("ib-{id:?}"));
    let probe = crate::ui::smoke_overlay::probe_for(&id);
    let tip = tip.into();
    let lead = if enabled {
        stateful_icon(
            group.clone(),
            glyph,
            px(icon_size::CHROME),
            active,
            theme.mono_icon_colors(fg),
            theme.icon_colors(if active { theme.accent } else { theme.text }),
        )
    } else {
        icon(glyph, px(icon_size::CHROME), theme.mono_icon_colors(fg))
    };
    quiet(id)
        .disabled(!enabled)
        .selected(active)
        .group(group)
        .relative()
        .children(probe)
        .size(px(box_px))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(crate::ui::radius::CONTROL)
        .when(active, |b| b.bg(theme.selected).border_1().border_color(theme.accent))
        .when(enabled && !active, |b| {
            b.hover(move |s| s.bg(theme.hover))
                .accessibility_label(tip.clone())
                .tooltip_above(crate::ui::tooltip::tooltip(tip))
        })
        .when(enabled, |b| b.cursor(CursorStyle::PointingHand).on_mouse_down(MouseButton::Left, on_down))
        .when(!enabled, |b| b.opacity(0.5))
        .child(lead)
}
