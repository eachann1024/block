//! Block-level commands shared by the "+" menu, the slash menu, and code block actions.
use gpui::{Context, Pixels, Point, Window};
use super::{input::transactions::ListChange, model::{Block, Kind, Table},
    view::{Editor, selection::Position}};
use crate::icons::Icon;

#[path = "block_menu_cmd.rs"] mod cmd;
#[path = "block_menu_render.rs"] mod render;
pub(super) use cmd::{Cmd, LANGUAGES, language_label};
pub(super) use render::overlay;
use cmd::SLASH;

#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum Mode { Handle, Slash }
#[derive(Clone, Copy)]
pub(super) struct BlockMenu {
    pub block: u64, pub anchor: Point<Pixels>, pub mode: Mode, pub index: usize,
}
/// What type of block this is, used by the handle's type mark.
#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum BlockType { Text(Kind), Callout, Code, Table, Image, Divider, Source }
pub(super) fn block_type(block: &Block) -> BlockType {
    if block.meta.table.is_some() { BlockType::Table }
    else if block.meta.callout.is_some() { BlockType::Callout }
    else if block.meta.code_language.is_some() { BlockType::Code }
    else if is_divider(block) { BlockType::Divider }
    else if super::image_source::spec_of(block).is_some() { BlockType::Image }
    else if block.meta.raw_source.is_some() { BlockType::Source }
    else { BlockType::Text(block.kind) }
}
/// The line icon for a text block type, shared by menus and the handle's type mark.
pub(super) fn kind_icon(kind: Kind) -> Icon {
    match kind {
        Kind::Paragraph => Icon::Text, Kind::H1 => Icon::Heading1, Kind::H2 => Icon::Heading2, Kind::H3 => Icon::Heading3,
        Kind::Bullet => Icon::ListBullet, Kind::Numbered => Icon::ListNumbered, Kind::Todo => Icon::Todo, Kind::Quote => Icon::Quote,
    }
}

pub(super) fn is_divider(block: &Block) -> bool {
    block.meta.raw_source.as_deref().is_some_and(|s| matches!(s.trim(), "---" | "***" | "___"))
}
fn raw_block(text: &str) -> Block {
    let mut block = Block::new(0, Kind::Paragraph, text.to_owned()); block.meta.raw_source = Some(text.to_owned()); block
}
fn paragraph() -> Block { Block::new(0, Kind::Paragraph, String::new()) }

impl Editor {
    pub(super) fn open_block_menu(&mut self, id: u64, anchor: Point<Pixels>, mode: Mode, cx: &mut Context<Self>) {
        self.table_menu = None;
        self.block_menu = Some(BlockMenu { block: id, anchor, mode, index: 0 });
        cx.notify();
    }
    /// Entries currently shown; the slash menu filters by what follows the slash.
    pub(super) fn block_menu_entries(&self) -> Vec<Option<Cmd>> {
        let Some(menu) = self.block_menu else { return Vec::new() };
        match menu.mode {
            Mode::Slash => {
                let Some(block) = self.doc.get(menu.block) else { return Vec::new() };
                if !block.text.is_empty() && !block.text.starts_with('/') { return Vec::new(); }
                let query = block.text.trim_start_matches('/').to_lowercase();
                if query.is_empty() { return SLASH.to_vec(); }
                let mut hits: Vec<Cmd> = SLASH.into_iter().flatten()
                    .filter(|c| c.label().to_lowercase().contains(&query) || c.keywords().contains(&query)).collect();
                hits.sort_by_key(|c| !c.label().to_lowercase().starts_with(&query));
                hits.into_iter().map(Some).collect()
            }
            Mode::Handle => Vec::new(),
        }
    }
    fn selectable_menu_cmds(&self) -> Vec<Cmd> {
        self.block_menu_entries().into_iter().flatten().collect()
    }
    pub(super) fn block_menu_step(&mut self, down: bool, cx: &mut Context<Self>) -> bool {
        let Some(mut menu) = self.block_menu else { return false };
        let count = self.selectable_menu_cmds().len();
        if count == 0 { return false; }
        let cur = menu.index.min(count - 1);
        menu.index = if down { (cur + 1) % count } else { (cur + count - 1) % count };
        self.block_menu = Some(menu); cx.notify(); true
    }
    pub(super) fn block_menu_pick(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(menu) = self.block_menu else { return false };
        let items = self.selectable_menu_cmds();
        let Some(cmd) = items.get(menu.index.min(items.len().saturating_sub(1))).copied() else { return false };
        self.run_block_cmd(menu.block, menu.mode, cmd, window, cx); true
    }
    pub(super) fn run_block_cmd(&mut self, id: u64, mode: Mode, cmd: Cmd, window: &mut Window, cx: &mut Context<Self>) {
        let menu_anchor = self.block_menu.as_ref().map(|m| m.anchor);
        self.block_menu = None;
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
            Cmd::Columns => { self.insert_columns(index, slash_target, window, cx); return; }
            Cmd::Table | Cmd::Divider => {
                let col = block.meta.column;
                let mut new = if cmd == Cmd::Table { Block::from_table(0, Table::blank(3, 3)) } else { raw_block("***\n") };
                new.meta.column = col;
                let trailing = index + 1 >= self.doc.blocks.len() || cmd == Cmd::Divider
                    || self.doc.blocks.get(index + 1).is_some_and(|n| n.meta.column != col);
                let mut blocks = vec![new];
                if trailing { let mut p = paragraph(); p.meta.column = col; blocks.push(p); }
                let (start, remove) = if slash_target { (index, 1) } else { (index + 1, 0) };
                let count = blocks.len();
                self.doc.splice_blocks(start..start + remove, blocks);
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
        window.focus(&self.focus); cx.notify();
    }
}

pub(super) const SLASH_MENU_WIDTH: f32 = 224.;
