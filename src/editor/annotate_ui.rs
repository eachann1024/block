//! Right-rail annotations, narrow-mode inline/badge adaptive layout, and reaction picker.
use gpui::{
    AnyElement, BoxShadow, Context, IntoElement, MouseButton, ParentElement, Pixels,
    SharedString, Styled, Window, div, point, prelude::*, px, rgba,
};
use crate::{theme::Theme, ui::annotation_note};
use super::{
    annotate::{ReactionPicker, resolve_annotation_span},
    annotate_fit::{
        BADGE_STEP, INLINE_GAP, INLINE_MAX_W, INLINE_RIGHT, card_height, card_width,
        composer_height, fit_inline_card, measure_row, refresh_from_render, row_bottom,
        row_target, state_kind,
    },
    annotate_pop,
    annotate_rail::{RAIL_W, annotation_row, composer_row},
    view::Editor,
};

/// Extra width beside `std_w` needed to fit the 224px micro-strip rail (`8px` extra gap + `224px` rail).
pub(super) const RAIL_SLOT: f32 = RAIL_W + 8.;

/// True when the editor pane has enough horizontal room for `std_w` + gap + `224px` rail + scrollbar gutter
/// without shrinking the standard 672px body column.
pub(super) fn can_fit_wide_rail(editor: &Editor, std_w: Pixels) -> bool {
    let min_w = std_w + px(RAIL_SLOT + crate::ui::scrollbar::GUTTER);
    let actual_w = if editor.annotate.editor_width > px(0.) {
        editor.annotate.editor_width
    } else {
        editor.list.viewport_bounds().size.width
    };
    actual_w > px(0.) && actual_w >= min_w
}
const REACTIONS: [&str; 8] = ["👍", "❤️", "🎉", "🤔", "👀", "🔥", "✅", "💡"];

fn popover_shadows() -> Vec<BoxShadow> {
    let shadow = |color, y, blur| BoxShadow { color, offset: point(px(0.), px(y)), blur_radius: px(blur), spread_radius: px(0.), inset: false };
    vec![shadow(rgba(0x0000001f).into(), 1., 2.), shadow(rgba(0x00000014).into(), 2., 8.)]
}

