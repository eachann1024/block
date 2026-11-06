//! Selectable Markdown replies use the shared UI renderer.
use gpui::{AnyElement, IntoElement};

pub(crate) fn render(source: &str, key: &str) -> AnyElement {
    crate::ui::selectable_message::message(format!("ai-reply-{key}"), source, true).into_any_element()
}
