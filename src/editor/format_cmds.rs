//! Selection formatting beyond marks: underline, colour, links and clear-format.
use std::ops::Range;
use gpui::{Context, Pixels, Point, Window, point, px};
use super::{model::{Doc, MarkKind}, view::Editor};

/// Popovers opened from the selection toolbar. `anchor` is fixed at open time
/// because the toolbar hides while the link field owns focus.
#[derive(Clone)]
pub(super) enum FormatPop {
    Link { url: String, existing: bool, anchor: Point<Pixels> },
    Palette { anchor: Point<Pixels> },
    /// Text-type and list dropdowns of the selection toolbar.
    Kinds { anchor: Point<Pixels>, current: Option<super::model::Kind> },
    Lists { anchor: Point<Pixels>, current: Option<super::model::Kind> },
    Ai(Box<crate::editor::ai::session::AiSession>),
}

/// Bare domains get `https://`; anything with a scheme, path or fragment is kept.
pub(super) fn normalize_url(raw: &str) -> String {
    let url = raw.trim();
    let has_scheme = url.split_once(':').is_some_and(|(s, _)| !s.is_empty() && s.chars().all(|c| c.is_ascii_alphanumeric() || "+-.".contains(c)));
    if url.is_empty() || has_scheme || url.starts_with(['/', '#', '.']) || !url.contains('.') { url.to_owned() } else { format!("https://{url}") }
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
        if ranges.is_empty() || state.table { return state; }
        let all = |mark: MarkKind| ranges.iter().all(|(id, r)| self.doc.get(*id).is_some_and(|b| {
            b.spans.iter().filter(|s| s.marks.contains(&mark)).map(|s| s.end.min(r.end).saturating_sub(s.start.max(r.start))).sum::<usize>() >= r.len()
        }));
        state.bold = all(MarkKind::Bold); state.italic = all(MarkKind::Italic);
        state.strike = all(MarkKind::Strike); state.code = all(MarkKind::Code);
        state.underline = ranges.iter().all(|(id, r)| self.doc.is_underlined(*id, r.clone()));
        state.link = ranges.iter().any(|(id, r)| self.doc.get(*id).and_then(|b| b.link_at(r.start)).is_some());
        state.colored = ranges.iter().any(|(id, r)| self.doc.get(*id).is_some_and(|b| b.meta.inline.iter().any(|t|
            t.name == "span" && t.start < r.end && t.end > r.start
            && ["color", "background-color"].iter().any(|p| crate::editor::text_runs::css(&t.attrs, p).is_some()))));
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
    /// `prop` is `color` or `background-color`; `None` resets to the default.
    pub(super) fn apply_style(&mut self, prop: &str, value: Option<&str>, cx: &mut Context<Self>) {
        self.edit_selection(cx, |doc, id, range| doc.set_style(id, range, prop, value));
    }
    fn pop_anchor(&self, window: &Window) -> Point<Pixels> {
        self.toolbar_anchor.unwrap_or_else(|| window.mouse_position() + point(px(0.), px(12.)))
    }
    pub(super) fn open_palette(&mut self, window: &Window, cx: &mut Context<Self>) {
        if self.selection_ranges().is_empty() { return; }
        self.format = Some(FormatPop::Palette { anchor: self.pop_anchor(window) + point(px(0.), px(40.)) });
        cx.notify();
    }
    pub(super) fn open_link(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some((id, range)) = self.selection_ranges().first().cloned() else { return };
        let current = self.doc.get(id).and_then(|b| b.link_at(range.start)).map(|(_, url)| url);
        self.format = Some(FormatPop::Link { existing: current.is_some(), url: current.unwrap_or_default(), anchor: self.pop_anchor(window) });
        window.focus(&self.link_focus);
        cx.notify();
    }
    pub(super) fn close_format(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if let Some(FormatPop::Ai(s)) = self.format.as_ref() { s.cancel.store(true, std::sync::atomic::Ordering::Relaxed); }
        if self.format.take().is_some() { window.focus(&self.focus); }
        cx.notify();
    }
    pub(super) fn link_edit(&mut self, f: impl FnOnce(&mut String), cx: &mut Context<Self>) {
        if let Some(FormatPop::Link { url, .. }) = self.format.as_mut() { f(url); cx.notify(); }
    }
    /// Saves the field; an empty URL removes the link, like thinkly's 移除.
    pub(super) fn link_save(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(FormatPop::Link { url, .. }) = self.format.clone() else { return };
        let href = normalize_url(&url);
        self.edit_selection(cx, |doc, id, range| doc.set_link(id, range, (!href.is_empty()).then_some(href.as_str())));
        self.close_format(window, cx);
    }
    pub(super) fn link_remove(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.edit_selection(cx, |doc, id, range| doc.set_link(id, range, None));
        self.close_format(window, cx);
    }
    /// Cmd-click on a link opens it instead of moving the caret.
    pub(super) fn open_link_at(&mut self, position: Point<Pixels>, cx: &mut Context<Self>) -> bool {
        let Some(at) = self.hit(position) else { return false };
        let Some((_, url)) = self.doc.get(at.block).and_then(|b| b.link_at(at.offset)) else { return false };
        cx.open_url(&url); true
    }
}
