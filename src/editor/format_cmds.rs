//! Selection formatting beyond marks: underline, colour, links and clear-format.
#[path = "palette.rs"]
pub(super) mod palette;

use std::{ops::Range, rc::Rc};
use gpui::{AppContext as _, Context, Entity, Pixels, Point, Subscription, Window, point, px};
use gpui_component::input::{InputEvent, InputState};
use super::{model::{Doc, MarkKind}, view::Editor};

/// Popovers opened from the selection toolbar. `anchor` is fixed at open time
/// because the toolbar hides while the link field owns focus.
#[derive(Clone)]
pub(super) enum FormatPop {
    Link { input: Entity<InputState>, existing: bool, anchor: Point<Pixels>, _sub: Rc<Subscription> },
    Palette { anchor: Point<Pixels> },
    /// Text-type and list dropdowns of the selection toolbar.
    Kinds { anchor: Point<Pixels>, current: Option<super::model::Kind> },
    Lists { anchor: Point<Pixels>, current: Option<super::model::Kind> },
    Ai(Box<crate::editor::ai::session::AiSession>),
    /// Right-click menu over a non-empty selection; keeps the selection alive.
    SelectionMenu { anchor: Point<Pixels> },
}

/// Bare domains get `https://`, bare emails get `mailto:`; anything with a scheme, path or fragment is kept.
pub(crate) fn normalize_url(raw: &str) -> String {
    let url = raw.trim();
    let has_scheme = url.split_once(':').is_some_and(|(s, _)| !s.is_empty() && s.chars().all(|c| c.is_ascii_alphanumeric() || "+-.".contains(c)));
    if url.is_empty() || has_scheme || url.starts_with(['/', '#', '.']) || !url.contains('.') {
        url.to_owned()
    } else if url.contains('@') && !url.contains('/') {
        format!("mailto:{url}")
    } else {
        format!("https://{url}")
    }
}

/// Opens a URL or local path via the system default app on a background thread so AppKit
/// activation callbacks (`windowDidResignKey:`) never re-enter GPUI's `AppCell` borrow.
pub(crate) fn open_external_url(raw: &str, base_note: Option<&std::path::Path>) {
    let trimmed = raw.trim();
    if trimmed.is_empty() { return; }
    let target = base_note
        .and_then(|p| p.parent())
        .and_then(|parent| {
            let candidate = parent.join(trimmed);
            candidate.exists().then(|| candidate.to_string_lossy().into_owned())
        })
        .unwrap_or_else(|| normalize_url(trimmed));
    if target.is_empty() { return; }
    #[cfg(target_family = "wasm")]
    {
        if let Some(win) = web_sys::window() { let _ = win.open_with_url_and_target(&target, "_blank"); }
        return;
    }
    #[cfg(not(target_family = "wasm"))]
    std::thread::spawn(move || {
        use std::process::{Command, Stdio};
        #[cfg(target_os = "macos")]
        let res = Command::new("/usr/bin/open")
            .arg(&target)
            .stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null())
            .status();
        #[cfg(target_os = "linux")]
        let res = Command::new("xdg-open")
            .arg(&target)
            .stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null())
            .status();
        #[cfg(target_os = "windows")]
        let res = Command::new("rundll32")
            .args(["url.dll,FileProtocolHandler", &target])
            .stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null())
            .status();
        if let Err(err) = res {
            crate::log::warn(&format!("failed to open url {target}: {err}"));
        }
    });
}

/// Pressed states for the selection toolbar; a mark is on only when it covers every selected character.
#[derive(Default, Clone, Copy)]
pub(super) struct ToolbarState { pub bold: bool, pub italic: bool, pub strike: bool, pub code: bool, pub underline: bool, pub link: bool, pub colored: bool, pub table: bool, pub grid: bool }

