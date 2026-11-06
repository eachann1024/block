//! Shared save and hide policy for native close and the last-tab shortcut.
use gpui::{App, Entity, Window};
use crate::editor::Editor;

pub(crate) fn should_close(window: &mut Window, editor: &Entity<Editor>, cx: &mut App) -> bool {
    let bounds = crate::window_bounds::capture_window_bounds(window, cx);
    let _ = crate::files::update_state(|s| s.main_window_bounds = Some(bounds));
    let ready = editor.update(cx, |editor, cx| {
        let ready = editor.prepare_to_quit();
        if !ready { cx.notify(); }
        ready
    });
    ready && !crate::search::window::hide_main_window_on_close()
}

pub(crate) fn close(window: &mut Window, editor: &Entity<Editor>, cx: &mut App) {
    if should_close(window, editor, cx) { window.remove_window(); }
}
