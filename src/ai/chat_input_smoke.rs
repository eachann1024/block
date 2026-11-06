//! Composer observations restricted to the isolated smoke sandbox.
use super::*;

pub(crate) fn snapshot(window: &mut Window, cx: &mut App) -> (String, gpui::Point<gpui::Pixels>) {
    assert!(crate::smoke::is_requested());
    let chat = chat(window, cx);
    let input = chat.read(cx).input.read(cx);
    (input.value().to_string(), input.scroll_offset())
}

pub(crate) fn is_focused(window: &mut Window, cx: &mut App) -> bool {
    use gpui::Focusable;
    assert!(crate::smoke::is_requested());
    chat(window, cx).read(cx).input.read(cx).focus_handle(cx).is_focused(window)
}
