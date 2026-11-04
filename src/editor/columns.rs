//! Multi-column layout (`<div class="columns"><div class="column">...</div></div>`):
//! contiguous blocks in `Doc::blocks` sharing `BlockMeta::column = Some((group_id, col_index))`
//! render side-by-side in flex columns while keeping full per-block editing and block handles.
use std::ops::Range;
use gpui::{AnyElement, Context, MouseButton, Window, div, prelude::*, px, rgba};
use super::{
    chrome::{HANDLE_WIDTH, RIGHT_INSET, ROW_GAP, grip_button, render_block_row},
    input::transactions::ListChange,
    model::{Block, Kind},
    view::{Editor, interaction::DropTarget, selection::Position},
};

pub(in crate::editor) const GROUP_HANDLE_BIT: u64 = 1 << 62;
pub(in crate::editor) fn group_handle_id(gid: u64) -> u64 { GROUP_HANDLE_BIT | gid }
pub(in crate::editor) fn group_from_handle(id: u64) -> Option<u64> {
    (id & GROUP_HANDLE_BIT != 0).then_some(id & !GROUP_HANDLE_BIT)
}

impl Editor {
    /// Range `start..end` of all contiguous blocks sharing the column `group_id` at `index`.
    pub(in crate::editor) fn column_group_range(&self, index: usize) -> Option<Range<usize>> {
        let (gid, _) = self.doc.blocks.get(index)?.meta.column?;
        let mut start = index;
        while start > 0 && self.doc.blocks[start - 1].meta.column.is_some_and(|(g, _)| g == gid) { start -= 1; }
        let mut end = index + 1;
        while end < self.doc.blocks.len() && self.doc.blocks[end].meta.column.is_some_and(|(g, _)| g == gid) { end += 1; }
        Some(start..end)
    }

    pub(in crate::editor) fn column_group_by_id(&self, gid: u64) -> Option<Range<usize>> {
        let first = self.doc.blocks.iter().position(|b| b.meta.column.is_some_and(|(g, _)| g == gid))?;
        self.column_group_range(first)
    }

    /// Range `start..end` of all contiguous blocks in the same individual column `(group_id, col_index)`.
    pub(in crate::editor) fn column_range(&self, index: usize) -> Option<Range<usize>> {
        let col = self.doc.blocks.get(index)?.meta.column?;
        let mut start = index;
        while start > 0 && self.doc.blocks[start - 1].meta.column == Some(col) { start -= 1; }
        let mut end = index + 1;
        while end < self.doc.blocks.len() && self.doc.blocks[end].meta.column == Some(col) { end += 1; }
        Some(start..end)
    }

    /// Force `gpui::ListState` to re-measure the `group.start` container row of every column group.
    pub(in crate::editor) fn invalidate_column_groups(&mut self) {
        let mut i = 0;
        while i < self.doc.blocks.len() {
            if let Some(r) = self.column_group_range(i) {
                self.list.splice(r.start..r.start + 1, 1);
                i = r.end;
            } else { i += 1; }
        }
    }

    /// Ensure every surviving column group has at least 2 columns (`0..=max_col.max(1)`) and each
    /// column holds at least one block (I2). Entirely deleted groups have no blocks here (I3).
    pub(in crate::editor) fn repair_column_groups(&mut self) -> usize {
        let (mut added, mut i) = (0, 0);
        while i < self.doc.blocks.len() {
            let Some(r) = self.column_group_range(i) else { i += 1; continue };
            let Some((gid, _)) = self.doc.blocks.get(r.start).and_then(|b| b.meta.column) else { i = r.end.max(i + 1); continue };
            let max_col = (r.clone()).filter_map(|k| self.doc.blocks[k].meta.column.map(|(_, c)| c)).max().unwrap_or(1).max(1);
            let mut rebuilt: Vec<Block> = Vec::new();
            for col in 0..=max_col {
                let mut col_blocks: Vec<Block> = (r.clone())
                    .filter(|&k| self.doc.blocks[k].meta.column == Some((gid, col)))
                    .map(|k| self.doc.blocks[k].clone()).collect();
                if col_blocks.is_empty() {
                    let mut p = Block::new(0, Kind::Paragraph, String::new());
                    p.meta.column = Some((gid, col)); col_blocks.push(p); added += 1;
                }
                rebuilt.extend(col_blocks);
            }
            let end = r.start + rebuilt.len();
            self.doc.splice_blocks(r, rebuilt);
            i = end;
        }
        added
    }

