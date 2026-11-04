//! Wide-window right rail: unified annotation cards (`Draft`, `Editing`, `Generating`, `Review`,
//! `Reject`, `Accepted`, `Error`) and inline composer.
use gpui::{Context, CursorStyle, Entity, IntoElement, MouseButton, ParentElement, SharedString, Styled, div, prelude::*, px, rgb};
use gpui_component::input::{Escape, Input, InputState};
use crate::{
    icons::{Icon, IconColors, icon},
    theme::Theme,
    ui::{
        annotation_card::{CardState, card_shell, on_press, render_card_state},
        annotation_note,
    },
};
use super::{annotate::{Annotation, ANNOTATION_PENDING}, view::Editor};

pub(super) const RAIL_W: f32 = 224.;

pub(super) fn build_card_state(
    note: &Annotation,
    editing: Option<Entity<InputState>>,
    followup: Option<Entity<InputState>>,
    cx: &mut Context<Editor>,
) -> CardState {
    let id = note.id;
    if let Some(input) = editing {
        return CardState::Editing {
            input,
            on_cancel: on_press(cx.listener(|this, _, window, cx| {
                cx.stop_propagation();
                this.cancel_edit_annotation(window, cx);
            })),
            on_save: on_press(cx.listener(|this, _, window, cx| {
                cx.stop_propagation();
                this.save_edit_annotation(window, cx);
            })),
        };
    }
    if note.streaming {
        return CardState::Generating { body: note.body.clone().into() };
    }
    if note.is_applied() {
        return CardState::Accepted {
            on_undo: on_press(cx.listener(move |this, _, window, cx| {
                cx.stop_propagation();
                this.restore_annotation(id, window, cx);
            })),
        };
    }
    if let Some(message) = note.ai_error.clone() {
        return CardState::Error {
            message: message.into(),
            on_retry: on_press(cx.listener(move |this, _, _, cx| {
                cx.stop_propagation();
                this.retry_annotation(id, cx);
            })),
            on_delete: on_press(cx.listener(move |this, _, window, cx| {
                cx.stop_propagation();
                this.delete_annotation(id, window, cx);
            })),
        };
    }
    if note.is_generated() {
        let rewritten: SharedString = note.ai_rewritten.clone().unwrap_or_default().into();
        if let Some(input) = followup {
            return CardState::Reject {
                rewritten,
                input,
                on_cancel: on_press(cx.listener(|this, _, window, cx| {
                    cx.stop_propagation();
                    this.close_annotation_followup(window, cx);
                })),
                on_submit: on_press(cx.listener(|this, _, window, cx| {
                    cx.stop_propagation();
                    this.submit_annotation_followup(window, cx);
                })),
            };
        }
        return CardState::Review {
            rewritten,
            on_accept: on_press(cx.listener(move |this, _, window, cx| {
                cx.stop_propagation();
                this.apply_annotation(id, window, cx);
            })),
            on_reject: on_press(cx.listener(move |this, _, window, cx| {
                cx.stop_propagation();
                this.open_reject_annotation(id, window, cx);
            })),
            on_regen: on_press(cx.listener(move |this, _, _, cx| {
                cx.stop_propagation();
                this.regenerate_annotation(id, cx);
            })),
            on_delete: on_press(cx.listener(move |this, _, window, cx| {
                cx.stop_propagation();
                this.delete_annotation(id, window, cx);
            })),
        };
    }
    CardState::Draft {
        body: note.body.clone().into(),
        on_edit: on_press(cx.listener(move |this, _, window, cx| {
            cx.stop_propagation();
            this.start_edit_annotation(id, window, cx);
        })),
        on_delete: on_press(cx.listener(move |this, _, window, cx| {
            cx.stop_propagation();
            this.delete_annotation(id, window, cx);
        })),
    }
}

pub(super) fn annotation_row(
    note: Annotation,
    active_id: Option<u64>,
    hovered_id: Option<u64>,
    editing: Option<Entity<InputState>>,
    followup: Option<Entity<InputState>>,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    let id = note.id;
    let is_active = active_id == Some(id);
    let is_highlighted = is_active || hovered_id == Some(id);
    let reaction_pills = note.reactions.first().map(|emoji| {
        let emoji_str = emoji.clone();
        div().w_full().flex().flex_wrap().items_center().gap(px(6.)).child(
            annotation_note::reaction_pill(SharedString::from(format!("reaction-pill-{id}-0")), emoji, is_highlighted)
                .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx)))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    this.toggle_reaction_on_note(id, &emoji_str, cx);
                })),
        )
    });
    if note.body.is_empty() {
        return div().id(SharedString::from(format!("annotation-row-{id}"))).w_full().children(reaction_pills);
    }
    let has_focus_input = editing.is_some() || followup.is_some();
    let state = build_card_state(&note, editing, followup, cx);
    let card = card_shell(SharedString::from(format!("annotation-row-{id}")), is_active || has_focus_input)
        .key_context("AnnotationCard")
        .on_action(cx.listener(move |this, _: &Escape, window, cx| {
            if this.annotate.editing.as_ref().is_some_and(|e| e.annotation_id == id) {
                this.cancel_edit_annotation(window, cx);
            } else if this.annotate.followup.as_ref().is_some_and(|f| f.annotation_id == id) {
                this.close_annotation_followup(window, cx);
            }
            cx.stop_propagation();
        }))
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx)))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
            this.toggle_annotation_detail(id, window, cx);
        }))
        .child(render_card_state(id, state));
    div().id(SharedString::from(format!("annotation-wrap-{id}"))).w_full().child(card)
        .children(reaction_pills.map(|p| p.mt(px(4.))))
}

pub(super) fn composer_row(input: Entity<InputState>, has_text: bool, cx: &mut Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean();
    let field = div().flex_1().min_w_0().h(px(28.)).flex().items_center().text_size(px(14.)).text_color(theme.text)
        .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).px(px(0.)));
    let color = rgb(0xffffff).into();
    let submit = div().id("annotate-submit").size(px(26.)).flex_none().rounded(px(13.)).flex().items_center().justify_center()
        .bg(if has_text { theme.accent } else { theme.btn_border })
        .when(has_text, |button| button.cursor(CursorStyle::PointingHand))
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.submit_annotation(window, cx)))
        .child(icon(Icon::ArrowUp, px(14.), IconColors { line: color, fill: gpui::transparent_black(), accent: color }));
    annotation_note::rail_note("annotate-composer", ANNOTATION_PENDING,
        div().w_full().h(px(30.)).flex().items_center().gap(px(6.)).child(field).child(submit))
        .key_context("AnnotateComposer")
        .on_action(cx.listener(|this, _: &Escape, window, cx| { this.close_annotation_composer(window, cx); cx.stop_propagation(); }))
}
