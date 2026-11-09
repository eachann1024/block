//! Renderable code blocks (`mermaid`, `latex`/`math`) with `Source` toggle
//! and error bar matching `C-Code-Block.dc.html`.
use std::{cell::RefCell, collections::HashMap};
use gpui::{
    AnyElement, Context, CursorStyle, Entity, IntoElement, MouseButton, SharedString, div,
    prelude::*, px, rgb, rgba,
};
use crate::{
    icons::{Icon, stateful_icon},
    theme::{Theme, is_dark, tr},
};
use super::{code_block, view::Editor};

thread_local! {
    static SOURCE_OPEN: RefCell<HashMap<gpui::EntityId, HashMap<u64, bool>>> = Default::default();
}

#[path = "math_paper.rs"]
mod math_paper;

pub(super) fn is_source_open(editor: gpui::EntityId, block_id: u64, source: &str) -> bool {
    SOURCE_OPEN.with(|map| {
        map.borrow().get(&editor).and_then(|blocks| blocks.get(&block_id)).copied().unwrap_or_else(|| source.trim().is_empty())
    })
}

fn set_source_open(editor: gpui::EntityId, block_id: u64, open: bool) {
    SOURCE_OPEN.with(|map| {
        map.borrow_mut().entry(editor).or_default().insert(block_id, open);
    });
}

/// Render a previewable code block (`mermaid`, `latex`) with top-left `Source`
/// toggle, top-right language pill, rendered preview, collapsible source editor, and error bar.
pub(super) fn preview_block(
    editor: Entity<Editor>,
    block_id: u64,
    language: &str,
    source: &str,
    active: bool,
    hovered: bool,
    source_el: AnyElement,
    minimal: bool,
    cx: &mut Context<Editor>,
) -> AnyElement {
    if code_block::is_math(language) {
        return math_paper::preview_math_block(
            editor, block_id, source, active, hovered, source_el, minimal, cx,
        );
    }

    let dark = is_dark();
    let zoom = super::zoom::zoom_factor(minimal);
    let open = is_source_open(editor.entity_id(), block_id, source);
    let menu_open = code_block::is_menu_open_for(block_id);
    let show_controls = hovered || active || open || menu_open;

    let (rendered_el, err_msg) = render_preview_body(editor.clone(), block_id, language, source, open, minimal, cx);
    let has_err = err_msg.is_some();
    let show_frame = open || has_err;

    let border_col = if dark { rgb(0x4a4d55) } else { rgb(0xb8b6af) };
    let source_bg = if dark { rgba(0xffffff0a) } else { rgba(0x00000009) };
    let toggle_fg: gpui::Hsla = if dark { rgb(0x9aa3b2).into() } else { rgb(0x52514e).into() };
    let toggle_hover_bg = if dark { rgb(0x252a34) } else { rgb(0xf2f0ea) };
    let err_fg = rgb(0xc65949);

    let caret_icon = if open { Icon::TbChevron } else { Icon::ChevronRight };
    let theme = Theme::ocean();
    let src_group = SharedString::from(format!("cb-src-tgl-{block_id}"));

    let source_btn = div()
        .id(("cb-source-toggle", block_id as usize))
        .group(src_group.clone())
        .h(px(20. * zoom))
        .pl(px(2. * zoom))
        .pr(px(4. * zoom))
        .rounded(px(6. * zoom))
        .flex()
        .items_center()
        .gap(px(4. * zoom))
        .cursor(CursorStyle::PointingHand)
        .text_size(px(13. * zoom))
        .line_height(px(17. * zoom))
        .text_color(toggle_fg)
        .hover(move |s| s.bg(toggle_hover_bg))
        .child(stateful_icon(src_group, caret_icon, px(14. * zoom), open, theme.mono_icon_colors(toggle_fg), theme.icon_colors(theme.text)))
        .child(tr("源码", "Source"))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
            cx.stop_propagation();
            let next = !open;
            set_source_open(cx.entity_id(), block_id, next);
            if next {
                this.block_sel = None;
                this.focus_block(block_id, window, cx);
            }
            cx.notify();
        }));

    let top_left = div()
        .absolute()
        .top(px(4. * zoom))
        .left(px(4. * zoom))
        .h(px(22. * zoom))
        .flex()
        .items_center()
        .gap(px(8. * zoom))
        .when(!show_controls && !has_err, |d| d.invisible())
        .child(source_btn)
        .when(has_err, |d| {
            d.child(
                div()
                    .text_size(px(12. * zoom))
                    .line_height(px(16. * zoom))
                    .text_color(err_fg)
                    .child(tr("无法渲染", "Cannot render")),
            )
        });

    let top_right = code_block::top_controls(
        editor,
        block_id,
        language.to_owned(),
        show_controls,
        false,
        true,
        minimal,
        cx,
    );

    let mut wrapper = div()
        .id(("cb-render-wrap", block_id as usize))
        .relative()
        .w_full()
        .min_w_0()
        .when(show_frame, |d| d.border(px(1.5)).border_color(border_col).rounded(px(8. * zoom)));

    if let Some(body) = rendered_el {
        wrapper = wrapper.child(div().w_full().min_w_0().pt(px(24. * zoom)).child(body));
    } else {
        wrapper = wrapper.child(div().w_full().h(px(28. * zoom)));
    }

    if open {
        wrapper = wrapper.child(
            div()
                .w_full()
                .min_w_0()
                .border_t_1()
                .border_color(border_col)
                .bg(source_bg)
                .when(err_msg.is_none(), |d| d.rounded_b(px(6.5 * zoom)))
                .child(source_el),
        );
    }

    if let Some(msg) = err_msg {
        wrapper = wrapper.child(
            div()
                .w_full()
                .min_w_0()
                .border_t_1()
                .border_color(border_col)
                .px(px(8. * zoom))
                .py(px(4. * zoom))
                .text_size(px(13. * zoom))
                .line_height(px(19.5 * zoom))
                .text_color(err_fg)
                .child(msg),
        );
    }

    wrapper.child(top_left).child(top_right).into_any_element()
}

