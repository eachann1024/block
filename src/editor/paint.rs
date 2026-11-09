//! Visible block shaping supplies drawing, selection, hit testing and IME geometry.
use std::{cell::RefCell, rc::Rc};
use gpui::{App, AvailableSpace, Bounds, Element, ElementId, FontWeight, GlobalElementId,
    IntoElement, LayoutId, Pixels, SharedString, Style, TextAlign, Window,
    fill, point, px, relative, rgb, size};
use crate::theme::Theme;
use super::{model::Kind, text_runs::{runs, plain}, table_layout,
    view::{Editor, layout::TextLayout}};

pub(super) struct BlockText { pub editor: gpui::Entity<Editor>, pub block: u64 }
type Measured = Rc<RefCell<Option<TextLayout>>>;
impl Element for BlockText {
    type RequestLayoutState = Measured;
    type PrepaintState = (TextLayout, Option<gpui::Hitbox>);
    fn id(&self) -> Option<ElementId> { None }
    fn source_location(&self) -> Option<&'static core::panic::Location<'static>> { None }
    fn request_layout(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, window: &mut Window, cx: &mut App) -> (LayoutId, Measured) {
        let theme = Theme::ocean();
        let measured: Measured = Rc::new(RefCell::new(None));
        let Some((kind, level, fenced, nowrap, composing, content, minimal)) = ({
            let editor = self.editor.read(cx);
            let minimal = editor.minimal;
            editor.doc.get(self.block).map(|block| {
                let nowrap = block.meta.code_language.is_some() && editor.nowrap.contains(&self.block);
                let composing = editor.caret.composing.is_some() && editor.caret.head.block == self.block;
                (block.kind, block.meta.heading_level, block.meta.code_language.clone(), nowrap, composing, super::view::layout::block_hash(block, minimal) ^ editor.ai_preview_key(self.block) ^ table_layout::widths_signature(self.editor.entity_id(), self.block), minimal)
            })
        }) else {
            return (window.request_layout(Style::default(), [], cx), measured);
        };
        let output = measured.clone();
        // The row owns the gutters; measure text only in its remaining flex space.
        let mut style = Style::default();
        style.flex_grow = 1.; style.flex_shrink = 1.;
        style.flex_basis = relative(0.).into();
        let zoom = super::zoom::zoom_factor(minimal);
        if let Some(language) = &fenced { style.margin = super::code_block::margin(language, zoom); }
        style.min_size.width = if nowrap { gpui::Length::Auto } else { px(0.).into() };
        let (size_px, factor) = heading_metrics(kind, level, fenced.is_some(), minimal, f32::from(theme.editor_font), zoom);
        let (font_size, line_height) = (px(size_px), px(size_px) * factor);
        let editor_entity = self.editor.clone();
        let block_id = self.block;
        let id = window.request_measured_layout(style, move |known, available, window, cx| {
            let room = known.width.or(match available.width { AvailableSpace::Definite(w) => Some(w), _ => None }).unwrap_or(px(0.));
            let width = known.width.unwrap_or_else(|| match available.width { AvailableSpace::Definite(w) => w, _ => px(640.) }).max(px(1.));
            let key = super::view::layout::LayoutKey { content, width_bits: if nowrap { 0 } else { f32::from(width).to_bits() }, nowrap };
            let editor_id = editor_entity.entity_id();
            if !composing && super::view::layout::get_key(editor_id, block_id) == Some(key) {
                let borrow = output.borrow();
                let editor = editor_entity.read(cx);
                if let Some(cached) = borrow.as_ref().or_else(|| editor.layouts.get(&block_id)) {
                    if nowrap {
                        let widest = cached.lines.iter().map(|(_, l)| l.width()).fold(px(0.), |a, b| a.max(b)) + px(2.);
                        return size(widest.max(room).max(px(1.)), cached.height().max(line_height));
                    }
                    return size(width, cached.bounds.size.height.max(line_height));
                }
            }
            let (text, runs, table, code_spans) = {
                let editor = editor_entity.read(cx);
                let Some(block) = editor.doc.get(block_id) else { return size(width, line_height); };
                let text: SharedString = if block.text.is_empty() { "\u{200b}".into() } else { block.text.clone().into() };
                let mut runs = runs(block, &text, theme.text);
                if matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3) {
                    let is_h6 = block.kind == Kind::H3 && block.meta.heading_level == Some(6);
                    let weight = FontWeight(if is_h6 { 500. } else { 600. });
                    for run in &mut runs { run.font.weight = weight; if is_h6 { run.color = theme.text_secondary; } }
                }
                let language = block.meta.code_language.as_deref().or_else(|| {
                    block.meta.raw_source.as_ref().map(|_| if text.trim_start().starts_with('<') { "html" } else { "markdown" })
                });
                if let Some(language) = language {
                    let code_fg: gpui::Hsla = if crate::theme::is_dark() { theme.text } else { rgb(0x393a34).into() };
                    let mut base = plain(text.len(), code_fg); base.font.family = crate::theme::mono_font_family().into(); base.font.fallbacks = None;
                    runs = super::view::syntax::runs(&text, language, base);
                }
                // Inline AI review: the rewrite is diffed in place (thinkly inlinePreview).
                let (text, runs, has_ai) = match editor.ai_preview(block_id, &runs) { Some((t, r)) => (t.into(), r, true), None => (text, runs, false) };
                let code_spans = if has_ai { Vec::new() } else { super::inline_code::code_spans(block) };
                let (text, runs) = super::inline_code::prepare_shaping(&text, runs, &code_spans);
                (text, runs, block.meta.table.clone(), code_spans)
            };
            if nowrap {
                let shaped = window.text_system().shape_text(text, font_size, &runs, None, None).unwrap_or_default();
                let widest = shaped.iter().map(|l| l.width()).fold(px(0.), |a, b| a.max(b)) + px(2.);
                let mut start = 0;
                let lines = shaped.into_iter().map(|line| { let offset = start; start += line.len() + 1; (offset, line) }).collect();
                let layout = TextLayout { bounds: Bounds::default(), lines, line_height, cells: Vec::new(), alignment: TextAlign::Left, code_spans };
                let height = layout.height().max(line_height);
                *output.borrow_mut() = Some(layout); super::view::layout::set_key(editor_id, block_id, key);
                return size(widest.max(room).max(px(1.)), height);
            }
            if let Some(table) = &table {
                let mut layout = table_layout::measure(table, width, &table_layout::widths_of(editor_id, block_id), window, zoom);
                let height = layout.height() + px(table_layout::EXTEND_ROOM * zoom);
                layout.bounds.size = size(width, height);
                *output.borrow_mut() = Some(layout); super::view::layout::set_key(editor_id, block_id, key);
                return size(width, height);
            }
            let shaped = window.text_system().shape_text(text, font_size, &runs, Some(width), None).unwrap_or_default();
            let mut start = 0;
            let lines = shaped.into_iter().map(|line| { let offset = start; start += line.len() + 1; (offset, line) }).collect();
            let mut layout = TextLayout { bounds: Bounds::default(), lines, line_height, cells: Vec::new(), alignment: TextAlign::Left, code_spans };
            let height = layout.height().max(line_height);
            layout.bounds.size = size(width, height);
            *output.borrow_mut() = Some(layout); super::view::layout::set_key(editor_id, block_id, key);
            size(width, height)
        });
        (id, measured)
    }
    fn prepaint(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, bounds: Bounds<Pixels>, state: &mut Measured, window: &mut Window, cx: &mut App) -> (TextLayout, Option<gpui::Hitbox>) {
        let fresh = state.borrow().is_some();
        let mut layout = state.borrow_mut().take()
            .or_else(|| self.editor.update(cx, |this, _| this.layouts.remove(&self.block)))
            .unwrap_or_else(|| TextLayout::empty(px(21.)));
        let delta = bounds.origin - layout.bounds.origin;
        if delta.x != px(0.) || delta.y != px(0.) { layout.translate(delta); }
        layout.bounds = bounds; super::block_menu::record_live_block_bounds(self.editor.entity_id(), self.block, bounds);
        let editor = self.editor.read(cx);
        if !layout.cells.is_empty() {
            // Wide tables scroll sideways; keep the caret's cell in view when it moves.
            let caret = (editor.reveal_caret && editor.caret.head.block == self.block).then_some(editor.caret.head.offset);
            table_layout::settle(self.editor.entity_id(), self.block, &mut layout, fresh, caret);
        }
        if editor.reveal_caret && editor.caret.head.block == self.block {
            let offset = vertical_offset(editor, self.block, &layout).unwrap_or(editor.caret.head.offset);
            window.request_autoscroll(Bounds::new(layout.point(offset), size(px(2.), layout.line_height)));
        }
        let hitbox = (!layout.cells.is_empty())
            .then(|| super::table_actions::reach_rect(&layout))
            .flatten()
            .map(|reach| window.insert_hitbox(reach, gpui::HitboxBehavior::Normal));
        (layout, hitbox)
    }
    fn paint(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, bounds: Bounds<Pixels>, _: &mut Measured, (layout, hitbox): &mut (TextLayout, Option<gpui::Hitbox>), window: &mut Window, cx: &mut App) {
        self.editor.update(cx, |this, _| {
            if let Some(offset) = vertical_offset(this, self.block, layout)
                && let Some((_, _, _, extend)) = this.pending_vertical.take()
            {
                this.caret.head.offset = offset;
                if !extend { this.caret.anchor = this.caret.head; }
            }
        });
        let (focused, selected, head, empty, composing, placeholder, link_ranges) = {
            let editor = self.editor.read(cx);
            let placeholder = editor.doc.get(self.block).and_then(|block| placeholder_text(block,
                editor.is_first_line_delete_armed(self.block)));
            (editor.focus.is_focused(window), editor.selected_range(self.block), editor.caret.head,
                editor.caret.is_empty(), editor.caret.composing.clone(),
                placeholder.filter(|_| editor.path.is_some() && editor.caret.head.block == self.block && editor.caret.composing.is_none()),
                editor.active_link_ranges(self.block, window))
        };
        let chip = super::inline_code_chip::ChipState { caret: (focused && empty && head.block == self.block
                && super::code_escape::side(self.editor.entity_id(), head).is_none()).then_some(head.offset),
            selection: if focused { selected.clone() } else { None }, hover: super::inline_code::hovered(self.editor.entity_id(), self.block) };
        let (fenced, minimal) = {
            let ed = self.editor.read(cx);
            (ed.doc.get(self.block).and_then(|b| b.meta.code_language.clone()), ed.minimal)
        };
        let zoom = super::zoom::zoom_factor(minimal);
        if let Some(language) = &fenced { super::code_block::paint_card(layout.bounds, language, window, cx, zoom); }
        // A table scrolls inside the block minus the extend-button room; everything it draws is clipped to that.
        let is_table = !layout.cells.is_empty();
        let clip = is_table.then(|| gpui::ContentMask { bounds: table_layout::visible(layout) });
        if is_table { table_layout::track_wheel(self.editor.clone(), self.block, layout, hitbox.clone(), window); }
        window.with_content_mask(clip, |window| {
            table_layout::paint_background(layout, window);
            super::find_bar::paint_matches(self.editor.read(cx), self.block, layout, window);
            super::annotate::paint_highlights(self.editor.read(cx), self.block, layout, window);
        });
        super::ai::preview::paint_outline(self.editor.read(cx), self.block, layout, window);
        if is_table { super::table_actions::track_hover(self.editor.clone(), self.block, layout, hitbox.clone(), window); }
        let tint = {
            let editor = self.editor.read(cx);
            match (editor.table_sel, editor.doc.get(self.block).and_then(|b| b.meta.table.as_ref())) {
                (Some((id, sel)), Some(table)) if id == self.block && table.columns > 0
                    && layout.cells.len() == table.rows.len() * table.columns => Some((sel, table.columns)),
                _ => None,
            }
        };
        window.with_content_mask(clip, |window| {
            if let Some((sel, columns)) = tint { table_layout::paint_selection(layout, sel, columns, window); }
            super::inline_code_chip::paint_fills(layout, &chip, window);
            paint_lines(layout, true, window, cx);
            let mut sel_rects = Vec::new();
            if focused {
                if !tint.is_some_and(|(s, _)| matches!(s, super::table_actions::TableSel::Whole)) {
                    if let Some(range) = selected {
                        // Crossing cells selects whole cells with the accent tint; otherwise highlight the text.
                        if !table_layout::paint_cell_selection(layout, &range, window) {
                            sel_rects = super::inline_code_chip::paint_selection(layout, range, window);
                        }
                    }
                }
                // Hide the caret and IME composition chrome when the window is inactive while keeping selection visible.
                if head.block == self.block && window.is_window_active() {
                    let p = layout.point(head.offset);
                    let p = super::code_escape::side(self.editor.entity_id(), head).map_or(p, |end| super::code_escape::caret_point(layout, p, end));
                    let line_h = layout.cells.first().map_or(layout.line_height, |c| c.text.line_height);
                    let font_px = layout.cells.first()
                        .and_then(|c| c.text.lines.first().map(|(_, l)| l.font_size()))
                        .or_else(|| layout.lines.first().map(|(_, l)| l.font_size()))
                        .unwrap_or(line_h * 0.67);
                    let caret_h = (font_px * 1.125).round().min(line_h);
                    let origin = point(p.x, p.y + ((line_h - caret_h) * 0.5).round());
                    if empty {
                        window.paint_quad(fill(Bounds::new(origin, size(px(1.5), caret_h)), if self.editor.read(cx).is_first_line_delete_armed(self.block) { crate::ui::reveal::delete_arm_caret_color() } else { Theme::ocean().accent }));
                        crate::sync_ime_cursor_area(f32::from(origin.x), f32::from(origin.y), f32::from(caret_h));
                    }
                    if let Some(range) = composing {
                        let rects = layout.rects(range);
                        if let Some(rect) = rects.last().or_else(|| rects.first()) {
                            crate::sync_ime_cursor_area(f32::from(rect.left()), f32::from(rect.top()), f32::from(rect.size.height));
                        }
                        for rect in rects {
                            window.paint_quad(fill(Bounds::new(point(rect.left(), rect.bottom()-px(1.)), size(rect.size.width, px(1.))), Theme::ocean().accent));
                        }
                    } else if !empty {
                        let start = self.editor.read(cx).ordered().0;
                        let sp = if start.block == self.block { layout.point(start.offset) } else { p };
                        crate::sync_ime_cursor_area(f32::from(sp.x), f32::from(sp.y), f32::from(line_h));
                    }
                }
            }
            super::inline_code_chip::paint_borders(layout, &chip, window);
            paint_lines(layout, false, window, cx);
            for rect in &sel_rects {
                window.with_content_mask(Some(gpui::ContentMask { bounds: *rect }), |window| super::text_runs::paint_tinted_lines(layout, Theme::ocean().selection_text, window));
            }
            let link_ul = gpui::UnderlineStyle { color: Some(rgb(0x256abf).into()), thickness: px(1.), wavy: false };
            for range in link_ranges {
                for rect in layout.rects(range) { window.paint_underline(point(rect.left(), rect.bottom() - px(3.)), rect.size.width, &link_ul); }
            }
            if is_table {
                table_layout::paint_edge(layout, window);
                super::annotate::paint_table_highlights(self.editor.read(cx), self.block, layout, window);
            }
        });
        if let Some(text) = placeholder { paint_placeholder(layout, text, self.editor.read(cx).is_first_line_delete_armed(self.block), window, cx); }
        let saved = std::mem::replace(layout, TextLayout::empty(px(0.)));
        self.editor.update(cx, |this, _| { this.layouts.insert(self.block, saved); });
        let _ = bounds;
    }
}
pub(super) use crate::ui::editor_content::heading_metrics;
fn placeholder_text(block: &super::model::Block, delete_armed: bool) -> Option<&'static str> {
    if !block.text.is_empty() || block.meta.table.is_some() || block.meta.raw_source.is_some()
        || block.meta.code_language.is_some() { return None; }
    use crate::theme::tr;
    Some(match (block.kind, block.meta.heading_level) {
        (Kind::Paragraph, _) if delete_armed => crate::ui::reveal::delete_arm_placeholder_text(),
        (Kind::Paragraph, _) => tr("输入 / 或 、 唤出菜单，或开始输入…", "Type / to open menu, or start typing…"),
        (Kind::H1, _) => tr("标题 1", "Heading 1"),
        (Kind::H2, _) => tr("标题 2", "Heading 2"),
        (Kind::H3, Some(4)) => tr("标题 4", "Heading 4"),
        (Kind::H3, Some(5)) => tr("标题 5", "Heading 5"),
        (Kind::H3, Some(6)) => tr("标题 6", "Heading 6"),
        (Kind::H3, _) => tr("标题 3", "Heading 3"),
        _ => return None,
    })
}
thread_local! { static PLACEHOLDER: RefCell<Option<(u64, &'static str, gpui::ShapedLine)>> = const { RefCell::new(None) }; }
/// Hint drawn over the zero-width placeholder glyph; never part of the text or hit testing.
fn paint_placeholder(layout: &TextLayout, text: &'static str, delete_armed: bool, window: &mut Window, cx: &mut App) {
    let dark = crate::theme::is_dark();
    let font_size = layout.lines.first().map_or(Theme::ocean().editor_font, |(_, l)| l.font_size());
    let key = crate::theme::font_settings_hash() ^ ((crate::theme::is_en() as u64) << 32)
        ^ ((dark as u64) << 33) ^ (((f32::from(font_size) as u64) & 0xff) << 34) ^ ((delete_armed as u64) << 42);
    let line = PLACEHOLDER.with(|slot| {
        let mut slot = slot.borrow_mut();
        if slot.as_ref().is_none_or(|(c, cached_text, _)| *c != key || *cached_text != text) {
            let color = if delete_armed { crate::ui::reveal::delete_arm_placeholder_color() } else if dark { rgb(0x6e6d68).into() } else { rgb(0x9b9a95).into() };
            let mut run = plain(text.len(), color); run.font.weight = FontWeight::NORMAL;
            *slot = Some((key, text, window.text_system().shape_line(SharedString::from(text), font_size, &[run], None)));
        }
        slot.as_ref().map(|(_, _, l)| l.clone()).unwrap()
    });
    let _ = line.paint(layout.bounds.origin, layout.line_height, gpui::TextAlign::Left, None, window, cx);
}
impl IntoElement for BlockText { type Element = Self; fn into_element(self) -> Self { self } }
fn vertical_offset(editor: &Editor, block: u64, layout: &TextLayout) -> Option<usize> {
    let (id, x, down, _) = editor.pending_vertical?;
    if id != block { return None; }
    let y = if down { layout.bounds.top() } else { layout.bounds.bottom()-px(1.) };
    Some(super::view::selection::boundary(editor.block_text(), layout.hit(point(x, y))))
}
fn paint_lines(layout: &TextLayout, backgrounds: bool, window: &mut Window, cx: &mut App) {
    for cell in &layout.cells { paint_lines(&cell.text, backgrounds, window, cx); }
    let mut y = layout.bounds.top();
    for (_, line) in &layout.lines {
        let origin = point(layout.bounds.left(),y);
        let _ = if backgrounds { line.paint_background(origin,layout.line_height,layout.alignment,Some(layout.bounds),window,cx) }
            else { line.paint(origin,layout.line_height,layout.alignment,Some(layout.bounds),window,cx) };
        y += line.size(layout.line_height).height;
    }
}
