use gpui::ClipboardItem;
use serde::{Deserialize, Serialize};
use super::model::{Block, InlineTag, Kind, Span};
use super::view::Editor;

const BLOCKS_V1: &str = "com.block.editor.blocks.v1";
const INLINE_V1: &str = "com.block.editor.inline.v1";

#[derive(Deserialize, Serialize)]
struct InlinePayload {
    kind: String,
    #[serde(default)]
    clipboard: String,
    text: String,
    spans: Vec<Span>,
    inline: Vec<InlineTag>,
}

fn inline_markdown(fragment: &Block) -> String {
    if fragment.spans.is_empty() && fragment.meta.inline.is_empty() {
        return fragment.text.clone();
    }
    let mut parts = Vec::new();
    let mut start = 0;
    for (idx, ch) in fragment.text.char_indices() {
        if ch == '\n' {
            let mut line = fragment.slice(start..idx);
            line.kind = Kind::Paragraph;
            line.meta.indent = 0;
            let md = super::markdown::serialize(&super::model::Doc::from_blocks(vec![line]));
            parts.push(md.strip_suffix('\n').unwrap_or(&md).to_owned());
            start = idx + 1;
        }
    }
    let mut tail = fragment.slice(start..fragment.text.len());
    tail.kind = Kind::Paragraph;
    tail.meta.indent = 0;
    let md = super::markdown::serialize(&super::model::Doc::from_blocks(vec![tail]));
    parts.push(md.strip_suffix('\n').unwrap_or(&md).to_owned());
    parts.join("\n")
}

/// Text selections carry only inline styles; their heading/list/container is not selected.
pub(super) fn inline_item(fragment: &Block) -> ClipboardItem {
    let clipboard = inline_markdown(fragment);
    let payload = InlinePayload {
        kind: INLINE_V1.into(),
        clipboard: clipboard.clone(),
        text: fragment.text.clone(),
        spans: fragment.spans.clone(),
        inline: fragment.meta.inline.clone(),
    };
    match serde_json::to_string(&payload) {
        Ok(metadata) => ClipboardItem::new_string_with_metadata(clipboard, metadata),
        Err(_) => ClipboardItem::new_string(clipboard),
    }
}

pub(super) fn inline_fragment(item: &ClipboardItem, text: &str) -> Option<Block> {
    let payload: InlinePayload = serde_json::from_str(item.metadata()?).ok()?;
    let expected = if payload.clipboard.is_empty() { &payload.text } else { &payload.clipboard };
    let valid = |start, end| start <= end && end <= payload.text.len()
        && payload.text.is_char_boundary(start) && payload.text.is_char_boundary(end);
    if payload.kind != INLINE_V1 || expected != text
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

fn is_atomic_block(block: &Block) -> bool {
    super::image_source::spec_of(block).is_some() || super::block_menu::is_divider(block)
}

fn normalize_copied_blocks(blocks: &mut [Block]) {
    let mut ancestors: Vec<(usize, usize)> = Vec::new();
    for block in blocks {
        let is_list = matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo)
            && block.meta.table.is_none()
            && !is_atomic_block(block)
            && block.meta.code_language.is_none();
        if let Some(code) = &mut block.meta.list_code {
            while ancestors.last().is_some_and(|&(orig, _)| orig > code.parent_indent) {
                ancestors.pop();
            }
            if let Some(&(orig_parent, new_parent)) = ancestors.last()
                && orig_parent == code.parent_indent
            {
                let delta = orig_parent - new_parent;
                code.parent_indent = new_parent;
                block.meta.indent = block.meta.indent.saturating_sub(delta);
            } else {
                block.meta.list_code = None;
                block.meta.indent = 0;
            }
            continue;
        }
        let orig = block.meta.indent;
        while ancestors.last().is_some_and(|&(p_orig, _)| p_orig >= orig) {
            ancestors.pop();
        }
        if let Some(&(p_orig, p_new)) = ancestors.last() {
            block.meta.indent = orig.saturating_sub(p_orig - p_new);
        } else if orig > 0
            && (is_list
                || block.kind != Kind::Paragraph
                || block.meta.code_language.is_some()
                || block.meta.table.is_some()
                || is_atomic_block(block)
                || block.meta.callout.is_some())
        {
            block.meta.indent = 0;
        }
        if is_list {
            ancestors.push((orig, block.meta.indent));
        }
    }
}

