//! Cardless right-rail annotations, compact follow-up input, and reaction picker.
use std::{cell::RefCell, collections::HashMap};
use gpui::{
    AnyElement, BoxShadow, Context, IntoElement, MouseButton, ParentElement, Pixels,
    SharedString, Styled, Window, div, point, prelude::*, px, rgba,
};
use crate::{theme::Theme, ui::annotation_note};
use super::{
    annotate::{ReactionPicker, resolve_annotation_span},
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
    actual_w <= px(0.) || actual_w >= min_w
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
    let std_w = super::chrome::standard_width(Theme::ocean().content_width);
    let narrow = !can_fit_wide_rail(editor, std_w);

    let rail = has_rail.then(|| {
        if narrow {
            let mut markers = Vec::new();
            let mut popover = None;
            let mut next_y = px(0.);
            let notes = editor.annotate.annotations.iter().filter(|note| !note.is_applied() || note.has_rewrite()).cloned().collect::<Vec<_>>();
            for (index, note) in notes.into_iter().enumerate() {
                let target = resolve_annotation_span(&editor.doc, &note).map(|(id, range, _)| (id, range.start));
                let y = target.map(|(id, offset)| row_target(editor, id, offset)).unwrap_or(px(24.)).max(next_y + px(28.));
                let id = note.id;
                let is_active = editor.annotate.active_annotation == Some(id);
                let is_highlighted = is_active || editor.annotate.hovered_annotation == Some(id);
                next_y = y;
                if note.body.is_empty() && !note.reactions.is_empty() {
                    let emoji = note.reactions[0].clone();
                    markers.push(annotation_note::reaction_pill(SharedString::from(format!("annotation-marker-{id}")), &emoji, is_highlighted)
                        .absolute().top(y).right(px(12.)).occlude()
                        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| this.hover_annotation(id, *hovered, cx)))
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                            cx.stop_propagation();
                            this.toggle_reaction_on_note(id, &emoji, cx);
                        })).into_any_element());
                } else {
                    markers.push(div().absolute().top(y).right(px(12.)).child(annotate_pop::badge(&note, index + 1, is_active, cx)).into_any_element());
                    if is_active {
                        let followup = editor.annotate.followup.as_ref().filter(|field| field.annotation_id == id).map(|field| field.input.clone());
                        popover = Some(annotate_pop::popover_layer(
                            note,
                            followup,
                            y,
                            editor.annotate.editor_width,
                            editor.annotate.editor_height,
                            cx,
                        ));
                    }
                }
            }
            let mut layer = div().id("annotate-rail").absolute().top_0().left_0().size_full().children(markers).children(popover);
            if let Some(composer) = &editor.annotate.composer {
                let input = composer.input.clone();
                let has_text = !input.read(cx).value().trim().is_empty();
                let y = row_target(editor, composer.block_id, composer.range.start);
                layer = layer.child(div().absolute().top(y).right(px(16.)).w(px(RAIL_W)).occlude()
                    .child(composer_row(input, has_text, cx)));
            }
            return layer;
        }
        let mut rows: Vec<(Pixels, u64, AnyElement)> = editor.annotate.annotations.iter()
            .filter(|note| !note.is_applied() || note.has_rewrite()).cloned().map(|note| {
                let target = resolve_annotation_span(&editor.doc, &note).map(|(id, range, _)| (id, range.start));
                let y = target.map(|(id, offset)| row_target(editor, id, offset)).unwrap_or(px(24.));
                let id = note.id;
                let followup = editor.annotate.followup.as_ref()
                    .filter(|field| field.annotation_id == id).map(|field| field.input.clone());
                (y, id, annotation_row(note, editor.annotate.active_annotation, editor.annotate.hovered_annotation, followup, cx).into_any_element())
            }).collect();
        if let Some(composer) = &editor.annotate.composer {
            let input = composer.input.clone();
            let has_text = !input.read(cx).value().trim().is_empty();
            rows.push((row_target(editor, composer.block_id, composer.range.start), 0,
                composer_row(input, has_text, cx).into_any_element()));
        }
        rows.sort_by(|a, b| a.0.partial_cmp(&b.0).unwrap_or(std::cmp::Ordering::Equal));
        let mut bottom = px(12.);
        let rail_right = ((editor.annotate.editor_width - px(crate::ui::scrollbar::GUTTER) - (std_w + px(RAIL_SLOT))) / 2.)
            .max(px(0.)) + px(crate::ui::scrollbar::GUTTER);
        let mut rail = div().id("annotate-rail").absolute().top_0().right(rail_right).w(px(RAIL_W)).h_full();
        for (target, id, row) in rows {
            let top = target.max(bottom + px(12.));
            let height = ROW_HEIGHTS.with(|heights| heights.borrow().get(&id).copied()).unwrap_or(px(24.));
            bottom = top + height;
            let entity = cx.entity().downgrade();
            let measure = gpui::canvas(move |bounds, _, _| {
                ROW_HEIGHTS.with(|heights| heights.borrow_mut().insert(id, bounds.size.height) != Some(bounds.size.height))
            }, move |_, changed, _, cx| {
                if changed {
                    let entity = entity.clone();
                    cx.defer(move |cx| { entity.update(cx, |_, cx| cx.notify()).ok(); });
                }
            }).absolute().size_full();
            rail = rail.child(div().absolute().top(top).w_full().occlude().child(row).child(measure));
        }
        rail
    });

    let reactions = editor.annotate.reactions.clone().map(|picker| {
        let selected = picker.annotation_id
            .and_then(|id| editor.annotate.annotations.iter().find(|n| n.id == id))
            .and_then(|n| n.reactions.first().cloned());
        reaction_popover(picker, selected, cx)
    });
    Some(div().absolute().top_0().left_0().size_full().children(rail).children(reactions).into_any_element())
}

thread_local! { static ROW_HEIGHTS: RefCell<HashMap<u64, Pixels>> = RefCell::new(HashMap::new()); }

fn row_target(editor: &Editor, block_id: u64, offset: usize) -> Pixels {
    editor.layouts.get(&block_id).map(|layout| layout.point(offset).y - editor.annotate.editor_top)
        .or_else(|| editor.doc.index_of(block_id).and_then(|index| editor.list.bounds_for_item(index)).map(|bounds| bounds.top() - editor.annotate.editor_top))
        .unwrap_or(px(24.)).max(px(24.))
}

fn reaction_popover(picker: ReactionPicker, selected_reaction: Option<String>, cx: &mut Context<Editor>) -> impl IntoElement {
    let x = (picker.anchor.x - px(120.)).max(px(24.));
    let y = (picker.anchor.y + px(8.)).max(px(48.));
    let bar = REACTIONS.iter().enumerate().fold(
        div().id("annotate-reactions").absolute().left(x).top(y).px(px(8.)).py(px(6.))
            .rounded(px(10.)).bg(Theme::ocean().menu).shadow(popover_shadows())
            .flex().items_center().gap(px(4.)).occlude(),
        |row, (index, &emoji)| {
            let selected = selected_reaction.as_deref() == Some(emoji);
            row.child(
                annotation_note::reaction_picker_item(SharedString::from(format!("pick-emoji-{index}")), emoji, selected)
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                        cx.stop_propagation();
                        this.pick_reaction(emoji, cx);
                    })),
            )
        },
    );
    div().id("annotate-reactions-backdrop").absolute().top_0().left_0().size_full()
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
            this.annotate.reactions = None;
            cx.notify();
        }))
        .child(bar)
}
