//! The block editor: a column of blocks with one focused caret.

use std::ops::Range;

use gpui::{
    App, Bounds, Context, EntityInputHandler, FocusHandle, Focusable, Pixels, Point,
    UTF16Selection, Window, actions, div, prelude::*,
};

use crate::theme::Theme;

use super::model::Doc;


actions!(
    editor,
    [Backspace, Delete, Left, Right, SelectLeft, SelectRight, SelectAll, Up, Down, Enter, Copy, Cut, Paste, Bold, Italic, Strike, Code]
);

#[derive(Clone, Copy)]
pub(super) enum EditorAction {
    Backspace,
    Delete,
    Left,
    Right,
    SelectLeft,
    SelectRight,
    SelectAll,
    Up,
    Down,
    Enter,
    Copy,
    Cut,
    Paste,
    Bold,
    Italic,
    Strike,
    Code,
}

pub(super) struct Caret {
    pub block: u64,
    pub range: Range<usize>,
    pub reversed: bool,
    pub composing: Option<Range<usize>>,
}

pub struct Editor {
    pub(super) doc: Doc,
    pub(super) focus: FocusHandle,
    pub(super) caret: Caret,
    pub(super) hover: Option<u64>,
    pub(super) dragging: Option<u64>,
    pub(super) drag_over: Option<usize>,
    pub(super) block_bounds: Vec<(u64, Bounds<Pixels>)>,
    pub(super) dirty: bool,
    pub(super) path: Option<std::path::PathBuf>,
}

impl Editor {
    pub fn new(cx: &mut Context<Self>) -> Self {
        let doc = Doc::empty();
        let block = doc.blocks[0].id;
        Self {
            doc,
            focus: cx.focus_handle(),
            caret: Caret { block, range: 0..0, reversed: false, composing: None },
            hover: None,
            dragging: None,
            drag_over: None,
            block_bounds: Vec::new(),
            dirty: false,
            path: None,
        }
    }

    pub fn open(&mut self, path: std::path::PathBuf, cx: &mut Context<Self>) {
        let source = std::fs::read_to_string(&path).unwrap_or_default();
        self.doc = super::markdown::parse(&source);
        self.caret.block = self.doc.blocks[0].id;
        self.caret.range = 0..0;
        self.caret.composing = None;
        self.path = Some(path);
        self.dirty = false;
        cx.notify();
    }

    pub fn export_blocks(&self, selected_only: bool) -> Vec<super::model::Block> {
        if selected_only {
            self.doc.blocks.iter().filter(|block| block.id == self.caret.block).cloned().collect()
        } else {
            self.doc.blocks.clone()
        }
    }

    pub(super) fn focus_block(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        let len = self.doc.blocks.iter().find(|b| b.id == id).map(|b| b.text.len()).unwrap_or(0);
        self.caret.block = id;
        self.move_to(len, cx);
        window.focus(&self.focus);
    }

    fn on_action(&mut self, action: EditorAction, window: &mut Window, cx: &mut Context<Self>) {
        self.handle(&action, window, cx);
    }
}

