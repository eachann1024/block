use gpui::ClipboardItem;
use serde::{Deserialize, Serialize};

const BLOCKS_V1: &str = "com.block.editor.blocks.v1";

#[derive(Deserialize, Serialize)]
struct BlockPayload {
    kind: String,
    text: String,
    markdown: String,
}

pub(super) fn block_item(text: String, markdown: &str) -> ClipboardItem {
    let payload = BlockPayload { kind: BLOCKS_V1.into(), text: text.clone(), markdown: markdown.to_owned() };
    match serde_json::to_string(&payload) {
        Ok(metadata) => ClipboardItem::new_string_with_metadata(text, metadata),
        Err(_) => ClipboardItem::new_string(text),
    }
}

pub(super) fn block_markdown(item: &ClipboardItem, text: &str) -> Option<String> {
    let payload: BlockPayload = serde_json::from_str(item.metadata()?).ok()?;
    (payload.kind == BLOCKS_V1 && payload.text == text && !payload.markdown.is_empty())
        .then_some(payload.markdown)
}

pub(crate) fn forward_focused_input_action(action: &super::view::EditorAction, window: &mut gpui::Window, cx: &mut gpui::App) -> bool {
    if gpui_component::WindowExt::focused_input(window, cx).is_none() { return false; }
    use super::view::EditorAction::*;
    let a: Option<Box<dyn gpui::Action>> = match action {
        Paste => Some(Box::new(gpui_component::input::Paste)), Copy => Some(Box::new(gpui_component::input::Copy)),
        Cut => Some(Box::new(gpui_component::input::Cut)), SelectAll => Some(Box::new(gpui_component::input::SelectAll)),
        Undo => Some(Box::new(gpui_component::input::Undo)), Redo => Some(Box::new(gpui_component::input::Redo)),
        _ => None,
    };
    let Some(a) = a else { return false };
    window.dispatch_action(a, cx); true
}
