//! Selection formatting beyond marks: underline, colour, links and clear-format.
#[path = "palette.rs"]
pub(super) mod palette;

use std::{ops::Range, rc::Rc};
use gpui::{AppContext as _, Context, Entity, Pixels, Point, Subscription, Window, point, px};
use gpui_component::input::{InputEvent, InputState};
use super::{model::{Doc, MarkKind}, view::Editor};

/// Popovers opened from the selection toolbar or inline link actions.
#[derive(Clone)]
pub(super) enum FormatPop {
    Link { input: Entity<InputState>, existing: bool, spec: crate::dropdown::PlacementSpec, _sub: Rc<Subscription> },
    Palette { spec: crate::dropdown::PlacementSpec },
    /// Text-type and list dropdowns of the selection toolbar.
    Kinds { spec: crate::dropdown::PlacementSpec, current: Option<super::model::Kind> },
    Lists { spec: crate::dropdown::PlacementSpec, current: Option<super::model::Kind> },
    Ai(Box<crate::editor::ai::session::AiSession>),
    /// Right-click menu over a non-empty selection; keeps the selection alive.
    SelectionMenu { spec: crate::dropdown::PlacementSpec },
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
pub(super) struct ToolbarState {
    pub bold: bool, pub italic: bool, pub strike: bool, pub code: bool,
    pub underline: bool, pub link: bool, pub colored: bool,
    pub available: ToolbarCapabilities,
}

/// Intersect block capabilities separately from the selection's active formatting.
#[derive(Default, Clone, Copy)]
pub(super) struct ToolbarCapabilities {
    pub block_type: bool,
    pub rich_text: bool,
    pub bold: bool,
    pub link: bool,
}

impl Editor {
    fn toolbar_capabilities(&self, ranges: &[(u64, Range<usize>)]) -> ToolbarCapabilities {
        if !self.is_markdown_note() || self.caret.is_empty() { return ToolbarCapabilities::default(); }
        let (_, end, first, last) = self.ordered();
        // Include empty interior blocks, but exclude a final block touched only at offset zero.
        let mut available = ToolbarCapabilities { block_type: true, rich_text: true, bold: true, link: false };
        let mut table = false;
        for index in first..=last {
            if first != last && index == last && end.offset == 0 { break; }
            let block = &self.doc.blocks[index];
            let rich = block.meta.raw_source.is_none() && block.meta.code_language.is_none();
            table |= block.meta.table.is_some();
            available.block_type &= rich && block.meta.table.is_none();
            available.rich_text &= rich;
            available.bold &= rich && !matches!(block.kind,
                super::model::Kind::H1 | super::model::Kind::H2 | super::model::Kind::H3);
        }
        let grid = ranges.len() > 1 && table || ranges.iter().any(|(id, range)| self.doc.get(*id)
            .and_then(|b| b.meta.table.as_ref()).is_some_and(|t| t.cell_at_range(range).is_none()));
        available.link = available.rich_text && !ranges.is_empty() && !grid;
        available
    }

