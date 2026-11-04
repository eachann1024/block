//! Narrow-window annotations: numbered margin badges that open an in-place popover.
use gpui::{AnyElement, Context, Entity, IntoElement, MouseButton, ParentElement, SharedString, Styled, Window, div, prelude::*, px, rgb};
use gpui_component::input::{Escape, Input, InputState};
use crate::{theme::Theme, ui::{annotation_actions::{BatchTone, batch_button}, annotation_note, popover}};
use super::{annotate::{Annotation, ANNOTATION_GENERATED, ANNOTATION_PENDING}, view::Editor};

const POP_W: f32 = 280.;

fn close(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) {
    editor.annotate.active_annotation = None;
    editor.annotate.followup = None;
    window.focus(&editor.focus, cx);
    cx.notify();
}

pub(super) fn badge(note: &Annotation, index: usize, active: bool, cx: &mut Context<Editor>) -> impl IntoElement {
    let id = note.id;
    let status = if note.has_rewrite() { ANNOTATION_GENERATED } else { ANNOTATION_PENDING };
    annotation_note::badge(SharedString::from(format!("annotation-marker-{id}")), index, status, active)
        .occlude()
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx)))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
            cx.stop_propagation();
            this.toggle_annotation_detail(id, window, cx);
        }))
}

/// Backdrop (click outside closes) plus the popover card for the active annotation.
pub(super) fn popover_layer(note: Annotation, followup: Option<Entity<InputState>>, y: gpui::Pixels, cx: &mut Context<Editor>) -> AnyElement {
    let id = note.id;
    let theme = Theme::ocean();
    let status = if note.has_rewrite() { ANNOTATION_GENERATED } else { ANNOTATION_PENDING };
    let applied = note.is_applied();
    let actions = note.has_rewrite().then(|| {
        div().mt(px(6.)).flex().items_center().gap(px(4.))
            .child(batch_button(SharedString::from(format!("pop-apply-{id}")), if applied { "✓ 已应用" } else { "✓ 应用" }, BatchTone::Accent, !applied,
                cx.listener(move |this, _, window, cx| { this.apply_annotation(id, window, cx); })))
            .child(batch_button(SharedString::from(format!("pop-restore-{id}")), "↺ 还原", BatchTone::Neutral, true,
                cx.listener(move |this, _, window, cx| { this.restore_annotation(id, window, cx); })))
    });
    let input = (!applied).then_some(followup).flatten().map(|input| {
        div().mt(px(6.)).h(px(30.)).px(px(8.)).rounded(px(5.)).bg(theme.shell).border_1().border_color(theme.btn_border).flex().items_center()
            .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).px(px(0.)))
    });
    let error = note.ai_error.clone().map(|m| div().mt(px(4.)).text_size(px(14.)).line_height(px(20.)).text_color(rgb(0xb42318)).child(m));
    let head = div().flex().items_start().gap(px(8.)).px(px(6.)).pt(px(6.))
        .child(annotation_note::status_dot(status).mt(px(7.)))
        .child(div().flex_1().min_w_0().text_size(px(14.)).line_height(px(20.)).text_color(theme.text).child(note.body.clone()));
    let card = popover::card(SharedString::from(format!("annotation-pop-{id}")), Some(px(POP_W)))
        .absolute().top(y + px(28.)).right(px(16.)).key_context("AnnotationPopover")
        .on_action(cx.listener(|this, _: &Escape, window, cx| { close(this, window, cx); cx.stop_propagation(); }))
        .child(div().p(px(4.)).child(head).children(actions).children(input).children(error));
    div().id("annotation-pop-backdrop").absolute().top_0().left_0().size_full()
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| close(this, window, cx)))
        .child(card).into_any_element()
}
