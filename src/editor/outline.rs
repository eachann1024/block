use gpui::{AnyElement, Context, Pixels, Window, px};
use super::{surface::Surface, view::Editor};
use crate::ui::outline_rail::OutlineEntry;

#[derive(Default)]
pub(crate) struct OutlineState {
    pub hovered: bool,
    /// Clicking a heading closes the card while the pointer is still inside it.
    /// Ignore hover-open until the pointer actually leaves.
    pub pinned_closed: bool,
    pub force_narrow: bool,
}

pub(crate) const MIN_LEFT_MARGIN: f32 = 36.0;

pub(crate) fn left_margin(editor: &Editor, window: &Window) -> Pixels {
    if editor.outline.force_narrow { return px(0.); }
    let editor_w = if editor.annotate.editor_width > px(0.) {
        editor.annotate.editor_width
    } else if editor.list.viewport_bounds().size.width > px(0.) {
        editor.list.viewport_bounds().size.width
    } else {
        window.viewport_size().width
    };
    let std_w = super::chrome::standard_width(crate::theme::Theme::ocean().content_width);
    let rail_slot = if editor.annotate.has_visible_rail() && super::annotate_ui::can_fit_wide_rail(editor, std_w) {
        px(super::annotate_ui::RAIL_SLOT)
    } else { px(0.) };
    let inner_w = (editor_w - px(crate::ui::scrollbar::GUTTER)).max(px(0.));
    let col_w = inner_w.min(std_w + rail_slot);
    ((inner_w - col_w) * 0.5).max(px(0.))
}

pub(crate) fn can_fit(editor: &Editor, window: &Window) -> bool {
    !editor.minimal && editor.path.is_some() && matches!(editor.surface, Surface::Note)
        && left_margin(editor, window) >= px(MIN_LEFT_MARGIN)
}

pub(crate) fn entries(editor: &Editor) -> Vec<OutlineEntry> {
    let headings: Vec<_> = editor.doc.blocks.iter().enumerate().filter_map(|(idx, block)| {
        super::fold::heading_level(block).filter(|_| !block.text.trim().is_empty())
            .map(|level| (idx, block, level))
    }).collect();
    if headings.is_empty() { return Vec::new(); }
    let anchor_idx = editor.doc.index_of(editor.caret.head.block).unwrap_or(editor.scroll_top.0);
    let active_pos = headings.iter().rposition(|(idx, _, _)| *idx <= anchor_idx).unwrap_or(0);
    let (active_block_idx, _, _) = headings[active_pos];
    let section_end = super::fold::fold_end(&editor.doc, active_block_idx);
    headings.iter().enumerate().map(|(pos, &(block_idx, block, level))| OutlineEntry {
        block_id: block.id, level, title: block.text.trim().to_owned(), active: pos == active_pos,
        bar_lit: pos == active_pos || (block_idx > active_block_idx && block_idx < section_end),
    }).collect()
}

pub(super) fn select_heading(editor: &mut Editor, block_id: u64, window: &mut Window, cx: &mut Context<Editor>) {
    editor.ensure_block_visible(block_id);
    if let Some(index) = editor.doc.index_of(block_id) {
        crate::ui::scrollbar::reveal_item(&editor.list, index, px(36.));
    }
    editor.focus_block(block_id, window, cx);
    editor.outline.hovered = false;
    editor.outline.pinned_closed = true;
    cx.notify();
}

pub(super) fn overlay(editor: &mut Editor, window: &Window, cx: &mut Context<Editor>) -> Option<AnyElement> {
    if !can_fit(editor, window) || entries(editor).is_empty() {
        editor.outline.hovered = false;
        editor.outline.pinned_closed = false;
        crate::ui::motion::popup_reset("outline-popover");
        return None;
    }
    let items = entries(editor);
    Some(crate::ui::outline_rail::outline_rail(items, editor.outline.hovered, |this: &mut Editor, hovered, _, cx| {
        if this.outline.pinned_closed {
            if !hovered { this.outline.pinned_closed = false; }
            return;
        }
        if this.outline.hovered != hovered { this.outline.hovered = hovered; cx.notify(); }
    }, select_heading, cx))
}
