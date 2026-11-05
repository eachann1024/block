//! Pointer gestures keep word/block selection and insertion-slot dragging separate.
use gpui::{Context, Point, Pixels, Window, point, px};
use super::{Editor, selection::Position};

#[derive(Clone, Copy, PartialEq, Eq)]
pub(in crate::editor) struct DropTarget { pub index: usize, pub after: bool }

impl Editor {
    pub(in crate::editor) fn track_pointer_lifecycle(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.pointer_subscriptions.is_empty() { return; }
        self.pointer_subscriptions.push(cx.observe_window_activation(window, |this, window, cx| {
            if !window.is_window_active() {
                this.clear_pointer();
                this.link_ui.reset_hover(); crate::editor::inline_code::clear_hover();
                this.toolbar_anchor = None;
            } else {
                // Active state settles after the activation callback; paint again shortly so the
                // selection toolbar anchor is recomputed without waiting for the next input.
                cx.spawn(async move |this, cx| {
                    cx.background_executor().timer(std::time::Duration::from_millis(60)).await;
                    this.update(cx, |_, cx| cx.notify()).ok();
                }).detach();
            }
            cx.notify();
        }));
        self.pointer_subscriptions.push(cx.on_focus(&self.focus, window, |_, _, cx| cx.notify()));
        self.pointer_subscriptions.push(cx.on_blur(&self.focus, window, |this, _, cx| {
            this.clear_pointer(); this.link_ui.reset_hover(); this.toolbar_anchor = None; cx.notify();
        }));
    }
    pub(in crate::editor) fn clear_pointer(&mut self) {
        self.dragging = None; self.drag_over = None; self.selecting = false;
        self.selection_origin = None; self.table_drag = None; self.cancel_extend(); crate::editor::table_resize::end();
    }
    pub(in crate::editor) fn hover_block(&mut self, id: u64, hovered: bool, cx: &mut Context<Self>) {
        if self.minimal || self.dragging.is_some() || self.table_drag.is_some() || self.table_extend.is_some() || crate::editor::table_resize::active().is_some() || self.selecting { return; }
        self.hover_gen = self.hover_gen.wrapping_add(1);
        if hovered {
            if self.hover != Some(id) { self.hover = Some(id); cx.notify(); }
            return;
        }
        if self.hover != Some(id) { return; }
        let generation = self.hover_gen;
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(std::time::Duration::from_millis(200)).await;
            this.update(cx, |this, cx| {
                if this.hover_gen == generation && this.hover == Some(id) {
                    this.hover = None;
                    cx.notify();
                }
            }).ok();
        }).detach();
    }
    pub(in crate::editor) fn begin_drag(&mut self, id: u64, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        self.commit_composition(cx); self.clear_pointer(); self.link_ui.reset_hover();
        self.hover_gen = self.hover_gen.wrapping_add(1);
        self.dragging = Some(id); self.hover = Some(id); self.toolbar_anchor = None;
        cx.notify();
    }
    pub(in crate::editor) fn focus_block(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        let at = Position { block: id, offset: 0 };
        self.set_head(at, false, cx); self.selecting = false; self.block_menu = None;
        window.focus(&self.focus, cx);
    }
    fn extent(&self, at: Position, clicks: usize) -> (Position, Position) {
        let Some(block) = self.doc.get(at.block) else { return (at, at); };
        let text = &block.text;
        let range = if clicks >= 3 { 0..text.len() }
            else if clicks == 2 { self.word_range_at(at) }
            else { at.offset..at.offset };
        (Position { offset: range.start, ..at }, Position { offset: range.end, ..at })
    }
    pub(in crate::editor) fn pointer_down(&mut self, event: &gpui::MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        self.table_menu = None; self.table_sel = None; self.block_menu = None; self.block_sel = None;
        let prev_active = self.annotate.active_annotation.take();
        self.annotate.followup = None;
        crate::editor::code_block::close_menus();
        self.close_format(window, cx);
        if self.path.is_none() || !self.list.viewport_bounds().contains(&event.position) { return; }
        if (event.modifiers.platform || (!cfg!(target_os = "macos") && event.modifiers.control))
            && (self.open_link_at(event.position, cx) || self.copy_code_at(event.position, cx)) { return; }
        self.commit_composition(cx); self.clear_pointer(); self.pending_vertical = None;
        let clicks = event.click_count.max(1);
        let hit = if clicks == 2 { self.hit_glyph(event.position) } else { self.hit(event.position) };
        if let Some(at) = hit {
            let (start, end) = self.extent(at, clicks);
            self.set_head(end, event.modifiers.shift, cx);
            if !event.modifiers.shift { self.caret.anchor = start; }
            self.selection_origin = Some((self.caret.anchor, end, clicks));
            if clicks == 1 && !event.modifiers.shift { self.code_escape_click(event.position, cx); }
            self.selecting = true; self.preferred_x = None; self.toolbar_anchor = None;
            self.reveal_caret = false; window.focus(&self.focus, cx);
            if clicks == 1 && !event.modifiers.shift
                && let Some(note_id) = self.annotation_at(at)
                && prev_active != Some(note_id)
            {
                self.activate_target_annotation(note_id, window, cx);
            }
        }
    }
    fn drag_span(&self, id: u64) -> Option<(usize, usize, Option<(u64, usize)>)> {
        let from = self.doc.index_of(id)?;
        let list = |b: &crate::editor::model::Block| matches!(b.kind, crate::editor::model::Kind::Bullet | crate::editor::model::Kind::Numbered | crate::editor::model::Kind::Todo);
        let head = &self.doc.blocks[from];
        let col = head.meta.column;
        let n = if list(head) {
            1 + (from + 1..self.doc.blocks.len()).take_while(|&i| {
                let b = &self.doc.blocks[i];
                list(b) && b.meta.column == col && b.meta.indent > head.meta.indent
            }).count()
        } else { 1 };
        Some((from, n, col))
    }
    fn update_drop(&mut self, position: Point<Pixels>) {
        self.drag_over = self.hit(position).and_then(|at| {
            let layout = self.layouts.get(&at.block)?;
            let index = self.doc.index_of(at.block)?;
            let after = position.y >= layout.bounds.top() + layout.bounds.size.height / 2.;
            if let Some(id) = self.dragging.filter(|&id| crate::editor::columns::group_from_handle(id).is_none()) {
                if let Some((from, n, col)) = self.drag_span(id) {
                    let dst_col = self.doc.blocks.get(index).and_then(|b| b.meta.column);
                    let slot = index + usize::from(after);
                    if col == dst_col && slot >= from && slot <= from + n { return None; }
                }
            }
            Some(DropTarget { index, after })
        });
    }
    pub(in crate::editor) fn pointer_move(&mut self, event: &gpui::MouseMoveEvent, cx: &mut Context<Self>) {
        if crate::editor::table_resize::active().is_some() {
            if !event.dragging() { self.clear_pointer(); cx.notify(); } else { self.resize_move(event.position.x, cx); }
            return;
        }
        if self.table_extend.is_some() {
            if !event.dragging() { self.clear_pointer(); cx.notify(); } else { self.extend_move(event.position.x, cx); }
            return;
        }
        if let Some(mut drag) = self.table_drag {
            if !event.dragging() { self.clear_pointer(); cx.notify(); return; }
            drag.moved |= (event.position - drag.start).magnitude() > 3.;
            drag.position = Some(event.position);
            drag.target = if drag.moved { self.table_chrome.as_ref().and_then(|c| c.target_at(drag, event.position)) } else { None };
            self.table_drag = Some(drag);
            if drag.moved { self.ensure_drag_ticker(cx); }
            cx.notify();
            return;
        }
        if !event.dragging() {
            self.update_link_hover(event.position, cx);
            self.update_code_hover(event.position, cx);
            if self.selecting || self.dragging.is_some() { self.clear_pointer(); cx.notify(); }
            return;
        }
        if self.dragging.is_none() && !self.selecting { return; }
        let viewport = self.list.viewport_bounds();
        let mut scrolled = false;
        if event.position.y < viewport.top()+px(24.) { self.list.scroll_by(px(-18.)); scrolled = true; }
        if event.position.y > viewport.bottom()-px(24.) { self.list.scroll_by(px(18.)); scrolled = true; }
        if self.dragging.is_some() {
            let prev = self.drag_over;
            self.update_drop(event.position);
            if self.drag_over != prev || scrolled { cx.notify(); }
            return;
        }
        if let Some((start, end, clicks)) = self.selection_origin {
            let hit = if clicks == 2 { self.hit_glyph(event.position) } else { self.hit(event.position) };
            if let Some(at) = hit {
                if clicks > 1 {
                    let (a, b) = self.extent(at, clicks);
                    let before = (self.doc.index_of(at.block), at.offset) < (self.doc.index_of(start.block), start.offset);
                    self.caret.anchor = if before { end } else { start };
                    self.set_head(if before { a } else { b }, true, cx);
                } else { self.set_head(at, true, cx); }
                if !self.caret.is_empty() {
                    self.annotate.active_annotation = None;
                    self.annotate.followup = None;
                }
                self.reveal_caret = false;
            }
        }
        cx.notify();
    }
    pub(in crate::editor) fn finish_drag(&mut self, position: Point<Pixels>, cx: &mut Context<Self>) {
        if self.table_extend.is_some() { self.finish_extend(cx); return; }
        if self.finish_resize(cx) { return; }
        if let Some(drag) = self.table_drag.take() {
            if drag.moved { self.table_sel = None; self.table_drop(drag, position, cx); }
            else { self.open_table_menu(drag.block, drag.sel, position + point(px(0.), px(14.)), cx); }
            cx.notify();
            return;
        }
        if self.dragging.is_some() && self.drag_over.is_some() { self.update_drop(position); }
        let dragging = self.dragging; let target = self.drag_over;
        self.clear_pointer();
        if let (Some(id), Some(target)) = (dragging, target) {
            if self.path.is_some() && self.list.viewport_bounds().contains(&position) {
                if let Some(gid) = crate::editor::columns::group_from_handle(id) {
                    self.finish_group_drag(gid, target, cx);
                } else if let Some((from, n, col)) = self.drag_span(id) {
                    let dst_col = self.doc.blocks.get(target.index).and_then(|b| b.meta.column);
                    let slot = target.index + usize::from(target.after);
                    let to = if slot > from { slot.saturating_sub(n) } else { slot }.min(self.doc.blocks.len() - n);
                    if (from != to || col != dst_col) && !(slot > from && slot < from + n) {
                        let keep_table_sel = self.table_sel == Some((id, crate::editor::table_actions::TableSel::Whole));
                        self.commit_composition(cx);
                        let before = self.snapshot();
                        let moved: Vec<_> = (from..from + n).map(|i| { let mut b = self.doc.blocks[i].clone(); b.meta.column = dst_col; b }).collect();
                        self.doc.splice_blocks(from..from + n, Vec::new());
                        self.doc.splice_blocks(to..to, moved);
                        self.repair_column_groups();
                        self.record(before, cx);
                        self.hover = Some(id);
                        if keep_table_sel { self.select_whole_table(id, cx); }
                    }
                }
            }
        }
        cx.notify();
    }
    /// Read only the geometry published by final paint, never prepaint attempts.
    pub(in crate::editor) fn selection_toolbar_anchor(&self, bounds: gpui::Bounds<Pixels>, window: &Window) -> Option<Point<Pixels>> {
        if self.path.is_none() || self.caret.is_empty() || self.selecting || self.dragging.is_some()
            || self.caret.composing.is_some() || !window.is_window_active() || !self.focus.is_focused(window) { return None; }
        let viewport = self.list.viewport_bounds();
        use crate::editor::chrome::{TOOLBAR_HEIGHT, TOOLBAR_WIDTH};
        let height = if viewport.size.width - px(16.) < TOOLBAR_WIDTH { TOOLBAR_HEIGHT * 2. - px(8.) } else { TOOLBAR_HEIGHT };
        if let Some((table_id, sel)) = self.table_sel {
            if sel != crate::editor::table_actions::TableSel::Whole { return None; }
            let tbl = self.table_chrome.as_ref().filter(|c| c.block == table_id).map(|c| c.bounds)
                .or_else(|| self.layouts.get(&table_id).and_then(crate::editor::table_layout::grid_rect))?;
            if tbl.bottom() <= viewport.top() || tbl.top() >= viewport.bottom() { return None; }
            let bar_w = px(396.);
            let x = (tbl.left() + (tbl.size.width - bar_w) / 2.)
                .max(bounds.left() + px(8.)).min((bounds.right() - bar_w - px(8.)).max(bounds.left()));
            let above = tbl.top() - height - px(8.);
            let y = if above >= viewport.top() { above } else { tbl.bottom() + px(8.) };
            return Some(point(x, y.min((viewport.bottom() - height).max(viewport.top())).max(viewport.top())));
        }
        let rect = self.layouts.iter().filter_map(|(id, layout)| {
            let range = self.selected_range(*id)?;
            if range.is_empty() { return None; }
            layout.rects(range).into_iter().find(|r| r.bottom() > viewport.top() && r.top() < viewport.bottom())
        }).min_by(|a, b| a.top().partial_cmp(&b.top()).unwrap_or(std::cmp::Ordering::Equal))?;
        let x = rect.left().max(bounds.left()+px(8.)).min((bounds.right()-TOOLBAR_WIDTH-px(8.)).max(bounds.left()));
        let above = rect.top() - height - px(8.);
        let y = if above >= viewport.top() { above } else { rect.bottom()+px(8.) };
        Some(point(x, y.min((viewport.bottom()-height).max(viewport.top())).max(viewport.top())))
    }
}
