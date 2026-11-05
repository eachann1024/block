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
