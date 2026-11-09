//! Image width previews follow the pointer until release, then persist as one undoable edit.
use gpui::{App, Entity, EntityId, Global, MouseButton, Pixels, Window};
use super::{image_view, view::Editor};

pub(super) const MIN_SHARE: f32 = 0.1;

#[derive(Clone, Copy)]
struct Drag {
    editor: EntityId, block: u64, start_x: Pixels, start_width: f32,
    width: f32, max: f32, direction: f32, zoom: f32,
}

#[derive(Default)]
struct ImageResize(Option<Drag>);
impl Global for ImageResize {}

pub(super) fn width(editor: EntityId, block: u64, cx: &App) -> Option<f32> {
    cx.try_global::<ImageResize>()?.0
        .filter(|d| d.editor == editor && d.block == block).map(|d| d.width)
}

pub(super) fn clear(editor: EntityId, cx: &mut App) {
    let state = cx.default_global::<ImageResize>();
    if state.0.is_some_and(|d| d.editor == editor) { state.0 = None; }
}

pub(super) fn begin(editor: &Entity<Editor>, block: u64, direction: f32, zoom: f32, x: Pixels, window: &mut Window, cx: &mut App) {
    let minimal = editor.read(cx).minimal;
    let (max, measured) = image_view::sizes(cx, editor.entity_id(), block, minimal);
    let start_width = if measured > 0. { measured } else { max };
    editor.update(cx, |this, cx| {
        this.commit_composition(cx);
        this.clear_pointer();
        this.image_menu = None;
        this.focus_block(block, window, cx);
        cx.notify();
    });
    cx.default_global::<ImageResize>().0 = Some(Drag {
        editor: editor.entity_id(), block, start_x: x, start_width,
        width: start_width, max, direction, zoom,
    });
}

fn move_to(editor: &Entity<Editor>, block: u64, x: Pixels, cx: &mut App) {
    let Some(drag) = cx.default_global::<ImageResize>().0.as_mut()
        .filter(|d| d.editor == editor.entity_id() && d.block == block) else { return };
    let next = (drag.start_width + f32::from(x - drag.start_x) / drag.zoom * drag.direction)
        .clamp(drag.max * MIN_SHARE, drag.max);
    if (drag.width - next).abs() < 0.01 { return; }
    drag.width = next;
    image_view::relayout(editor, block, cx);
}

fn finish(editor: &Entity<Editor>, block: u64, commit: bool, cx: &mut App) {
    if width(editor.entity_id(), block, cx).is_none() { return; }
    let Some(drag) = cx.default_global::<ImageResize>().0.take() else { return };
    if commit && (drag.width - drag.start_width).abs() >= 1. {
        let width = (drag.width < drag.max - 0.5).then(|| drag.width.round().max(1.) as u32);
        editor.update(cx, |this, cx| this.edit_image(block, |s| s.width = width, cx));
    } else {
        image_view::relayout(editor, block, cx);
    }
}

/// Register from paint so capture continues beyond the image, editor and other child hitboxes.
pub(super) fn track(editor: &Entity<Editor>, focused: bool, window: &mut Window, cx: &mut App) {
    let Some(drag) = cx.default_global::<ImageResize>().0.filter(|d| d.editor == editor.entity_id()) else { return };
    let block = drag.block;
    if !window.is_window_active() || !focused {
        finish(editor, block, false, cx);
        return;
    }
    window.set_window_cursor_style(gpui::CursorStyle::ResizeLeftRight);
    window.on_mouse_event({
        let editor = editor.clone();
        move |event: &gpui::MouseMoveEvent, phase, _, cx| {
            if phase != gpui::DispatchPhase::Capture || width(editor.entity_id(), block, cx).is_none() { return; }
            if event.pressed_button == Some(MouseButton::Left) { move_to(&editor, block, event.position.x, cx); }
            else { finish(&editor, block, true, cx); }
            cx.stop_propagation();
        }
    });
    window.on_mouse_event({
        let editor = editor.clone();
        move |event: &gpui::MouseUpEvent, phase, _, cx| {
            if phase != gpui::DispatchPhase::Capture || event.button != MouseButton::Left
                || width(editor.entity_id(), block, cx).is_none() { return; }
            move_to(&editor, block, event.position.x, cx);
            finish(&editor, block, true, cx);
            cx.stop_propagation();
        }
    });
}
