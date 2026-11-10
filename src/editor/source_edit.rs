//! Raw-source edits. Formatting and Markdown input rules never run here.
use std::ops::Range;
use gpui::{ClipboardItem, Context, Pixels, Point, Window, px};
use super::source_text::{Edit, Newline, SourceDoc, column_of, diff_edit, offset_of_column, step_offset};
use super::view::{Editor, EditorAction};

impl Editor {
    pub(super) fn handle_source(&mut self, action: &EditorAction, window: &mut Window, cx: &mut Context<Self>) {
        use EditorAction::*;
        if !matches!(action, Cancel) { self.finish_source_composition(cx); }
        let vertical = matches!(action, Up | Down | SelectUp | SelectDown);
        if !vertical { if let Some(doc) = self.source_doc_mut() { doc.preferred_col = None; } }
        match action {
            Left | Right | SelectLeft | SelectRight => self.source_step(matches!(action, Right | SelectRight), matches!(action, SelectLeft | SelectRight), cx),
            Up | Down | SelectUp | SelectDown => self.source_vertical(matches!(action, Down | SelectDown), matches!(action, SelectUp | SelectDown), cx),
            SelectAll => self.source_select_all(cx),
            Backspace | Delete => self.source_delete(matches!(action, Delete), cx),
            Enter => self.source_insert_newline(cx),
            NextCell => self.source_insert("\t", cx),
            Copy => self.source_copy(false, cx),
            Cut => self.source_copy(true, cx),
            Paste => self.source_paste(None, cx),
            Undo => self.source_undo(false, cx),
            Redo => self.source_undo(true, cx),
            Save => self.save(cx),
            Cancel => { self.find = None; cx.notify(); }
            _ => {}
        }
        let _ = window;
    }
    pub(super) fn source_line_edge(&mut self, to_end: bool, extend: bool, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc() else { return };
        let index = doc.text.line_of(doc.head);
        let (start, visible) = doc.text.line_visible(index);
        let next = if to_end { start + visible.len() } else { start };
        self.source_place(next, extend, cx);
    }
    pub(super) fn source_doc_edge(&mut self, end: bool, extend: bool, cx: &mut Context<Self>) {
        let Some(len) = self.source_doc().map(|doc| doc.text.len()) else { return };
        self.source_place(if end { len } else { 0 }, extend, cx);
    }
    pub(super) fn finish_source_composition(&mut self, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc_mut() else { return };
        doc.composing = None;
        let Some(before) = doc.compose_before.take() else { return };
        let after = doc.text.body().to_owned();
        if let Some(edit) = diff_edit(&before, &after) { push_edit(doc, edit); }
        if before != after { self.touch(cx); } else { cx.notify(); }
    }
    pub(super) fn source_replace_utf16(&mut self, range: Option<Range<usize>>, text: &str, cx: &mut Context<Self>) {
        self.finish_source_composition(cx);
        let Some(doc) = self.source_doc() else { return };
        let body = doc.text.body().to_owned();
        let range = range.map(|r| super::input::from_utf16(&body, &r)).unwrap_or_else(|| doc.selection());
        self.source_apply(range, text, cx);
    }
    pub(super) fn source_mark(&mut self, range: Option<Range<usize>>, text: &str, selected: Option<Range<usize>>, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc_mut() else { return };
        if doc.compose_before.is_none() { doc.compose_before = Some(doc.text.body().to_owned()); }
        let body = doc.text.body().to_owned();
        let range = range.map(|r| super::input::from_utf16(&body, &r)).unwrap_or_else(|| doc.composing.clone().unwrap_or_else(|| doc.selection()));
        let placed = doc.text.replace(range, text);
        doc.note_highlight();
        doc.head = placed.end;
        doc.anchor = placed.end;
        if let Some(selected) = selected {
            let local = super::input::from_utf16(text, &selected);
            doc.anchor = placed.start + local.start;
            doc.head = placed.start + local.end;
        }
        doc.composing = (!text.is_empty()).then_some(placed.start..placed.end);
        doc.rev = doc.rev.wrapping_add(1);
        doc.reveal = true;
        doc.sync_list();
        self.source_layouts.clear();
        cx.notify();
    }
    fn source_step(&mut self, forward: bool, extend: bool, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc() else { return };
        let at = if !extend && doc.anchor != doc.head {
            let sel = doc.selection();
            if forward { sel.end } else { sel.start }
        } else { step_offset(doc.text.body(), doc.head, forward) };
        self.source_place(at, extend, cx);
    }
    fn source_vertical(&mut self, down: bool, extend: bool, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc_mut() else { return };
        let index = doc.text.line_of(doc.head);
        let (start, visible) = doc.text.line_visible(index);
        let col = doc.preferred_col.unwrap_or_else(|| column_of(visible, doc.head.saturating_sub(start)));
        doc.preferred_col = Some(col);
        let next = if down { index + 1 } else { index.saturating_sub(1) };
        if next == index { return; }
        if next >= doc.text.line_count() { return; }
        let (nstart, nvis) = doc.text.line_visible(next);
        let at = nstart + offset_of_column(nvis, col);
        let keep = doc.preferred_col;
        self.source_place(at, extend, cx);
        if let Some(doc) = self.source_doc_mut() { doc.preferred_col = keep; }
    }
    pub(super) fn source_word(&mut self, forward: bool, extend: bool, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc() else { return };
        let body = doc.text.body();
        let mut at = doc.head;
        if forward {
            while at < body.len() && !word_char(body, at) { at = step_offset(body, at, true); }
            while at < body.len() && word_char(body, at) { at = step_offset(body, at, true); }
        } else {
            while at > 0 && !word_char(body, step_offset(body, at, false)) { at = step_offset(body, at, false); }
            while at > 0 && word_char(body, step_offset(body, at, false)) { at = step_offset(body, at, false); }
        }
        self.source_place(at, extend, cx);
    }
    fn source_select_all(&mut self, cx: &mut Context<Self>) {
        let Some(len) = self.source_doc().map(|d| d.text.len()) else { return };
        if let Some(doc) = self.source_doc_mut() { doc.anchor = 0; doc.head = len; doc.reveal = true; }
        cx.notify();
    }
    fn source_insert_newline(&mut self, cx: &mut Context<Self>) {
        let nl = self.source_doc().map(|d| d.newline).unwrap_or(Newline::Lf);
        self.source_insert(nl.as_str(), cx);
    }
    fn source_insert(&mut self, text: &str, cx: &mut Context<Self>) {
        let range = self.source_doc().map(|d| d.selection()).unwrap_or(0..0);
        self.source_apply(range, text, cx);
    }
    fn source_delete(&mut self, forward: bool, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc() else { return };
        let range = if doc.anchor != doc.head { doc.selection() } else if forward {
            doc.head..step_offset(doc.text.body(), doc.head, true)
        } else {
            step_offset(doc.text.body(), doc.head, false)..doc.head
        };
        self.source_apply(range, "", cx);
    }
    pub(super) fn source_delete_word(&mut self, forward: bool, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc() else { return };
        if doc.anchor != doc.head { self.source_delete(forward, cx); return; }
        let body = doc.text.body().to_owned();
        let head = doc.head;
        let range = if forward { word_edge(&body, head, true) } else { word_edge(&body, head, false) };
        self.source_apply(range, "", cx);
    }
    pub(super) fn source_delete_line(&mut self, to_end: bool, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc() else { return };
        let index = doc.text.line_of(doc.head);
        let (start, visible) = doc.text.line_visible(index);
        let head = doc.head;
        let range = if to_end { head..(start + visible.len()).max(head) } else { start.min(head)..head };
        self.source_apply(range, "", cx);
    }
    fn source_copy(&mut self, cut: bool, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc() else { return };
        let range = doc.selection();
        if range.is_empty() { return; }
        let text = doc.text.body()[range.clone()].to_owned();
        // Source selections are ordinary Markdown text; only the explicit Copy Markdown
        // command forces a block paste. A single copied word must still paste inline.
        cx.write_to_clipboard(ClipboardItem::new_string(text));
        if cut { self.source_apply(range, "", cx); } else { cx.notify(); }
    }
    pub(super) fn source_paste(&mut self, item: Option<ClipboardItem>, cx: &mut Context<Self>) {
        let item = item.or_else(|| cx.read_from_clipboard());
        let Some(item) = item else { return };
        let text = super::clipboard_files::string_text(&item).unwrap_or_default();
        if text.is_empty() { return; }
        let range = self.source_doc().map(|d| d.selection()).unwrap_or(0..0);
        self.source_apply(range, &text, cx);
    }
    fn source_undo(&mut self, redo: bool, cx: &mut Context<Self>) {
        self.finish_source_composition(cx);
        let Some(doc) = self.source_doc_mut() else { return };
        let Some(edit) = (if redo { doc.redo.pop() } else { doc.undo.pop() }) else { return };
        let (drop_len, put) = if redo { (edit.removed.len(), edit.inserted.clone()) } else { (edit.inserted.len(), edit.removed.clone()) };
        let end = edit.start + drop_len;
        doc.text.replace(edit.start..end, &put);
        doc.anchor = edit.start + put.len();
        doc.head = doc.anchor;
        if redo { doc.undo.push(edit); } else { doc.redo.push(edit); }
        doc.rev = doc.rev.wrapping_add(1);
        doc.runs_rev = 0;
        doc.note_highlight();
        doc.reveal = true;
        doc.sync_list();
        self.source_layouts.clear();
        self.touch(cx);
    }
    fn source_apply(&mut self, range: Range<usize>, insert: &str, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc_mut() else { return };
        let start = doc.text.snap(range.start.min(range.end));
        let end = doc.text.snap(range.end.max(range.start)).max(start);
        if start == end && insert.is_empty() { return; }
        let removed = doc.text.body().get(start..end).unwrap_or("").to_owned();
        doc.text.replace(start..end, insert);
        doc.head = start + insert.len();
        doc.anchor = doc.head;
        doc.rev = doc.rev.wrapping_add(1);
        doc.reveal = true;
        doc.runs_rev = 0;
        doc.note_highlight();
        doc.sync_list();
        push_edit(doc, Edit { start, removed, inserted: insert.to_owned() });
        self.source_layouts.clear();
        self.touch(cx);
    }
    fn source_place(&mut self, offset: usize, extend: bool, cx: &mut Context<Self>) {
        let Some(doc) = self.source_doc_mut() else { return };
        let offset = doc.text.snap(offset);
        doc.head = offset;
        if !extend { doc.anchor = offset; }
        doc.reveal = true;
        cx.notify();
    }
    pub(super) fn source_hit(&self, point: Point<Pixels>) -> Option<usize> {
        let doc = self.source_doc()?;
        let list = &doc.list;
        let view = list.viewport_bounds();
        if view.size.height > px(0.) && (point.y < view.top() || point.y > view.bottom()) { return None; }
        let containing = self.source_layouts.iter().find(|(_, layout)| {
            point.y >= layout.bounds.top() && point.y < layout.bounds.bottom()
        });
        let (index, layout) = match containing {
            Some(hit) => hit,
            None => self.source_layouts.iter().min_by(|a, b| {
                let da = (a.1.bounds.center().y - point.y).abs();
                let db = (b.1.bounds.center().y - point.y).abs();
                da.partial_cmp(&db).unwrap_or(std::cmp::Ordering::Equal)
            })?,
        };
        if point.y < layout.bounds.top() - px_gap() || point.y > layout.bounds.bottom() + px_gap() { return None; }
        let (start, visible) = doc.text.line_visible(*index);
        let local = layout.hit(point).min(visible.len());
        Some(doc.text.snap(start + local))
    }
}

