//! Narrow-window annotations: 24×24 numbered margin badges that open an in-place popover.
use gpui::{AnyElement, Context, Entity, IntoElement, MouseButton, ParentElement, Pixels, SharedString, Styled, Window, div, prelude::*, px, rgb};
use gpui_component::input::{Escape, Input, InputState};
use crate::{
    theme::{Theme, tr},
    ui::{
        annotation_actions::{BatchTone, compact_action_button},
        annotation_note,
        popover,
    },
};
use super::{annotate::{Annotation, ANNOTATION_GENERATED, ANNOTATION_PENDING}, view::Editor};

const POP_MAX_W: f32 = 360.;

fn close(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) {
    editor.annotate.active_annotation = None;
    editor.annotate.followup = None;
    window.focus(&editor.focus, cx);
    cx.notify();
}

pub(super) fn badge(note: &Annotation, index: usize, active: bool, cx: &mut Context<Editor>) -> impl IntoElement {
    let id = note.id;
    annotation_note::badge(SharedString::from(format!("annotation-marker-{id}")), index, note.has_rewrite(), active)
        .occlude()
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx)))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
            cx.stop_propagation();
            this.toggle_annotation_detail(id, window, cx);
        }))
}

/// In-place popover card for the active annotation in narrow mode, clamped to the editor bounds.
pub(super) fn popover_layer(
    note: Annotation,
    followup: Option<Entity<InputState>>,
    target_y: Pixels,
    editor_w: Pixels,
    editor_h: Pixels,
    cx: &mut Context<Editor>,
) -> AnyElement {
    let id = note.id;
    let theme = Theme::ocean();
    let status = if note.has_rewrite() { ANNOTATION_GENERATED } else { ANNOTATION_PENDING };
    let applied = note.is_applied();
    let margin = px(16.);
    let card_w = if editor_w > px(80.) { (editor_w - px(56.)).min(px(POP_MAX_W)).max(px(240.)) } else { px(POP_MAX_W) };
    let reserve = if editor_h > px(0.) { (editor_h * 0.45).clamp(px(140.), px(260.)) } else { px(220.) };
    let max_top = if editor_h > margin * 2. { (editor_h - margin - reserve).max(margin) } else { margin };
    let top = (target_y - px(6.)).max(margin).min(max_top);
    let max_h = if editor_h > top + margin { (editor_h - top - margin).max(px(96.)) } else { px(304.) };

    let rewrite_actions = note.has_rewrite().then(|| {
        div().flex_none().flex().items_center().gap(px(4.))
            .child(compact_action_button(
                SharedString::from(format!("pop-apply-{id}")),
                if applied { tr("✓ 已应用", "✓ Applied") } else { tr("✓ 应用", "✓ Apply") },
                BatchTone::Accent,
                !applied,
                cx.listener(move |this, _, window, cx| {
                    cx.stop_propagation();
                    this.apply_annotation(id, window, cx);
                }),
            ))
            .child(compact_action_button(
                SharedString::from(format!("pop-restore-{id}")),
                tr("↺ 还原", "↺ Restore"),
                BatchTone::Neutral,
                true,
                cx.listener(move |this, _, window, cx| {
                    cx.stop_propagation();
                    this.restore_annotation(id, window, cx);
                }),
            ))
    });
    let close_btn = annotation_note::close_pill(SharedString::from(format!("pop-close-{id}")), tr("收起", "Close"))
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
            cx.stop_propagation();
            close(this, window, cx);
        }));
    let head = div().w_full().flex().items_start().gap(px(8.))
        .child(annotation_note::popover_dot(status).mt(px(6.)))
        .child(div().flex_1().min_w_0().text_size(px(14.)).line_height(px(20.)).text_color(theme.text).child(note.body.clone()))
        .children(rewrite_actions)
        .child(close_btn);

    let rewrite_preview = (!applied).then(|| note.ai_rewritten.clone()).flatten()
        .filter(|s| !s.is_empty())
        .map(|rewritten| {
            div().w_full().px(px(10.)).py(px(6.)).rounded(crate::ui::radius::CONTROL).bg(theme.shell)
                .text_size(px(14.)).line_height(px(20.)).text_color(theme.text_secondary).child(rewritten)
        });

    let input_row = (!applied).then_some(followup).flatten().map(|input| {
        div().w_full().flex_none().flex().items_center().gap(px(6.))
            .child(div().flex_1().min_w_0().h(px(30.)).px(px(10.)).rounded(crate::ui::radius::CONTROL).bg(theme.shell)
                .border_1().border_color(theme.btn_border).flex().items_center()
                .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).px(px(0.))))
            .child(annotation_note::send_button(SharedString::from(format!("pop-send-{id}")), tr("发送 ↵", "Send ↵"), false)
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                    cx.stop_propagation();
                    this.submit_annotation_followup(window, cx);
                })))
    });
    let error = note.ai_error.clone().map(|m| div().text_size(px(14.)).line_height(px(20.)).text_color(rgb(0xb42318)).child(m));
    let scroll_body = div().id(SharedString::from(format!("annotation-pop-scroll-{id}")))
        .w_full().flex_1().min_h_0().overflow_y_scroll().on_scroll(|_, _, cx| cx.stop_propagation())
        .flex().flex_col().gap(px(8.)).child(head).children(rewrite_preview).children(error);

    popover::annotation_card(SharedString::from(format!("annotation-pop-{id}")), card_w)
        .max_h(max_h).absolute().top(top).right(px(42.)).key_context("AnnotationPopover")
        .on_action(cx.listener(|this, _: &Escape, window, cx| { close(this, window, cx); cx.stop_propagation(); }))
        .child(scroll_body)
        .children(input_row)
        .into_any_element()
}
