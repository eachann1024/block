//! Tab / Shift-Tab outside tables: list nesting through `meta.indent`, 4 spaces in code, otherwise nothing.
use gpui::Context;
use super::{input::transactions::ListChange, model::{Block, Kind}, view::Editor};

fn is_list(block: &Block) -> bool {
    matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) && block.meta.table.is_none() && block.meta.raw_source.is_none()
}

impl Editor {
    pub(in crate::editor) fn tab_outside_table(&mut self, forward: bool, cx: &mut Context<Self>) {
        let (_, _, ai, bi) = self.ordered();
        if ai != bi { self.tab_group(ai, bi, forward, cx); return; }
        let id = self.caret.head.block;
        let Some(block) = self.doc.get(id) else { return };
        // Never insert a literal tab: it serializes as `&#9;`.
        if block.meta.code_language.is_some() {
            if forward { self.replace_selection("    ", false, cx); }
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
    /// Move out to the nearest preceding list item that is shallower, else the left edge.
    fn outdent_target(&self, index: usize, old: usize) -> Option<usize> {
        if old == 0 { return None; }
        let parent = (0..index).rev().map(|i| &self.doc.blocks[i]).take_while(|b| is_list(b))
            .map(|b| b.meta.indent).find(|indent| *indent < old);
        Some(parent.unwrap_or(0))
    }
    /// Tab / Shift-Tab over a selection of several list items moves the group as one,
    /// keeping relative nesting (and the last item's children).
    fn tab_group(&mut self, ai: usize, mut bi: usize, forward: bool, cx: &mut Context<Self>) {
        let lists: Vec<usize> = (ai..=bi).filter(|i| is_list(&self.doc.blocks[*i])).collect();
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
