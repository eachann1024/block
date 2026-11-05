//! Independent editor content zoom (70% - 200%, 10% steps), persisted in state.json.
use std::{
    cell::RefCell,
    sync::atomic::{AtomicU32, Ordering},
};
use gpui::{Context, Entity, WeakEntity};
use super::view::Editor;

pub const MIN_ZOOM: u32 = 70;
pub const MAX_ZOOM: u32 = 200;
pub const STEP_ZOOM: u32 = 10;
pub const DEFAULT_ZOOM: u32 = 100;

static MAIN_ZOOM_PERCENT: AtomicU32 = AtomicU32::new(DEFAULT_ZOOM);
static QUICK_ZOOM_PERCENT: AtomicU32 = AtomicU32::new(DEFAULT_ZOOM);

thread_local! {
    static EDITORS: RefCell<Vec<WeakEntity<Editor>>> = const { RefCell::new(Vec::new()) };
}

pub fn register_editor(editor: &Entity<Editor>) {
    let weak = editor.downgrade();
    EDITORS.with(|slot| {
        let mut list = slot.borrow_mut();
        list.retain(|w| w.upgrade().is_some());
        if !list.iter().any(|w| w.entity_id() == weak.entity_id()) {
            list.push(weak);
        }
    });
}

pub fn zoom_percent(minimal: bool) -> u32 {
    if minimal {
        QUICK_ZOOM_PERCENT.load(Ordering::Relaxed)
    } else {
        MAIN_ZOOM_PERCENT.load(Ordering::Relaxed)
    }
}

pub fn zoom_factor(minimal: bool) -> f32 {
    zoom_percent(minimal) as f32 / 100.0
}

fn snap_zoom(val: f32) -> u32 {
    let pct = ((val * 100.0).round() as u32).clamp(MIN_ZOOM, MAX_ZOOM);
    let snapped = ((pct + 5) / 10) * 10;
    snapped.clamp(MIN_ZOOM, MAX_ZOOM)
}

pub fn init_zoom(main_persisted: Option<f32>, quick_persisted: Option<f32>) {
    let main_val = main_persisted
        .filter(|v| v.is_finite() && *v > 0.)
        .map(snap_zoom)
        .unwrap_or(DEFAULT_ZOOM);
    MAIN_ZOOM_PERCENT.store(main_val, Ordering::Relaxed);

    let quick_val = quick_persisted
        .filter(|v| v.is_finite() && *v > 0.)
        .map(snap_zoom)
        .unwrap_or(DEFAULT_ZOOM);
    QUICK_ZOOM_PERCENT.store(quick_val, Ordering::Relaxed);
}

pub fn zoom_in(editor: &mut Editor, cx: &mut Context<Editor>) {
    step_zoom(editor, STEP_ZOOM as i32, cx);
}

pub fn zoom_out(editor: &mut Editor, cx: &mut Context<Editor>) {
    step_zoom(editor, -(STEP_ZOOM as i32), cx);
}

pub fn zoom_reset(editor: &mut Editor, cx: &mut Context<Editor>) {
    set_zoom(editor, DEFAULT_ZOOM, cx);
}

fn step_zoom(editor: &mut Editor, delta: i32, cx: &mut Context<Editor>) {
    let cur = zoom_percent(editor.minimal) as i32;
    let next = (cur + delta).clamp(MIN_ZOOM as i32, MAX_ZOOM as i32) as u32;
    set_zoom(editor, next, cx);
}

fn set_zoom(editor: &mut Editor, target: u32, cx: &mut Context<Editor>) {
    let target = target.clamp(MIN_ZOOM, MAX_ZOOM);
    let minimal = editor.minimal;
    let old_pct = zoom_percent(minimal);
    if target == old_pct {
        return;
    }
    if minimal {
        QUICK_ZOOM_PERCENT.store(target, Ordering::Relaxed);
        if let Err(err) = crate::files::update_state(move |s| {
            s.quick_editor_zoom = (target != DEFAULT_ZOOM).then_some(target as f32 / 100.0);
        }) {
            crate::log::warn(&format!("failed to persist quick editor zoom: {err}"));
        }
    } else {
        MAIN_ZOOM_PERCENT.store(target, Ordering::Relaxed);
        if let Err(err) = crate::files::update_state(move |s| {
            s.editor_zoom = (target != DEFAULT_ZOOM).then_some(target as f32 / 100.0);
        }) {
            crate::log::warn(&format!("failed to persist editor zoom: {err}"));
        }
    }

    let caller_id = cx.entity_id();
    super::view::layout::clear_editor_keys(caller_id);
    super::image_view::clear_editor_measured(caller_id, cx);

    let (old_zoom, new_zoom) = (old_pct, target);
    on_zoom_changed(editor, old_zoom, new_zoom, cx);

    cx.defer(move |cx| {
        let editors = EDITORS.with(|slot| {
            let mut list = slot.borrow_mut();
            list.retain(|w| w.upgrade().is_some());
            list.clone()
        });
        for weak in editors {
            let other_id = weak.entity_id();
            if other_id == caller_id {
                continue;
            }
            if let Some(entity) = weak.upgrade() {
                entity.update(cx, |this, cx| {
                    if this.minimal == minimal {
                        super::view::layout::clear_editor_keys(other_id);
                        super::image_view::clear_editor_measured(other_id, cx);
                        on_zoom_changed(this, old_zoom, new_zoom, cx);
                    }
                });
            }
        }
    });
}

pub fn on_zoom_changed(editor: &mut Editor, old_zoom: u32, new_zoom: u32, cx: &mut Context<Editor>) {
    let ratio = if old_zoom > 0 { new_zoom as f32 / old_zoom as f32 } else { 1.0 };
    let ratio = if ratio.is_finite() && ratio > 0. { ratio } else { 1.0 };
    let top = editor.list.logical_scroll_top();
    let new_offset = gpui::px(f32::from(top.offset_in_item) * ratio);
    editor.reset_layouts();
    editor.list.reset(editor.doc.blocks.len());
    editor.list.scroll_to(gpui::ListOffset {
        item_ix: top.item_ix,
        offset_in_item: new_offset,
    });
    editor.scroll_top = (top.item_ix, new_offset);
    editor.table_chrome = None;
    editor.toolbar_anchor = None;
    editor.format = None;
    editor.block_menu = None;
    editor.table_menu = None;
    editor.image_menu = None;
    editor.link_ui.reset_hover();
    editor.dragging = None;
    editor.drag_over = None;
    editor.table_drag = None;
    editor.table_extend = None;
    editor.preferred_x = None;
    editor.pending_vertical = None;
    super::table_resize::end();
    cx.notify();
}
