//! Trailing "+" control below the final block, matching thinkly's `EditorTrailingAdd`.
use gpui::{AnyElement, Bounds, Context, Entity, Pixels, Point, div, point, prelude::*, px, size};
use crate::{
    icons::Icon,
    theme::{Theme, build_tooltip, tr},
    ui::{reveal::handle_button, tooltip::TooltipExt},
};
use super::{
    block_menu::{Mode, SLASH_MENU_WIDTH},
    block_ops::is_plain_empty_paragraph,
    chrome::{HANDLE_WIDTH, ROW_GAP, collapsed_gap, margins},
    model::{Block, Kind},
    view::Editor,
};

#[derive(Clone, Copy)]
struct TrailingSlot {
    button: Bounds<Pixels>,
    line_top: Pixels,
    line_h: f32,
    row_left: Pixels,
    zoom: f32,
}

impl Editor {
    fn trailing_slot(&self) -> Option<TrailingSlot> {
        if self.minimal || !self.is_markdown_note() || self.editing_source()
            || self.has_active_overlay() || self.dragging.is_some() || self.selecting
        {
            return None;
        }
        let last_idx = self.last_visible_index()?;
        let last_block = &self.doc.blocks[last_idx];
        if is_plain_empty_paragraph(last_block) {
            return None;
        }
        let last_row = *self.painted_rows.get(&last_block.id)?;
        let vp = self.list.viewport_bounds();
        let zoom = super::zoom::zoom_factor(false);
        let pb = margins(last_block).1 * zoom;
        let gap = collapsed_gap(last_block, &Block::new(0, Kind::Paragraph, String::new())) * zoom;
        let line_top = last_row.bottom() - px(pb) + px(gap);
        let (font_sz, factor) = super::paint::heading_metrics(
            Kind::Paragraph, None, false, false, f32::from(Theme::ocean().editor_font), zoom,
        );
        let line_h = font_sz * factor;
        let btn = 24. * zoom;
        let top = line_top + px((line_h - btn).max(0.) * 0.5);
        if top < vp.top() || top + px(btn) > vp.bottom() {
            return None;
        }
        let left = last_row.left() + px(HANDLE_WIDTH - btn);
        Some(TrailingSlot {
            button: Bounds::new(point(left, top), size(px(btn), px(btn))),
            line_top,
            line_h,
            row_left: last_row.left(),
            zoom,
        })
    }

    /// Updates trailing-blank hover state: reuses the final empty paragraph's own "+" when present,
    /// otherwise lights up the trailing "+" below the last block.
    pub(in crate::editor) fn update_trailing_hover(&mut self, pos: Point<Pixels>, cx: &mut Context<Self>) -> bool {
        if self.minimal || !self.is_markdown_note() || self.editing_source()
            || self.has_active_overlay() || self.dragging.is_some() || self.selecting
        {
            if self.trailing_plus {
                self.trailing_plus = false;
                cx.notify();
            }
            return false;
        }
        let Some(last_idx) = self.last_visible_index() else {
            if self.trailing_plus { self.trailing_plus = false; cx.notify(); }
            return false;
        };
        let last_block = &self.doc.blocks[last_idx];
        let last_id = last_block.id;
        let Some(&last_row) = self.painted_rows.get(&last_id) else {
            if self.trailing_plus { self.trailing_plus = false; cx.notify(); }
            return false;
        };
        let vp = self.list.viewport_bounds();
        let in_blank_below = pos.y >= last_row.bottom()
            && pos.y <= vp.bottom()
            && pos.x >= last_row.left() - px(16.)
            && pos.x <= last_row.right();
        if is_plain_empty_paragraph(last_block) {
            if self.trailing_plus { self.trailing_plus = false; cx.notify(); }
            if in_blank_below {
                if self.hover != Some(last_id) {
                    self.hover_gen = self.hover_gen.wrapping_add(1);
                    self.hover = Some(last_id);
                    cx.notify();
                }
                return true;
            }
            return false;
        }
        let Some(slot) = self.trailing_slot() else {
            if self.trailing_plus { self.trailing_plus = false; cx.notify(); }
            return false;
        };
        let corridor = Bounds::from_corners(
            point(slot.button.left() - px(8.), slot.button.top().min(last_row.bottom()) - px(6.)),
            point(slot.row_left + px(HANDLE_WIDTH + 32.), slot.button.bottom().max(last_row.bottom()) + px(8.)),
        );
        let active = in_blank_below || (self.trailing_plus && corridor.contains(&pos));
        if active {
            let mut changed = !self.trailing_plus;
            self.trailing_plus = true;
            if self.hover.take().is_some() {
                self.hover_gen = self.hover_gen.wrapping_add(1);
                changed = true;
            }
            if changed { cx.notify(); }
            return true;
        }
        if self.trailing_plus {
            self.trailing_plus = false;
            cx.notify();
        }
        false
    }
}

pub(super) fn overlay(editor_ref: &Editor, editor: Entity<Editor>) -> Option<AnyElement> {
    if !editor_ref.trailing_plus { return None; }
    let slot = editor_ref.trailing_slot()?;
    let origin = slot.button.origin - editor_ref.editor_bounds.origin;
    let trigger = Bounds::new(
        point(slot.row_left + px(HANDLE_WIDTH + ROW_GAP), slot.line_top),
        size(px(SLASH_MENU_WIDTH), px(slot.line_h)),
    );
    let btn = handle_button(Icon::Plus, ("trailing-add", 0), slot.zoom, move |_, window, cx| {
        editor.update(cx, |this, cx| {
            this.trailing_plus = false;
            if this.insert_bottom_paragraph(cx) {
                let id = this.caret.head.block;
                window.focus(&this.focus, cx);
                this.open_block_menu_at(id, trigger, Mode::Slash, cx);
            }
        });
        cx.stop_propagation();
    })
    .tooltip_above(|window, cx| build_tooltip(tr("添加块", "Add block"), window, cx));
    Some(
        div()
            .absolute()
            .left(origin.x)
            .top(origin.y)
            .child(crate::ui::motion::reveal_fade(btn, "trailing-add-reveal"))
            .into_any_element(),
    )
}