impl Editor {
    pub(super) fn toolbar_state(&self) -> ToolbarState {
        let ranges = self.selection_ranges();
        let mut state = ToolbarState { table: ranges.iter().any(|(id, _)| self.doc.get(*id).is_some_and(|b| b.meta.table.is_some())), ..Default::default() };
        state.grid = ranges.len() > 1 && state.table || ranges.iter().any(|(id, r)| self.doc.get(*id)
            .and_then(|b| b.meta.table.as_ref()).is_some_and(|t| t.cell_at_range(r).is_none()));
        if ranges.is_empty() { return state; }
        let span_cov = |spans: &[super::model::Span], r: &Range<usize>, mark: MarkKind| {
            spans.iter().filter(|s| s.marks.contains(&mark)).map(|s| s.end.min(r.end).saturating_sub(s.start.max(r.start))).sum::<usize>() >= r.len()
        };
        let all = |mark: MarkKind| ranges.iter().all(|(id, r)| self.doc.get(*id).is_some_and(|b| {
            if let Some(table) = &b.meta.table {
                let mut any = false;
                for (row, cell, body) in table.cell_ranges() {
                    let start = r.start.max(body.start).min(body.end) - body.start;
                    let end = r.end.max(body.start).min(body.end) - body.start;
                    if start < end {
                        any = true;
                        if !span_cov(&table.rows[row].cells[cell].content.spans, &(start..end), mark) { return false; }
                    }
                }
                any
            } else {
                span_cov(&b.spans, r, mark)
            }
        }));
        state.bold = all(MarkKind::Bold); state.italic = all(MarkKind::Italic);
        state.strike = all(MarkKind::Strike); state.code = all(MarkKind::Code);
        state.underline = ranges.iter().all(|(id, r)| self.doc.is_underlined(*id, r.clone()));
        state.link = ranges.iter().any(|(id, r)| self.doc.get(*id).and_then(|b| b.link_at(r.start)).is_some());
        state.colored = self.has_selection_color();
        state
    }
    pub(super) fn selection_ranges(&self) -> Vec<(u64, Range<usize>)> {
        if self.path.is_none() || self.caret.is_empty() { return Vec::new(); }
        let (_, _, start, end) = self.ordered();
        (start..=end).filter_map(|i| {
            let id = self.doc.blocks[i].id;
            self.selected_range(id).filter(|r| !r.is_empty()).map(|r| (id, r))
        }).collect()
    }
    fn edit_selection(&mut self, cx: &mut Context<Self>, mut f: impl FnMut(&mut Doc, u64, Range<usize>)) {
        let ranges = self.selection_ranges();
        if ranges.is_empty() { return; }
        self.commit_composition(cx);
        let before = self.snapshot();
        for (id, range) in ranges { f(&mut self.doc, id, range); }
        self.record(before, cx);
    }
    pub(super) fn underline(&mut self, cx: &mut Context<Self>) {
        let on = !self.selection_ranges().iter().all(|(id, r)| self.doc.is_underlined(*id, r.clone()));
        self.edit_selection(cx, |doc, id, range| doc.toggle_underline(id, range, on));
    }
    pub(super) fn clear_formatting(&mut self, cx: &mut Context<Self>) {
        self.edit_selection(cx, |doc, id, range| doc.clear_format(id, range));
    }
    /// Toggles the yellow highlight on the selection (⌘⇧H).
    pub(super) fn toggle_highlight(&mut self, cx: &mut Context<Self>) {
        if self.selection_ranges().is_empty() { return; }
        let on = matches!(self.selection_colors().1, palette::SelectionColor::Single("yellow"));
        self.apply_style("background-color", (!on).then_some("yellow"), cx);
    }
    /// Pastes the clipboard text verbatim, skipping link, block and table conversion (⌘⇧V).
    pub(super) fn paste_plain(&mut self, cx: &mut Context<Self>) {
        if let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) { self.replace_selection(&text, false, cx); }
    }
    /// `prop` is `color` or `background-color`; `None` resets to the default.
    pub(super) fn apply_style(&mut self, prop: &str, value: Option<&str>, cx: &mut Context<Self>) {
        self.edit_selection(cx, |doc, id, range| doc.set_style(id, range, prop, value));
    }
    fn pop_anchor(&self, window: &Window) -> Point<Pixels> {
        self.toolbar_anchor.unwrap_or_else(|| window.mouse_position() + point(px(0.), px(12.)))
    }
    #[allow(dead_code)]
    pub(super) fn open_palette(&mut self, window: &Window, cx: &mut Context<Self>) {
        if self.selection_ranges().is_empty() { return; }
        self.format = Some(FormatPop::Palette { anchor: self.pop_anchor(window) + point(px(0.), px(40.)) });
        cx.notify();
    }
    pub(super) fn show_link_popover(&mut self, initial: String, existing: bool, anchor: Point<Pixels>, window: &mut Window, cx: &mut Context<Self>) {
        let input = cx.new(|cx| {
            let mut state = InputState::new(window, cx).placeholder(crate::theme::tr("粘贴或输入链接", "Paste or enter a link"));
            if !initial.is_empty() { state.set_value(initial, window, cx); }
            state
        });
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| {
            if let InputEvent::PressEnter { secondary: false, .. } = event { this.link_save(window, cx); }
        });
        let focus_input = input.clone();
        cx.defer_in(window, move |_, window, cx| {
            focus_input.update(cx, |s, cx| s.focus(window, cx));
        });
        self.format = Some(FormatPop::Link { input, existing, anchor, _sub: Rc::new(sub) });
        cx.notify();
    }
    pub(super) fn open_link(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if let Some((id, range)) = self.selection_ranges().first().cloned() {
            let current = self.doc.get(id).and_then(|b| b.link_at(range.start)).map(|(_, url)| url);
            let existing = current.is_some();
            let anchor = self.pop_anchor(window);
            self.show_link_popover(current.unwrap_or_default(), existing, anchor, window, cx);
            return;
        }
        let at = self.caret.head;
        if let Some((range, url)) = self.doc.get(at.block).and_then(|b| b.link_at(at.offset)) {
            self.caret.anchor = super::view::selection::Position { block: at.block, offset: range.start };
            self.caret.head = super::view::selection::Position { block: at.block, offset: range.end };
            let anchor = self.layouts.get(&at.block).and_then(|l| l.rects(range).first().copied())
                .map(|r| point(r.left(), r.bottom())).unwrap_or_else(|| self.pop_anchor(window));
            self.show_link_popover(url, true, anchor, window, cx);
            return;
        }
        cx.propagate();
    }
    /// Copies the whole note as Markdown (the page menu's 「复制 Markdown」).
    pub(crate) fn copy_markdown(&mut self, cx: &mut Context<Self>) {
        cx.write_to_clipboard(gpui::ClipboardItem::new_string(super::markdown::serialize(&self.doc)));
        self.status = crate::theme::tr("已复制 Markdown", "Copied Markdown").into();
        cx.notify();
    }
    /// Opens the 「生成图片」 view for the selection, or the whole note when nothing is selected.
    pub(crate) fn export_image(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.format = None;
        if self.selection_ranges().is_empty() { super::export_document(self, window, cx) } else { super::export_selected(self, window, cx) }
    }
    /// Right button: over a selection it opens the context menu, otherwise it moves the caret
    /// (and opens the table menu inside a table cell).
    pub(super) fn right_click(&mut self, event: &gpui::MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        if !self.selection_ranges().is_empty() {
            self.table_menu = None; self.block_menu = None;
            self.format = Some(FormatPop::SelectionMenu { anchor: event.position });
            cx.notify();
            return;
        }
        self.pointer_down(&gpui::MouseDownEvent { button: gpui::MouseButton::Left, click_count: 1, ..event.clone() }, window, cx);
        self.selecting = false;
        if let Some((id, r, c)) = self.cell_of_caret() { self.open_table_menu(id, super::table_actions::TableSel::Cell(r, c), event.position, cx); }
    }
    pub(super) fn close_format(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if let Some(FormatPop::Ai(s)) = self.format.as_ref() { s.cancel.store(true, std::sync::atomic::Ordering::Relaxed); }
        if self.format.take().is_some() { window.focus(&self.focus, cx); }
        cx.notify();
    }
    /// Saves the field; an empty URL removes the link, like thinkly's 移除.
    pub(super) fn link_save(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(FormatPop::Link { input, .. }) = self.format.clone() else { return };
        let url = input.read(cx).value().to_string();
        let href = normalize_url(&url);
        self.edit_selection(cx, |doc, id, range| doc.set_link(id, range, (!href.is_empty()).then_some(href.as_str())));
        self.close_format(window, cx);
    }
    pub(super) fn link_remove(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.edit_selection(cx, |doc, id, range| doc.set_link(id, range, None));
        self.close_format(window, cx);
    }
    /// Cmd/Ctrl-click on a link opens it instead of moving the caret.
    pub(super) fn open_link_at(&mut self, position: Point<Pixels>, _cx: &mut Context<Self>) -> bool {
        let Some((block, range)) = self.link_under(position) else { return false };
        let Some((_, url)) = self.doc.get(block).and_then(|b| b.link_at(range.start)) else { return false };
        open_external_url(&url, self.path.as_deref());
        true
    }
}
