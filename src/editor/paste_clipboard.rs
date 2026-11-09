use gpui::ClipboardItem;
use serde::{Deserialize, Serialize};
use super::model::{Block, InlineTag, Kind, Span};

const BLOCKS_V1: &str = "com.block.editor.blocks.v1";
const INLINE_V1: &str = "com.block.editor.inline.v1";

#[derive(Deserialize, Serialize)]
struct InlinePayload {
    kind: String,
    text: String,
    spans: Vec<Span>,
    inline: Vec<InlineTag>,
}

/// Text selections carry only inline styles; their heading/list/container is not selected.
pub(super) fn inline_item(fragment: &Block) -> ClipboardItem {
    let payload = InlinePayload { kind: INLINE_V1.into(), text: fragment.text.clone(),
        spans: fragment.spans.clone(), inline: fragment.meta.inline.clone() };
    match serde_json::to_string(&payload) {
        Ok(metadata) => ClipboardItem::new_string_with_metadata(fragment.text.clone(), metadata),
        Err(_) => ClipboardItem::new_string(fragment.text.clone()),
    }
}

pub(super) fn inline_fragment(item: &ClipboardItem, text: &str) -> Option<Block> {
    let payload: InlinePayload = serde_json::from_str(item.metadata()?).ok()?;
    let valid = |start, end| start <= end && end <= text.len()
        && text.is_char_boundary(start) && text.is_char_boundary(end);
    if payload.kind != INLINE_V1 || payload.text != text
        || !payload.spans.iter().all(|span| valid(span.start, span.end))
        || !payload.inline.iter().all(|tag| valid(tag.start, tag.end)) { return None; }
    let mut fragment = Block::new(0, Kind::Paragraph, payload.text);
    fragment.spans = payload.spans; fragment.meta.inline = payload.inline;
    fragment.normalize_spans();
    Some(fragment)
}

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