    /// Up/down target block index: moves within the current column, then exits the group above or below.
    pub(in crate::editor) fn vertical_target(&self, index: usize, down: bool) -> Option<usize> {
        if let Some((gid, col)) = self.doc.blocks.get(index).and_then(|b| b.meta.column) {
            if down {
                if index + 1 < self.doc.blocks.len() && self.doc.blocks[index + 1].meta.column == Some((gid, col)) {
                    return Some(index + 1);
                }
                let ge = self.column_group_range(index)?.end;
                return self.next_visible_index(ge.saturating_sub(1), true);
            }
            if index > 0 && self.doc.blocks[index - 1].meta.column == Some((gid, col)) {
                return Some(index - 1);
            }
            let gs = self.column_group_range(index)?.start;
            return self.next_visible_index(gs, false);
        }
        let next = self.next_visible_index(index, down)?;
        let Some(r) = self.column_group_range(next) else { return Some(next) };
        let Some((gid, _)) = self.doc.blocks.get(r.start).and_then(|b| b.meta.column) else { return Some(next) };
        let max_col = (r.clone()).filter_map(|k| self.doc.blocks[k].meta.column.map(|(_, c)| c)).max().unwrap_or(0);
        let probe_x = self.preferred_x.or_else(|| self.layouts.get(&self.caret.head.block).map(|l| l.point(self.caret.head.offset).x));
        let chosen_col = probe_x.and_then(|x| {
            (0..=max_col).min_by(|&ca, &cb| {
                let dist = |c: usize| -> f32 {
                    (r.clone()).find(|&k| self.doc.blocks[k].meta.column == Some((gid, c)))
                        .and_then(|k| self.layouts.get(&self.doc.blocks[k].id))
                        .map(|l| f32::from((l.bounds.left() - x).max(x - l.bounds.right()).max(px(0.))))
                        .unwrap_or(f32::MAX)
                };
                dist(ca).partial_cmp(&dist(cb)).unwrap_or(std::cmp::Ordering::Equal)
            })
        }).unwrap_or(0);
        if down {
            (r.clone()).find(|&k| self.doc.blocks[k].meta.column == Some((gid, chosen_col))).or(Some(r.start))
        } else {
            (r.clone()).rev().find(|&k| self.doc.blocks[k].meta.column == Some((gid, chosen_col))).or(Some(r.end - 1))
        }
    }

    pub(in crate::editor) fn exit_columns_at_end(&mut self, index: usize, cx: &mut Context<Self>) -> bool {
        let Some(r) = self.column_group_range(index).filter(|r| r.end >= self.doc.blocks.len()) else { return false };
        let before = self.snapshot();
        self.doc.splice_blocks(r.end..r.end, vec![Block::new(0, Kind::Paragraph, String::new())]);
        let id = self.doc.blocks[r.end].id;
        self.preferred_x = None;
        self.set_head(Position { block: id, offset: 0 }, false, cx);
        self.record_changes(before, vec![ListChange { start: r.end, before: 0, after: 1 }], true, cx);
        true
    }

    pub(in crate::editor) fn insert_columns(&mut self, index: usize, slash_target: bool, window: &mut Window, cx: &mut Context<Self>) {
        let before = self.snapshot();
        let gid = self.doc.fresh_id();
        let mut c0 = Block::new(0, Kind::Paragraph, String::new()); c0.meta.column = Some((gid, 0));
        let mut c1 = Block::new(0, Kind::Paragraph, String::new()); c1.meta.column = Some((gid, 1));
        let (start, remove) = if slash_target { (index, 1) } else { (index + 1, 0) };
        let mut blocks = vec![c0, c1];
        if start + remove >= self.doc.blocks.len() { blocks.push(Block::new(0, Kind::Paragraph, String::new())); }
        let count = blocks.len();
        self.doc.splice_blocks(start..start + remove, blocks);
        let at = Position { block: self.doc.blocks[start].id, offset: 0 };
        self.caret.anchor = at; self.caret.head = at;
        self.record_changes(before, vec![ListChange { start, before: remove, after: count }], true, cx);
        self.layouts.clear(); window.focus(&self.focus, cx); cx.notify();
    }

