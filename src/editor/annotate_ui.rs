//! Wide-window right-rail annotations and selection-toolbar reaction picker.
//! Narrow windows render Scheme B block-foot strips inline via `annotate_foot`.
use gpui::{
    AnyElement, Bounds, BoxShadow, Context, IntoElement, MouseButton, ParentElement, Pixels,
    Point, SharedString, Styled, Window, deferred, div, point, prelude::*, px, rgba, size,
};
use crate::{theme::Theme, ui::annotation_note};
use super::{
    annotate::{ReactionPicker, resolve_annotation_span},
    annotate_fit::{
        card_height, card_width, composer_height, measure_row, refresh_from_render,
        row_target, state_kind,
    },
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
pub(super) const REACTIONS: [&str; 8] = ["👍", "❤️", "🎉", "🤔", "👀", "🔥", "✅", "💡"];

fn popover_shadows() -> Vec<BoxShadow> {
    let shadow = |color, y, blur| BoxShadow { color, offset: point(px(0.), px(y)), blur_radius: px(blur), spread_radius: px(0.), inset: false };
    vec![shadow(rgba(0x0000001f).into(), 1., 2.), shadow(rgba(0x00000014).into(), 2., 8.)]
}

pub(super) fn overlay(editor: &mut Editor, _window: &mut Window, cx: &mut Context<Editor>) -> Option<AnyElement> {
    if editor.minimal || editor.path.is_none() { return None; }
    let std_w = super::chrome::standard_width(Theme::ocean().content_width);
    let wide_rail = editor.annotate.has_visible_rail() && can_fit_wide_rail(editor, std_w);
    let has_reactions = editor.annotate.reactions.is_some();
    if !wide_rail && !has_reactions { return None; }
    if wide_rail {
        refresh_from_render(editor);
    }

    let rail = wide_rail.then(|| {
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
            .and_then(|n| n.reactions.first().cloned());
        reaction_popover(picker, selected, cx)
    });
    Some(div().absolute().top_0().left_0().size_full().children(rail).children(reactions).into_any_element())
}

/// Window-space box for the reaction control the picker should hang from.
/// The selection toolbar's second button sits at padding 4 + 28 + gap 4, py 6.
fn reaction_button(origin: Point<Pixels>) -> Bounds<Pixels> {
    Bounds::new(point(origin.x + px(36.), origin.y + px(6.)), size(px(28.), px(28.)))
}

fn reaction_trigger(editor: &Editor, window: &Window, fallback: Point<Pixels>) -> Bounds<Pixels> {
    if !editor.caret.is_empty()
        && let Some(bounds) = super::toolbar::tracked_button(crate::theme::tr("添加表情回应", "Add reaction"))
            .or_else(|| super::toolbar::tracked_button(super::toolbar::more_key()))
    {
        return bounds;
    }
    let viewport = editor.list.viewport_bounds();
    if let Some(origin) = editor.selection_toolbar_anchor(viewport, window) {
        return reaction_button(origin);
    }
    if !editor.caret.is_empty()
        && let Some(rect) = editor.layouts.iter().filter_map(|(id, layout)| {
            let range = editor.selected_range(*id)?;
            if range.is_empty() { return None; }
            layout.rects(range).into_iter().next()
        }).min_by(|a, b| a.top().partial_cmp(&b.top()).unwrap_or(std::cmp::Ordering::Equal))
    {
        let height = super::chrome::TOOLBAR_HEIGHT;
        let bar_w = editor.selection_toolbar_width(viewport.size.width - px(16.));
        let x = rect.left().max(viewport.left() + px(8.)).min((viewport.right() - bar_w - px(8.)).max(viewport.left()));
        let above = rect.top() - height - px(8.);
        let y = if above >= viewport.top() { above } else { rect.bottom() + px(8.) };
        let y = y.min((viewport.bottom() - height).max(viewport.top())).max(viewport.top());
        return reaction_button(point(x, y));
    }
    if let Some((id, bounds)) = super::image_size::reaction_bounds()
        && id == editor.caret.head.block
    {
        return bounds;
    }
    Bounds::new(fallback, size(px(28.), px(28.)))
}

fn reaction_popover(picker: ReactionPicker, selected_reaction: Option<String>, cx: &mut Context<Editor>) -> impl IntoElement {
    let editor = cx.entity();
    let fallback = picker.anchor;
    let bar = REACTIONS.iter().enumerate().fold(
        div().id("annotate-reactions").px(px(8.)).py(px(6.))
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
        .child(deferred(crate::dropdown::smart_anchored_with(move |window, cx| {
            let editor = editor.read(cx);
            let viewport = editor.list.viewport_bounds();
            crate::dropdown::PlacementSpec::below(reaction_trigger(&editor, window, fallback))
                .align_x(crate::dropdown::AlignX::Center)
                .gap(px(6.))
                .margin(px(8.))
                .viewport(viewport)
        }).child(bar)).with_priority(4))
}
