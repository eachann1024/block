//! Scheme B (`Rename-V2-RowCapsule`): whole-row inline rename focus capsule for sidebar rows.
use std::path::Path;
use gpui::{
    AnyElement, App, CursorStyle, Div, Entity, Focusable, FontWeight, Hsla, MouseButton,
    Stateful, div, prelude::*, px, rgb,
};
use gpui_component::input::{Input, InputState};
use crate::icons::IconColors;
use crate::theme::{Theme, is_dark, tr};
use crate::ui::settings_controls::mono_font;

pub struct RenameVisual {
    pub border: Hsla,
    pub icon_colors: IconColors,
    pub conflict: Option<&'static str>,
}

pub fn inspect(path: &Path, dir: bool, input: &Entity<InputState>, cx: &App) -> RenameVisual {
    let val = input.read(cx).value();
    let trimmed = val.trim();
    if !trimmed.is_empty()
        && let Err(err) = crate::files::ops::rename_target(path, trimmed, dir)
    {
        let label = if err.kind() == std::io::ErrorKind::AlreadyExists {
            tr("已存在", "Exists")
        } else {
            tr("无效", "Invalid")
        };
        let danger: Hsla = rgb(if is_dark() { 0xe05247 } else { 0xc2362b }).into();
        let fill: Hsla = rgb(if is_dark() { 0x4c1d1d } else { 0xfde8e8 }).into();
        return RenameVisual {
            border: danger,
            icon_colors: IconColors { line: danger, fill, accent: danger },
            conflict: Some(label),
        };
    }
    let theme = Theme::ocean();
    RenameVisual {
        border: theme.accent,
        icon_colors: theme.icon_colors(theme.accent),
        conflict: None,
    }
}

pub fn style_capsule(row: Stateful<Div>, visual: &RenameVisual) -> Stateful<Div> {
    let theme = Theme::ocean();
    row.bg(theme.paper)
        .border_1()
        .border_color(visual.border)
        .cursor(CursorStyle::IBeam)
}

pub fn input_slot(input: &Entity<InputState>) -> Div {
    let theme = Theme::ocean();
    div()
        .flex_1()
        .min_w_0()
        .h(px(24.))
        .flex()
        .items_center()
        .child(
            Input::new(input)
                .appearance(false)
                .bordered(false)
                .focus_bordered(false)
                .h(px(24.))
                .px(px(0.))
                .py(px(0.))
                .text_size(theme.ui_font)
                .line_height(px(20.)),
        )
}

pub fn trailing_slot(input: &Entity<InputState>, visual: &RenameVisual) -> AnyElement {
    if let Some(label) = visual.conflict {
        let danger: Hsla = rgb(if is_dark() { 0xe05247 } else { 0xc2362b }).into();
        let fill: Hsla = rgb(if is_dark() { 0x4c1d1d } else { 0xfde8e8 }).into();
        return div()
            .h(px(18.))
            .px(px(5.))
            .flex_none()
            .flex()
            .items_center()
            .justify_center()
            .rounded(px(4.))
            .bg(fill)
            .text_size(px(11.))
            .line_height(px(12.))
            .font_weight(FontWeight(500.))
            .text_color(danger)
            .child(label)
            .into_any_element();
    }
    let theme = Theme::ocean();
    let input = input.clone();
    div()
        .id("rename-confirm")
        .w(px(22.))
        .h(px(20.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(4.))
        .bg(theme.shell)
        .border_1()
        .border_color(theme.btn_border)
        .font(mono_font())
        .text_size(px(12.))
        .line_height(px(12.))
        .text_color(theme.text_secondary)
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(theme.hover).text_color(theme.text))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            cx.stop_propagation();
            let fh = input.read(cx).focus_handle(cx);
            fh.dispatch_action(&gpui_component::input::Enter { secondary: false, shift: false }, window, cx);
        })
        .child("↵")
        .into_any_element()
}
