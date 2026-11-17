//! Narrow-mode Scheme B block-foot strip: anchors reaction pills (`🤔 1`, `+`) and inline
//! annotation capsules/cards directly in document flow below the selection's ending block,
//! keeping the body column at 100% width with zero right-margin text overlap.
use gpui::{AnyElement, Context, Entity, IntoElement, MouseButton, ParentElement, SharedString, Styled, Window, div, prelude::*, px};
use gpui_component::input::{Escape, InputState};
use crate::{
    theme::Theme,
    ui::{
        annotation_card::{card_shell, on_press, render_card_state},
        annotation_note::{self, FootChipKind},
    },
};
use super::{
    annotate::{AnnotateComposer, Annotation, resolve_annotation_span, resolve_annotation_spans},
    annotate_rail::{build_card_state, composer_row},
    annotate_ui::{REACTIONS, can_fit_wide_rail},
    chrome::{HANDLE_WIDTH, RIGHT_INSET, ROW_GAP, block_gutter, standard_width},
    model::Block,
    surface::Surface,
    view::Editor,
};

fn close_active(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) {
    editor.annotate.active_annotation = None;
    editor.annotate.hovered_annotation = None;
    editor.annotate.followup = None;
    editor.annotate.editing = None;
    editor.annotate.foot_picker_note = None;
    editor.remeasure_annotation_rows();
    window.focus(&editor.focus, cx);
    cx.notify();
}

/// Maps `raw_index` to itself when visible, or walks up to the governing visible block
/// (e.g. a folded heading) so annotations inside folded sections never vanish into hidden rows.
fn visible_block_or_ancestor(editor: &Editor, raw_index: usize) -> Option<u64> {
    if editor.doc.blocks.is_empty() {
        return None;
    }
    let clamped = raw_index.min(editor.doc.blocks.len() - 1);
    if editor.minimal {
        return Some(editor.doc.blocks[clamped].id);
    }
    (0..=clamped)
        .rev()
        .find(|&ix| !editor.is_index_hidden(ix))
        .or_else(|| (0..editor.doc.blocks.len()).find(|&ix| !editor.is_index_hidden(ix)))
        .map(|ix| editor.doc.blocks[ix].id)
}

/// Resolves the last visible block of a single- or multi-block annotation span, falling back to
/// the nearest visible ancestor or stored block index when text changed or headings are folded.
pub(in crate::editor) fn anchor_block_for_note(editor: &Editor, note: &Annotation) -> Option<u64> {
    let spans = resolve_annotation_spans(&editor.doc, note);
    if let Some(&(_, _, _)) = spans.first() {
        if let Some(&(bid, _, _)) = spans
            .iter()
            .rev()
            .find(|&&(bid, _, _)| editor.doc.index_of(bid).is_some_and(|ix| editor.minimal || !editor.is_index_hidden(ix)))
        {
            return Some(bid);
        }
        if let Some(last_ix) = spans.last().and_then(|&(bid, _, _)| editor.doc.index_of(bid)) {
            return visible_block_or_ancestor(editor, last_ix);
        }
    }
    if let Some((bid, _, _)) = resolve_annotation_span(&editor.doc, note)
        && let Some(ix) = editor.doc.index_of(bid)
    {
        return visible_block_or_ancestor(editor, ix);
    }
    let fallback_ix = note.anchors.last().map_or(note.block_index, |a| a.block_index);
    visible_block_or_ancestor(editor, fallback_ix)
}

pub(in crate::editor) fn anchor_block_for_composer(editor: &Editor, composer: &AnnotateComposer) -> Option<u64> {
    if let Some(t) = composer
        .targets
        .iter()
        .rev()
        .find(|t| editor.doc.index_of(t.block_id).is_some_and(|ix| editor.minimal || !editor.is_index_hidden(ix)))
    {
        return Some(t.block_id);
    }
    let raw_ix = composer
        .targets
        .last()
        .and_then(|t| editor.doc.index_of(t.block_id).or(Some(t.block_index)))
        .or_else(|| editor.doc.index_of(composer.block_id))
        .unwrap_or(0);
    visible_block_or_ancestor(editor, raw_ix)
}

/// Wraps a shrink-to-fit capsule so it hugs short text while clamping to `min(100%, 460px)` in
/// narrow or deeply indented containers (preventing right-edge button clipping in Taffy flexbox)
/// and isolating clicks strictly to the capsule's own bounds (no full-width click dead zone).
fn wrap_chip(chip: impl IntoElement) -> AnyElement {
    div()
        .w_full()
        .max_w(px(460.))
        .min_w_0()
        .flex()
        .items_start()
        .child(
            div()
                .max_w_full()
                .min_w_0()
                .occlude()
                .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .on_mouse_up(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation())
                .child(chip),
        )
        .into_any_element()
}