    pub(in crate::editor) fn delete_single_block(&mut self, id: u64, index: usize, changes: &mut Vec<ListChange>, caret: &mut Option<Position>) {
        if let Some(col) = self.doc.blocks.get(index).and_then(|b| b.meta.column) {
            let col_r = self.column_range(index).unwrap_or(index..index + 1);
            if col_r.len() <= 1 {
                let mut p = Block::new(0, Kind::Paragraph, String::new()); p.meta.column = Some(col);
                self.doc.splice_blocks(index..index + 1, vec![p]);
                changes.push(ListChange { start: index, before: 1, after: 1 });
                *caret = Some(Position { block: self.doc.blocks[index].id, offset: 0 });
            } else {
                let next_idx = if index + 1 < col_r.end { index } else { index.saturating_sub(1) };
                self.doc.splice_blocks(index..index + 1, Vec::new());
                changes.push(ListChange { start: index, before: 1, after: 0 });
                *caret = Some(Position { block: self.doc.blocks[next_idx].id, offset: 0 });
            }
            return;
        }
        let only = self.doc.blocks.len() == 1;
        self.doc.remove(id);
        changes.push(ListChange { start: if only { 0 } else { index }, before: 1, after: usize::from(only) });
        let at = index.min(self.doc.blocks.len() - 1);
        *caret = Some(Position { block: self.doc.blocks[at].id, offset: 0 });
    }

    pub(in crate::editor) fn finish_group_drag(&mut self, gid: u64, target: DropTarget, cx: &mut Context<Self>) {
        let Some(r) = self.column_group_by_id(gid) else { return };
        let slot = target.index + usize::from(target.after);
        if slot >= r.start && slot <= r.end { return; }
        let n = r.len();
        let to = if slot > r.start { slot.saturating_sub(n) } else { slot }.min(self.doc.blocks.len().saturating_sub(n));
        if to == r.start { return; }
        self.commit_composition(cx);
        let before = self.snapshot();
        let moved: Vec<Block> = (r.clone()).map(|i| self.doc.blocks[i].clone()).collect();
        self.doc.splice_blocks(r.clone(), Vec::new());
        self.doc.splice_blocks(to..to, moved);
        self.record_changes(before, vec![ListChange { start: r.start, before: n, after: 0 }, ListChange { start: to, before: 0, after: n }], true, cx);
    }
}

pub(in crate::editor) fn columns_row(this: &Editor, group: Range<usize>, cx: &mut Context<Editor>) -> AnyElement {
    let gid = this.doc.blocks[group.start].meta.column.map_or(0, |(g, _)| g);
    let handle_id = group_handle_id(gid);
    let max_col = (group.clone()).filter_map(|i| this.doc.blocks[i].meta.column.map(|(_, c)| c)).max().unwrap_or(1).max(1);
    let group_active = !this.minimal && this.path.is_some() && (
        this.hover == Some(handle_id) || this.dragging == Some(handle_id)
        || (group.clone()).any(|i| { let bid = this.doc.blocks[i].id; this.hover == Some(bid) || this.caret.head.block == bid })
    );
    let mut cols_el = div().flex().flex_row().w_full().gap(px(12.));
    for col in 0..=max_col {
        let indices: Vec<(usize, u64)> = (group.clone())
            .filter(|&i| this.doc.blocks[i].meta.column == Some((gid, col)))
            .map(|i| (i, this.doc.blocks[i].id)).collect();
        let col_focused = indices.iter().any(|&(_, bid)| this.caret.head.block == bid);
        let border = if col_focused { rgba(0x547dd066) } else if group_active { rgba(0x1f23281f) } else { rgba(0x1f232812) };
        let mut card = div().flex_1().min_w(px(0.)).flex().flex_col().py(px(4.)).pr(px(6.)).rounded(px(6.)).border_1().border_color(border);
        for (idx, _) in indices { card = card.child(render_block_row(this, idx, cx)); }
        cols_el = cols_el.child(card);
    }
    let grip_dragging = this.dragging == Some(handle_id);
    let handle = div().w(px(HANDLE_WIDTH)).min_h(px(32.)).flex_shrink_0().relative()
        .on_mouse_move(cx.listener(move |this, _, _, cx| this.hover_block(handle_id, true, cx)))
        .when(group_active, |row| row.child(
            div().absolute().top(px(6.)).right(px(ROW_GAP)).h(px(26.)).flex().items_center().child(
                grip_button(("col-grip", gid as usize), grip_dragging, cx.listener(move |this, _: &gpui::MouseDownEvent, _, cx| {
                    this.begin_drag(handle_id, cx); cx.stop_propagation();
                })).on_mouse_up(MouseButton::Left, cx.listener(move |this, event: &gpui::MouseUpEvent, _, cx| {
                    if this.dragging == Some(handle_id) { this.finish_drag(event.position, cx); cx.stop_propagation(); }
                })))));
    div().id(("columns-group", gid as usize)).w_full().flex().items_start()
        .on_hover(cx.listener(move |this, hovered: &bool, _, cx| if !*hovered { this.hover_block(handle_id, false, cx); }))
        .child(handle)
        .child(div().flex_1().min_w(px(0.)).pr(px(RIGHT_INSET)).py(px(4.)).child(cols_el))
        .into_any_element()
}
