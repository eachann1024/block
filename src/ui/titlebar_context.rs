//! Dynamic tools for the right side of the window title bar.
use gpui::{
    App, ClickEvent, MouseButton, MouseDownEvent, SharedString, Stateful, Window, div, prelude::*, px,
};
use crate::{
    icons::Icon,
    theme::{Theme, tr},
    ui::{icon_button::dock_icon_button, radius},
};

/// Compact zoom group: out, percent reset, in, and fit.
pub fn zoom_cluster(
    id_prefix: impl Into<SharedString>,
    pct_label: impl Into<SharedString>,
    fit_active: bool,
    on_out: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_reset: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_in: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_fit: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Div {
    let theme = Theme::ocean();
    let prefix: SharedString = id_prefix.into();
    let pct_label: SharedString = pct_label.into();
    div()
        .flex_none()
        .flex()
        .items_center()
        .gap(px(2.))
        .child(dock_icon_button(
            SharedString::from(format!("{prefix}-out")),
            Icon::Minus,
            false,
            tr("缩小", "Zoom out"),
            on_out,
        ))
        .child(
            div()
                .id(SharedString::from(format!("{prefix}-pct")))
                .h(px(28.))
                .min_w(px(50.))
                .px(px(6.))
                .flex_none()
                .flex()
                .items_center()
                .justify_center()
                .rounded(radius::CONTROL)
                .cursor_pointer()
                .font(crate::ui::settings_controls::mono_font())
                .text_size(px(13.))
                .text_color(theme.text)
                .hover(|s| s.bg(theme.hover))
                .tooltip(crate::ui::tooltip::tooltip(tr("重置为 100%", "Reset to 100%")))
                .on_mouse_down(MouseButton::Left, on_reset)
                .child(pct_label),
        )
        .child(dock_icon_button(
            SharedString::from(format!("{prefix}-in")),
            Icon::Plus,
            false,
            tr("放大", "Zoom in"),
            on_in,
        ))
        .child(dock_icon_button(
            SharedString::from(format!("{prefix}-fit")),
            Icon::WindowMax,
            fit_active,
            tr("适应窗口", "Fit"),
            on_fit,
        ))
}

/// 28px capsule for the export preview title bar: dimensions, then zoom.
pub fn export_zoom_cluster(
    dim_label: impl Into<SharedString>,
    zoom_label: impl Into<SharedString>,
    on_out: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    on_reset: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    on_in: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> gpui::Div {
    let theme = Theme::ocean();
    let dim_label: SharedString = dim_label.into();
    let zoom_label: SharedString = zoom_label.into();
    div()
        .h(px(28.))
        .pl(px(10.))
        .pr(px(4.))
        .flex_none()
        .flex()
        .items_center()
        .gap(px(6.))
        .rounded(radius::CONTROL)
        .bg(theme.paper)
        .border_1()
        .border_color(theme.btn_border)
        .child(
            div()
                .flex_none()
                .whitespace_nowrap()
                .text_size(px(12.))
                .text_color(theme.text_secondary)
                .child(dim_label),
        )
        .child(div().w(px(1.)).h(px(14.)).flex_none().bg(theme.hairline))
        .child(
            div()
                .flex_none()
                .flex()
                .items_center()
                .gap(px(2.))
                .child(zoom_hit("export-zoom-out", "−", false, on_out))
                .child(zoom_hit("export-zoom-reset", zoom_label, true, on_reset))
                .child(zoom_hit("export-zoom-in", "+", false, on_in)),
        )
}

pub fn reveal_button(
    id: impl Into<SharedString>,
    on_click: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<gpui::Div> {
    dock_icon_button(id.into(), Icon::FolderReveal, false, crate::files::ops::reveal_label(), on_click)
}

fn zoom_hit(
    id: &'static str,
    label: impl Into<SharedString>,
    wide: bool,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .h(px(22.))
        .when(!wide, |hit| hit.w(px(22.)))
        .when(wide, |hit| hit.min_w(px(36.)).px(px(6.)))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(radius::CONTROL)
        .cursor_pointer()
        .text_size(px(if wide { 13. } else { 15. }))
        .text_color(if wide { theme.text } else { theme.text_secondary })
        .hover(|hit| hit.bg(theme.hover).text_color(theme.text))
        .on_click(on_click)
        .child(label.into())
}
