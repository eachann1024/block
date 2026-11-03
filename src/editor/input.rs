//! Caret, selection and text mutation for the focused block. The view owns an
//! `Editor`; this module only borrows it.

use std::ops::Range;
use std::time::Duration;

use gpui::{ClipboardItem, Context, Window};
use unicode_segmentation::UnicodeSegmentation;

use super::model::{Kind, MarkKind};
use super::view::{Editor, EditorAction};

const SAVE_DELAY_MS: u64 = 400;

impl Editor {
    pub(super) fn save_now(&mut self) {
        let Some(path) = &self.path else { return };
        if std::fs::write(path, super::markdown::serialize(&self.doc)).is_ok() {
            self.dirty = false;
        }
    }

    pub(super) fn touch(&mut self, cx: &mut Context<Self>) {
        self.dirty = true;
        cx.notify();
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(Duration::from_millis(SAVE_DELAY_MS)).await;
            let _ = this.update(cx, |this, cx| {
                if this.dirty {
                    this.save_now();
                    cx.notify();
                }
            });
        })
        .detach();
    }

    pub(super) fn block_text(&self) -> &str {
        self.doc
            .blocks
            .iter()
            .find(|block| block.id == self.caret.block)
            .map(|block| block.text.as_str())
            .unwrap_or("")
    }

    pub(super) fn caret_offset(&self) -> usize {
        if self.caret.reversed { self.caret.range.start } else { self.caret.range.end }
    }

    pub(super) fn move_to(&mut self, offset: usize, cx: &mut Context<Self>) {
        self.caret.reversed = false;
        self.caret.range = offset..offset;
        self.caret.composing = None;
        cx.notify();
    }

    fn select_to(&mut self, offset: usize, cx: &mut Context<Self>) {
        if self.caret.reversed {
            self.caret.range.start = offset;
        } else {
            self.caret.range.end = offset;
        }
        if self.caret.range.end < self.caret.range.start {
            self.caret.reversed = !self.caret.reversed;
            self.caret.range = self.caret.range.end..self.caret.range.start;
        }
        cx.notify();
    }

    fn step(&self, offset: usize, forward: bool) -> usize {
        let text = self.block_text();
        if forward {
            text.grapheme_indices(true)
                .find_map(|(index, _)| (index > offset).then_some(index))
                .unwrap_or(text.len())
        } else {
            text.grapheme_indices(true)
                .rev()
                .find_map(|(index, _)| (index < offset).then_some(index))
                .unwrap_or(0)
        }
    }

    pub(super) fn handle(&mut self, action: &EditorAction, window: &mut Window, cx: &mut Context<Self>) {
        use EditorAction::*;
        match action {
            Left => self.collapse(false, cx),
            Right => self.collapse(true, cx),
            SelectLeft => self.select_to(self.step(self.caret_offset(), false), cx),
            SelectRight => self.select_to(self.step(self.caret_offset(), true), cx),
            SelectAll => {
                let len = self.block_text().len();
                self.move_to(0, cx);
                self.select_to(len, cx);
            }
            Up => self.shift_block(-1, cx),
            Down => self.shift_block(1, cx),
            Enter => self.split(cx),
            Backspace => self.backspace(window, cx),
            Delete => {
                if self.caret.range.is_empty() {
                    self.select_to(self.step(self.caret_offset(), true), cx);
                }
                self.replace(None, "", window, cx);
            }
            Copy => self.copy(cx),
            Cut => {
                self.copy(cx);
                self.replace(None, "", window, cx);
            }
            Paste => {
                if let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) {
                    self.replace(None, &text, window, cx);
                }
            }
            Bold => self.mark(MarkKind::Bold, cx),
            Italic => self.mark(MarkKind::Italic, cx),
            Strike => self.mark(MarkKind::Strike, cx),
            Code => self.mark(MarkKind::Code, cx),
        }
    }

    fn collapse(&mut self, forward: bool, cx: &mut Context<Self>) {
        let offset = if self.caret.range.is_empty() {
            self.step(self.caret_offset(), forward)
        } else if forward {
            self.caret.range.end
        } else {
            self.caret.range.start
        };
        self.move_to(offset, cx);
    }

    fn shift_block(&mut self, delta: isize, cx: &mut Context<Self>) {
        let Some(index) = self.doc.index_of(self.caret.block) else { return };
        let next = index as isize + delta;
        if !(0..self.doc.blocks.len() as isize).contains(&next) {
            return;
        }
        let block = &self.doc.blocks[next as usize];
        let offset = self.caret_offset().min(block.text.len());
        self.caret.block = block.id;
        self.move_to(offset, cx);
    }

    fn split(&mut self, cx: &mut Context<Self>) {
        let Some(index) = self.doc.index_of(self.caret.block) else { return };
        let text = self.doc.blocks[index].text.clone();
        let kind = self.doc.blocks[index].kind;
        let at = self.caret.range.start.min(text.len());
        let (left, right) = text.split_at(at);
        self.doc.set_text(self.caret.block, left.to_string());
        let carry = match kind {
            Kind::Bullet | Kind::Numbered | Kind::Quote | Kind::Todo
                if left.is_empty() && right.is_empty() =>
            {
                self.doc.set_kind(self.caret.block, Kind::Paragraph);
                Kind::Paragraph
            }
            Kind::Bullet | Kind::Numbered | Kind::Quote | Kind::Todo => kind,
            _ => Kind::Paragraph,
        };
        if let Some(id) = self.doc.insert_after(self.caret.block, carry, right.to_string()) {
            self.caret.block = id;
            self.move_to(0, cx);
        }
        self.touch(cx);
    }

    fn backspace(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.caret.range.is_empty() && self.caret.range.start == 0 {
            self.merge_up(cx);
            return;
        }
        if self.caret.range.is_empty() {
            self.select_to(self.step(self.caret_offset(), false), cx);
        }
        self.replace(None, "", window, cx);
    }

    fn merge_up(&mut self, cx: &mut Context<Self>) {
        let Some(index) = self.doc.index_of(self.caret.block) else { return };
        if index == 0 {
            if self.doc.blocks[0].kind != Kind::Paragraph {
                self.doc.set_kind(self.caret.block, Kind::Paragraph);
                self.touch(cx);
            }
            return;
        }
        let prev = self.doc.blocks[index - 1].id;
        let join_at = self.doc.blocks[index - 1].text.len();
        let merged = format!("{}{}", self.doc.blocks[index - 1].text, self.doc.blocks[index].text);
        self.doc.set_text(prev, merged);
        self.doc.remove(self.caret.block);
        self.caret.block = prev;
        self.move_to(join_at, cx);
        self.touch(cx);
    }

    fn copy(&mut self, cx: &mut Context<Self>) {
        if !self.caret.range.is_empty() {
            let text = self.block_text()[self.caret.range.clone()].to_string();
            cx.write_to_clipboard(ClipboardItem::new_string(text));
        }
    }

    pub(super) fn mark(&mut self, mark: MarkKind, cx: &mut Context<Self>) {
        if self.caret.range.is_empty() {
            return;
        }
        self.doc.toggle_mark(self.caret.block, self.caret.range.clone(), mark);
        self.touch(cx);
    }

    /// Replaces the selection (or an IME range) and applies line-start markers.
    pub(super) fn replace(
        &mut self,
        range_utf16: Option<Range<usize>>,
        new_text: &str,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let text = self.block_text().to_string();
        let range = range_utf16
            .as_ref()
            .map(|range| from_utf16(&text, range))
            .or(self.caret.composing.clone())
            .unwrap_or(self.caret.range.clone());
        let mut lines = new_text.split(['\n', '\r']).filter(|line| !line.is_empty());
        let first = lines.next().unwrap_or("");
        let end = range.end.min(text.len());
        let updated = format!("{}{}{}", &text[..range.start], first, &text[end..]);
        let caret = if let Some((kind, eaten)) = super::model::marker_kind(&updated) {
            self.doc.set_kind(self.caret.block, kind);
            self.doc.set_text(self.caret.block, updated[eaten..].to_string());
            updated.len() - eaten - (text.len() - end)
        } else {
            self.doc.set_text(self.caret.block, updated);
            range.start + first.len()
        };
        self.caret.composing = None;
        self.move_to(caret, cx);
        for extra in lines {
            if let Some(id) = self.doc.insert_after(self.caret.block, Kind::Paragraph, extra.to_string())
            {
                self.caret.block = id;
                self.move_to(extra.len(), cx);
            }
        }
        self.touch(cx);
    }

    pub(super) fn add_below(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        if let Some(new_id) = self.doc.insert_after(id, Kind::Paragraph, String::new()) {
            self.caret.block = new_id;
            self.move_to(0, cx);
            window.focus(&self.focus);
            self.touch(cx);
        }
    }

    pub(super) fn finish_drag(&mut self, fallback: usize, cx: &mut Context<Self>) {
        if let Some(id) = self.dragging.take() {
            self.doc.move_block(id, self.drag_over.unwrap_or(fallback));
            self.drag_over = None;
            self.touch(cx);
        }
    }
}

pub(super) fn to_utf16(text: &str, range: &Range<usize>) -> Range<usize> {
    fn one(text: &str, offset: usize) -> usize {
        text.get(..offset).unwrap_or(text).chars().map(|ch| ch.len_utf16()).sum()
    }
    one(text, range.start)..one(text, range.end)
}

pub(super) fn from_utf16(text: &str, range: &Range<usize>) -> Range<usize> {
    fn one(text: &str, units: usize) -> usize {
        let mut seen = 0;
        for (index, ch) in text.char_indices() {
            if seen >= units {
                return index;
            }
            seen += ch.len_utf16();
        }
        text.len()
    }
    one(text, range.start)..one(text, range.end)
}
