//! Wide-rail card measurement and layout anchor synchronization.
use std::{
    cell::RefCell,
    collections::{HashMap, hash_map::DefaultHasher},
    hash::{Hash, Hasher},
};
use gpui::{Bounds, Context, IntoElement, Pixels, prelude::*, px};
use super::{
    annotate::{Annotation, resolve_annotation_span},
    view::Editor,
};

thread_local! {
    static ROW_HEIGHTS: RefCell<HashMap<(u64, u8), Pixels>> = RefCell::new(HashMap::new());
}

#[derive(Clone, Default, Debug)]
pub(super) struct LayoutSnapshot {
    pub targets: Vec<((u64, usize), Pixels)>,
    pub signature: u64,
}

fn quant(v: Pixels) -> i32 { (f32::from(v) * 2.).round() as i32 }

pub(super) fn state_kind(note: &Annotation, has_edit: bool, has_followup: bool) -> u8 {
    if has_edit { 1 }
    else if note.streaming { 2 }
    else if note.is_applied() { 5 }
    else if note.ai_error.is_some() { 6 }
    else if note.is_generated() { if has_followup { 4 } else { 3 } }
    else { 0 }
}

fn text_width_px(text: &str) -> f32 {
    text.lines()
        .map(|line| line.chars().map(|ch| if ch.is_ascii() { 8.0 } else { 14.5 }).sum::<f32>())
        .fold(0.0_f32, f32::max)
}

pub(super) fn card_width(note: &Annotation, has_edit: bool, has_followup: bool, max_w: Pixels) -> Pixels {
    let kind = state_kind(note, has_edit, has_followup);
    let body_w = text_width_px(&note.body);
    let rewrite_w = note.ai_rewritten.as_deref().map_or(0.0, text_width_px);
    let err_w = note.ai_error.as_deref().map_or(0.0, text_width_px);
    let ideal = match kind {
        0 => (116.0 + body_w).max(136.0),
        1 => (48.0 + body_w).max(200.0),
        2 => (54.0 + body_w).max(168.0),
        3 => (52.0 + rewrite_w).max(168.0),
        4 => (52.0 + rewrite_w).max(236.0),
        5 => 148.0,
        _ => (100.0 + err_w).max(168.0),
    };
    px(ideal).min(max_w)
}

pub(super) fn card_height(note: &Annotation, has_edit: bool, has_followup: bool) -> Pixels {
    let kind = state_kind(note, has_edit, has_followup);
    if let Some(h) = ROW_HEIGHTS.with(|heights| heights.borrow().get(&(note.id, kind)).copied()) {
        return h;
    }
    let fallback = match kind {
        0 => 46. + (note.body.chars().count().max(1).div_ceil(14).saturating_sub(1) as f32) * 20.,
        1 => 88.,
        2..=4 => 96.,
        5 => 46.,
        _ => 56.,
    };
    px(fallback)
}

pub(super) fn composer_height() -> Pixels {
    ROW_HEIGHTS.with(|heights| heights.borrow().get(&(0, 0)).copied()).unwrap_or(px(44.))
}

pub(super) fn measure_row(id: u64, kind: u8, cx: &mut Context<Editor>) -> impl IntoElement {
    let entity = cx.entity().downgrade();
    gpui::canvas(
        move |bounds, _, _| {
            ROW_HEIGHTS.with(|heights| {
                let prev = heights.borrow_mut().insert((id, kind), bounds.size.height);
                prev.is_none_or(|p| (f32::from(p) - f32::from(bounds.size.height)).abs() > 0.5)
            })
        },
        move |_, changed, _, cx| {
            if changed {
                let entity = entity.clone();
                cx.defer(move |cx| { entity.update(cx, |_, cx| cx.notify()).ok(); });
            }
        },
    )
    .absolute()
    .size_full()
}

pub(super) fn row_target(editor: &Editor, block_id: u64, offset: usize) -> Pixels {
    editor.layouts.get(&block_id).map(|layout| layout.point(offset).y - editor.annotate.editor_top)
        .or_else(|| {
            editor.annotate.fit_snapshot.targets.iter()
                .find_map(|&((bid, off), y)| (bid == block_id && off == offset).then_some(y))
        })
        .or_else(|| {
            editor.doc.index_of(block_id).and_then(|index| editor.list.bounds_for_item(index))
                .map(|bounds| bounds.top() - editor.annotate.editor_top)
        })
        .unwrap_or(px(24.)).max(px(24.))
}

fn build_snapshot(editor: &Editor, editor_top: Pixels, editor_w: Pixels, editor_h: Pixels) -> LayoutSnapshot {
    let mut blocks: Vec<u64> = editor.layouts.keys().copied().collect();
    blocks.sort_unstable();
    let mut targets = Vec::new();
    for note in &editor.annotate.annotations {
        if let Some((bid, range, _)) = resolve_annotation_span(&editor.doc, note)
            && let Some(layout) = editor.layouts.get(&bid)
        {
            targets.push(((bid, range.start), (layout.point(range.start).y - editor_top).max(px(24.))));
        }
    }
    if let Some(c) = &editor.annotate.composer && let Some(layout) = editor.layouts.get(&c.block_id) {
        targets.push(((c.block_id, c.range.start), (layout.point(c.range.start).y - editor_top).max(px(24.))));
    }
    let mut hasher = DefaultHasher::new();
    (quant(editor_w), quant(editor_h), &blocks).hash(&mut hasher);
    for &((bid, off), y) in &targets { (bid, off, quant(y)).hash(&mut hasher); }
    LayoutSnapshot { targets, signature: hasher.finish() }
}

pub(super) fn refresh_from_render(editor: &mut Editor) {
    if !editor.layouts.is_empty() && editor.annotate.editor_width > px(0.) {
        editor.annotate.fit_snapshot = build_snapshot(
            editor, editor.annotate.editor_top,
            editor.annotate.editor_width, editor.annotate.editor_height,
        );
    }
}

pub(super) fn sync_after_paint(editor: &mut Editor, bounds: Bounds<Pixels>) -> bool {
    let width_changed = editor.annotate.editor_width != bounds.size.width;
    let mut changed = editor.annotate.editor_left != bounds.left()
        || editor.annotate.editor_top != bounds.top()
        || width_changed
        || editor.annotate.editor_height != bounds.size.height;
    (editor.annotate.editor_left, editor.annotate.editor_top, editor.annotate.editor_width, editor.annotate.editor_height) =
        (bounds.left(), bounds.top(), bounds.size.width, bounds.size.height);
    let std_w = super::chrome::standard_width(crate::theme::Theme::ocean().content_width);
    let now_wide = super::annotate_ui::can_fit_wide_rail(editor, std_w);
    let mode_flipped = editor.annotate.was_wide_rail != Some(now_wide);
    if mode_flipped {
        editor.annotate.was_wide_rail = Some(now_wide);
        if now_wide {
            editor.annotate.foot_picker_note = None;
        }
    }
    if editor.annotate.has_visible_rail() && (mode_flipped || (!now_wide && width_changed)) {
        editor.remeasure_annotation_rows();
        changed = true;
    }
    if !editor.layouts.is_empty() {
        let next = build_snapshot(editor, bounds.top(), bounds.size.width, bounds.size.height);
        if editor.annotate.fit_snapshot.signature != next.signature {
            editor.annotate.fit_snapshot = next;
            if editor.annotate.has_visible_rail() { changed = true; }
        }
    }
    changed
}
