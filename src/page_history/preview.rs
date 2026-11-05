//! Center reading surface for Page History. Diff blocks are supplied by the caller.
use std::{path::Path, rc::Rc};
use gpui::{
    App, FontWeight, IntoElement, MouseDownEvent, ParentElement, Styled,
    Window, div, prelude::*, px,
};
use crate::{
    theme::{tr, Theme},
    ui::{radius, segmented_control::{SegmentSpec, segmented_control}, text_action::{accent_action, plain_action}},
};
use super::types::{DiffBlock, HistoryError, HistoryVersion, PreviewMode};

#[path = "preview_body.rs"]
mod body;
#[path = "preview_image.rs"]
mod preview_image;

pub fn render_preview_surface(
    note_path: Option<&Path>,
    selected_version: Option<&HistoryVersion>,
    selected_content: Option<&str>,
    current_content: &str,
    mode: PreviewMode,
    error: Option<&HistoryError>,
    diff_blocks: Option<&[DiffBlock]>,
    preview_key: &str,
    on_mode_change: impl Fn(PreviewMode, &mut Window, &mut App) + 'static,
    on_re_diff: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_retry_restore: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let _ = current_content;
    let theme = Theme::ocean();
    let title = selected_version.map(|version| version.display_time.as_str()).unwrap_or("");
    let same_as_current = selected_version.map(|version| version.same_as_current).unwrap_or(false);

    div()
        .flex_1()
        .min_w_0()
        .h_full()
        .bg(theme.paper)
        .flex()
        .flex_col()
        .child(render_preview_header(title, mode, on_mode_change))
        .children(render_error_banner(error, on_re_diff, on_retry_restore))
        .child(body::render_preview_body(
            note_path, selected_content, mode, same_as_current, diff_blocks, preview_key,
        ))
}

pub fn restore_availability(
    selected_version: Option<&HistoryVersion>,
    selected_content: Option<&str>,
    is_readonly: bool,
    restoring: bool,
    error: Option<&HistoryError>,
) -> (bool, String) {
    if restoring {
        return (true, tr("正在恢复…", "Restoring…").to_string());
    }
    let restore = tr("恢复此版本", "Restore this version").to_string();
    if is_readonly || matches!(error, Some(HistoryError::ReadOnly)) {
        return (true, tr("此页面只读，无法恢复。", "This page is read-only; cannot restore.").to_string());
    }
    if let Some(err) = error {
        return (true, err.message());
    }
    let Some(version) = selected_version else {
        return (true, restore);
    };
    if selected_content.is_none() {
        return (true, tr("正在加载历史…", "Loading history…").to_string());
    }
    if version.is_current {
        return (true, tr("当前版本无需恢复", "Current version; no need to restore").to_string());
    }
    if version.same_as_current {
        return (true, tr("与当前版本内容相同", "Identical to current version").to_string());
    }
    (false, restore)
}

fn render_preview_header(
    title: &str,
    mode: PreviewMode,
    on_mode: impl Fn(PreviewMode, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let on_mode = Rc::new(on_mode);
    let on_clean = on_mode.clone();
    div()
        .h(px(44.))
        .px(px(28.))
        .border_b_1()
        .border_color(theme.hairline)
        .bg(theme.sidebar)
        .flex()
        .items_center()
        .justify_between()
        .gap(px(12.))
        .child(
            div()
                .min_w_0()
                .text_size(px(14.))
                .line_height(px(20.))
                .font_weight(FontWeight(560.))
                .text_color(theme.text)
                .child(title.to_string()),
        )
        .child(segmented_control("history-preview-mode", vec![
            SegmentSpec {
                label: tr("原文", "Original").into(),
                selected: mode == PreviewMode::Clean,
                on_select: Box::new(move |_, window, cx| on_clean(PreviewMode::Clean, window, cx)),
            },
            SegmentSpec {
                label: tr("对比", "Diff").into(),
                selected: mode == PreviewMode::Diff,
                on_select: Box::new(move |_, window, cx| on_mode(PreviewMode::Diff, window, cx)),
            },
        ]))
}

pub fn render_restore_button(
    disabled: bool,
    hint: impl Into<gpui::SharedString>,
    on_restore: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    accent_action("history-restore-action", tr("恢复此版本", "Restore this version"), !disabled, hint, on_restore)
}

fn render_error_banner(
    error: Option<&HistoryError>,
    on_re_diff: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_retry_restore: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Option<impl IntoElement> {
    let theme = Theme::ocean();
    let err = error?;
    let (msg, action_label, is_conflict) = match err {
        HistoryError::ExternalConflict => (
            tr("页面已更新，请重新对比。", "Page has been updated externally; please re-diff."),
            tr("重新对比", "Re-diff"),
            true,
        ),
        HistoryError::RestoreFailed(_) => (
            tr("恢复失败，当前内容未更改。", "Restore failed; current content untouched."),
            tr("重试", "Retry"),
            false,
        ),
        HistoryError::ReadOnly => (
            tr("此页面只读，无法恢复。", "This page is read-only; cannot restore."),
            "",
            false,
        ),
        HistoryError::LoadFailed(_) => return None,
    };

    Some(
        div()
            .mx(px(28.))
            .mt(px(12.))
            .p(px(10.))
            .rounded(radius::CONTROL)
            .bg(theme.shell)
            .border_1()
            .border_color(theme.hairline)
            .flex()
            .items_center()
            .justify_between()
            .gap(px(12.))
            .child(
                div()
                    .text_size(px(14.))
                    .line_height(px(20.))
                    .text_color(theme.text_secondary)
                    .child(msg),
            )
            .when(!action_label.is_empty(), |banner| {
                let label = action_label.to_string();
                banner.child(if is_conflict {
                    plain_action("history-error-action", label, 28., 10., 14., on_re_diff).into_any_element()
                } else {
                    plain_action("history-error-action", label, 28., 10., 14., on_retry_restore).into_any_element()
                })
            }),
    )
}
