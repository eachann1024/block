use gpui::Context;
use super::{input::transactions::ListChange, model::{Block, Kind, MarkKind}, view::{Editor, selection::Position}};

pub(super) const MARKDOWN_CLIPBOARD_V1: &str = "com.block.editor.markdown.v1";

fn structured(block: &Block) -> bool {
    block.kind != Kind::Paragraph || block.meta.table.is_some() || block.meta.raw_source.is_some()
        || block.meta.code_language.is_some() || block.meta.html_paragraph.is_some()
        || block.meta.indent > 0 || block.meta.callout.is_some()
}

fn plain_blocks(text: &str) -> Vec<Block> {
    text.split('\n').map(|line| Block::new(0, Kind::Paragraph, line.to_owned())).collect()
}

impl Editor {
    pub(super) fn paste_text(&mut self, text: &str, plain: bool, force_markdown: bool,
        block_markdown: Option<&str>, cx: &mut Context<Self>) {
        if self.path.is_none() || text.is_empty() { return; }
        let text = text.replace("\r\n", "\n").replace('\r', "\n");
        self.commit_composition(cx);
        if self.block_sel.is_some() {
            if plain || (!force_markdown && block_markdown.is_none() && text.trim().is_empty()) {
                self.insert_paste_blocks(plain_blocks(&text), cx);
            } else {
                let source = block_markdown.unwrap_or(text.as_str());
                let blocks = super::markdown::parse(source).blocks.iter().cloned().collect();
                self.insert_paste_blocks(blocks, cx);
            }
            return;
        }
        let (a, b, ai, bi) = self.ordered();
        if !self.can_replace_selection() { self.table_edit_rejected(cx); return; }
        let Some(first) = self.doc.blocks.get(ai) else { return };
        let Some(last) = self.doc.blocks.get(bi) else { return };
        if a.offset > first.text.len() || b.offset > last.text.len()
            || !first.text.is_char_boundary(a.offset) || !last.text.is_char_boundary(b.offset) { return; }
        let focused_atomic = super::image_source::spec_of(first).is_some() || super::block_menu::is_divider(first);
        let literal = ai == bi && !focused_atomic && (first.meta.code_language.is_some() || first.meta.raw_source.is_some()
            || first.spans.iter().any(|span| span.marks.contains(&MarkKind::Code)
                && span.start < a.offset && b.offset <= span.end));
        if focused_atomic && self.caret.is_empty() && plain {
            self.insert_paste_blocks(plain_blocks(&text), cx); return;
        }
        if self.table_sel.is_some_and(|(_, selection)| !matches!(selection, super::table_actions::TableSel::Whole)) {
            self.table_edit_rejected(cx); return;
        }
        let whole_table = self.table_sel.is_some();
        if plain && whole_table { self.insert_paste_blocks(plain_blocks(&text), cx); return; }
        if plain || literal { self.replace_selection(&text, false, cx); return; }
        let in_cell = ai == bi && first.meta.table.as_ref().is_some_and(|table|
            !whole_table && table.cell_at_range(&(a.offset..b.offset)).is_some());
        let empty_first = first.text.is_empty();
        if in_cell {
            if block_markdown.is_none() && self.caret.is_empty() && tsv_safe(&text) && self.paste_tsv(&text, cx) { return; }
            self.replace_selection(&text, false, cx); return;
        }
        if !force_markdown && block_markdown.is_none() && !whole_table && self.paste_url_over_selection(&text, cx) { return; }
        if !force_markdown && block_markdown.is_none() && text.trim().is_empty() { self.replace_selection(&text, false, cx); return; }
        if !force_markdown && block_markdown.is_none() && self.caret.is_empty() && empty_first && tsv_safe(&text) && self.paste_tsv(&text, cx) { return; }
        let source = block_markdown.unwrap_or(text.as_str());
        let parsed = super::markdown::parse(source.strip_prefix('\u{feff}').unwrap_or(source));
        let blocks: Vec<Block> = parsed.blocks.iter().cloned().collect();
        let rich = blocks.iter().any(|block| !block.spans.is_empty() || !block.meta.inline.is_empty())
            || ((text.contains('&') || text.contains('\\'))
                && blocks.iter().map(|block| block.text.as_str()).collect::<Vec<_>>().join("\n") != text.trim_end_matches('\n'));
        let is_block = force_markdown || block_markdown.is_some() || whole_table || focused_atomic || blocks.iter().any(structured) || (blocks.len() > 1 && rich);
        if is_block { self.insert_paste_blocks(blocks, cx); return; }
        if blocks.len() == 1 && (!blocks[0].spans.is_empty() || !blocks[0].meta.inline.is_empty()
            || blocks[0].text != text && !text.contains('\n')) {
            self.insert_paste_inline(&blocks[0], cx); return;
        }
        self.replace_selection(&text, false, cx);
    }