fn render_preview_body(
    editor: Entity<Editor>,
    block_id: u64,
    language: &str,
    source: &str,
    source_open: bool,
    minimal: bool,
    cx: &mut Context<Editor>,
) -> (Option<AnyElement>, Option<String>) {
    let dark = is_dark();
    let zoom = super::zoom::zoom_factor(minimal);
    if code_block::is_mermaid(language) {
        let (el, err) = super::diagram_preview::render_state(editor, block_id, source, zoom, cx);
        let body = el.map(|inner| {
            div().w_full().min_w_0().py(px(16. * zoom)).px(px(12. * zoom)).child(inner).into_any_element()
        });
        (body, err)
    } else {
        let (el, err) = super::math_preview::render_state(editor, block_id, source, zoom, cx);
        let bg = if dark { rgb(0x16191e) } else { rgb(0xf8f8f6) };
        let body = el.map(|inner| {
            div()
                .w_full()
                .min_w_0()
                .py(px(16. * zoom))
                .px(px(12. * zoom))
                .bg(bg)
                .when(!source_open, |d| d.rounded(px(6. * zoom)))
                .when(source_open, |d| d.rounded_t(px(6. * zoom)))
                .child(inner)
                .into_any_element()
        });
        (body, err)
    }
}

pub(super) fn take_sources(editor: gpui::EntityId) -> HashMap<u64, bool> {
    SOURCE_OPEN.with(|s| s.borrow_mut().remove(&editor).unwrap_or_default())
}
pub(super) fn restore_sources(editor: gpui::EntityId, sources: HashMap<u64, bool>) {
    SOURCE_OPEN.with(|s| { s.borrow_mut().insert(editor, sources); });
}
