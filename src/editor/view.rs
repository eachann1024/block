//! Editor entity: shared state, virtual list, and public document lifecycle.
use std::{collections::HashMap, path::PathBuf};
use gpui::{App, Context, FocusHandle, Focusable, ListAlignment, ListState, MouseButton,
    Pixels, Window, actions, div, list, prelude::*, px};
use crate::theme::Theme;
use super::model::Doc;
use super::input::transactions::{Snapshot, HistoryEntry};
#[path = "selection.rs"] pub(super) mod selection;
#[path = "layout.rs"] pub(super) mod layout;
#[path = "syntax.rs"] pub(super) mod syntax;
#[path = "protocol.rs"] mod protocol;
#[path = "storage.rs"] mod storage;
#[path = "lifecycle.rs"] mod lifecycle;
use selection::Caret;
use layout::TextLayout;
use lifecycle::Persistence;

actions!(editor, [Backspace, Delete, Left, Right, SelectLeft, SelectRight, SelectAll,
    Up, Down, SelectUp, SelectDown, Enter, Copy, Cut, Paste, Bold, Italic, Strike, Code,
    Undo, Redo, Cancel, Save]);
pub(super) enum EditorAction {
    Backspace, Delete, Left, Right, SelectLeft, SelectRight, SelectAll, Up, Down,
    SelectUp, SelectDown, Enter, Copy, Cut, Paste, Bold, Italic, Strike, Code, Undo, Redo, Cancel, Save,
}
pub struct Editor {
    pub(super) doc: Doc,
    pub(super) focus: FocusHandle,
    pub(super) caret: Caret,
    pub(super) hover: Option<u64>,
    pub(super) dragging: Option<u64>,
    pub(super) drag_over: Option<usize>,
    pub(super) selecting: bool,
    pub(super) layouts: HashMap<u64, TextLayout>,
    pub(super) list: ListState,
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
}
impl Editor {
    pub fn new(cx: &mut Context<Self>) -> Self {
        cx.on_app_quit(|editor, _| { editor.prepare_to_quit(); async {} }).detach();
        let doc = Doc::empty(); let caret = Caret::new(doc.blocks[0].id);
        Self { doc, caret, focus: cx.focus_handle(), hover: None, dragging: None,
            drag_over: None, selecting: false, layouts: HashMap::new(),
            list: ListState::new(1, ListAlignment::Top, px(120.)), preferred_x: None, pending_vertical: None,
            reveal_caret: false, dirty: false, path: None, status: String::new(),
            history: Vec::new(), redo: Vec::new(), composition_before: None,
            persistence: Persistence::default() }
    }
    fn reveal(&mut self) {
        if !self.reveal_caret { return; }
        if let Some(layout) = self.layouts.get(&self.caret.head.block) {
            let p = layout.point(self.caret.head.offset); let viewport = self.list.viewport_bounds();
            if p.y < viewport.top() { self.list.scroll_by(p.y - viewport.top()); }
            else if p.y + layout.line_height > viewport.bottom() {
                self.list.scroll_by(p.y + layout.line_height - viewport.bottom());
            }
        } else if let Some(index) = self.doc.index_of(self.caret.head.block) {
            self.list.scroll_to(gpui::ListOffset { item_ix: index, offset_in_item: px(0.) });
        }
    }
}
impl Focusable for Editor { fn focus_handle(&self, _: &App) -> FocusHandle { self.focus.clone() } }
impl Render for Editor {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean(); self.reveal();
        let input_editor = cx.entity();
        let input = gpui::canvas(|_, _, _| (), move |bounds, (), window, cx| {
            input_editor.update(cx, |this, _| { this.layouts.clear(); });
            let editor = input_editor.read(cx);
            if editor.path.is_some() {
                window.handle_input(&editor.focus, gpui::ElementInputHandler::new(bounds, input_editor.clone()), cx);
            }
        }).absolute().size_full();
        let editor = cx.entity();
        let rows = list(self.list.clone(), move |index, _, cx| {
            editor.update(cx, |this, cx| {
                let block = &this.doc.blocks[index];
                let number = this.doc.list_number(block.id);
                super::chrome::block_row(cx.entity(), block, this.hover == Some(block.id), this.drag_over == Some(index), number, cx).into_any_element()
            })
        }).size_full();
        div().id("editor").key_context("Editor").track_focus(&self.focus).size_full()
            .relative().flex().flex_col().bg(theme.paper)
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
            .on_mouse_move(cx.listener(|this, event, _, cx| this.pointer_move(event, cx)))
            .on_mouse_up(MouseButton::Left, cx.listener(|this, _, _, cx| this.finish_drag(cx)))
            .on_mouse_up_out(MouseButton::Left, cx.listener(|this, _, _, cx| { this.dragging = None; this.drag_over = None; this.selecting = false; cx.notify(); }))
            .child(super::chrome::toolbar(cx.entity(), self.path.is_some(), self.caret.head.block))
            .child(div().flex_1().min_h_0().w_full().max_w(theme.content_width).mx_auto()
                .when(self.path.is_some(), |paper| paper.child(rows))
                .when(self.path.is_none(), |paper| paper.flex().flex_col().gap_3().items_center().justify_center()
                    .text_color(theme.text_secondary).child("Select a document to start editing")
                    .child(div().id("recover-draft").px_3().py_1().rounded_md().bg(theme.hover)
                        .cursor(gpui::CursorStyle::PointingHand).child("Recover latest draft")
                        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.recover_latest_draft(cx))))))
            .child(div().min_h(px(26.)).flex_none().px_3().flex().items_center().gap_3().text_size(px(12.)).text_color(theme.text_secondary)
                .child(div().flex_1().child(self.status.clone()))
                .when(self.path.is_some() && self.dirty, |bar| bar.child(div().id("keep-draft-close")
                    .px_2().py_1().rounded_md().bg(theme.hover).cursor(gpui::CursorStyle::PointingHand)
                    .child("Keep draft and close")
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.keep_draft_and_close(cx); })))))
    }
}
