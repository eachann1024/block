//! One Markdown-aware transaction for every AI rewrite entry point.
use gpui::Context;
use super::{input::transactions::ListChange, markdown, model::{Block, Kind}, view::{Editor, selection::Position}};

impl Editor {
    /// Returns the actual inserted range (rendered text, not Markdown source bytes).
    /// Code stays literal; a table-cell rewrite may change inline content only.
    pub(in crate::editor) fn replace_ai_markdown(&mut self, source: &str, cx: &mut Context<Self>) -> Option<(Position, Position)> {
        if self.path.is_none() || !self.can_replace_selection() { return None; }
        let (a, b, first, last) = self.ordered();
        if first != last && self.crosses_hidden_fold(first, last) && !self.selection_covers_doc(a, b, first, last) { return None; }
        let start = self.doc.blocks.get(first)?;
        let end = self.doc.blocks.get(last)?;
        start.text.get(..a.offset)?; end.text.get(b.offset..)?;
        let source = source.replace("\r\n", "\n").replace('\r', "\n");
        let literal = first == last && (start.meta.code_language.is_some()
            || (start.meta.raw_source.is_some() && (a.offset != 0 || b.offset != start.text.len())));
        if literal || source.is_empty() {
            self.block_sel = None; self.table_sel = None;
            self.replace_selection(&source, false, cx);
            return Some((a, self.caret.head));
        }
        let doc = markdown::parse(&source);
        let blocks: Vec<Block> = doc.blocks.iter().cloned().collect();
        let inline = blocks.len() == 1 && blocks[0].kind == Kind::Paragraph
            && blocks[0].meta.table.is_none() && blocks[0].meta.code_language.is_none()
            && blocks[0].meta.raw_source.is_none() && blocks[0].meta.callout.is_none()
            && blocks[0].meta.indent == 0;
        let in_cell = (first == last && (a.offset != 0 || b.offset != start.text.len()))
            .then(|| start.meta.table.as_ref()?.cell_at_range(&(a.offset..b.offset))).flatten();
        if let Some((row, col, range)) = in_cell {
            // A structural result cannot replace part of a table without destroying neighbours.
            if !inline { return None; }
            let cell = &start.meta.table.as_ref()?.cell(row, col)?.content;
            let mut combined = cell.slice(0..range.start);
            combined.append(&blocks[0]);
            combined.append(&cell.slice(range.end..cell.text.len()));
            let before = self.snapshot();
            let block = self.doc.get_mut(a.block)?;
            block.meta.table.as_mut()?.cell_mut(row, col)?.content = combined;
            block.sync_table_text(); self.doc.refresh_block(a.block);
            let at = Position { block: a.block, offset: a.offset + blocks[0].text.len() };
            self.caret.anchor = at; self.caret.head = at; self.caret.composing = None;
            self.block_sel = None; self.table_sel = None;
            self.record_changes(before, vec![ListChange { start: first, before: 1, after: 1 }], true, cx);
            return Some((a, at));
        }
        self.block_sel = None; self.table_sel = None;
        if inline && start.meta.table.is_none() && start.meta.raw_source.is_none()
            && start.meta.code_language.is_none()
            && (first..=last).all(|i| self.doc.blocks[i].meta.table.is_none()
                && self.doc.blocks[i].meta.raw_source.is_none() && self.doc.blocks[i].meta.code_language.is_none()) {
            self.insert_paste_inline(&blocks[0], cx);
            Some((a, self.caret.head))
        } else {
            let lead = usize::from(a.offset > 0);
            if !self.insert_paste_blocks(blocks, cx) { return None; }
            let start = Position { block: self.doc.blocks[first + lead].id, offset: 0 };
            Some((start, self.caret.head))
        }
    }
}
