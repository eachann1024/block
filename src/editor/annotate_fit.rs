//! Narrow-mode inline card fit test and layout geometry synchronization.
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

pub(super) const INLINE_RIGHT: f32 = 12.;
pub(super) const INLINE_CLEARANCE: f32 = 12.;
pub(super) const INLINE_MIN_W: f32 = 200.;
pub(super) const INLINE_MAX_W: f32 = 340.;
pub(super) const INLINE_GAP: f32 = 10.;
pub(super) const BADGE_STEP: f32 = 28.;

thread_local! {
    static ROW_HEIGHTS: RefCell<HashMap<(u64, u8), Pixels>> = RefCell::new(HashMap::new());
}

#[derive(Clone, Copy, Debug)]
pub(super) struct LineBand {
    pub top: Pixels,
    pub bottom: Pixels,
    pub right: Pixels,
}

#[derive(Clone, Default, Debug)]
pub(super) struct LayoutSnapshot {
    pub editor_w: Pixels,
    pub editor_h: Pixels,
    pub lines: Vec<LineBand>,
    pub blocks: Vec<u64>,
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
    ROW_HEIGHTS.with(|heights| heights.borrow().get(&(0, 0)).copied()).unwrap_or(px(34.))
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

fn build_snapshot(editor: &Editor, editor_left: Pixels, editor_top: Pixels, editor_w: Pixels, editor_h: Pixels) -> LayoutSnapshot {
    let mut lines = Vec::new();
    let mut blocks = Vec::with_capacity(editor.layouts.len());
    for (&block_id, layout) in &editor.layouts {
        blocks.push(block_id);
        let block = editor.doc.get(block_id);
        if block.is_some_and(|b| b.meta.code_language.is_some()) {
            lines.push(LineBand {
                top: layout.bounds.top() - px(26.) - editor_top,
                bottom: layout.bounds.bottom() + px(12.) - editor_top,
                right: layout.bounds.right() - editor_left,
            });
            continue;
        }
        if block.is_some_and(|b| b.meta.callout.is_some()) {
            lines.push(LineBand {
                top: layout.bounds.top() - px(12.) - editor_top,
                bottom: layout.bounds.bottom() + px(12.) - editor_top,
                right: layout.bounds.right() + px(16.) - editor_left,
            });
            continue;
        }
        if !layout.cells.is_empty() || block.is_some_and(|b| b.meta.table.is_some()) {
            let grid = super::table_layout::grid_rect(layout).unwrap_or(layout.bounds);
            lines.push(LineBand { top: grid.top() - editor_top, bottom: grid.bottom() - editor_top, right: grid.right() - editor_left });
            continue;
        }
        let mut y = layout.bounds.top();
        for (_, line) in &layout.lines {
            let line_h = line.size(layout.line_height).height;
            let mut r_start = 0usize;
            for (v, r_end) in line.wrap_boundaries.iter()
                .map(|wb| line.unwrapped_layout.runs[wb.run_ix].glyphs[wb.glyph_ix].index)
                .chain(std::iter::once(line.len()))
                .enumerate()
            {
                let row_top = y + layout.line_height * (v as f32) - editor_top;
                let row_x0 = line.unwrapped_layout.x_for_index(r_start);
                let row_x1 = line.unwrapped_layout.x_for_index(r_end);
                let right = layout.bounds.left() + layout.aligned_x(line, v) + (row_x1 - row_x0) - editor_left;
                lines.push(LineBand { top: row_top, bottom: row_top + layout.line_height, right });
                r_start = r_end;
            }
            y += line_h;
        }
    }
    let start_ix = editor.list.logical_scroll_top().item_ix;
    for index in start_ix..editor.doc.blocks.len() {
        let Some(item) = editor.list.bounds_for_item(index) else { continue };
        let top = item.top() - editor_top;
        if editor_h > px(0.) && top > editor_h + px(200.) { break; }
        let bottom = item.bottom() - editor_top;
        if bottom < px(-200.) { continue; }
        let block = &editor.doc.blocks[index];
        if !editor.layouts.contains_key(&block.id)
            && (super::block_menu::is_divider(block) || super::image_source::spec_of(block).is_some() || block.meta.code_language.is_some())
        {
            lines.push(LineBand { top: top + px(8.), bottom: (bottom - px(8.)).max(top + px(12.)), right: item.right() - editor_left });
        }
    }
    blocks.sort_unstable();
    lines.sort_by_key(|l| (quant(l.top), quant(l.bottom), quant(l.right)));
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
    for l in &lines { (quant(l.top), quant(l.bottom), quant(l.right)).hash(&mut hasher); }
    for &((bid, off), y) in &targets { (bid, off, quant(y)).hash(&mut hasher); }
    LayoutSnapshot { editor_w, editor_h, lines, blocks, targets, signature: hasher.finish() }
}

pub(super) fn refresh_from_render(editor: &mut Editor) {
    if !editor.layouts.is_empty() && editor.annotate.editor_width > px(0.) {
        editor.annotate.fit_snapshot = build_snapshot(
            editor, editor.annotate.editor_left, editor.annotate.editor_top,
            editor.annotate.editor_width, editor.annotate.editor_height,
        );
    }
}

pub(super) fn sync_after_paint(editor: &mut Editor, bounds: Bounds<Pixels>) -> bool {
    let mut changed = editor.annotate.editor_left != bounds.left()
        || editor.annotate.editor_top != bounds.top()
        || editor.annotate.editor_width != bounds.size.width
        || editor.annotate.editor_height != bounds.size.height;
    (editor.annotate.editor_left, editor.annotate.editor_top, editor.annotate.editor_width, editor.annotate.editor_height) =
        (bounds.left(), bounds.top(), bounds.size.width, bounds.size.height);
    if !editor.layouts.is_empty() {
        let next = build_snapshot(editor, bounds.left(), bounds.top(), bounds.size.width, bounds.size.height);
        if editor.annotate.fit_snapshot.signature != next.signature {
            editor.annotate.fit_snapshot = next;
            if editor.annotate.has_visible_rail() { changed = true; }
        }
    }
    changed
}

/// Checks if an annotation card of height `card_h` fits in the empty margin to the right of
/// rendered lines overlapping its vertical band (with 12px clearance and >=200px width).
/// Returns `Some((top, card_w))` when expanded inline, or `None` when it must collapse to a badge.
pub(super) fn fit_inline_card(editor: &Editor, target_block: Option<u64>, target_y: Pixels, min_top: Pixels, card_h: Pixels) -> Option<(Pixels, Pixels)> {
    let snap = &editor.annotate.fit_snapshot;
    let block_id = target_block?;
    if snap.editor_w <= px(0.) || snap.blocks.binary_search(&block_id).is_err() { return None; }
    let card_right = snap.editor_w - px(INLINE_RIGHT);
    let base_top = target_y.max(min_top);
    if snap.editor_h > px(0.) && base_top > snap.editor_h { return None; }
    let candidates = if target_y > min_top {
        [base_top, (target_y - px(6.)).max(min_top), (target_y - px(12.)).max(min_top)]
    } else {
        [base_top, base_top, base_top]
    };
    for (i, cand_top) in candidates.into_iter().enumerate() {
        if i > 0 && cand_top == candidates[i - 1] { continue; }
        let band_top = cand_top + px(2.);
        let band_bottom = (cand_top + card_h - px(2.)).max(band_top + px(1.));
        let mut max_right = px(60.);
        for line in &snap.lines {
            if line.bottom > band_top && line.top < band_bottom && line.right > max_right {
                max_right = line.right;
            }
        }
        let available = card_right - (max_right + px(INLINE_CLEARANCE));
        if available >= px(INLINE_MIN_W) {
            return Some((cand_top, available.min(px(INLINE_MAX_W))));
        }
    }
    None
}