impl EntityInputHandler for Editor {
    fn text_for_range(
        &mut self,
        range: Range<usize>,
        actual: &mut Option<Range<usize>>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<String> {
        let text = self.block_text();
        let utf8 = super::input::from_utf16(text, &range);
        actual.replace(super::input::to_utf16(text, &utf8));
        text.get(utf8.clone()).map(|slice| slice.to_string()).or(Some(String::new()))
    }

    fn selected_text_range(
        &mut self,
        _: bool,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<UTF16Selection> {
        Some(UTF16Selection {
            range: super::input::to_utf16(self.block_text(), &self.caret.range),
            reversed: self.caret.reversed,
        })
    }

    fn marked_text_range(&self, _: &mut Window, _: &mut Context<Self>) -> Option<Range<usize>> {
        self.caret.composing.as_ref().map(|range| super::input::to_utf16(self.block_text(), range))
    }

    fn unmark_text(&mut self, _: &mut Window, cx: &mut Context<Self>) {
        self.caret.composing = None;
        cx.notify();
    }

    fn replace_text_in_range(
        &mut self,
        range: Option<Range<usize>>,
        text: &str,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.replace(range, text, window, cx);
    }

    fn replace_and_mark_text_in_range(
        &mut self,
        range: Option<Range<usize>>,
        text: &str,
        selected: Option<Range<usize>>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.replace(range, text, window, cx);
        if !text.is_empty() {
            let end = self.caret.range.end;
            let start = end.saturating_sub(text.len());
            self.caret.composing = Some(start..end);
            if let Some(selection) = selected {
                let utf8 = super::input::from_utf16(text, &selection);
                self.caret.range = start + utf8.start..start + utf8.end;
            }
        }
        cx.notify();
    }

    fn bounds_for_range(
        &mut self,
        _: Range<usize>,
        bounds: Bounds<Pixels>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<Bounds<Pixels>> {
        Some(bounds)
    }

    fn character_index_for_point(
        &mut self,
        _: Point<Pixels>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<usize> {
        Some(0)
    }
}

impl Focusable for Editor {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

fn block_element(
    entity: gpui::Entity<Editor>,
    block: super::model::Block,
    hover: Option<u64>,
    caret: u64,
    selected: bool,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    let id = block.id;
    super::chrome::block_row(
        entity.clone(),
        block,
        hover == Some(id),
        selected && caret == id,
        cx.listener(move |this, hovered: &bool, _, cx| {
            this.hover = hovered.then_some(id);
            cx.notify();
        }),
        cx.listener(move |this, _, window, cx| this.focus_block(id, window, cx)),
        cx.listener(move |this, _, window, cx| {
            this.add_below(id, window, cx);
            cx.stop_propagation();
        }),
        cx.listener(move |this, _, _, cx| {
            this.dragging = Some(id);
            cx.stop_propagation();
        }),
        cx.listener(move |this, event: &gpui::MouseMoveEvent, _, cx| {
            if this.dragging == Some(id) {
                this.drag_over = Some(drag_index(&this.block_bounds, event.position.y));
                cx.notify();
            }
        }),
        cx.listener(move |this, _: &gpui::MouseUpEvent, _, cx| {
            let fallback = this.doc.index_of(id).unwrap_or(0);
            this.finish_drag(fallback, cx);
        }),
    )
}

fn drag_index(bounds: &[(u64, Bounds<Pixels>)], y: Pixels) -> usize {
    bounds
        .iter()
        .position(|(_, row)| y < row.bottom())
        .unwrap_or(bounds.len().saturating_sub(1))
}

impl Render for Editor {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let snapshot: Vec<_> = self.doc.blocks.clone();
        let hover = self.hover;
        let caret = self.caret.block;
        let selected = !self.caret.range.is_empty();
        let entity = cx.entity();
        let mut rows = div()
            .id("paper-scroll")
            .w(theme.content_width)
            .overflow_y_scroll()
            .mx_auto()
            .py_8()
            .flex()
            .flex_col()
            .gap_1();
        for block in snapshot {
            rows = rows.child(block_element(entity.clone(), block, hover, caret, selected, cx));
        }
        div()
            .id("editor")
            .key_context("Editor")
            .track_focus(&self.focus)
            .size_full()
            .bg(theme.paper)
            .on_action(cx.listener(|this, _: &Left, window, cx| this.on_action(EditorAction::Left, window, cx)))
            .on_action(cx.listener(|this, _: &Right, window, cx| this.on_action(EditorAction::Right, window, cx)))
            .on_action(cx.listener(|this, _: &SelectLeft, window, cx| this.on_action(EditorAction::SelectLeft, window, cx)))
            .on_action(cx.listener(|this, _: &SelectRight, window, cx| this.on_action(EditorAction::SelectRight, window, cx)))
            .on_action(cx.listener(|this, _: &SelectAll, window, cx| this.on_action(EditorAction::SelectAll, window, cx)))
            .on_action(cx.listener(|this, _: &Up, window, cx| this.on_action(EditorAction::Up, window, cx)))
            .on_action(cx.listener(|this, _: &Down, window, cx| this.on_action(EditorAction::Down, window, cx)))
            .on_action(cx.listener(|this, _: &Enter, window, cx| this.on_action(EditorAction::Enter, window, cx)))
            .on_action(cx.listener(|this, _: &Backspace, window, cx| this.on_action(EditorAction::Backspace, window, cx)))
            .on_action(cx.listener(|this, _: &Delete, window, cx| this.on_action(EditorAction::Delete, window, cx)))
            .on_action(cx.listener(|this, _: &Copy, window, cx| this.on_action(EditorAction::Copy, window, cx)))
            .on_action(cx.listener(|this, _: &Cut, window, cx| this.on_action(EditorAction::Cut, window, cx)))
            .on_action(cx.listener(|this, _: &Paste, window, cx| this.on_action(EditorAction::Paste, window, cx)))
            .on_action(cx.listener(|this, _: &Bold, window, cx| this.on_action(EditorAction::Bold, window, cx)))
            .on_action(cx.listener(|this, _: &Italic, window, cx| this.on_action(EditorAction::Italic, window, cx)))
            .on_action(cx.listener(|this, _: &Strike, window, cx| this.on_action(EditorAction::Strike, window, cx)))
            .on_action(cx.listener(|this, _: &Code, window, cx| this.on_action(EditorAction::Code, window, cx)))
            .child(rows)
    }
}

