//! Editor entity: shared state, virtual list, and public document lifecycle.
use std::{collections::HashMap, path::PathBuf};
use gpui::{App, Context, FocusHandle, Focusable, ListAlignment, ListState, MouseButton,
    Pixels, Point, Window, actions, div, list, prelude::*, px};
use crate::theme::Theme;
use crate::ui::scrollbar;
use super::model::Doc;
use super::input::transactions::{Snapshot, HistoryEntry};
#[path = "selection.rs"] pub(super) mod selection;
#[path = "layout.rs"] pub(super) mod layout;
#[path = "interaction.rs"] pub(super) mod interaction;
#[path = "syntax.rs"] pub(super) mod syntax;
#[path = "protocol.rs"] mod protocol;
#[path = "storage.rs"] pub(super) mod storage;
#[path = "lifecycle.rs"] mod lifecycle;
#[path = "session.rs"] mod session;
use {selection::Caret, layout::TextLayout, lifecycle::Persistence};
/// Space kept between the caret line and the viewport edge when the caret scrolls into view.
const REVEAL_MARGIN: f32 = 80.;

actions!(editor, [Backspace, Delete, Left, Right, SelectLeft, SelectRight, SelectAll,
    Up, Down, SelectUp, SelectDown, Enter, Copy, Cut, Paste, Bold, Italic, Strike, Code,
    Undo, Redo, Cancel, Save, NextCell, PreviousCell, TableMenuKey,
    SetParagraph, SetH1, SetH2, SetH3, SetTodo, SetBullet, SetNumbered, SetCode, SetQuote,
    MoveBlockUp, MoveBlockDown, DuplicateBlock, CmdEnter, Underline, EditLink, OpenLinkAtCaret, ExitBlock,
    DocStart, DocEnd, SelectDocStart, SelectDocEnd, Find, ImageGrow, ImageShrink,
    LineStart, LineEnd, SelectLineStart, SelectLineEnd, LogicalLineStart, LogicalLineEnd,
    WordLeft, WordRight, SelectWordLeft, SelectWordRight,
    DeleteWordLeft, DeleteWordRight, DeleteLineLeft, DeleteLineRight, ExportImage, CopyMarkdown, Annotate,
    ToggleHighlight, ClearFormat, ToggleFold, PastePlain]);