fn expanded_card(
    note: Annotation,
    editing: Option<Entity<InputState>>,
    followup: Option<Entity<InputState>>,
    cx: &mut Context<Editor>,
) -> AnyElement {
    let id = note.id;
    let state = build_card_state(&note, editing, followup, cx);
    card_shell(SharedString::from(format!("annotation-foot-card-{id}")), true)
        .w_full()
        .max_w(px(440.))
        .min_w_0()
        .occlude()
        .key_context("AnnotationPopover")
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx)))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_mouse_up(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation())
        .on_action(cx.listener(move |this, _: &Escape, window, cx| {
            if this.annotate.editing.as_ref().is_some_and(|e| e.annotation_id == id) {
                this.cancel_edit_annotation(window, cx);
            } else if this.annotate.followup.as_ref().is_some_and(|f| f.annotation_id == id) {
                this.close_annotation_followup(window, cx);
            } else {
                close_active(this, window, cx);
            }
            cx.stop_propagation();
        }))
        .child(render_card_state(id, state))
        .into_any_element()
}

fn render_note_entry(note: Annotation, editor: &Editor, cx: &mut Context<Editor>) -> AnyElement {
    let id = note.id;
    let is_active = editor.annotate.active_annotation == Some(id);
    let is_highlighted = is_active || editor.annotate.hovered_annotation == Some(id);
    let editing = editor.annotate.editing.as_ref().filter(|f| f.annotation_id == id).map(|f| f.input.clone());
    let followup = editor.annotate.followup.as_ref().filter(|f| f.annotation_id == id).map(|f| f.input.clone());

    if editing.is_some() || followup.is_some() || note.ai_error.is_some() {
        return expanded_card(note, editing, followup, cx);
    }

    if note.is_generated() {
        let rewritten: SharedString = note.ai_rewritten.clone().unwrap_or_default().into();
        let chip = wrap_chip(
            annotation_note::foot_note_chip(
                SharedString::from(format!("annotation-foot-chip-{id}")),
                id,
                is_highlighted,
                FootChipKind::Review {
                    rewritten,
                    expanded: is_active,
                    on_toggle: on_press(cx.listener(move |this, _, window, cx| {
                        cx.stop_propagation();
                        this.toggle_annotation_detail(id, window, cx);
                    })),
                    on_accept: on_press(cx.listener(move |this, _, window, cx| {
                        cx.stop_propagation();
                        this.apply_annotation(id, window, cx);
                    })),
                },
            )
            .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx))),
        );

        if is_active {
            return div()
                .w_full()
                .min_w_0()
                .flex()
                .flex_col()
                .items_start()
                .gap(px(8.))
                .child(chip)
                .child(expanded_card(note, None, None, cx))
                .into_any_element();
        }
        return chip;
    }

    if is_active {
        return expanded_card(note, None, None, cx);
    }

    if note.streaming {
        return wrap_chip(
            annotation_note::foot_note_chip(
                SharedString::from(format!("annotation-foot-chip-{id}")),
                id,
                is_highlighted,
                FootChipKind::Generating {
                    body: note.body.into(),
                    on_toggle: on_press(cx.listener(move |this, _, window, cx| {
                        cx.stop_propagation();
                        this.toggle_annotation_detail(id, window, cx);
                    })),
                },
            )
            .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx))),
        );
    }

    wrap_chip(
        annotation_note::foot_note_chip(
            SharedString::from(format!("annotation-foot-chip-{id}")),
            id,
            is_highlighted,
            FootChipKind::Draft {
                body: note.body.into(),
                on_toggle: on_press(cx.listener(move |this, _, window, cx| {
                    cx.stop_propagation();
                    this.toggle_annotation_detail(id, window, cx);
                })),
                on_edit: on_press(cx.listener(move |this, _, window, cx| {
                    cx.stop_propagation();
                    this.start_edit_annotation(id, window, cx);
                })),
                on_delete: on_press(cx.listener(move |this, _, window, cx| {
                    cx.stop_propagation();
                    this.delete_annotation(id, window, cx);
                })),
            },
        )
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx))),
    )
}

