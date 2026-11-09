//! Input commands share cross-block positions and persistent edit transactions.
use std::ops::Range;
use gpui::{ClipboardItem, Context, Window};
use unicode_segmentation::GraphemeCursor;
use super::{model::{Kind, MarkKind}, view::{Editor, EditorAction, selection::{Position, boundary}}};
#[path = "transactions.rs"] pub(super) mod transactions;

impl Editor {
    pub(crate) fn handle(&mut self, action: &EditorAction, window: &mut Window, cx: &mut Context<Self>) {
        #[cfg(not(target_family = "wasm"))] if crate::editor::ai::plan_link::create_preview_active(cx.entity_id()) { return; }
        if !matches!(action, EditorAction::Backspace) { super::block_shortcuts::clear_first_line_delete(); }
        use EditorAction::*;
        if !self.minimal && crate::quick::is_quick_panel_active()
            && matches!(action, Paste | Copy | Cut | SelectAll | Undo | Redo)
            && crate::quick::dispatch_editor_action(action, cx) { return; }
        if super::paste_clipboard::forward_focused_input_action(action, window, cx) { return; }
        if self.path.is_none() || (!self.is_markdown_note() && !self.editing_source()) {
            if matches!(action, Copy) && let Some(path) = self.path.clone() {
                self.copy_current_file(&path, cx);
            }
            return;
        }
        if self.editing_source() { self.handle_source(action, window, cx); return; }
        self.pending_vertical = None;
        if !matches!(action, Cancel) { self.commit_composition(cx); }
        match action {
            Up | Down if self.step_open_menu(matches!(action, Down), cx) => return,
            Enter if self.activate_open_menu(window, cx) => return,
            Cancel if super::code_block::close_lang_picker_if_open(cx) => return,
            Cancel if self.block_menu.is_some() => { self.block_menu = None; cx.notify(); return; }
            Cancel if self.annotate.reactions.is_some() => { self.annotate.reactions = None; cx.notify(); return; }
            Cancel if self.annotate.composer.is_some() => { self.close_annotation_composer(window, cx); return; }
            Cancel if self.annotate.editing.is_some() => { self.cancel_edit_annotation(window, cx); return; }
            Cancel if self.annotate.followup.is_some() => { self.close_annotation_followup(window, cx); return; }
            Cancel if self.annotate.active_annotation.is_some() => {
                self.annotate.active_annotation = None;
                cx.notify();
                return;
            }
            Cancel if self.ai_session().is_some() => { self.ai_escape(window, cx); return; }
            _ => {}
        }
        if self.block_selection_key(action, window, cx) { return; }
        match action {
            Left | Right | SelectLeft | SelectRight => {
                self.preferred_x = None;
                let forward = matches!(action, Right | SelectRight);
                let extend = matches!(action, SelectLeft | SelectRight);
                if !extend && self.code_escape_key(forward, cx) { return; }
                let at = if !extend && !self.caret.is_empty() {
                    let (a,b,_,_) = self.ordered(); if forward { b } else { a }
                } else { self.step(forward) };
                self.set_head(at, extend, cx);
                if !extend { self.code_escape_arrive(forward, cx); }
            }
            SelectAll if self.table_select_all(cx) => {}
            // First press selects the caret's block, the next one the whole document.
            SelectAll if { let (a, b, ai, bi) = self.ordered(); let block = &self.doc.blocks[ai];
                ai == bi && block.meta.code_language.is_some() && !(a.offset == 0 && b.offset == block.text.len()) } => {
                let id = self.caret.head.block; let len = self.block_text().len();
                self.caret.anchor = Position { block: id, offset: 0 };
                self.set_head(Position { block: id, offset: len }, true, cx);
            }
            SelectAll => {
                self.caret.anchor = Position { block: self.doc.blocks[0].id, offset: 0 };
                let last = &self.doc.blocks[self.doc.blocks.len()-1];
                if self.doc.blocks.len() == 1 && last.text.is_empty() { self.block_sel = Some((0, 0)); }
                self.set_head(Position { block: last.id, offset: last.text.len() }, true, cx);
            }
            Up | Down | SelectUp | SelectDown => self.vertical(matches!(action, Down | SelectDown), matches!(action, SelectUp | SelectDown), cx),
            // A focused image or divider hides its source line, so keys act on the block instead of editing it.
            Enter if let Some(id) = self.focused_atomic() => self.add_below(id, window, cx),
            Backspace | Delete if let Some(id) = self.focused_image() =>
                self.run_block_cmd(id, super::block_menu::Mode::Handle, super::block_menu::Cmd::Delete, window, cx),
            Enter if self.enter_on_folded_heading(cx) => {}
            Enter if self.divider_on_enter(cx) => {}
            Enter if self.table_enter(cx) => {}
            Enter if self.enter_rules(cx) => {}
            Enter => {
                let id = self.caret.head.block;
                self.unfold_heading(id, cx);
                if self.caret.is_empty() && self.block_text().is_empty() && self.doc.get(id).is_some_and(|b| b.kind != Kind::Paragraph) {
                    let before = self.snapshot(); self.doc.set_kind(id, Kind::Paragraph); self.record(before, cx);
                } else { self.replace_selection("\n", false, cx); }
            }
            NextCell | PreviousCell => self.table_tab(matches!(action, NextCell), cx),
            Backspace | Delete if let Some((id, super::table_actions::TableSel::Whole)) = self.table_sel => {
                self.table_command(id, super::table_actions::TableSel::Whole, super::table_actions::TableCmd::DeleteTable, cx);
            }
            Backspace | Delete if self.fold_gap_edit(matches!(action, Delete), cx) => {}
            Backspace | Delete if self.boundary_rules(matches!(action, Delete), cx) => {}
            Backspace | Delete if self.delete_across(cx) => {}
            Backspace | Delete => {
                let caret_before = self.caret.clone();
                if self.caret.is_empty() {
                    let at = self.step(matches!(action, Delete));
                    self.caret.head = at;
                }
                if !self.can_replace_selection() {
                    self.caret = caret_before; self.table_edit_rejected(cx);
                } else { self.replace_selection("", false, cx); }
            }
            Copy | Cut => {
                if !self.caret.is_empty() {
                    if !self.copy_selected_images(cx) {
                        let Some(item) = self.selection_clipboard_item() else { return; };
                        cx.write_to_clipboard(item);
                    }
                    if matches!(action, Cut) && !self.boundary_rules(false, cx) && !self.delete_across(cx) { self.replace_selection("", false, cx); }
                } else if self.copy_whole_block(cx) && matches!(action, Cut) { self.handle(&EditorAction::Backspace, window, cx); }
            }
            Paste => match cx.read_from_clipboard() {
                // Browsers deliver clipboard data through the DOM `paste` event (-> `EntityInputHandler::paste`);
                // propagating keeps gpui-web from cancelling that event on the Cmd+V keydown.
                None if cfg!(target_family = "wasm") => cx.propagate(),
                item => self.paste_clipboard_item(item, window, cx),
            },
            Bold => self.mark(MarkKind::Bold, cx), Italic => self.mark(MarkKind::Italic, cx),
            Strike => self.mark(MarkKind::Strike, cx), Code => self.mark(MarkKind::Code, cx),
            Undo => self.undo(false, cx), Redo => self.undo(true, cx), Save => self.save(cx),
            Cancel => {
                if let Some(before) = self.composition_before.take() {
                    self.doc = before.doc; self.caret = before.caret;
                    self.list.reset(self.doc.blocks.len()); self.changed(cx);
                }
                self.caret.composing = None;
                let callout_closed = crate::ui::callout_controls::close_menu();
                let idle = !callout_closed && self.block_menu.is_none() && self.format.is_none() && self.table_menu.is_none() && self.table_sel.is_none() && self.image_menu.is_none() && self.find.is_none();
                if idle {
                    let (_, _, ai, bi) = self.ordered();
                    self.block_sel = Some((ai, bi));
                    self.caret.anchor = self.caret.head;
                }
                self.clear_pointer(); self.toolbar_anchor = None; self.block_menu = None;
                self.table_menu = None; self.image_menu = None; self.format = None; self.link_ui = Default::default(); self.find = None;
                cx.notify();
            }
        }
        let _ = window;
    }
    /// Smart paste shared by `Cmd+V` and the web `paste` event: images, plain text, tables, blocks, links.
    pub(super) fn paste_clipboard_item(&mut self, item: Option<ClipboardItem>, window: &mut Window, cx: &mut Context<Self>) {
        if self.editing_source() { self.source_paste(item, cx); return; }
        if !self.is_markdown_note() { return; }
        if let Some(md) = super::clipboard_files::markdown_file_text(item.as_ref()) {
            self.paste_text(&md, false, true, None, cx); return;
        }
        let image = item.as_ref().and_then(|item| item.entries().iter().find_map(|entry| match entry {
            gpui::ClipboardEntry::Image(image) => Some((image.bytes.clone(), match image.format {
                gpui::ImageFormat::Jpeg => "jpg", gpui::ImageFormat::Gif => "gif", gpui::ImageFormat::Webp => "webp", _ => "png" })),
            _ => None }));
        let files = super::clipboard_files::image_paths(item.as_ref());
        if !files.is_empty() {
            let id = self.caret.head.block; self.import_images(id, super::image_import::Place::After, files, window, cx);
        } else if let Some((bytes, ext)) = image.filter(|_| item.as_ref().and_then(super::clipboard_files::string_text).is_none()) {
            let id = self.caret.head.block; self.import_image_bytes(id, bytes, ext, window, cx);
        } else if let Some(item) = item && let Some(text) = super::clipboard_files::string_text(&item) {
            if let Some(fragment) = super::paste_clipboard::inline_fragment(&item, &text) {
                self.paste_inline_fragment(&fragment, cx); return;
            }
            let markdown = item.metadata().is_some_and(|metadata| metadata == super::paste::MARKDOWN_CLIPBOARD_V1);
            let blocks = super::paste_clipboard::block_markdown(&item, &text);
            self.paste_text(&text, false, markdown, blocks.as_deref(), cx);
        }
    }
    pub(super) fn step(&self, forward: bool) -> Position {
        let at = self.caret.head;
        let atomic = self.doc.get(at.block).is_some_and(|b| super::image_source::spec_of(b).is_some() || super::block_menu::is_divider(b));
        if !atomic {
            let text = self.block_text();
            let mut cursor = GraphemeCursor::new(boundary(text, at.offset), text.len(), true);
            let next = if forward { cursor.next_boundary(text, 0) } else { cursor.prev_boundary(text, 0) };
            if let Ok(Some(offset)) = next { return Position { offset, ..at }; }
        }
        let index = self.doc.index_of(at.block).unwrap_or(0);
        let next = self.next_visible_index(index, forward);
        next.map(|i| {
            let b = &self.doc.blocks[i];
            let next_atomic = super::image_source::spec_of(b).is_some() || super::block_menu::is_divider(b);
            Position { block: b.id, offset: if forward || next_atomic { 0 } else { b.text.len() } }
        }).unwrap_or(at)
    }
    /// The image block holding a collapsed caret; its source is hidden and must not be typed into.
    pub(super) fn focused_image(&self) -> Option<u64> {
        let id = self.caret.head.block;
        (self.caret.is_empty() && self.doc.get(id).is_some_and(|b| super::image_source::spec_of(b).is_some())).then_some(id)
    }
    pub(super) fn focused_atomic(&self) -> Option<u64> {
        let id = self.caret.head.block;
        (self.caret.is_empty() && self.doc.get(id).is_some_and(|b| super::image_source::spec_of(b).is_some() || super::block_menu::is_divider(b))).then_some(id)
    }
    pub(super) fn replace(&mut self, range: Option<Range<usize>>, text: &str, window: &mut Window, cx: &mut Context<Self>) {
        super::block_shortcuts::clear_first_line_delete();
        if self.editing_source() { self.source_replace_utf16(range, text, cx); return; }
        if !self.is_markdown_note() { return; }
        if self.path.is_none() { return; }
        let range = match self.focused_atomic() { Some(id) if !text.is_empty() => { self.add_below(id, window, cx); None } _ => range };
        let caret_before = self.caret.clone();
        self.set_replacement_range(range);
        let text = if text == "、" && self.slash_alias_ready() { "/" } else { text };
        if !self.can_replace_selection() {
            self.caret = caret_before; self.table_edit_rejected(cx); return;
        }
        let composing = self.composition_before.is_some();
        if !composing && self.wrap_selection(text, cx) { return; }
        // thinkly `emptyParagraphAiShortcut`: space in an empty paragraph opens inline AI.
        if !self.minimal && text == " " && !composing && self.caret.is_empty() {
            let id = self.caret.head.block;
            if self.doc.get(id).is_some_and(|b| b.text.is_empty() && b.kind == Kind::Paragraph && b.meta.table.is_none() && b.meta.raw_source.is_none() && b.meta.code_language.is_none()) {
                let anchor = self.layouts.get(&id).map(|l| gpui::point(l.bounds.left(), l.bounds.bottom() + gpui::px(4.)));
                self.open_ai_block(id, false, anchor, window, cx);
                return;
            }
        }
        let pre_bounds = self
            .layouts
            .get(&self.caret.head.block)
            .map(|l| l.bounds);
        self.replace_selection(text, !composing, cx);
        if composing {
            self.commit_composition(cx);
            let mut id = self.caret.head.block;
            let mut offset = self.caret.head.offset;
            let before = self.snapshot();
            if text.ends_with(' ') || text.ends_with('\u{3000}') {
                self.space_rule(&mut id, &mut offset);
            } else if matches!(text, "-" | "*" | "_" | "－" | "＊" | "＿" | "—" | "——") {
                self.divider_rule(&mut id, &mut offset);
            }
            if self.doc != before.doc {
                let at = Position { block: id, offset };
                self.caret.anchor = at; self.caret.head = at;
                self.record(before, cx);
            }
        }
        if matches!(text, ">" | "-" | "》" | "—") { self.arrow_rule(cx); }
        else if matches!(text, "*" | "_" | "~" | "`" | "=" | ")") { self.inline_rule(text, cx); }
        if text == "/" {
            let id = self.caret.head.block;
            if self.doc.get(id).is_some_and(|b| b.text == "/" && b.kind == Kind::Paragraph && b.meta.table.is_none() && b.meta.raw_source.is_none() && b.meta.code_language.is_none()) {
                let trigger = pre_bounds
                    .or_else(|| self.block_trigger_bounds(cx.entity_id(), id))
                    .unwrap_or_else(|| gpui::Bounds::new(gpui::point(gpui::px(300.), gpui::px(176.)), gpui::size(gpui::px(224.), gpui::px(24.))));
                self.open_block_menu_at(id, trigger, super::block_menu::Mode::Slash, cx);
            }
        }
    }
    pub(super) fn set_replacement_range(&mut self, range: Option<Range<usize>>) {
        let range = range.map(|r| from_utf16(self.block_text(), &r)).or(self.caret.composing.clone());
        if let Some(range) = range {
            self.caret.anchor = Position { block: self.caret.head.block, offset: range.start };
            self.caret.head.offset = range.end;
        }
    }
    pub(super) fn add_below(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx);
        let Some(index) = self.doc.index_of(id) else { return; };
        let before = self.snapshot();
        if let Some(id) = self.doc.insert_after(id, Kind::Paragraph, String::new()) {
            self.record_changes(before, vec![transactions::ListChange { start: index + 1, before: 0, after: 1 }], true, cx);
            if self.show_hidden_tail(id) {
                self.set_head(Position { block: id, offset: 0 }, false, cx);
            }
            window.focus(&self.focus, cx);
        }
    }
}
pub(super) fn to_utf16(text: &str, range: &Range<usize>) -> Range<usize> {
    let one = |offset| text[..boundary(text, offset)].encode_utf16().count();
    one(range.start)..one(range.end)
}
pub(super) fn from_utf16(text: &str, range: &Range<usize>) -> Range<usize> {
    fn one(text: &str, units: usize) -> usize {
        let mut seen = 0;
        for (index, ch) in text.char_indices() {
            if seen + ch.len_utf16() > units { return index; }
            seen += ch.len_utf16();
        }
        text.len()
    }
    let start = one(text, range.start);
    start..one(text, range.end).max(start)
}
