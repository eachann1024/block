use gpui::ClipboardItem;
use serde::{Deserialize, Serialize};
use super::model::{Block, InlineTag, Kind, Span};
use super::view::Editor;

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

pub(super) fn blocks_item(blocks: Vec<Block>) -> ClipboardItem {
    let text = blocks.iter().map(|block| block.text.as_str()).collect::<Vec<_>>().join("\n");
    let markdown = super::markdown::serialize(&super::model::Doc::from_blocks(blocks));
    block_item(text, &markdown)
}

impl Editor {
    pub(super) fn selection_clipboard_item(&self) -> Option<ClipboardItem> {
        let (a, b, first, last) = self.ordered();
        if a == b { return None; }
        let block = &self.doc.blocks[first];
        if first == last {
            let whole = a.offset == 0 && b.offset == block.text.len();
            let cell_text = block.meta.table.as_ref().is_some_and(|table|
                self.table_sel != Some((block.id, super::table_actions::TableSel::Whole))
                    && table.cell_at_range(&(a.offset..b.offset)).is_some());
            let atomic = block.meta.table.is_some() || super::image_source::spec_of(block).is_some()
                || super::block_menu::is_divider(block);
            if cell_text || !whole || !atomic {
                return Some(inline_item(&block.text_slice(a.offset..b.offset)?));
            }
        }
        let blocks = (first..=last).filter_map(|i| {
            // The next block's type is not selected when the range ends at its first byte.
            if i == last && first != last && b.offset == 0 { return None; }
            let block = &self.doc.blocks[i];
            self.selected_range(block.id).map(|range| {
                if range == (0..block.text.len()) { block.clone() }
                else if block.meta.raw_source.is_some() { block.text_slice(range).unwrap_or_else(|| block.clone()) }
                else { block.slice(range) }
            })
        }).collect();
        Some(blocks_item(blocks))
    }

    /// Copy controls copy a block's text, not its surrounding code/callout container.
    pub(crate) fn copy_block_text(&self, id: u64, cx: &mut gpui::Context<Self>) {
        if let Some(block) = self.doc.get(id)
            && let Some(fragment) = block.text_slice(0..block.text.len()) {
            cx.write_to_clipboard(inline_item(&fragment));
        }
    }
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