fn px_gap() -> Pixels { gpui::px(4.) }
fn push_edit(doc: &mut SourceDoc, edit: Edit) {
    if let Some(prev) = doc.undo.last_mut()
        && prev.removed.is_empty() && edit.removed.is_empty()
        && prev.start + prev.inserted.len() == edit.start
        && !edit.inserted.contains('\n') && !prev.inserted.contains('\n')
        && prev.inserted.len() + edit.inserted.len() < 64
    {
        prev.inserted.push_str(&edit.inserted);
    } else {
        doc.undo.push(edit);
        while doc.undo.len() > 200 || undo_bytes(doc) > UNDO_BYTES {
            if doc.undo.len() <= 1 { break; }
            doc.undo.remove(0);
        }
    }
    doc.redo.clear();
}
const UNDO_BYTES: usize = 512 * 1024;
fn undo_bytes(doc: &SourceDoc) -> usize {
    doc.undo.iter().map(|edit| edit.removed.len() + edit.inserted.len()).sum()
}
fn word_edge(text: &str, head: usize, forward: bool) -> Range<usize> {
    if forward {
        let mut at = head;
        while at < text.len() && !word_char(text, at) { at = step_offset(text, at, true); }
        let start = at;
        while at < text.len() && word_char(text, at) { at = step_offset(text, at, true); }
        if start == head { head..step_offset(text, head, true) } else { head..at }
    } else {
        let mut at = head;
        while at > 0 && !word_char(text, step_offset(text, at, false)) { at = step_offset(text, at, false); }
        let end = at;
        while at > 0 && word_char(text, step_offset(text, at, false)) { at = step_offset(text, at, false); }
        if end == head { step_offset(text, head, false)..head } else { at..head }
    }
}
fn word_char(text: &str, offset: usize) -> bool {
    text.get(offset..).and_then(|s| s.chars().next()).is_some_and(|c| c.is_alphanumeric() || c == '_')
}