pub(super) enum EditorAction {
    Backspace, Delete, Left, Right, SelectLeft, SelectRight, SelectAll, Up, Down,
    SelectUp, SelectDown, Enter, Copy, Cut, Paste, Bold, Italic, Strike, Code, Undo, Redo, Cancel, Save,
    NextCell, PreviousCell,
}
pub struct Editor {
    pub(super) doc: Doc,
    pub(super) focus: FocusHandle,
    pub(super) caret: Caret,
    pub(super) hover: Option<u64>,
    pub(super) hover_gen: u64,
    pub(super) dragging: Option<u64>,
    pub(super) drag_over: Option<interaction::DropTarget>,
    pub(super) selecting: bool,
    pub(super) block_menu: Option<super::block_menu::BlockMenu>,
    /// Code blocks the user switched to no-wrap (view state only, never saved).
    pub(super) nowrap: std::collections::HashSet<u64>,
    /// Folded heading block IDs for the active note (view/session state, never serialized to Markdown).
    pub(super) folded: std::collections::HashSet<u64>,
    pub(super) table_sel: Option<(u64, super::table_actions::TableSel)>,
    pub(super) table_menu: Option<super::table_actions::TableMenu>,
    pub(super) table_drag: Option<super::table_actions::TableDrag>,
    pub(super) table_chrome: Option<super::table_actions::TableChrome>,
    pub(super) selection_origin: Option<(selection::Position, selection::Position, usize)>,
    pub(super) toolbar_anchor: Option<Point<Pixels>>,
    pub(super) format: Option<super::format_cmds::FormatPop>,
    pub(super) image_menu: Option<(u64, super::image_edit::ImagePop)>,
    pub(super) link_ui: super::link_pop::LinkUi,
    pub(super) layouts: HashMap<u64, TextLayout>,
    pub(super) list: ListState,
    pub(super) scroll_top: (usize, Pixels),
    pub(super) preferred_x: Option<Pixels>,
    pub(super) pending_vertical: Option<(u64, Pixels, bool, bool)>,
    pub(super) reveal_caret: bool,
    pub(super) dirty: bool,
    pub(super) path: Option<PathBuf>,
    pub(super) status: String,
    pub(super) history: Vec<HistoryEntry>,
    pub(super) redo: Vec<HistoryEntry>,
    pub(super) composition_before: Option<Snapshot>,
    persistence: Persistence,
    pointer_subscriptions: Vec<gpui::Subscription>,
    /// Last in-app block copy: (plain text put on the clipboard, Markdown of the copied blocks).
    pub(super) block_clip: Option<(String, String)>,
    /// Esc block selection: (anchor index, head index).
    pub(super) block_sel: Option<(usize, usize)>,
    /// Quick-note mode: no floating toolbar, no block handles, narrower padding, no status bar.
    pub(super) minimal: bool,
    pub(super) find: Option<super::find::FindState>,
    pub(super) annotate: super::annotate::AnnotateState,
    sessions: HashMap<PathBuf, session::TabSession>,
}
impl Editor {
    pub fn new(cx: &mut Context<Self>) -> Self {
        crate::dropdown::register_editor(&cx.entity());
        cx.on_app_quit(|editor, _| { editor.prepare_to_quit(); async {} }).detach();
        let doc = Doc::empty(); let caret = Caret::new(doc.blocks[0].id);
        Self { doc, caret, focus: cx.focus_handle(), hover: None, hover_gen: 0, dragging: None,
            drag_over: None, selecting: false, block_menu: None, nowrap: Default::default(), folded: Default::default(), table_sel: None, table_menu: None, table_drag: None, table_chrome: None, selection_origin: None, toolbar_anchor: None, format: None, image_menu: None, link_ui: Default::default(), layouts: HashMap::new(),
            list: ListState::new(1, ListAlignment::Top, px(120.)), scroll_top: (0, px(0.)), preferred_x: None, pending_vertical: None,
            reveal_caret: false, dirty: false, path: None, status: String::new(),
            history: Vec::new(), redo: Vec::new(), composition_before: None,
            persistence: Persistence::default(), pointer_subscriptions: Vec::new(), block_clip: None, block_sel: None, minimal: false, find: None, annotate: Default::default(), sessions: HashMap::new() }
    }
    pub fn set_minimal(&mut self, minimal: bool) { self.minimal = minimal; }
    pub fn reset_layouts(&mut self) { self.layouts.clear(); }
    pub fn status_text(&self) -> &str { &self.status }
    pub fn is_dirty(&self) -> bool { self.dirty }
    pub(crate) fn dismiss_dropdowns(&mut self) -> bool {
        self.block_menu.take().is_some() | self.table_menu.take().is_some()
            | self.format.take().is_some() | self.image_menu.take().is_some() | super::code_block::close_menus()
    }
    fn reveal(&mut self) {
        if !self.reveal_caret { return; }
        let margin = px(REVEAL_MARGIN);
        if let Some(layout) = self.layouts.get(&self.caret.head.block) {
            scrollbar::reveal_line(&self.list, layout.point(self.caret.head.offset).y, layout.line_height, margin);
        } else if let Some(index) = self.doc.index_of(self.caret.head.block) {
            scrollbar::reveal_item(&self.list, self.column_group_range(index).map_or(index, |r| r.start), margin);
        }
    }
}
impl Focusable for Editor { fn focus_handle(&self, _: &App) -> FocusHandle { self.focus.clone() } }
impl Render for Editor {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.sync_annotations(); self.track_pointer_lifecycle(window, cx);
        let theme = Theme::ocean(); self.reveal();
        let input_editor = cx.entity();
        let input = gpui::canvas(|_, _, _| (), move |bounds, (), window, cx| {
            input_editor.update(cx, |this, _| { this.layouts.clear(); });
            let editor = input_editor.read(cx);
            if editor.path.is_some() { window.handle_input(&editor.focus, gpui::ElementInputHandler::new(bounds, input_editor.clone()), cx); }
        }).absolute().size_full();
        let editor = cx.entity();
        let pad = if self.minimal { (18., 12.) } else { (64., 160.) };
        if self.list.item_count() != self.doc.blocks.len() {
            crate::log::warn(&format!("editor list item_count ({}) != doc.blocks.len() ({}); resetting", self.list.item_count(), self.doc.blocks.len()));
            self.list.reset(self.doc.blocks.len());
        }
        let rows = list(self.list.clone(), move |index, _, cx| {
            editor.update(cx, |this, cx| {
                if index >= this.doc.blocks.len() {
                    crate::log::warn(&format!("list render index {index} out of bounds (blocks={})", this.doc.blocks.len()));
                    return div().into_any_element();
                }
                if !this.minimal && this.is_index_hidden(index) {
                    let id = this.doc.blocks[index].id; this.layouts.remove(&id);
                    return div().id(("folded-row", id as usize)).into_any_element();
                }
                if let Some(range) = this.column_group_range(index) {
                    if index == range.start { return super::columns::columns_row(this, range, cx); }
                    return div().id(("col-member", this.doc.blocks[index].id as usize)).into_any_element();
                }
                super::chrome::render_block_row(this, index, cx)
            })
        }).size_full().pt(px(pad.0)).pb(px(pad.1));
        let rows = if self.minimal { div().flex_1().min_w_0().h_full().ml(px(-32.)).child(rows).into_any_element() } else { rows.into_any_element() };
        let anchor_editor = cx.entity();
        // Runs after all rows paint: layout retries never publish overlay geometry.
        let anchor_sync = gpui::canvas(|_, _, _| (), move |bounds, (), window, cx| {
            anchor_editor.update(cx, |this, cx| {
                // `cx.notify()` is dropped while the window is painting, so changes here are
                // published by a deferred notify that runs once this frame has finished.
                let mut changed = false;
                let top = this.list.logical_scroll_top();
                let cur_scroll = (top.item_ix, top.offset_in_item);
                if this.scroll_top != cur_scroll { this.scroll_top = cur_scroll; crate::dropdown::dismiss_all(cx); }
                if this.annotate.editor_top != bounds.top() { this.annotate.editor_top = bounds.top(); changed = true; }
                if let Some(menu) = &mut this.block_menu && menu.mode == super::block_menu::Mode::Slash
                    && let Some(l) = this.layouts.get(&menu.block)
                {
                    let next = gpui::point(l.bounds.left(), l.bounds.bottom() + px(4.));
                    if menu.anchor != next { menu.anchor = next; changed = true; }
                }
                let anchor = this.selection_toolbar_anchor(bounds, window);
                if this.toolbar_anchor != anchor { this.toolbar_anchor = anchor; changed = true; }
                let chrome = this.table_chrome_geometry(window);
                let same = match (&this.table_chrome, &chrome) {
                    (None, None) => true,
                    (Some(a), Some(b)) => a.block == b.block && a.cell == b.cell && a.bounds == b.bounds
                        && a.columns == b.columns && a.rows == b.rows,
                    _ => false };
                if !same { this.table_chrome = chrome; changed = true; }
                if changed { let me = cx.entity().downgrade(); cx.defer(move |cx| { me.update(cx, |_, cx| cx.notify()).ok(); }); }
            });
        }).absolute().size_full();
        let floating = self.toolbar_anchor.filter(|_| !self.minimal && !self.caret.is_empty() && !self.selecting && self.dragging.is_none()
            && self.caret.composing.is_none() && !matches!(self.format, Some(super::format_cmds::FormatPop::SelectionMenu { .. }))
            && self.block_menu.is_none() && self.table_menu.is_none() && window.is_window_active() && self.focus.is_focused(window));
        let std_w = super::chrome::standard_width(theme.content_width);
        let vw = self.list.viewport_bounds().size.width;
        let rail_w = if !self.minimal && self.annotate.has_visible_rail() && super::annotate_ui::is_wide(vw) { px(248.) } else { px(0.) };
        div().size_full().min_w_0().relative()
            .on_action(cx.listener(|this, _: &Find, w, cx| this.open_find(w, cx)))
            .child(div().id("editor").key_context("Editor").track_focus(&self.focus).size_full().min_w_0()
                .relative().flex().flex_col().overflow_hidden().bg(theme.paper)
                .children(crate::ui::smoke_overlay::probe("editor"))
                .child(input)
                .on_action(cx.listener(|this, _: &Left, w, cx| this.handle(&EditorAction::Left, w, cx)))
                .on_action(cx.listener(|this, _: &Right, w, cx| this.handle(&EditorAction::Right, w, cx)))
                .on_action(cx.listener(|this, _: &SelectLeft, w, cx| this.handle(&EditorAction::SelectLeft, w, cx)))
                .on_action(cx.listener(|this, _: &SelectRight, w, cx| this.handle(&EditorAction::SelectRight, w, cx)))
                .on_action(cx.listener(|this, _: &SelectAll, w, cx| this.handle(&EditorAction::SelectAll, w, cx)))
                .on_action(cx.listener(|this, _: &Up, w, cx| this.handle(&EditorAction::Up, w, cx)))
                .on_action(cx.listener(|this, _: &Down, w, cx| this.handle(&EditorAction::Down, w, cx)))
                .on_action(cx.listener(|this, _: &SelectUp, w, cx| this.handle(&EditorAction::SelectUp, w, cx)))
                .on_action(cx.listener(|this, _: &SelectDown, w, cx| this.handle(&EditorAction::SelectDown, w, cx)))
                .on_action(cx.listener(|this, _: &Enter, w, cx| this.handle(&EditorAction::Enter, w, cx)))
                .on_action(cx.listener(|this, _: &NextCell, w, cx| this.handle(&EditorAction::NextCell, w, cx)))
                .on_action(cx.listener(|this, _: &PreviousCell, w, cx| this.handle(&EditorAction::PreviousCell, w, cx)))
                .on_action(cx.listener(|this, _: &Backspace, w, cx| this.handle(&EditorAction::Backspace, w, cx)))
                .on_action(cx.listener(|this, _: &Delete, w, cx| this.handle(&EditorAction::Delete, w, cx)))
                .on_action(cx.listener(|this, _: &Copy, w, cx| this.handle(&EditorAction::Copy, w, cx)))
                .on_action(cx.listener(|this, _: &Cut, w, cx| this.handle(&EditorAction::Cut, w, cx)))
                .on_action(cx.listener(|this, _: &Paste, w, cx| this.handle(&EditorAction::Paste, w, cx)))
                .on_action(cx.listener(|this, _: &Bold, w, cx| this.handle(&EditorAction::Bold, w, cx)))
                .on_action(cx.listener(|this, _: &Italic, w, cx| this.handle(&EditorAction::Italic, w, cx)))
                .on_action(cx.listener(|this, _: &Strike, w, cx| this.handle(&EditorAction::Strike, w, cx)))
                .on_action(cx.listener(|this, _: &Code, w, cx| this.handle(&EditorAction::Code, w, cx)))
                .on_action(cx.listener(|this, _: &Undo, w, cx| this.handle(&EditorAction::Undo, w, cx)))
                .on_action(cx.listener(|this, _: &Redo, w, cx| this.handle(&EditorAction::Redo, w, cx)))
                .on_action(cx.listener(|this, _: &Cancel, w, cx| this.handle(&EditorAction::Cancel, w, cx)))
                .on_action(cx.listener(|this, _: &Save, w, cx| this.handle(&EditorAction::Save, w, cx)))
                .on_mouse_down(MouseButton::Left, cx.listener(|this, event, window, cx| this.pointer_down(event, window, cx)))
                .on_scroll(|_, _, cx| crate::dropdown::dismiss_all(cx))
                .on_mouse_move(cx.listener(|this, event, _, cx| this.pointer_move(event, cx)))
                .on_mouse_up(MouseButton::Left, cx.listener(|this, event: &gpui::MouseUpEvent, _, cx| this.finish_drag(event.position, cx)))
                .on_mouse_up_out(MouseButton::Left, cx.listener(|this, _, _, cx| { this.clear_pointer(); cx.notify(); }))
                // The column is centred after the scrollbar gutter, leaving 248px (224px rail + gutter) for the annotation rail.
                .child(div().flex_1().min_h_0().min_w_0().w_full().relative().flex().pr(px(scrollbar::GUTTER) + rail_w)
                    .on_scroll(scrollbar::forward_wheel(&self.list))
                    .child(div().h_full().min_w_0().w_full().mx_auto().max_w(std_w + rail_w).flex().justify_end()
                        .child(div().h_full().min_w_0().w_full().max_w(std_w)
                            .when(self.minimal, |paper| paper.flex())
                            // Minimal mode pulls the (unused) handle gutter out of view: 20px text padding.
                            .when(self.path.is_some(), |paper| paper.child(rows))
                            .when(self.path.is_none() && !self.persistence.loading, |paper| paper.child(crate::mascot::editor_empty(cx)))))
                    .when(self.path.is_some(), |area| area.child(scrollbar::vertical("editor-scrollbar", &self.list, px(pad.0 + pad.1)))))
                .when_some(self.table_chrome.clone(), |root, chrome| {
                    let dims = self.table_dimensions(chrome.block).unwrap_or((1, 1));
                    root.children(super::table_chrome::overlay(cx.entity(), &chrome, self.table_sel, self.table_menu, self.table_drag, dims))
                })
                .on_drop(cx.listener(|this, paths: &gpui::ExternalPaths, window, cx| {
                    let (id, place) = this.drop_target(window.mouse_position())
                        .unwrap_or((this.caret.head.block, super::image_import::Place::After));
                    this.import_images(id, place, paths.paths().to_vec(), window, cx);
                }))
                .when_some(self.block_menu, |root, menu| {
                    let entries = self.block_menu_entries();
                    if entries.is_empty() { root } else { root.child(super::block_menu::overlay(cx.entity(), menu, entries)) }
                })
                .on_action(cx.listener(|this, _: &SetParagraph, _, cx| this.set_kind_shortcut(super::model::Kind::Paragraph, cx)))
                .on_action(cx.listener(|this, _: &SetH1, _, cx| this.set_kind_shortcut(super::model::Kind::H1, cx)))
                .on_action(cx.listener(|this, _: &SetH2, _, cx| this.set_kind_shortcut(super::model::Kind::H2, cx)))
                .on_action(cx.listener(|this, _: &SetH3, _, cx| this.set_kind_shortcut(super::model::Kind::H3, cx)))
                .on_action(cx.listener(|this, _: &SetTodo, _, cx| this.set_kind_shortcut(super::model::Kind::Todo, cx)))
                .on_action(cx.listener(|this, _: &SetBullet, _, cx| this.set_kind_shortcut(super::model::Kind::Bullet, cx)))
                .on_action(cx.listener(|this, _: &SetNumbered, _, cx| this.set_kind_shortcut(super::model::Kind::Numbered, cx)))
                .on_action(cx.listener(|this, _: &SetCode, _, cx| this.set_code_shortcut(cx)))
                .on_action(cx.listener(|this, _: &SetQuote, _, cx| this.set_kind_shortcut(super::model::Kind::Quote, cx)))
                .on_action(cx.listener(|this, _: &DuplicateBlock, w, cx| this.duplicate_blocks(w, cx)))
                .on_action(cx.listener(|this, _: &CmdEnter, w, cx| this.cmd_enter(w, cx)))
                .on_action(cx.listener(|this, _: &MoveBlockUp, w, cx| this.move_block_shortcut(true, w, cx)))
                .on_action(cx.listener(|this, _: &MoveBlockDown, w, cx| this.move_block_shortcut(false, w, cx)))
                .on_action(cx.listener(|this, _: &DocStart, _, cx| this.doc_edge(false, false, cx)))
                .on_action(cx.listener(|this, _: &DocEnd, _, cx| this.doc_edge(true, false, cx)))
                .on_action(cx.listener(|this, _: &SelectDocStart, _, cx| this.doc_edge(false, true, cx)))
                .on_action(cx.listener(|this, _: &SelectDocEnd, _, cx| this.doc_edge(true, true, cx)))
                .on_action(cx.listener(|this, _: &LineStart, _, cx| this.line_edge(false, false, false, cx)))
                .on_action(cx.listener(|this, _: &LineEnd, _, cx| this.line_edge(true, false, false, cx)))
                .on_action(cx.listener(|this, _: &SelectLineStart, _, cx| this.line_edge(false, true, false, cx)))
                .on_action(cx.listener(|this, _: &SelectLineEnd, _, cx| this.line_edge(true, true, false, cx)))
                .on_action(cx.listener(|this, _: &LogicalLineStart, _, cx| this.line_edge(false, false, true, cx)))
                .on_action(cx.listener(|this, _: &LogicalLineEnd, _, cx| this.line_edge(true, false, true, cx)))
                .on_action(cx.listener(|this, _: &WordLeft, _, cx| this.move_word(false, false, cx)))
                .on_action(cx.listener(|this, _: &WordRight, _, cx| this.move_word(true, false, cx)))
                .on_action(cx.listener(|this, _: &SelectWordLeft, _, cx| this.move_word(false, true, cx)))
                .on_action(cx.listener(|this, _: &SelectWordRight, _, cx| this.move_word(true, true, cx)))
                .on_action(cx.listener(|this, _: &DeleteWordLeft, w, cx| this.delete_word(false, w, cx)))
                .on_action(cx.listener(|this, _: &DeleteWordRight, w, cx| this.delete_word(true, w, cx)))
                .on_action(cx.listener(|this, _: &DeleteLineLeft, w, cx| this.delete_line(false, w, cx)))
                .on_action(cx.listener(|this, _: &DeleteLineRight, w, cx| this.delete_line(true, w, cx)))
                .on_action(cx.listener(|this, _: &TableMenuKey, _, cx| this.open_cell_menu(cx)))
                .on_action(cx.listener(|this, _: &Underline, _, cx| this.underline(cx)))
                .on_action(cx.listener(|this, _: &EditLink, w, cx| this.open_link(w, cx)))
                .on_action(cx.listener(|this, _: &OpenLinkAtCaret, w, cx| this.open_link_at_caret(w, cx)))
                .on_action(cx.listener(|this, _: &ImageGrow, _, cx| this.step_selected_image(10, cx)))
                .on_action(cx.listener(|this, _: &ImageShrink, _, cx| this.step_selected_image(-10, cx)))
                .on_action(cx.listener(|this, _: &ExitBlock, w, cx| this.exit_block(w, cx)))
                .on_action(cx.listener(|this, _: &ExportImage, w, cx| this.export_image(w, cx)))
                .on_action(cx.listener(|this, _: &CopyMarkdown, _, cx| this.copy_markdown(cx)))
                .on_action(cx.listener(|this, _: &Annotate, w, cx| this.start_annotation(w, cx)))
                .on_action(cx.listener(|this, _: &ToggleHighlight, _, cx| this.toggle_highlight(cx)))
                .on_action(cx.listener(|this, _: &ClearFormat, _, cx| this.clear_formatting(cx)))
                .on_action(cx.listener(|this, _: &ToggleFold, _, cx| this.toggle_fold_at_caret(cx)))
                .on_action(cx.listener(|this, _: &PastePlain, _, cx| this.paste_plain(cx)))
                .on_mouse_down(MouseButton::Right, cx.listener(|this, event: &gpui::MouseDownEvent, window, cx| this.right_click(event, window, cx)))
                .when_some(floating, |root, anchor| root.child(gpui::deferred(
                    super::chrome::selection_toolbar(cx.entity(), self.caret.head.block, anchor))))
                .when_some(self.format.clone(), |root, pop| root.child(super::format_ui::popover(cx.entity(), &pop)))
                .children(self.link_overlays(cx.entity(), window))
                .child(anchor_sync))
            .children(super::find_bar::find_bar(self, cx))
            .children(super::annotate_ui::overlay(self, window, cx))
    }
}
