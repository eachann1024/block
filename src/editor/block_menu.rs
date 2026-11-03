//! Block-level commands shared by the "+" menu, the slash menu, the block handle menu and the top toolbar.
use gpui::{Context, Pixels, Point, Window, point, px};
use super::{input::transactions::ListChange, model::{Block, Kind, Table},
    image_source::Align, table_actions::{TableCmd, TableSel}, view::{Editor, selection::Position}};
use crate::icons::Icon;

#[path = "block_menu_cmd.rs"] mod cmd;
#[path = "block_menu_render.rs"] mod render;
pub(super) use cmd::Cmd;
pub(super) use render::overlay;
use cmd::{LANGUAGES, SLASH, language_label};

#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum Mode { Handle, Slash }
#[derive(Clone, Copy)]
pub(super) struct BlockMenu { pub block: u64, pub anchor: Point<Pixels>, pub mode: Mode, pub index: usize, pub sub: Sub }
/// Which page of the handle menu is showing; sub pages replace the root list in place.
#[derive(Clone, Copy, PartialEq, Eq, Default)]
pub(super) enum Sub { #[default] Root, Convert, Language }
/// What a handle menu is about, decided from the block itself.
#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum BlockType { Text(Kind), Code, Table, Image, Divider, Source }
pub(super) fn block_type(block: &Block) -> BlockType {
    if block.meta.table.is_some() { BlockType::Table }
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
                let left = grip.left() - px(MENU_GAP + MENU_WIDTH);
                point(if left >= px(24.) { left } else { grip.right() + px(MENU_GAP) }, grip.top())
            }
            _ => anchor,
        };
        self.block_menu = Some(BlockMenu { block: id, anchor, mode, index: 0, sub: Sub::Root });
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
                SLASH.into_iter().filter(|c| query.is_empty() || c.label().contains(&query) || c.keywords().contains(&query))
                    .map(Some).collect()
            }
            Mode::Handle => {
                let Some(block) = self.doc.get(menu.block) else { return Vec::new() };
                self.handle_entries(block, menu.sub)
            }
        }
    }
    /// Handle menu for one block: its own tools first, then the shared block actions, delete last.
    fn handle_entries(&self, block: &Block, sub: Sub) -> Vec<Option<Cmd>> {
        let kind = block_type(block);
        match sub {
            Sub::Convert => {
                let current = match kind { BlockType::Text(k) => Some(k), _ => None };
                let mut items = vec![Some(Cmd::Back), None];
                items.extend([Kind::Paragraph, Kind::H1, Kind::H2, Kind::H3, Kind::Todo, Kind::Bullet, Kind::Numbered, Kind::Quote]
                    .map(|k| Some(Cmd::TurnTo(k, current == Some(k)))));
                items.push(Some(Cmd::CodeTo(kind == BlockType::Code)));
                return items;
            }
            Sub::Language => {
                let current = block.meta.code_language.as_deref().map(language_label).unwrap_or("");
                let mut items = vec![Some(Cmd::Back), None];
                items.extend(LANGUAGES.iter().map(|(tag, label)| Some(Cmd::SetLanguage(tag, *label == current))));
                return items;
            }
            Sub::Root => {}
        }
        let mut items: Vec<Option<Cmd>> = Vec::new();
        match kind {
            BlockType::Code => {
                let language = block.meta.code_language.as_deref().map(language_label).unwrap_or("纯文本");
                items.extend([Some(Cmd::Open(Sub::Language, language)), Some(Cmd::CopyCode),
                    Some(Cmd::ToggleWrap(!self.nowrap.contains(&block.id))), Some(Cmd::Open(Sub::Convert, "代码块")), None]);
            }
            BlockType::Table => items.extend([Some(Cmd::TableOp(TableCmd::SelectTable)), Some(Cmd::TableOp(TableCmd::CopyTable)), None,
                Some(Cmd::TableOp(TableCmd::AppendRow)), Some(Cmd::TableOp(TableCmd::AppendColumn)), None]),
            BlockType::Image => {
                let align = super::image_source::spec_of(block).map(|s| s.align).unwrap_or_default();
                items.extend([Some(Cmd::CopyImage), Some(Cmd::RevealImage), None]);
                items.extend([Align::Left, Align::Center, Align::Right].map(|a| Some(Cmd::AlignImage(a, a == align))));
                items.push(None);
            }
            BlockType::Text(k) => {
                let label = Cmd::Turn(k).label();
                items.extend([Some(Cmd::Open(Sub::Convert, label)), Some(Cmd::CopyText), None]);
            }
            BlockType::Divider | BlockType::Source => {}
        }
        items.extend([Cmd::InsertAbove, Cmd::InsertBelow, Cmd::Duplicate, Cmd::Cut].map(Some));
        let index = self.doc.index_of(block.id).unwrap_or(0);
        if index > 0 { items.push(Some(Cmd::MoveUp)); }
        if index + 1 < self.doc.blocks.len() { items.push(Some(Cmd::MoveDown)); }
        items.push(None);
        items.push(Some(if kind == BlockType::Table { Cmd::TableOp(TableCmd::DeleteTable) } else { Cmd::Delete }));
        items
    }
    pub(super) fn block_menu_step(&mut self, down: bool, cx: &mut Context<Self>) -> bool {
        let Some(mut menu) = self.block_menu.filter(|m| m.mode == Mode::Slash) else { return false };
        let count = self.block_menu_entries().len();
        if count == 0 { return false; }
        menu.index = if down { (menu.index + 1) % count } else { (menu.index + count - 1) % count };
        self.block_menu = Some(menu); cx.notify(); true
    }
    pub(super) fn block_menu_pick(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(menu) = self.block_menu.filter(|m| m.mode == Mode::Slash) else { return false };
        let entries = self.block_menu_entries();
        let Some(Some(cmd)) = entries.get(menu.index.min(entries.len().saturating_sub(1))).copied() else { return false };
        self.run_block_cmd(menu.block, menu.mode, cmd, window, cx); true
    }
    /// Toolbar insertion: use the caret block when it is an empty paragraph, otherwise add below it.
    pub(super) fn insert_here(&mut self, cmd: Cmd, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        let id = self.caret.head.block;
        self.run_block_cmd(id, Mode::Slash, cmd, window, cx);
    }
    pub(super) fn run_block_cmd(&mut self, id: u64, mode: Mode, cmd: Cmd, window: &mut Window, cx: &mut Context<Self>) {
        match cmd {
            Cmd::Header(_) => return,
            Cmd::Open(sub, _) => {
                if let Some(menu) = self.block_menu.as_mut() { menu.sub = sub; menu.index = 0; }
                cx.notify(); return;
            }
            Cmd::Back => {
                if let Some(menu) = self.block_menu.as_mut() { menu.sub = Sub::Root; menu.index = 0; }
                cx.notify(); return;
            }
            _ => {}
        }
        let menu_anchor = self.block_menu.as_ref().map(|m| m.anchor);
        self.block_menu = None;
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
            Cmd::TurnTo(_, true) | Cmd::CodeTo(true) | Cmd::SetLanguage(_, true) => { cx.notify(); return; }
            Cmd::CopyCode => {
                let text = self.doc.get(id).map(|b| b.text.clone()).unwrap_or_default();
                cx.write_to_clipboard(gpui::ClipboardItem::new_string(text)); self.status = "已复制代码".into(); cx.notify(); return;
            }
            _ => {}
        }
        let cmd = match cmd { Cmd::TurnTo(kind, _) => Cmd::Turn(kind), Cmd::CodeTo(_) => Cmd::Code(""),
            Cmd::SetLanguage(tag, _) => Cmd::Code(tag), other => other };
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
                let mut blocks = vec![new]; if trailing { blocks.push(paragraph()); }
                let (start, remove) = if slash_target { (index, 1) } else { (index + 1, 0) };
                let count = blocks.len();
                self.doc.splice_blocks(start..start + remove, blocks);
                changes.push(ListChange { start, before: remove, after: count });
                let focus = if cmd == Cmd::Table { self.doc.blocks[start].id } else { self.doc.blocks[(start + 1).min(self.doc.blocks.len() - 1)].id };
                caret = Some(Position { block: focus, offset: 0 });
            }
            Cmd::Image => { self.pick_image(id, slash_target, window, cx); return; }
            Cmd::Ai => return,
            Cmd::InsertAbove | Cmd::InsertBelow => {
                let at = if cmd == Cmd::InsertAbove { index } else { index + 1 };
                self.doc.splice_blocks(at..at, vec![paragraph()]);
                changes.push(ListChange { start: at, before: 0, after: 1 });
                caret = Some(Position { block: self.doc.blocks[at].id, offset: 0 });
            }
            Cmd::Duplicate => {
                let mut copy = block.clone(); copy.id = 0;
                self.doc.splice_blocks(index + 1..index + 1, vec![copy]);
                changes.push(ListChange { start: index + 1, before: 0, after: 1 });
            }
            Cmd::CopyText => { cx.write_to_clipboard(gpui::ClipboardItem::new_string(block.text.clone())); return; }
            Cmd::MoveUp | Cmd::MoveDown => {
                let to = if cmd == Cmd::MoveUp { index.saturating_sub(1) } else { index + 1 };
                if to != index && to < self.doc.blocks.len() {
                    self.doc.move_block(id, to);
                    changes.push(ListChange { start: index, before: 1, after: 0 });
                    changes.push(ListChange { start: to, before: 0, after: 1 });
                }
            }
            Cmd::Header(_) | Cmd::Open(..) | Cmd::Back | Cmd::TurnTo(..) | Cmd::CodeTo(_) | Cmd::SetLanguage(..) | Cmd::CopyCode
                | Cmd::TableOp(_) | Cmd::CopyImage | Cmd::RevealImage | Cmd::AlignImage(..) | Cmd::ToggleWrap(_) => return,
            Cmd::Cut | Cmd::Delete => {
                if cmd == Cmd::Cut {
                    let markdown = super::markdown::serialize(&super::model::Doc::from_blocks(vec![block.clone()]));
                    let plain = markdown.trim_end().to_owned();
                    cx.write_to_clipboard(gpui::ClipboardItem::new_string(plain.clone()));
                    self.block_clip = Some((plain, markdown));
                }
                let only = self.doc.blocks.len() == 1;
                self.doc.remove(id);
                changes.push(if only { ListChange { start: 0, before: 1, after: 1 } } else { ListChange { start: index, before: 1, after: 0 } });
                let at = index.min(self.doc.blocks.len() - 1);
                caret = Some(Position { block: self.doc.blocks[at].id, offset: 0 });
            }
        }
        if let Some(at) = caret { self.caret.anchor = at; self.caret.head = at; }
        self.record_changes(before, changes, true, cx);
        self.layouts.clear();
        window.focus(&self.focus); cx.notify();
    }
}

const MENU_WIDTH: f32 = 224.;
const MENU_GAP: f32 = 6.;
