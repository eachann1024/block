//! Tab / Shift-Tab outside tables: list nesting through `meta.indent`, 4 spaces in code, otherwise nothing.
use gpui::Context;
use super::{input::transactions::ListChange, model::{Block, Kind}, view::Editor};

fn is_list(block: &Block) -> bool {
    matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) && block.meta.table.is_none() && block.meta.raw_source.is_none()
}
fn is_paragraph(block: &Block) -> bool {
    block.kind == Kind::Paragraph && block.meta.table.is_none() && block.meta.raw_source.is_none()
        && block.meta.code_language.is_none()
}

impl Editor {
    pub(in crate::editor) fn tab_outside_table(&mut self, forward: bool, cx: &mut Context<Self>) {
        let (_, _, ai, bi) = self.ordered();
        if ai != bi { self.tab_group(ai, bi, forward, cx); return; }
        let id = self.caret.head.block;
        let Some(block) = self.doc.get(id) else { return };
        // Never insert a literal tab: it serializes as `&#9;`.
        if block.meta.code_language.is_some() {
            self.code_tab(id, forward, cx);
            return;
        }
        if is_paragraph(block) {
            let Some(index) = self.doc.index_of(id) else { return };
            let old = block.meta.indent;
            let new = if forward { (old + 2).min(12) } else { old.saturating_sub(2) };
            if new == old { return; }
            self.commit_composition(cx);
            let before = self.snapshot();
            if let Some(b) = self.doc.get_mut(id) { b.meta.indent = new; b.meta.html_paragraph = None; }
            self.doc.refresh_block(id);
            self.record_changes(before, vec![ListChange { start: index, before: 1, after: 1 }], true, cx);
            return;
        }
        if !is_list(block) { return; }
        let Some(index) = self.doc.index_of(id) else { return };
        let old = block.meta.indent;
        let new = if forward { self.indent_target(index, old) } else { self.outdent_target(index, old) };
        let Some(new) = new.filter(|new| *new != old) else { return };
        self.commit_composition(cx);
        let before = self.snapshot();
        // Children (following deeper list items) keep their relative nesting.
        let mut last = index;
        while last + 1 < self.doc.blocks.len() && is_list(&self.doc.blocks[last + 1]) && self.doc.blocks[last + 1].meta.indent > old { last += 1; }
        for i in index..=last {
            let child = self.doc.blocks[i].id;
            let indent = self.doc.blocks[i].meta.indent;
            if let Some(b) = self.doc.get_mut(child) { b.meta.indent = (indent + new).saturating_sub(old); }
            self.doc.refresh_block(child);
        }
        let count = last - index + 1;
        self.record_changes(before, vec![ListChange { start: index, before: count, after: count }], true, cx);
    }
    /// Nest under the previous list item: at most one marker-width deeper than it.
    fn indent_target(&self, index: usize, old: usize) -> Option<usize> {
        let prev = self.doc.blocks.get(index.checked_sub(1)?).filter(|b| is_list(b))?;
        let width = match prev.kind {
            Kind::Numbered => self.doc.list_number(prev.id).unwrap_or(1).to_string().len() + 2,
            _ => 2,
        };
        let max = prev.meta.indent + width;
        if old >= max { None } else if old >= prev.meta.indent { Some(max) } else { Some(prev.meta.indent) }
    }
    /// Move out to the nearest preceding list item that is shallower, else one level shallower.
    fn outdent_target(&self, index: usize, old: usize) -> Option<usize> {
        if old == 0 { return None; }
        let parent = (0..index).rev().map(|i| &self.doc.blocks[i]).take_while(|b| is_list(b))
            .map(|b| b.meta.indent).find(|indent| *indent < old);
        Some(parent.unwrap_or_else(|| old.saturating_sub(2)))
    }
    /// In code blocks, Tab inserts 2 spaces (or indents each selected line by 2 spaces);
    /// Shift+Tab removes up to 2 leading spaces on the current or selected lines.
    fn code_tab(&mut self, id: u64, forward: bool, cx: &mut Context<Self>) {
        let (a, b, _, _) = self.ordered();
        let Some(text) = self.doc.get(id).map(|blk| blk.text.clone()) else { return };
        if forward && !text[a.offset..b.offset].contains('\n') {
            self.replace_selection("  ", false, cx);
            return;
        }
        let line_start = text[..a.offset].rfind('\n').map_or(0, |p| p + 1);
        let end_probe = if b.offset > a.offset && text.as_bytes().get(b.offset - 1) == Some(&b'\n') {
            b.offset - 1
        } else { b.offset };
        let line_end = text[end_probe..].find('\n').map_or(text.len(), |p| end_probe + p);
        let region = &text[line_start..line_end];
        let mut repl = String::with_capacity(region.len() + 16);
        let mut changes: Vec<(usize, isize)> = Vec::new();
        let mut cur = line_start;
        for (i, line) in region.split('\n').enumerate() {
            if i > 0 { repl.push('\n'); }
            if forward {
                repl.push_str("  "); repl.push_str(line);
                changes.push((cur, 2));
            } else {
                let strip = line.bytes().take(2).take_while(|&c| c == b' ').count();
                repl.push_str(&line[strip..]);
                changes.push((cur, -(strip as isize)));
            }
            cur += line.len() + 1;
        }
        if repl == region { return; }
        self.commit_composition(cx);
        let before = self.snapshot();
        self.doc.replace_range(id, line_start..line_end, &repl);
        let map_pos = |pos: usize| -> usize {
            let mut delta = 0isize;
            for &(start, d) in &changes {
                if d > 0 {
                    if pos >= start { delta += d; }
                } else if pos > start {
                    delta -= (pos - start).min((-d) as usize) as isize;
                }
            }
            (pos as isize + delta).max(0) as usize
        };
        let anchor_first = self.caret.anchor == a;
        let na = super::view::selection::Position { block: id, offset: map_pos(a.offset) };
        let nb = super::view::selection::Position { block: id, offset: map_pos(b.offset) };
        self.caret.anchor = if anchor_first { na } else { nb };
        self.caret.head = if anchor_first { nb } else { na };
        self.caret.composing = None;
        self.record(before, cx);
    }
    /// Tab / Shift-Tab over a selection of several list items moves the group as one,
    /// keeping relative nesting (and the last item's children).
    fn tab_group(&mut self, ai: usize, mut bi: usize, forward: bool, cx: &mut Context<Self>) {
        let lists: Vec<usize> = (ai..=bi).filter(|i| is_list(&self.doc.blocks[*i])).collect();
        if lists.is_empty() {
            let paras: Vec<usize> = (ai..=bi).filter(|i| is_paragraph(&self.doc.blocks[*i])).collect();
            if !paras.iter().any(|&i| {
                let old = self.doc.blocks[i].meta.indent;
                (if forward { (old + 2).min(12) } else { old.saturating_sub(2) }) != old
            }) { return; }
            self.commit_composition(cx);
            let before = self.snapshot();
            for i in paras {
                let id = self.doc.blocks[i].id;
                let old = self.doc.blocks[i].meta.indent;
                let new = if forward { (old + 2).min(12) } else { old.saturating_sub(2) };
                if let Some(b) = self.doc.get_mut(id) { b.meta.indent = new; b.meta.html_paragraph = None; }
                self.doc.refresh_block(id);
            }
            let count = bi - ai + 1;
            self.record_changes(before, vec![ListChange { start: ai, before: count, after: count }], true, cx);
            return;
        }
        let Some(&first) = lists.first() else { return };
        let delta: isize = if forward {
            let old = self.doc.blocks[first].meta.indent;
            let Some(new) = self.indent_target(first, old) else { return };
            new as isize - old as isize
        } else {
            let Some(&low) = lists.iter().min_by_key(|i| self.doc.blocks[**i].meta.indent) else { return };
            let old = self.doc.blocks[low].meta.indent;
            let Some(new) = self.outdent_target(low, old) else { return };
            new as isize - old as isize
        };
        if delta == 0 { return; }
        let tail = self.doc.blocks[bi].meta.indent;
        if is_list(&self.doc.blocks[bi]) {
            while bi + 1 < self.doc.blocks.len() && is_list(&self.doc.blocks[bi + 1]) && self.doc.blocks[bi + 1].meta.indent > tail { bi += 1; }
        }
        self.commit_composition(cx);
        let before = self.snapshot();
        for i in ai..=bi {
            if !is_list(&self.doc.blocks[i]) { continue; }
            let id = self.doc.blocks[i].id;
            let indent = (self.doc.blocks[i].meta.indent as isize + delta).max(0) as usize;
            if let Some(b) = self.doc.get_mut(id) { b.meta.indent = indent; }
            self.doc.refresh_block(id);
        }
        let count = bi - ai + 1;
        self.record_changes(before, vec![ListChange { start: ai, before: count, after: count }], true, cx);
    }
}
