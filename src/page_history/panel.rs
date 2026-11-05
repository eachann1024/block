//! 320px right sidebar panel displaying the reverse chronological version history list.
use std::rc::Rc;
use gpui::{
    App, CursorStyle, FontWeight, IntoElement, MouseButton, MouseDownEvent,
    ParentElement, Stateful, Styled, Window, div, prelude::*, px,
};
use crate::{
    icons::{Icon, stateful_icon},
    theme::{tr, Theme},
    ui::radius,
};
use super::types::HistoryVersion;

pub fn render_history_panel(
    versions: &[HistoryVersion],
    selected_id: Option<&str>,
    loading: bool,
    error: Option<&str>,
    on_select: impl Fn(&str, &mut Window, &mut App) + 'static,
    on_close: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_retry: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    div()
        .id("history-sidebar")
        .w(px(320.))
        .flex_none()
        .h_full()
        .bg(theme.sidebar)
        .border_l_1()
        .border_color(theme.sidebar_border)
        .p(px(12.))
        .flex()
        .flex_col()
        .gap(px(10.))
        .child(render_panel_header(on_close))
        .child(render_panel_content(
            versions,
            selected_id,
            loading,
            error,
            Rc::new(on_select),
            on_retry,
        ))
}

fn render_panel_header(
    on_close: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    div()
        .flex_none()
        .h(px(28.))
        .px(px(4.))
        .flex()
        .items_center()
        .justify_between()
        .child(
            div()
                .text_size(px(14.))
                .font_weight(FontWeight(500.))
                .text_color(theme.text_secondary)
                .child(tr("版本记录", "Versions")),
        )
        .child(
            div()
                .id("history-panel-close")
                .group("history-panel-close")
                .size(px(28.))
                .rounded(radius::CONTROL)
                .flex()
                .items_center()
                .justify_center()
                .cursor(CursorStyle::PointingHand)
                .hover(|s| s.bg(theme.hover))
                .on_mouse_down(MouseButton::Left, on_close)
                .child(stateful_icon(
                    "history-panel-close",
                    Icon::Close,
                    px(14.),
                    false,
                    theme.mono_icon_colors(theme.text_secondary),
                    theme.icon_colors(theme.text),
                )),
        )
}

fn render_panel_content(
    versions: &[HistoryVersion],
    selected_id: Option<&str>,
    loading: bool,
    error: Option<&str>,
    on_select: Rc<dyn Fn(&str, &mut Window, &mut App)>,
    on_retry: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();

    if let Some(err) = error {
        return div()
            .flex_1()
            .flex()
            .flex_col()
            .items_center()
            .justify_center()
            .gap(px(12.))
            .p(px(16.))
            .child(
                div()
                    .text_size(px(14.))
                    .text_color(theme.text_secondary)
                    .child(err.to_string()),
            )
            .child(
                div()
                    .id("history-retry-btn")
                    .h(px(28.))
                    .px(px(12.))
                    .rounded(radius::CONTROL)
                    .bg(theme.paper)
                    .border_1()
                    .border_color(theme.btn_border)
                    .text_size(px(14.))
                    .text_color(theme.text)
                    .flex()
                    .items_center()
                    .justify_center()
                    .cursor(CursorStyle::PointingHand)
                    .hover(|s| s.bg(theme.hover))
                    .on_mouse_down(MouseButton::Left, on_retry)
                    .child(tr("重试", "Retry")),
            )
            .into_any_element();
    }

    if loading && versions.is_empty() {
        return div()
            .flex_1()
            .flex()
            .items_center()
            .justify_center()
            .text_size(px(14.))
            .text_color(theme.text_secondary)
            .child(tr("正在加载历史…", "Loading history…"))
            .into_any_element();
    }

    if versions.is_empty() {
        return div()
            .flex_1()
            .flex()
            .items_center()
            .justify_center()
            .text_size(px(14.))
            .text_color(theme.text_secondary)
            .child(tr("暂无历史版本", "No history versions yet"))
            .into_any_element();
    }

    let mut list = div()
        .id("history-version-list")
        .flex_1()
        .min_h_0()
        .overflow_y_scroll()
        .flex()
        .flex_col()
        .gap(px(4.));

    for v in versions {
        let is_selected = selected_id == Some(v.id.as_str());
        let cb = on_select.clone();
        list = list.child(render_version_row(v, is_selected, move |vid, window, cx| cb(vid, window, cx)));
    }

    list.into_any_element()
}

fn render_version_row(
    version: &HistoryVersion,
    is_selected: bool,
    on_select: impl Fn(&str, &mut Window, &mut App) + 'static,
) -> Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let vid = version.id.clone();
    let current_label = if version.is_current {
        tr("当前", "Current")
    } else {
        ""
    };

    let row_id = gpui::SharedString::from(format!("history-row-{vid}"));

    div()
        .id(row_id)
        .h(px(36.))
        .w_full()
        .px(px(12.))
        .rounded(radius::CONTROL)
        .flex()
        .items_center()
        .justify_between()
        .cursor(CursorStyle::PointingHand)
        .when(is_selected, |b| {
            b.bg(theme.selected)
                .border_1()
                .border_color(theme.accent)
                .text_color(theme.text)
        })
        .when(!is_selected, |b| {
            b.bg(gpui::transparent_black())
                .hover(|s| s.bg(theme.hover))
                .text_color(theme.text)
        })
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            on_select(&vid, window, cx);
        })
        .child(
            div()
                .text_size(px(14.))
                .font_weight(FontWeight(560.))
                .child(version.display_time.clone()),
        )
        .when(!current_label.is_empty(), |row| {
            row.child(
                div()
                    .text_size(px(14.))
                    .text_color(theme.text_secondary)
                    .child(current_label),
            )
        })
}
