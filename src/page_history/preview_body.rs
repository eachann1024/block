//! Read-only Markdown body for a history version or a precomputed block diff.
use std::path::{Path, PathBuf};
use gpui::{AnyElement, IntoElement, ParentElement, SharedString, Styled, div, prelude::*, px};
use gpui_component::text::TextView;
use crate::{theme::{tr, Theme}, ui::radius};
use super::preview_image;
use super::super::types::{DiffBlock, DiffStatus, PreviewMode};

pub(super) fn render_preview_body(
    note_path: Option<&Path>,
    selected_content: Option<&str>,
    mode: PreviewMode,
    same_as_current: bool,
    diff_blocks: Option<&[DiffBlock]>,
    preview_key: &str,
) -> impl IntoElement {
    let note_dir = note_path.and_then(|path| path.parent().map(Path::to_path_buf));
    let show_diff = mode == PreviewMode::Diff && !same_as_current;
    let inner = if selected_content.is_none() {
        status_line(tr("正在加载历史…", "Loading history…"))
    } else if show_diff {
        match diff_blocks {
            Some(blocks) => render_diff_view(blocks, note_dir, preview_key).into_any_element(),
            None => status_line(tr("正在加载历史…", "Loading history…")),
        }
    } else {
        reading_view(
            SharedString::from(format!("history-clean-{preview_key}")),
            selected_content.unwrap_or(""),
            note_dir,
            false,
        )
        .into_any_element()
    };

    div()
        .id("history-preview-scroll")
        .flex_1()
        .min_h_0()
        .overflow_y_scroll()
        .child(
            div()
                .w_full()
                .max_w(px(720.))
                .mx_auto()
                .px(px(28.))
                .pt(px(36.))
                .pb(px(64.))
                .child(inner),
        )
}

fn status_line(label: &'static str) -> AnyElement {
    let theme = Theme::ocean();
    div()
        .text_size(px(14.))
        .line_height(px(22.))
        .text_color(theme.text_secondary)
        .child(label)
        .into_any_element()
}

fn reading_view(
    id: SharedString,
    markdown: &str,
    note_dir: Option<PathBuf>,
    deleted: bool,
) -> TextView {
    let theme = Theme::ocean();
    let color = if deleted { theme.text_secondary } else { theme.text };
    let view = TextView::markdown(id, markdown.to_string())
        .selectable(true)
        .scrollable(false)
        .text_size(px(16.))
        .line_height(px(26.))
        .text_color(color)
        .w_full()
        .image_source(preview_image::resolver(note_dir));
    if deleted { view.line_through() } else { view }
}

fn render_diff_view(blocks: &[DiffBlock], note_dir: Option<PathBuf>, preview_key: &str) -> impl IntoElement {
    let mut root = div().flex().flex_col().gap(px(14.));
    for (index, block) in blocks.iter().enumerate() {
        let id = SharedString::from(format!("history-diff-{preview_key}-{index}"));
        let markdown = if block.markdown.is_empty() { block.text.as_str() } else { block.markdown.as_str() };
        root = root.child(diff_card(id, block.status, markdown, note_dir.clone()));
    }
    root
}

fn diff_card(
    id: SharedString,
    status: DiffStatus,
    markdown: &str,
    note_dir: Option<PathBuf>,
) -> AnyElement {
    let theme = Theme::ocean();
    match status {
        DiffStatus::Unchanged => reading_view(id, markdown, note_dir, false).into_any_element(),
        DiffStatus::Deleted => card(
            theme.shell,
            theme.hairline,
            theme.hairline,
            theme.text_secondary,
            tr("已删除", "Deleted"),
            reading_view(id, markdown, note_dir, true),
        ),
        DiffStatus::Added => card(
            theme.selected,
            theme.accent,
            theme.paper,
            theme.accent,
            tr("+ 新增", "+ Added"),
            reading_view(id, markdown, note_dir, false),
        ),
    }
}

fn card(
    bg: impl Into<gpui::Hsla>,
    border: impl Into<gpui::Hsla>,
    chip_bg: impl Into<gpui::Hsla>,
    chip_color: impl Into<gpui::Hsla>,
    chip: &'static str,
    body: TextView,
) -> AnyElement {
    div()
        .w_full()
        .min_w_0()
        .p(px(12.))
        .rounded(radius::CONTROL)
        .bg(bg.into())
        .border_1()
        .border_color(border.into())
        .flex()
        .flex_col()
        .gap(px(6.))
        .child(
            div().flex().child(
                div()
                    .px(px(6.))
                    .py(px(1.))
                    .rounded(radius::MICRO_MD)
                    .bg(chip_bg.into())
                    .text_color(chip_color.into())
                    .text_size(px(14.))
                    .line_height(px(20.))
                    .child(chip),
            ),
        )
        .child(body)
        .into_any_element()
}