    pub(super) fn toolbar_state(&self) -> ToolbarState {
        let ranges = self.selection_ranges();
        let mut state = ToolbarState { available: self.toolbar_capabilities(&ranges), ..Default::default() };
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
        if !self.is_markdown_note() { return; }
        let on = !self.selection_ranges().iter().all(|(id, r)| self.doc.is_underlined(*id, r.clone()));
        self.edit_selection(cx, |doc, id, range| doc.toggle_underline(id, range, on));
    }
    pub(super) fn clear_formatting(&mut self, cx: &mut Context<Self>) {
        if !self.is_markdown_note() { return; }
        self.edit_selection(cx, |doc, id, range| doc.clear_format(id, range));
    }
    /// Toggles the yellow highlight on the selection (⌘⇧H).
    pub(super) fn toggle_highlight(&mut self, cx: &mut Context<Self>) {
        if !self.is_markdown_note() || self.selection_ranges().is_empty() { return; }
        let on = matches!(self.selection_colors().1, palette::SelectionColor::Single("yellow"));
        self.apply_style("background-color", (!on).then_some("yellow"), cx);
    }
    /// Pastes the clipboard text verbatim, skipping link, block and table conversion (⌘⇧V).
    pub(super) fn paste_plain(&mut self, cx: &mut Context<Self>) {
        if self.editing_source() { self.source_paste(None, cx); return; }
        if !self.is_markdown_note() { return; }
        if let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) { self.paste_text(&text, true, false, None, cx); }
    }
    /// `prop` is `color` or `background-color`; `None` resets to the default.
    pub(super) fn apply_style(&mut self, prop: &str, value: Option<&str>, cx: &mut Context<Self>) {
        self.edit_selection(cx, |doc, id, range| doc.set_style(id, range, prop, value));
    }
    fn pop_spec(&self, window: &Window) -> crate::dropdown::PlacementSpec {
        let vp = self.list.viewport_bounds();
        let sel_rect = self
            .selection_ranges()
            .into_iter()
            .filter_map(|(id, r)| {
                self.layouts
                    .get(&id)?
                    .rects(r)
                    .into_iter()
                    .reduce(|a, b| a.union(&b))
            })
            .reduce(|a, b| a.union(&b));
        if let Some(rect) = sel_rect {
            crate::dropdown::PlacementSpec::below(rect).viewport(vp).gap(px(6.))
        } else if let Some(anchor) = self.toolbar_anchor {
            crate::dropdown::PlacementSpec::below(gpui::Bounds::new(anchor, gpui::size(px(240.), px(40.))))
                .viewport(vp)
                .gap(px(4.))
        } else {
            crate::dropdown::PlacementSpec::point(window.mouse_position() + point(px(0.), px(12.))).viewport(vp)
        }
    }
    #[allow(dead_code)]
    pub(super) fn open_palette(&mut self, window: &Window, cx: &mut Context<Self>) {
        if self.selection_ranges().is_empty() { return; }
        self.format = Some(FormatPop::Palette { spec: self.pop_spec(window) });
        cx.notify();
    }
    pub(super) fn show_link_popover(&mut self, initial: String, existing: bool, spec: crate::dropdown::PlacementSpec, window: &mut Window, cx: &mut Context<Self>) {
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
        self.format = Some(FormatPop::Link { input, existing, spec, _sub: Rc::new(sub) });
        cx.notify();
    }
    pub(super) fn open_link(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if let Some((id, range)) = self.selection_ranges().first().cloned() {
            let current = self.doc.get(id).and_then(|b| b.link_at(range.start)).map(|(_, url)| url);
            let existing = current.is_some();
            let spec = self.pop_spec(window);
            self.show_link_popover(current.unwrap_or_default(), existing, spec, window, cx);
            return;
        }
        let at = self.caret.head;
        if let Some((range, url)) = self.doc.get(at.block).and_then(|b| b.link_at(at.offset)) {
            self.caret.anchor = super::view::selection::Position { block: at.block, offset: range.start };
            self.caret.head = super::view::selection::Position { block: at.block, offset: range.end };
            let spec = self
                .layouts
                .get(&at.block)
                .and_then(|l| l.rects(range).first().copied())
                .map(|r| crate::dropdown::PlacementSpec::below(r).viewport(self.list.viewport_bounds()).gap(px(4.)))
                .unwrap_or_else(|| self.pop_spec(window));
            self.show_link_popover(url, true, spec, window, cx);
            return;
        }
        cx.propagate();
    }
    /// Copies the whole note as Markdown (the page menu's 「复制 Markdown」).
    pub(crate) fn copy_markdown(&mut self, cx: &mut Context<Self>) {
        if !self.is_markdown_note() { return; }
        cx.write_to_clipboard(gpui::ClipboardItem::new_string_with_metadata(
            super::markdown::serialize(&self.doc), super::paste::MARKDOWN_CLIPBOARD_V1.to_owned()));
        self.status = crate::theme::tr("已复制 Markdown", "Copied Markdown").into();
        cx.notify();
    }
    /// Opens the 「生成图片」 view for the selection, or the whole note when nothing is selected.
    pub(crate) fn export_image(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.is_markdown_note() { return; }
        self.format = None;
        if self.selection_ranges().is_empty() { super::export_document(self, window, cx) } else { super::export_selected(self, window, cx) }
    }
    /// Right button: over a selection it opens the context menu, otherwise it moves the caret
    /// (and opens the table menu inside a table cell).
    pub(super) fn right_click(&mut self, event: &gpui::MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        if !self.selection_ranges().is_empty() {
            self.table_menu = None; self.block_menu = None;
            let spec = crate::dropdown::PlacementSpec::point(event.position).viewport(self.list.viewport_bounds());
            super::menu_keys::arm_format(0);
            self.format = Some(FormatPop::SelectionMenu { spec });
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
