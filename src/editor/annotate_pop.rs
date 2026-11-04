//! Narrow-window annotations: 24×24 numbered margin badges that open an in-place popover
//! sharing the unified `src/ui/annotation_card` component.
use gpui::{AnyElement, Context, Entity, IntoElement, MouseButton, ParentElement, Pixels, SharedString, Styled, Window, div, prelude::*, px};
use gpui_component::input::{Escape, InputState};
use crate::ui::{
    annotation_card::{card_shell, render_card_state},
    annotation_note,
    popover,
};
use super::{annotate::Annotation, annotate_rail::build_card_state, view::Editor};

const POP_MAX_W: f32 = 340.;

fn close(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) {
    editor.annotate.active_annotation = None;
    editor.annotate.followup = None;
    editor.annotate.editing = None;
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
    editing: Option<Entity<InputState>>,
    followup: Option<Entity<InputState>>,
    target_y: Pixels,
    editor_w: Pixels,
    editor_h: Pixels,
    cx: &mut Context<Editor>,
) -> AnyElement {
    let id = note.id;
    let margin = px(16.);
    let card_w = if editor_w > px(80.) { (editor_w - px(56.)).min(px(POP_MAX_W)).max(px(240.)) } else { px(POP_MAX_W) };
    let reserve = if editor_h > px(0.) { (editor_h * 0.45).clamp(px(140.), px(260.)) } else { px(220.) };
    let max_top = if editor_h > margin * 2. { (editor_h - margin - reserve).max(margin) } else { margin };
    let top = (target_y - px(6.)).max(margin).min(max_top);
    let max_h = if editor_h > top + margin { (editor_h - top - margin).max(px(96.)) } else { px(304.) };

    let state = build_card_state(&note, editing, followup, cx);
    let scroll_body = div()
        .id(SharedString::from(format!("annotation-pop-scroll-{id}")))
        .w_full()
        .flex_1()
        .min_h_0()
        .overflow_y_scroll()
        .on_scroll(|_, _, cx| cx.stop_propagation())
        .child(render_card_state(id, state));

    card_shell(SharedString::from(format!("annotation-pop-{id}")), true)
        .w(card_w)
        .max_h(max_h)
        .shadow(popover::shadows())
        .occlude()
        .absolute()
        .top(top)
        .right(px(42.))
        .key_context("AnnotationPopover")
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_action(cx.listener(move |this, _: &Escape, window, cx| {
            if this.annotate.editing.as_ref().is_some_and(|e| e.annotation_id == id) {
                this.cancel_edit_annotation(window, cx);
            } else if this.annotate.followup.as_ref().is_some_and(|f| f.annotation_id == id) {
                this.close_annotation_followup(window, cx);
            } else {
                close(this, window, cx);
            }
            cx.stop_propagation();
        }))
        .child(scroll_body)
        .into_any_element()
}
