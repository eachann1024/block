//! Paper-integrated math formula layout (Formula B).
//! Resting formula has no background/frame; thin neutral outline when open or on error.
//! Bottom contextual edit/copy action row shown on hover/active with constant reserved height.

use std::{cell::RefCell, collections::HashSet, time::Duration};
use gpui::{
    AnyElement, Context, Entity, Hsla, IntoElement,
    div, prelude::*, px, rgb, rgba,
};
use crate::{
    editor::view::Editor,
    icons::Icon,
    theme::{Theme, is_dark, tr},
    ui::icon_button::{copy_button, icon_button_zoomed},
};

thread_local! {
    static COPIED_MATH: RefCell<HashSet<u64>> = Default::default();
}

pub(super) fn preview_math_block(
    editor: Entity<Editor>,
    block_id: u64,
    source: &str,
    active: bool,
    hovered: bool,
    source_el: AnyElement,
    minimal: bool,
    cx: &mut Context<Editor>,
) -> AnyElement {
    let dark = is_dark();
    let theme = Theme::ocean();
    let open = super::is_source_open(editor.entity_id(), block_id, source);
    let zoom = crate::editor::zoom::zoom_factor(minimal);
    let (rendered_el, err_msg) = crate::editor::math_preview::render_state(editor.clone(), block_id, source, zoom, cx);
    let has_err = err_msg.is_some();

    // Single 1px outline always occupies the same space; colored when expanded or on error
    let outline_col: Hsla = if has_err {
        rgb(0xc65949).into()
    } else if open {
        theme.btn_border
    } else {
        gpui::transparent_black()
    };

    let copied = COPIED_MATH.with(|set| set.borrow().contains(&block_id));
    let show_actions = hovered || active || open;

    // Source drawer styling
    let source_divider = theme.btn_border;
    let source_bg = if dark { rgba(0xffffff05) } else { rgba(0x00000004) };

    // Source / Edit button using crate::ui::icon_button::icon_button
    let edit_label = if open { tr("收起源码", "Hide Source") } else { tr("编辑源码", "Source") };
    let edit_icon = if open { Icon::TbChevron } else { Icon::Pencil };
    let edit_editor = editor.clone();

    let edit_btn = icon_button_zoomed(
        ("math-edit-btn", block_id as usize),
        edit_icon,
        true,
        zoom,
        move |_, window, cx| {
            cx.stop_propagation();
            let _ = edit_editor.update(cx, |this, cx| {
                let next = !open;
                super::set_source_open(cx.entity_id(), block_id, next);
                if next {
                    this.block_sel = None;
                    this.focus_block(block_id, window, cx);
                }
                cx.notify();
            });
        },
    )
    .w_auto()
    .h(px(28. * zoom))
    .px(px(6. * zoom))
    .gap(px(4. * zoom))
    .child(
        div()
            .text_size(px(14. * zoom))
            .line_height(px(18. * zoom))
            .text_color(theme.text_secondary)
            .child(edit_label),
    );

    // Copy button using crate::ui::icon_button::copy_button
    let copy_label = if copied { tr("已复制", "Copied") } else { tr("复制", "Copy") };
    let copy_editor = editor.clone();

    let copy_btn = copy_button(
        ("math-copy-btn", block_id as usize),
        copied,
        zoom,
        move |_, _, cx| {
            cx.stop_propagation();
            let _ = copy_editor.update(cx, |this, cx| {
                this.copy_block_text(block_id, cx);
                COPIED_MATH.with(|set| { set.borrow_mut().insert(block_id); });
                cx.notify();
                let entity = cx.entity().downgrade();
                cx.spawn(async move |_, cx| {
                    cx.background_executor().timer(Duration::from_millis(1500)).await;
                    let _ = entity.update(cx, |_, cx| {
                        COPIED_MATH.with(|set| { set.borrow_mut().remove(&block_id); });
                        cx.notify();
                    });
                }).detach();
            });
        },
    )
    .w_auto()
    .h(px(28. * zoom))
    .px(px(6. * zoom))
    .gap(px(4. * zoom))
    .child(
        div()
            .text_size(px(14. * zoom))
            .line_height(px(18. * zoom))
            .text_color(theme.text_secondary)
            .child(copy_label),
    );

    // Contextual action row: constant reserved height (30px), invisible when idle to prevent reflow
    let action_row = div()
        .h(px(30. * zoom))
        .w_full()
        .flex()
        .items_center()
        .justify_between()
        .px(px(8. * zoom))
        .when(!show_actions, |d| d.invisible())
        .child(
            div()
                .text_size(px(14. * zoom))
                .line_height(px(18. * zoom))
                .text_color(theme.text_secondary)
                .child("LaTeX"),
        )
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(4. * zoom))
                .child(edit_btn)
                .child(copy_btn),
        );

    // Build formula container: 1px outline always present (transparent at rest)
    let mut container = div()
        .id(("cb-render-wrap", block_id as usize))
        .relative()
        .w_full()
        .min_w_0()
        .border_1()
        .border_color(outline_col)
        .rounded(px(8. * zoom));

    // Rendered formula preview body
    if let Some(body) = rendered_el {
        container = container.child(
            div()
                .w_full()
                .min_w_0()
                .py(px(12. * zoom))
                .px(px(12. * zoom))
                .flex()
                .justify_center()
                .child(body),
        );
    } else {
        container = container.child(div().w_full().h(px(28. * zoom)));
    }

    // Design B: action row BETWEEN preview and source drawer
    container = container.child(action_row);

    // Inline source drawer below action row
    if open {
        container = container.child(
            div()
                .w_full()
                .min_w_0()
                .border_t_1()
                .border_color(source_divider)
                .bg(source_bg)
                .child(source_el),
        );
    }

    // Error message drawer if any
    if let Some(msg) = err_msg {
        container = container.child(
            div()
                .w_full()
                .min_w_0()
                .border_t_1()
                .border_color(source_divider)
                .px(px(8. * zoom))
                .py(px(4. * zoom))
                .text_size(px(14. * zoom))
                .line_height(px(20. * zoom))
                .text_color(rgb(0xc65949))
                .child(msg),
        );
    }

    container.into_any_element()
}