pub(super) fn block_strip(editor: &Editor, block: &Block, zoom: f32, cx: &mut Context<Editor>) -> Option<AnyElement> {
    if editor.minimal
        || editor.path.is_none()
        || !matches!(editor.surface, Surface::Note)
        || !editor.annotate.has_visible_rail()
        || crate::export::active_view(cx).is_some()
    {
        return None;
    }
    let std_w = standard_width(Theme::ocean().content_width);
    if can_fit_wide_rail(editor, std_w) {
        return None;
    }

    let block_id = block.id;
    let anchored_notes: Vec<Annotation> = editor
        .annotate
        .annotations
        .iter()
        .filter(|note| (!note.is_applied() || !note.reactions.is_empty()) && anchor_block_for_note(editor, note) == Some(block_id))
        .cloned()
        .collect();

    let composer_input = editor
        .annotate
        .composer
        .as_ref()
        .filter(|c| anchor_block_for_composer(editor, c) == Some(block_id))
        .map(|c| c.input.clone());

    if anchored_notes.is_empty() && composer_input.is_none() {
        return None;
    }

    let mut children: Vec<AnyElement> = Vec::new();

    let reaction_notes: Vec<(u64, String, bool)> = anchored_notes
        .iter()
        .filter_map(|note| {
            let emoji = note.reactions.first()?.clone();
            let highlighted = editor.annotate.active_annotation == Some(note.id)
                || editor.annotate.hovered_annotation == Some(note.id)
                || editor.annotate.foot_picker_note == Some(note.id);
            Some((note.id, emoji, highlighted))
        })
        .collect();

    if let Some(&(last_rx_id, _, _)) = reaction_notes.last() {
        let &(target_rx_id, ref target_emoji, _) = reaction_notes
            .iter()
            .find(|&&(id, _, _)| editor.annotate.foot_picker_note == Some(id))
            .or_else(|| reaction_notes.iter().find(|&&(id, _, _)| editor.annotate.active_annotation == Some(id)))
            .or_else(|| reaction_notes.iter().find(|&&(id, _, _)| editor.annotate.hovered_annotation == Some(id)))
            .unwrap_or_else(|| reaction_notes.last().unwrap());
        let picker_open = editor.annotate.foot_picker_note == Some(target_rx_id);
        let selected_emoji = target_emoji.clone();
        let add_target_id = if picker_open { target_rx_id } else { last_rx_id };

        let mut rx_row = div()
            .max_w_full()
            .flex()
            .flex_wrap()
            .items_center()
            .gap(px(6.))
            .occlude()
            .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_mouse_up(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation());

        for (id, emoji, highlighted) in &reaction_notes {
            let id = *id;
            let emoji_str = emoji.clone();
            rx_row = rx_row.child(
                annotation_note::reaction_pill(
                    SharedString::from(format!("annotation-marker-{id}")),
                    emoji,
                    *highlighted,
                )
                .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx)))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    this.toggle_reaction_on_note(id, &emoji_str, cx);
                }))
                .on_mouse_down(MouseButton::Right, cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    this.toggle_foot_reaction_picker(id, cx);
                })),
            );
        }
        rx_row = rx_row.child(
            annotation_note::reaction_add_button(SharedString::from(format!("foot-rx-add-{block_id}")), picker_open)
                .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(add_target_id, *hovered, cx)))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    this.toggle_foot_reaction_picker(add_target_id, cx);
                })),
        );
        if picker_open {
            let bar = REACTIONS.iter().enumerate().fold(
                annotation_note::inline_reaction_bar(SharedString::from(format!("foot-rx-bar-{block_id}"))),
                |bar, (ix, &emoji)| {
                    let selected = selected_emoji == emoji;
                    bar.child(
                        annotation_note::reaction_picker_item(
                            SharedString::from(format!("foot-pick-emoji-{target_rx_id}-{ix}")),
                            emoji,
                            selected,
                        )
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                            cx.stop_propagation();
                            this.toggle_reaction_on_note(target_rx_id, emoji, cx);
                        })),
                    )
                },
            );
            rx_row = rx_row.child(bar);
        }
        children.push(rx_row.into_any_element());
    }

    for note in anchored_notes {
        if !note.body.is_empty() || note.has_non_reaction_data() {
            children.push(render_note_entry(note, editor, cx));
        }
    }

    if let Some(input) = composer_input {
        let has_text = !input.read(cx).value().trim().is_empty();
        children.push(
            div()
                .w_full()
                .max_w(px(360.))
                .min_w_0()
                .occlude()
                .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .on_mouse_up(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation())
                .child(composer_row(input, has_text, false, cx))
                .into_any_element(),
        );
    }

    let raw_left = HANDLE_WIDTH + ROW_GAP + block_gutter(block, zoom);
    let left_pad = if editor.annotate.editor_width > px(0.) {
        let max_left = (f32::from(editor.annotate.editor_width) - RIGHT_INSET - 200.).max(HANDLE_WIDTH + ROW_GAP);
        raw_left.min(max_left)
    } else {
        raw_left
    };
    let bottom_pad = if block.meta.table.is_some() { 10. * zoom } else { 2. * zoom };

    Some(
        div()
            .id(("annotate-foot", block_id as usize))
            .w_full()
            .min_w_0()
            .pl(px(left_pad))
            .pr(px(RIGHT_INSET))
            .pt(px(6. * zoom))
            .pb(px(bottom_pad))
            .child(
                div()
                    .w_full()
                    .min_w_0()
                    .max_w(Theme::ocean().content_width)
                    .flex()
                    .flex_col()
                    .items_start()
                    .gap(px(6.))
                    .children(children),
            )
            .into_any_element(),
    )
}