pub(super) fn overlay(editor: &mut Editor, _window: &mut Window, cx: &mut Context<Editor>) -> Option<AnyElement> {
    if editor.minimal || editor.path.is_none() { return None; }
    let has_rail = editor.annotate.has_visible_rail();
    let has_reactions = editor.annotate.reactions.is_some();
    if !has_rail && !has_reactions { return None; }
    refresh_from_render(editor);
    let std_w = super::chrome::standard_width(Theme::ocean().content_width);
    let narrow = !can_fit_wide_rail(editor, std_w);

    let rail = has_rail.then(|| {
        if narrow {
            let mut markers = Vec::new();
            let mut popover = None;
            let mut next_y = px(12.);
            let mut notes = editor.annotate.annotations.iter()
                .filter(|note| !note.is_applied() || !note.reactions.is_empty())
                .cloned()
                .map(|note| {
                    let target = resolve_annotation_span(&editor.doc, &note).map(|(id, range, _)| (id, range.start));
                    let y = target.map(|(id, offset)| row_target(editor, id, offset)).unwrap_or(px(24.));
                    (y, target.map(|(id, _)| id), note)
                })
                .collect::<Vec<_>>();
            notes.sort_by(|a, b| a.0.partial_cmp(&b.0).unwrap_or(std::cmp::Ordering::Equal));
            for (index, (target_y, target_block, note)) in notes.into_iter().enumerate() {
                let id = note.id;
                let is_active = editor.annotate.active_annotation == Some(id);
                let is_hovered = editor.annotate.hovered_annotation == Some(id);
                let is_highlighted = is_active || is_hovered;
                if note.body.is_empty() && !note.reactions.is_empty() {
                    let y = target_y.max(next_y);
                    next_y = y + px(BADGE_STEP);
                    let emoji = note.reactions[0].clone();
                    markers.push(annotation_note::reaction_pill(SharedString::from(format!("annotation-marker-{id}")), &emoji, is_highlighted)
                        .absolute().top(y).right(px(INLINE_RIGHT)).occlude()
                        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx)))
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                            cx.stop_propagation();
                            this.toggle_reaction_on_note(id, &emoji, cx);
                        })).into_any_element());
                    continue;
                }
                let editing = editor.annotate.editing.as_ref().filter(|f| f.annotation_id == id).map(|f| f.input.clone());
                let followup = editor.annotate.followup.as_ref().filter(|f| f.annotation_id == id).map(|f| f.input.clone());
                let has_edit = editing.is_some();
                let has_followup = followup.is_some();
                let kind = state_kind(&note, has_edit, has_followup);
                let card_h = card_height(&note, has_edit, has_followup);
                let pref_w = card_width(&note, has_edit, has_followup, px(INLINE_MAX_W));
                if let Some((top, card_w)) = fit_inline_card(editor, target_block, target_y, next_y, card_h, pref_w) {
                    next_y = top + card_h + px(INLINE_GAP);
                    let row = annotation_row(note, editor.annotate.active_annotation, editor.annotate.hovered_annotation, editing, followup, cx);
                    markers.push(
                        div().absolute().top(top).right(px(INLINE_RIGHT)).w(card_w).occlude()
                            .child(row)
                            .child(measure_row(id, kind, cx))
                            .into_any_element(),
                    );
                } else {
                    let y = target_y.max(next_y);
                    next_y = y + px(BADGE_STEP);
                    let has_input = has_edit || has_followup;
                    let show_pop = is_highlighted || has_input;
                    markers.push(div().absolute().top(y).right(px(INLINE_RIGHT))
                        .child(annotate_pop::badge(&note, index + 1, show_pop, cx)).into_any_element());
                    if is_hovered || (popover.is_none() && (is_active || has_input)) {
                        popover = Some(annotate_pop::popover_layer(
                            note, editing, followup, y,
                            editor.annotate.editor_width, editor.annotate.editor_height, cx,
                        ));
                    }
                }
            }
            let mut layer = div().id("annotate-rail").absolute().top_0().left_0().size_full().children(markers).children(popover);
            if let Some(composer) = &editor.annotate.composer {
                let input = composer.input.clone();
                let has_text = !input.read(cx).value().trim().is_empty();
                let y = row_target(editor, composer.block_id, composer.range.start);
                let comp_h = composer_height();
                if let Some((top, card_w)) = fit_inline_card(editor, Some(composer.block_id), y, next_y, comp_h, px(RAIL_W)) {
                    layer = layer.child(
                        div().absolute().top(top).right(px(INLINE_RIGHT)).w(card_w).occlude()
                            .child(composer_row(input, has_text, false, cx))
                            .child(measure_row(0, 0, cx)),
                    );
                } else {
                    let below_y = (row_bottom(editor, composer.block_id, y) + px(8.)).max(px(16.));
                    let ed_h = editor.annotate.editor_height;
                    let pop_top = if below_y + comp_h > ed_h - px(16.) && y > comp_h + px(16.) {
                        (y - comp_h - px(8.)).max(px(16.))
                    } else if ed_h > comp_h + px(24.) {
                        below_y.min((ed_h - comp_h - px(16.)).max(px(16.)))
                    } else {
                        below_y
                    };
                    let ed_w = editor.annotate.editor_width;
                    let pop_w = if ed_w > px(80.) { (ed_w - px(48.)).min(px(280.)).max(px(180.)) } else { px(RAIL_W) };
                    layer = layer.child(
                        div().absolute().top(pop_top).right(px(24.)).w(pop_w).occlude()
                            .child(composer_row(input, has_text, true, cx))
                            .child(measure_row(0, 0, cx)),
                    );
                }
            }
            return layer;
        }
        let mut rows: Vec<(Pixels, u64, u8, Pixels, Pixels, AnyElement)> = editor.annotate.annotations.iter()
            .filter(|note| !note.is_applied() || !note.reactions.is_empty()).cloned().map(|note| {
                let target = resolve_annotation_span(&editor.doc, &note).map(|(id, range, _)| (id, range.start));
                let y = target.map(|(id, offset)| row_target(editor, id, offset)).unwrap_or(px(24.));
                let id = note.id;
                let editing = editor.annotate.editing.as_ref()
                    .filter(|field| field.annotation_id == id).map(|field| field.input.clone());
                let followup = editor.annotate.followup.as_ref()
                    .filter(|field| field.annotation_id == id).map(|field| field.input.clone());
                let kind = state_kind(&note, editing.is_some(), followup.is_some());
                let h = card_height(&note, editing.is_some(), followup.is_some());
                let w = card_width(&note, editing.is_some(), followup.is_some(), px(RAIL_W));
                (y, id, kind, h, w, annotation_row(note, editor.annotate.active_annotation, editor.annotate.hovered_annotation, editing, followup, cx).into_any_element())
            }).collect();
        if let Some(composer) = &editor.annotate.composer {
            let input = composer.input.clone();
            let has_text = !input.read(cx).value().trim().is_empty();
            rows.push((row_target(editor, composer.block_id, composer.range.start), 0, 0, composer_height(), px(RAIL_W),
                composer_row(input, has_text, false, cx).into_any_element()));
        }
        rows.sort_by(|a, b| a.0.partial_cmp(&b.0).unwrap_or(std::cmp::Ordering::Equal));
        let mut bottom = px(12.);
        let rail_right = ((editor.annotate.editor_width - px(crate::ui::scrollbar::GUTTER) - (std_w + px(RAIL_SLOT))) / 2.)
            .max(px(0.)) + px(crate::ui::scrollbar::GUTTER);
        let mut rail = div().id("annotate-rail").absolute().top_0().right(rail_right).w(px(RAIL_W)).h_full();
        for (target, id, kind, height, width, row) in rows {
            let top = target.max(bottom + px(12.));
            bottom = top + height;
            rail = rail.child(div().absolute().top(top).left_0().w(width).occlude().child(row).child(measure_row(id, kind, cx)));
        }
        rail
    });

    let reactions = editor.annotate.reactions.clone().map(|picker| {
        let selected = picker.annotation_id
            .and_then(|id| editor.annotate.annotations.iter().find(|n| n.id == id))
            .and_then(|n| n.reactions.first().cloned())
            .filter(|emoji| picker.targets.iter().all(|target| editor.annotate.annotations.iter().any(|note|
                resolve_annotation_span(&editor.doc, note).is_some_and(|(id, range, _)| id == target.block_id && range == target.range)
                    && note.reactions.first() == Some(emoji))));
        reaction_popover(picker, selected, cx)
    });
    Some(div().absolute().top_0().left_0().size_full().children(rail).children(reactions).into_any_element())
}

fn reaction_popover(picker: ReactionPicker, selected_reaction: Option<String>, cx: &mut Context<Editor>) -> impl IntoElement {
    let x = (picker.anchor.x - px(120.)).max(px(24.));
    let y = (picker.anchor.y + px(8.)).max(px(48.));
    let bar = REACTIONS.iter().enumerate().fold(
        div().id("annotate-reactions").absolute().left(x).top(y).px(px(8.)).py(px(6.))
            .rounded(crate::ui::radius::SURFACE).bg(Theme::ocean().menu).shadow(popover_shadows())
            .flex().items_center().gap(px(4.)).occlude(),
        |row, (index, &emoji)| {
            let selected = selected_reaction.as_deref() == Some(emoji);
            row.child(
                annotation_note::reaction_picker_item(SharedString::from(format!("pick-emoji-{index}")), emoji, selected)
                    .children(crate::ui::smoke_overlay::probe(format!("pick-emoji-{index}")))
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                        cx.stop_propagation();
                        this.pick_reaction(emoji, cx);
                    })),
            )
        },
    );
    div().id("annotate-reactions-backdrop").absolute().top_0().left_0().size_full().occlude()
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
            this.annotate.reactions = None;
            cx.notify();
        }))
        .child(bar)
}