    fn insert_paste_inline(&mut self, fragment: &Block, cx: &mut Context<Self>) {
        let (a, b, ai, bi) = self.ordered();
        let block = &self.doc.blocks[ai];
        let end = &self.doc.blocks[bi];
        let atomic = |block: &Block| block.meta.table.is_some() || block.meta.raw_source.is_some()
            || block.meta.code_language.is_some() || super::image_source::spec_of(block).is_some()
            || super::block_menu::is_divider(block);
        if (ai..=bi).any(|index| atomic(&self.doc.blocks[index])) {
            self.insert_paste_blocks(vec![fragment.clone()], cx); return;
        }
        let mut combined = block.slice(0..a.offset);
        combined.append(fragment);
        combined.append(&end.slice(b.offset..end.text.len()));
        let before = self.snapshot();
        self.doc.splice_blocks(ai..bi + 1, vec![combined]);
        let at = Position { block: a.block, offset: a.offset + fragment.text.len() };
        self.caret.anchor = at; self.caret.head = at; self.caret.composing = None;
        self.record_changes(before, vec![ListChange { start: ai, before: bi - ai + 1, after: 1 }], true, cx);
    }

    pub(super) fn paste_blocks(&mut self, text: &str, cx: &mut Context<Self>) -> bool {
        let Some((plain, markdown)) = self.block_clip.as_ref() else { return false };
        if plain != text { return false; }
        let blocks = super::markdown::parse(markdown).blocks.iter().cloned().collect();
        self.insert_paste_blocks(blocks, cx)
    }

    fn insert_paste_blocks(&mut self, mut blocks: Vec<Block>, cx: &mut Context<Self>) -> bool {
        if blocks.is_empty() { return false; }
        let (a, b, mut first, mut last) = self.ordered();
        let selected = self.block_sel;
        if let Some((anchor, head)) = selected {
            if anchor >= self.doc.blocks.len() || head >= self.doc.blocks.len() { return false; }
            first = anchor.min(head); last = anchor.max(head);
        } else if !self.can_replace_selection() { self.table_edit_rejected(cx); return false; }
        let Some(start) = self.doc.blocks.get(first) else { return false };
        let Some(end) = self.doc.blocks.get(last) else { return false };
        let (start_offset, end_offset) = if selected.is_some() { (0, end.text.len()) } else { (a.offset, b.offset) };
        if start_offset > start.text.len() || end_offset > end.text.len()
            || !start.text.is_char_boundary(start_offset) || !end.text.is_char_boundary(end_offset) { return false; }
        let atomic = |block: &Block| block.meta.table.is_some() || block.meta.raw_source.is_some()
            || block.meta.code_language.is_some() || super::image_source::spec_of(block).is_some()
            || super::block_menu::is_divider(block);
        if selected.is_none() && ((atomic(start) && start_offset != 0 && start_offset != start.text.len())
            || (atomic(end) && end_offset != 0 && end_offset != end.text.len())) {
            self.table_edit_rejected(cx); return false;
        }
        let mut pieces = Vec::new();
        if start_offset > 0 { pieces.push(start.slice(0..start_offset)); }
        let lead = pieces.len();
        for block in &mut blocks {
            block.id = 0;
            if (first != 0 || lead != 0) && block.meta.code_language.as_deref() == Some("yaml-frontmatter") {
                block.meta.code_language = Some("yaml".into());
            }
        }
        let count = blocks.len();
        pieces.extend(blocks);
        if end_offset < end.text.len() {
            let mut tail = end.slice(end_offset..end.text.len());
            if first == last && start_offset > 0 { tail.id = 0; tail.meta.list_start = None; }
            pieces.push(tail);
        }
        let total = pieces.len();
        let before = self.snapshot();
        self.doc.splice_blocks(first..last + 1, pieces);
        let inserted = &self.doc.blocks[first + lead + count - 1];
        let at = Position { block: inserted.id, offset: inserted.text.len() };
        self.caret.anchor = at; self.caret.head = at; self.caret.composing = None;
        self.block_sel = None; self.table_sel = None; self.preferred_x = None;
        self.record_changes(before, vec![ListChange { start: first, before: last - first + 1, after: total }], true, cx);
        true
    }
}

fn tsv_safe(text: &str) -> bool {
    let mut lines = text.trim_end_matches('\n').split('\n');
    let Some(first) = lines.next() else { return false };
    let columns = first.split('\t').count();
    if columns < 2 || first.split('\t').any(|cell| cell.trim() != cell) { return false; }
    let mut count = 1;
    for line in lines {
        if line.split('\t').count() != columns || line.split('\t').any(|cell| cell.trim() != cell) { return false; }
        count += 1;
    }
    count >= 2
}
