//! Shared editor content zoom (70% - 200%, 10% steps), persisted in state.json.
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

static ZOOM_PERCENT: AtomicU32 = AtomicU32::new(DEFAULT_ZOOM);

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

pub fn zoom_percent() -> u32 {
    ZOOM_PERCENT.load(Ordering::Relaxed)
}

pub fn zoom_factor() -> f32 {
    zoom_percent() as f32 / 100.0
}

pub fn init_zoom(persisted: Option<f32>) {
    if let Some(val) = persisted {
        if val.is_finite() && val > 0. {
            let pct = ((val * 100.0).round() as u32).clamp(MIN_ZOOM, MAX_ZOOM);
            let snapped = ((pct + 5) / 10) * 10;
            ZOOM_PERCENT.store(snapped.clamp(MIN_ZOOM, MAX_ZOOM), Ordering::Relaxed);
        }
    }
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
    let cur = zoom_percent() as i32;
    let next = (cur + delta).clamp(MIN_ZOOM as i32, MAX_ZOOM as i32) as u32;
    set_zoom(editor, next, cx);
}

fn set_zoom(editor: &mut Editor, target: u32, cx: &mut Context<Editor>) {
    let target = target.clamp(MIN_ZOOM, MAX_ZOOM);
    let old_pct = zoom_percent();
    if target == old_pct {
        return;
    }
    ZOOM_PERCENT.store(target, Ordering::Relaxed);
    if let Err(err) = crate::files::update_state(move |s| {
        s.editor_zoom = (target != DEFAULT_ZOOM).then_some(target as f32 / 100.0);
    }) {
        crate::log::warn(&format!("failed to persist editor zoom: {err}"));
    }

    super::view::layout::clear_keys();
    super::image_view::clear_measured(cx);

    let (old_zoom, new_zoom) = (old_pct, target);
    on_zoom_changed(editor, old_zoom, new_zoom, cx);

    let caller_id = cx.entity_id();
    cx.defer(move |cx| {
        let editors = EDITORS.with(|slot| {
            let mut list = slot.borrow_mut();
            list.retain(|w| w.upgrade().is_some());
            list.clone()
        });
        for weak in editors {
            if weak.entity_id() == caller_id {
                continue;
            }
            if let Some(entity) = weak.upgrade() {
                entity.update(cx, |this, cx| {
                    on_zoom_changed(this, old_zoom, new_zoom, cx);
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
    super::image_view::cancel_resize(cx);
    crate::dropdown::dismiss_all(cx);
    cx.notify();
}
