//! Block-level commands shared by the "+" menu, the slash menu, the block handle menu and the top toolbar.
use gpui::{Context, Pixels, Point, Size, Window, point, px};
use super::{input::transactions::ListChange, model::{Block, Kind, Table},
    image_source::Align, table_actions::{TableCmd, TableSel}, view::{Editor, selection::Position}};
use crate::icons::Icon;

#[path = "block_menu_cmd.rs"] mod cmd;
#[path = "block_menu_render.rs"] mod render;
pub(super) use cmd::{Cmd, LANGUAGES, language_label, sub_entries};
pub(super) use render::overlay;
use cmd::SLASH;

#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum Mode { Handle, Slash }
#[derive(Clone, Copy)]
pub(super) struct BlockMenu {
    pub block: u64, pub anchor: Point<Pixels>, pub mode: Mode, pub index: usize,
    pub sub: Sub, pub sub_index: usize, pub kind: BlockType, pub heading_level: Option<u8>,
    pub lang: &'static str, pub viewport: Size<Pixels>,
}
/// Which cascading side submenu of the handle menu is open.
#[derive(Clone, Copy, PartialEq, Eq, Default)]
pub(super) enum Sub { #[default] Root, Convert, Language }
/// What a handle menu is about, decided from the block itself.
#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum BlockType { Text(Kind), Callout, Columns, Code, Table, Image, Divider, Source }
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
        // Handle menus open beside the handle (6px gap, top aligned), on its left when there is room, as in the reference.
        let anchor = match (mode, super::chrome::grip_bounds(id)) {
            (Mode::Handle, Some(grip)) => {
                let left = grip.left() - px(MENU_GAP + HANDLE_MENU_WIDTH);
                point(if left >= px(24.) { left } else { grip.right() + px(MENU_GAP) }, grip.top())
            }
            _ => anchor,
        };
        let (kind, heading_level, lang) = if super::columns::group_from_handle(id).is_some() {
            (BlockType::Columns, None, "")
        } else if let Some(b) = self.doc.get(id) {
            (block_type(b), b.meta.heading_level, b.meta.code_language.as_deref().map(language_label).unwrap_or(""))
        } else { (BlockType::Text(Kind::Paragraph), None, "") };
        self.block_menu = Some(BlockMenu { block: id, anchor, mode, index: 0, sub: Sub::Root, sub_index: 0, kind, heading_level, lang, viewport: Size::default() });
        cx.notify();
    }
    pub(super) fn open_submenu(&mut self, index: usize, sub: Sub, window: &Window, cx: &mut Context<Self>) {
        if let Some(menu) = self.block_menu.as_mut() {
            let viewport = window.viewport_size();
            if menu.sub != sub || menu.index != index || menu.viewport != viewport {
                menu.index = index;
                if menu.sub != sub { menu.sub = sub; menu.sub_index = 0; }
                menu.viewport = viewport;
                cx.notify();
            }
        }
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
            Mode::Handle => super::columns::group_from_handle(menu.block).map_or_else(
                || self.doc.get(menu.block).map_or_else(Vec::new, |b| self.handle_entries(b)),
                |gid| self.columns_group_entries(gid),
            ),
        }
    }
    /// Handle menu for one block: its own tools first, then the shared block actions, delete last.
    fn handle_entries(&self, block: &Block) -> Vec<Option<Cmd>> {
        let kind = block_type(block);
        let mut items: Vec<Option<Cmd>> = Vec::new();
        match kind {
            BlockType::Code => items.extend([Some(Cmd::Open(Sub::Language)), Some(Cmd::CopyCode),
                Some(Cmd::ToggleWrap(!self.nowrap.contains(&block.id))), Some(Cmd::Open(Sub::Convert)), None]),
            BlockType::Table => items.extend([Some(Cmd::TableOp(TableCmd::SelectTable)), Some(Cmd::TableOp(TableCmd::CopyTable)), None,
                Some(Cmd::TableOp(TableCmd::AppendRow)), Some(Cmd::TableOp(TableCmd::AppendColumn)), None]),
            BlockType::Image => {
                let align = super::image_source::spec_of(block).map(|s| s.align).unwrap_or_default();
                items.extend([Some(Cmd::CopyImage), Some(Cmd::RevealImage), None]);
                items.extend([Align::Left, Align::Center, Align::Right].map(|a| Some(Cmd::AlignImage(a, a == align))));
                items.push(None);
            }
            BlockType::Text(_) | BlockType::Callout => items.extend([Some(Cmd::Open(Sub::Convert)), Some(Cmd::CopyText), None]),
            BlockType::Divider | BlockType::Source | BlockType::Columns => {}
        }
        items.extend([Cmd::InsertAbove, Cmd::InsertBelow, Cmd::Duplicate, Cmd::Cut].map(Some));
        let index = self.doc.index_of(block.id).unwrap_or(0);
        if index > 0 && self.doc.blocks[index - 1].meta.column == block.meta.column { items.push(Some(Cmd::MoveUp)); }
        if index + 1 < self.doc.blocks.len() && self.doc.blocks[index + 1].meta.column == block.meta.column { items.push(Some(Cmd::MoveDown)); }
        items.extend([None, Some(if kind == BlockType::Table { Cmd::TableOp(TableCmd::DeleteTable) } else { Cmd::Delete })]);
        items
    }
    fn selectable_menu_cmds(&self) -> Vec<Cmd> {
        self.block_menu_entries().into_iter().flatten().filter(|c| !matches!(c, Cmd::Header(_))).collect()
    }
    pub(super) fn block_menu_step(&mut self, down: bool, cx: &mut Context<Self>) -> bool {
        let Some(mut menu) = self.block_menu else { return false };
        let in_sub = menu.mode == Mode::Handle && menu.sub != Sub::Root;
        let count = if in_sub { sub_entries(&menu).len() } else { self.selectable_menu_cmds().len() };
        if count == 0 { return false; }
        let slot = if in_sub { &mut menu.sub_index } else { &mut menu.index };
        let cur = (*slot).min(count - 1);
        *slot = if down { (cur + 1) % count } else { (cur + count - 1) % count };
        self.block_menu = Some(menu); cx.notify(); true
    }
    pub(super) fn block_menu_pick(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(menu) = self.block_menu else { return false };
        let (items, idx) = if menu.mode == Mode::Handle && menu.sub != Sub::Root {
            (sub_entries(&menu), menu.sub_index)
        } else {
            (self.selectable_menu_cmds(), menu.index)
        };
        let Some(cmd) = items.get(idx.min(items.len().saturating_sub(1))).copied() else { return false };
        self.run_block_cmd(menu.block, menu.mode, cmd, window, cx); true
    }
    pub(super) fn block_menu_nav(&mut self, right: bool, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(menu) = self.block_menu.filter(|m| m.mode == Mode::Handle) else { return false };
        if right {
            let cmds = self.selectable_menu_cmds();
            if let Some(cmd @ Cmd::Open(_)) = cmds.get(menu.index.min(cmds.len().saturating_sub(1))).copied() {
                self.run_block_cmd(menu.block, menu.mode, cmd, window, cx);
            }
        } else if menu.sub != Sub::Root {
            if let Some(m) = self.block_menu.as_mut() { m.sub = Sub::Root; m.sub_index = 0; }
            cx.notify();
        } else {
            self.block_menu = None; cx.notify();
        }
        true
    }
    /// Toolbar insertion: use the caret block when it is an empty paragraph, otherwise add below it.
    pub(super) fn insert_here(&mut self, cmd: Cmd, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.run_block_cmd(self.caret.head.block, Mode::Slash, cmd, window, cx);
    }
    pub(super) fn run_block_cmd(&mut self, id: u64, mode: Mode, cmd: Cmd, window: &mut Window, cx: &mut Context<Self>) {
        match cmd {
            Cmd::Header(_) => return,
            Cmd::Open(sub) => {
                if let Some(m) = self.block_menu.as_mut() { m.sub = sub; m.sub_index = 0; m.viewport = window.viewport_size(); }
                cx.notify(); return;
            }
            _ => {}
        }
        let menu_anchor = self.block_menu.as_ref().map(|m| m.anchor);
        self.block_menu = None;
        if let Some(gid) = super::columns::group_from_handle(id) {
            self.run_columns_group_cmd(gid, cmd, window, cx); return;
        }
        match cmd {
            Cmd::Ai => { self.open_ai_block(id, mode == Mode::Slash, menu_anchor, window, cx); return; }
            Cmd::TableOp(op) => { self.table_command(id, TableSel::Whole, op, cx); window.focus(&self.focus); cx.notify(); return; }
            Cmd::ToggleWrap(on) => {
                if on { self.nowrap.insert(id); } else { self.nowrap.remove(&id); }
                self.layouts.clear(); window.focus(&self.focus); cx.notify(); return;
            }
            Cmd::CopyImage => { self.copy_image(id, cx); return; }
            Cmd::RevealImage => { self.reveal_image(id, cx); return; }
            Cmd::AlignImage(align, _) => { self.edit_image(id, |s| s.align = align, cx); window.focus(&self.focus); return; }
            Cmd::TurnTo(_, true) | Cmd::TurnHeadingTo(_, true) | Cmd::CalloutTo(true) | Cmd::CodeTo(true) | Cmd::SetLanguage(_, true) => { cx.notify(); return; }
            Cmd::CopyCode => {
                let text = self.doc.get(id).map(|b| b.text.clone()).unwrap_or_default();
                cx.write_to_clipboard(gpui::ClipboardItem::new_string(text)); self.status = "已复制代码".into(); cx.notify(); return;
            }
            _ => {}
        }
        let cmd = match cmd { Cmd::TurnTo(kind, _) => Cmd::Turn(kind), Cmd::TurnHeadingTo(lvl, _) => Cmd::TurnHeading(lvl),
            Cmd::CalloutTo(_) => Cmd::Callout, Cmd::CodeTo(_) => Cmd::Code(""), Cmd::SetLanguage(tag, _) => Cmd::Code(tag), other => other };
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
            Cmd::TurnHeading(_) | Cmd::Callout => {
                if slash_target { self.doc.set_text(id, String::new()); }
                self.doc.set_kind(id, if cmd == Cmd::Callout { Kind::Quote } else { Kind::H3 });
                if let Some(b) = self.doc.get_mut(id) {
                    if let Cmd::TurnHeading(lvl) = cmd { b.meta.heading_level = Some(lvl); }
                    else { b.meta.callout = Some(super::model::CalloutKind::Note); }
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
            Cmd::Ai => return,
            Cmd::InsertAbove | Cmd::InsertBelow => {
                let at = if cmd == Cmd::InsertAbove { index } else { index + 1 };
                let mut p = paragraph(); p.meta.column = block.meta.column;
                self.doc.splice_blocks(at..at, vec![p]);
                changes.push(ListChange { start: at, before: 0, after: 1 });
                caret = Some(Position { block: self.doc.blocks[at].id, offset: 0 });
            }
            Cmd::Duplicate => {
                let hi_ext = self.list_subtree_end(index);
                let copies: Vec<Block> = (index..=hi_ext).map(|i| { let mut b = self.doc.blocks[i].clone(); b.id = 0; b }).collect();
                let count = copies.len();
                self.doc.splice_blocks(hi_ext + 1..hi_ext + 1, copies);
                changes.push(ListChange { start: hi_ext + 1, before: 0, after: count });
            }
            Cmd::CopyText => { cx.write_to_clipboard(gpui::ClipboardItem::new_string(block.text.clone())); return; }
            Cmd::MoveUp | Cmd::MoveDown => {
                self.move_block_range(index, index, cmd == Cmd::MoveUp, cx);
                window.focus(&self.focus); cx.notify(); return;
            }
            Cmd::Cut | Cmd::Delete => {
                if cmd == Cmd::Cut {
                    let markdown = super::markdown::serialize(&super::model::Doc::from_blocks(vec![block.clone()]));
                    let plain = markdown.trim_end().to_owned();
                    cx.write_to_clipboard(gpui::ClipboardItem::new_string(plain.clone()));
                    self.block_clip = Some((plain, markdown));
                }
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
pub(super) const HANDLE_MENU_WIDTH: f32 = 196.;
pub(super) const SUBMENU_WIDTH: f32 = 176.;
const MENU_GAP: f32 = 6.;