pub(super) fn blocks_item(mut blocks: Vec<Block>) -> ClipboardItem {
    normalize_copied_blocks(&mut blocks);
    let markdown = super::markdown::serialize(&super::model::Doc::from_blocks(blocks));
    let text = markdown.strip_suffix('\n').unwrap_or(&markdown).to_owned();
    block_item(text, &markdown)
}

impl Editor {
    pub(super) fn clipboard_block_slice(&self, index: usize, range: std::ops::Range<usize>) -> Block {
        let block = &self.doc.blocks[index];
        let mut part = if range == (0..block.text.len()) {
            block.clone()
        } else if is_atomic_block(block) || block.meta.table.is_some() {
            block.text_slice(range).unwrap_or_else(|| block.clone())
        } else {
            block.slice(range)
        };
        if let Some(number) = self.doc.list_number(block.id) {
            part.meta.list_start = Some(number);
        }
        part
    }

    pub(super) fn selection_clipboard_item(&self) -> Option<ClipboardItem> {
        let (a, b, first, last) = self.ordered();
        if a == b { return None; }
        let block = &self.doc.blocks[first];
        if first == last {
            let whole = a.offset == 0 && b.offset == block.text.len();
            let whole_table = self.table_sel == Some((block.id, super::table_actions::TableSel::Whole));
            let cell_text = block.meta.table.as_ref().is_some_and(|table|
                !whole_table && table.cell_at_range(&(a.offset..b.offset)).is_some());
            let multi_cell = block.meta.table.is_some() && !cell_text && !whole && !whole_table;
            let atomic = block.meta.table.is_some() || is_atomic_block(block);
            if multi_cell {
                let fragment = block.text_slice(a.offset..b.offset)?;
                return Some(ClipboardItem::new_string(inline_markdown(&fragment)));
            }
            if cell_text || !whole || !atomic {
                return Some(inline_item(&block.text_slice(a.offset..b.offset)?));
            }
        }
        if first + 1 == last && a.offset == block.text.len() && !block.text.is_empty() && b.offset == 0 {
            return Some(ClipboardItem::new_string("\n".into()));
        }
        let blocks: Vec<Block> = (first..=last).filter_map(|i| {
            // Unselected boundary endpoints do not carry their neighbour block's container.
            if i == last && first != last && b.offset == 0 { return None; }
            let cur = &self.doc.blocks[i];
            if i == first && first != last && a.offset == cur.text.len() && !cur.text.is_empty() {
                return None;
            }
            self.selected_range(cur.id).map(|range| self.clipboard_block_slice(i, range))
        }).collect();
        (!blocks.is_empty()).then(|| blocks_item(blocks))
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
    if matches!(action, Paste) && cx.read_from_clipboard().is_some_and(|item| super::clipboard_files::string_text(&item).is_none()) {
        return true;
    }
    let a: Option<Box<dyn gpui::Action>> = match action {
        Paste => Some(Box::new(gpui_component::input::Paste)), Copy => Some(Box::new(gpui_component::input::Copy)),
        Cut => Some(Box::new(gpui_component::input::Cut)), SelectAll => Some(Box::new(gpui_component::input::SelectAll)),
        Undo => Some(Box::new(gpui_component::input::Undo)), Redo => Some(Box::new(gpui_component::input::Redo)),
        _ => None,
    };
    let Some(a) = a else { return false };
    window.dispatch_action(a, cx); true
}
