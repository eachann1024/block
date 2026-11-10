//! Block-level commands shared by the "+" menu, the slash menu, and code block actions.
use std::{cell::RefCell, collections::HashMap};
use gpui::{Bounds, Context, EntityId, Pixels, Point, Window, point, px, size};
use super::{input::transactions::ListChange, model::{Block, Kind, Table},
    view::{Editor, selection::Position}};
use crate::icons::Icon;
use crate::ui::menu_nav::{self, Slot};

#[path = "block_menu_cmd.rs"] mod cmd;
#[path = "block_menu_render.rs"] mod render;
pub(super) use cmd::{Cmd, LANGUAGES, language_icon, language_label};
pub(crate) use cmd::canonical_lang_tag;
pub(super) use render::overlay;
use cmd::{SLASH, SLASH_MINIMAL};

thread_local! {
    static LIVE_BLOCK_BOUNDS: RefCell<HashMap<(EntityId, u64), Bounds<Pixels>>> = Default::default();
}

pub(super) fn record_live_block_bounds(editor: EntityId, block: u64, bounds: Bounds<Pixels>) {
    LIVE_BLOCK_BOUNDS.with(|m| { m.borrow_mut().insert((editor, block), bounds); });
}

pub(super) fn live_block_bounds(editor: EntityId, block: u64) -> Option<Bounds<Pixels>> {
    LIVE_BLOCK_BOUNDS.with(|m| m.borrow().get(&(editor, block)).copied())
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum Mode { Handle, Slash }
#[derive(Clone, Copy)]
pub(super) struct BlockMenu {
    pub block: u64, pub anchor: Point<Pixels>, pub trigger: Bounds<Pixels>, pub mode: Mode, pub index: usize,
}
/// The line icon for a text block type, shared by slash/block menus.
pub(super) fn kind_icon(kind: Kind) -> Icon {
    match kind {
        Kind::Paragraph => Icon::Text, Kind::H1 => Icon::Heading1, Kind::H2 => Icon::Heading2, Kind::H3 => Icon::Heading3,
        Kind::Bullet => Icon::ListBullet, Kind::Numbered => Icon::ListNumbered, Kind::Todo => Icon::Todo, Kind::Quote => Icon::Quote,
    }
}

pub(super) fn is_divider(block: &Block) -> bool {
    block.meta.raw_source.as_deref().is_some_and(|s| {
        let t = s.trim();
        (t.len() >= 3 && ['-', '*', '_'].iter().any(|c| t.chars().all(|v| v == *c)))
            || t.eq_ignore_ascii_case("<hr>")
            || t.eq_ignore_ascii_case("<hr/>")
            || t.eq_ignore_ascii_case("<hr />")
    })
}
fn raw_block(text: &str) -> Block {
    let mut block = Block::new(0, Kind::Paragraph, text.to_owned()); block.meta.raw_source = Some(text.to_owned()); block
}
fn paragraph() -> Block { Block::new(0, Kind::Paragraph, String::new()) }

impl Editor {
    pub(crate) fn block_trigger_bounds(&self, editor_id: EntityId, id: u64) -> Option<Bounds<Pixels>> {
        self.layouts
            .get(&id)
            .map(|l| l.bounds)
            .or_else(|| live_block_bounds(editor_id, id))
            .or_else(|| self.doc.index_of(id).and_then(|ix| self.list.bounds_for_item(ix)))
    }
    pub(super) fn open_block_menu_at(&mut self, id: u64, trigger: Bounds<Pixels>, mode: Mode, cx: &mut Context<Self>) {
        self.table_menu = None;
        self.block_menu_scroll.set_offset(Point::default());
        let anchor = point(trigger.left(), trigger.bottom() + px(4.));
        self.block_menu = Some(BlockMenu { block: id, anchor, trigger, mode, index: 0 });
        cx.notify();
    }
    #[allow(dead_code)]
    pub(super) fn open_block_menu(&mut self, id: u64, anchor: Point<Pixels>, mode: Mode, cx: &mut Context<Self>) {
        let trigger = self
            .block_trigger_bounds(cx.entity_id(), id)
            .unwrap_or_else(|| Bounds::new(point(anchor.x, (anchor.y - px(28.)).max(px(0.))), size(px(SLASH_MENU_WIDTH), px(24.))));
        self.open_block_menu_at(id, trigger, mode, cx);
    }
    pub(super) fn block_menu_entries_for(&self, menu: BlockMenu) -> Vec<Option<Cmd>> {
        match menu.mode {
            Mode::Slash => {
                let Some(block) = self.doc.get(menu.block) else { return Vec::new() };
                slash_entries_for_text(&block.text, self.minimal)
            }
            Mode::Handle => Vec::new(),
        }
    }
    fn entry_slots(entries: &[Option<Cmd>]) -> Vec<Slot> {
        entries.iter().map(|entry| if entry.is_some() { Slot::Item } else { Slot::Skip }).collect()
    }
    pub(super) fn block_menu_step(&mut self, down: bool, cx: &mut Context<Self>) -> bool {
        let Some(mut menu) = self.block_menu else { return false };
        let entries = self.block_menu_entries_for(menu);
        let slots = Self::entry_slots(&entries);
        if !menu_nav::has_item(&slots) { return false; }
        let next = menu_nav::step(menu.index, &slots, down);
        let changed = next != menu_nav::clamp_index(menu.index, &slots);
        menu.index = next;
        self.block_menu = Some(menu);
        if changed { menu_nav::reveal_or_defer(&self.block_menu_scroll, next, entries.len()); }
        cx.notify();
        true
    }
    pub(super) fn block_menu_pick(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(menu) = self.block_menu else { return false };
        let entries = self.block_menu_entries_for(menu);
        let slots = Self::entry_slots(&entries);
        if !menu_nav::has_item(&slots) { return false; }
        let index = menu_nav::clamp_index(menu.index, &slots);
        let Some(Some(cmd)) = entries.get(index).copied() else { return false };
        self.run_block_cmd(menu.block, menu.mode, cmd, window, cx); true
    }
    /// Keeps the slash highlight on a real row after the query changes, and
    /// recenters that row once the filtered list has been laid out.
    pub(super) fn sync_block_menu_after_edit(&mut self, cx: &mut Context<Self>) {
        let Some(menu) = self.block_menu else { return };
        if menu.mode != Mode::Slash { return; }
        let valid = self.caret.head.block == menu.block
            && self.doc.get(menu.block).is_some_and(|b| b.text.starts_with('/'));
        if !valid {
            self.block_menu = None;
            cx.notify();
            return;
        }
        let entries = self.block_menu_entries_for(menu);
        let slots = Self::entry_slots(&entries);
        if !menu_nav::has_item(&slots) {
            self.block_menu = None;
            cx.notify();
            return;
        }
        let next = menu_nav::clamp_index(menu.index, &slots);
        if let Some(menu) = &mut self.block_menu { menu.index = next; }
        menu_nav::reveal_or_defer(&self.block_menu_scroll, next, entries.len());
        cx.notify();
    }
    pub(super) fn run_block_cmd(&mut self, id: u64, mode: Mode, cmd: Cmd, window: &mut Window, cx: &mut Context<Self>) {
        let menu_anchor = self.block_menu.as_ref().map(|m| m.anchor);
        self.block_menu = None;
        if self.minimal && matches!(cmd, Cmd::Ai | Cmd::Callout | Cmd::Code("mermaid" | "math")) {
            cx.notify();
            return;
        }
        match cmd {
            Cmd::Ai => { self.open_ai_block(id, mode == Mode::Slash, menu_anchor, window, cx); return; }
            Cmd::SetLanguage(_, true) => { cx.notify(); return; }
            _ => {}
        }
        let cmd = match cmd { Cmd::SetLanguage(tag, _) => Cmd::Code(tag), other => other };
        self.commit_composition(cx);
        let Some(index) = self.doc.index_of(id) else { return };
        let Some(block) = self.doc.get(id) else { return };
        let slash_target = mode == Mode::Slash && block.meta.table.is_none() && block.meta.raw_source.is_none()
            && block.meta.code_language.is_none() && (block.text.is_empty() || block.text.starts_with('/'))
            && block.kind == Kind::Paragraph;
        let before = self.snapshot();
        let mut changes: Vec<ListChange> = Vec::new();
        let mut caret: Option<Position> = None;
        match cmd {
            Cmd::Turn(kind) => {
                if slash_target { self.doc.set_text(id, String::new()); }
                self.doc.set_kind(id, kind);
                changes.push(ListChange { start: index, before: 1, after: 1 });
                caret = Some(Position { block: id, offset: self.doc.get(id).map_or(0, |b| b.text.len()) });
            }
            Cmd::Callout => {
                if slash_target { self.doc.set_text(id, String::new()); }
                self.doc.set_kind(id, Kind::Quote);
                if let Some(b) = self.doc.get_mut(id) {
                    b.meta.callout = Some(super::model::CalloutKind::Note);
                }
                self.doc.refresh_block(id);
                changes.push(ListChange { start: index, before: 1, after: 1 });
                caret = Some(Position { block: id, offset: self.doc.get(id).map_or(0, |b| b.text.len()) });
            }
            Cmd::Code(language) => {
                // F6: switching an existing code block's language changes only the fence tag; text and caret stay.
                let was_code = self.doc.get(id).is_some_and(|b| b.meta.code_language.is_some());
                if slash_target { self.doc.set_text(id, String::new()); self.doc.set_kind(id, Kind::Paragraph); }
                else if mode == Mode::Handle && self.doc.get(id).is_some_and(|b| b.meta.code_language.is_none()) { self.doc.set_kind(id, Kind::Paragraph); }
                if let Some(b) = self.doc.get_mut(id) { if b.meta.table.is_none() && b.meta.raw_source.is_none() { b.meta.code_language = Some(language.to_owned()); } }
                self.doc.refresh_block(id);
                changes.push(ListChange { start: index, before: 1, after: 1 });
                if !was_code { caret = Some(Position { block: id, offset: 0 }); }
            }
            Cmd::Table | Cmd::Divider => {
                let new = if cmd == Cmd::Table { Block::from_table(0, Table::blank(3, 3)) } else { raw_block("***\n") };
                let trailing = index + 1 >= self.doc.blocks.len() || cmd == Cmd::Divider;
                let mut blocks = vec![new];
                if trailing { blocks.push(paragraph()); }
                let (start, remove) = if slash_target { (index, 1) } else { (index + 1, 0) };
                let count = blocks.len();
                self.doc.splice_blocks(start..start + remove, blocks);
                if cmd == Cmd::Table { super::table_layout::toggle_fit(cx.entity_id(), self.doc.blocks[start].id); }
                changes.push(ListChange { start, before: remove, after: count });
                let focus = if cmd != Cmd::Divider { self.doc.blocks[start].id } else { self.doc.blocks[(start + 1).min(self.doc.blocks.len() - 1)].id };
                caret = Some(Position { block: focus, offset: 0 });
            }
            Cmd::Image => { self.pick_image(id, slash_target, window, cx); return; }
            Cmd::Delete => {
                self.delete_single_block(id, index, &mut changes, &mut caret);
            }
            _ => return,
        }
        if let Some(at) = caret { self.caret.anchor = at; self.caret.head = at; }
        self.record_changes(before, changes, true, cx);
        self.layouts.clear();
        window.focus(&self.focus, cx); cx.notify();
    }

    pub(crate) fn set_callout_kind(&mut self, id: u64, kind: Option<super::model::CalloutKind>, cx: &mut Context<Self>) {
        self.commit_composition(cx);
        let Some(index) = self.doc.index_of(id) else { return };
        let Some(block) = self.doc.get(id) else { return };
        if block.meta.callout == kind && (kind.is_some() || block.kind == Kind::Paragraph) { return; }
        let before = self.snapshot();
        if let Some(k) = kind {
            self.doc.set_kind(id, Kind::Quote);
            if let Some(b) = self.doc.get_mut(id) { b.meta.callout = Some(k); }
            self.doc.refresh_block(id);
        } else {
            self.doc.set_kind(id, Kind::Paragraph);
        }
        self.record_changes(before, vec![ListChange { start: index, before: 1, after: 1 }], true, cx);
        self.layouts.clear();
        cx.notify();
    }

    pub(in crate::editor) fn delete_single_block(&mut self, id: u64, index: usize, changes: &mut Vec<ListChange>, caret: &mut Option<Position>) {
        let only = self.doc.blocks.len() == 1;
        self.doc.remove(id);
        changes.push(ListChange { start: if only { 0 } else { index }, before: 1, after: usize::from(only) });
        let at = index.min(self.doc.blocks.len() - 1);
        *caret = Some(Position { block: self.doc.blocks[at].id, offset: 0 });
    }
}

fn slash_entries_for_text(text: &str, minimal: bool) -> Vec<Option<Cmd>> {
    if !text.is_empty() && !text.starts_with('/') { return Vec::new(); }
    let slash: &[Option<Cmd>] = if minimal { &SLASH_MINIMAL } else { &SLASH };
    let Some(raw) = text.strip_prefix('/') else { return slash.to_vec() };
    let query = raw.to_lowercase();
    if query.is_empty() { return slash.to_vec(); }
    let mut hits: Vec<Cmd> = slash.iter().copied().flatten()
        .filter(|c| c.label().to_lowercase().contains(&query) || c.keywords().contains(&query)).collect();
    hits.sort_by_key(|c| !c.label().to_lowercase().starts_with(&query));
    hits.into_iter().map(Some).collect()
}

pub(super) const SLASH_MENU_WIDTH: f32 = 224.;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn repeated_slashes_do_not_keep_the_full_slash_menu_open() {
        assert!(!slash_entries_for_text("", false).is_empty());
        assert!(!slash_entries_for_text("/", false).is_empty());
        assert!(!slash_entries_for_text("/h1", false).is_empty());
        assert!(slash_entries_for_text("//", false).is_empty());
        assert!(slash_entries_for_text("////", false).is_empty());
        assert!(slash_entries_for_text("plain", false).is_empty());
    }
}
