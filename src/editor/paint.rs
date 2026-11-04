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
        let Some((kind, level, fenced, nowrap, composing, content)) = ({
            let editor = self.editor.read(cx);
            editor.doc.get(self.block).map(|block| {
                let nowrap = block.meta.code_language.is_some() && editor.nowrap.contains(&self.block);
                let composing = editor.caret.composing.is_some() && editor.caret.head.block == self.block;
                (block.kind, block.meta.heading_level, block.meta.code_language.clone(), nowrap, composing, super::view::layout::block_hash(block) ^ editor.ai_preview_key(self.block) ^ table_layout::widths_signature(self.editor.entity_id(), self.block))
            })
        }) else {
            return (window.request_layout(Style::default(), [], cx), measured);
        };
        let output = measured.clone();
        // The row owns the gutters; measure text only in its remaining flex space.
        let mut style = Style::default();
        style.flex_grow = 1.; style.flex_shrink = 1.;
        style.flex_basis = relative(0.).into();
        if let Some(language) = &fenced { style.margin = super::code_block::margin(language); }
        style.min_size.width = if nowrap { gpui::Length::Auto } else { px(0.).into() };
        let (size_px, factor) = heading_metrics(kind, level, fenced.is_some(), self.editor.read(cx).minimal, f32::from(theme.editor_font));
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
                let mut layout = table_layout::measure(table, width, &table_layout::widths_of(editor_id, block_id), window);
                let height = layout.height() + px(table_layout::EXTEND_ROOM);
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
        layout.bounds = bounds;
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
            if this.caret.head.block == self.block { this.reveal_caret = false; }
        });
        let (focused, selected, head, empty, composing, placeholder, link_ranges) = {
            let editor = self.editor.read(cx);
            let blank = editor.doc.get(self.block).is_some_and(|b| b.text.is_empty() && b.kind == Kind::Paragraph
                && b.meta.table.is_none() && b.meta.raw_source.is_none());
            (editor.focus.is_focused(window), editor.selected_range(self.block), editor.caret.head,
                editor.caret.is_empty(), editor.caret.composing.clone(),
                blank && editor.path.is_some() && editor.caret.head.block == self.block && editor.caret.composing.is_none(),
                editor.active_link_ranges(self.block, window))
        };
        let fenced = self.editor.read(cx).doc.get(self.block).and_then(|b| b.meta.code_language.clone());
        if let Some(language) = &fenced { super::code_block::paint_card(layout.bounds, language, window, cx); }
        // A table scrolls inside the block minus the extend-button room; everything it draws is clipped to that.
        let is_table = !layout.cells.is_empty();
        let clip = is_table.then(|| gpui::ContentMask { bounds: table_layout::visible(layout.bounds) });
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
            super::inline_code::paint_chips(layout, window);
            paint_lines(layout, true, window, cx);
            let mut sel_rects = Vec::new();
            if focused {
                if !tint.is_some_and(|(s, _)| matches!(s, super::table_actions::TableSel::Whole)) {
                    if let Some(range) = selected {
                        // Crossing cells selects whole cells with the accent tint; otherwise highlight the text.
                        if !table_layout::paint_cell_selection(layout, &range, window) {
                            sel_rects = super::inline_code::paint_selection(layout, range, window);
                        }
                    }
                }
                if head.block == self.block {
                    let p = layout.point(head.offset);
                    let line_h = layout.cells.first().map_or(layout.line_height, |c| c.text.line_height);
                    let font_px = layout.cells.first()
                        .and_then(|c| c.text.lines.first().map(|(_, l)| l.font_size()))
                        .or_else(|| layout.lines.first().map(|(_, l)| l.font_size()))
                        .unwrap_or(line_h * 0.67);
                    let caret_h = (font_px * 1.125).round().min(line_h);
                    let origin = point(p.x, p.y + ((line_h - caret_h) * 0.5).round());
                    if empty {
                        window.paint_quad(fill(Bounds::new(origin, size(px(1.5), caret_h)), Theme::ocean().accent));
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
        if placeholder { paint_placeholder(layout, window, cx); }
        let saved = std::mem::replace(layout, TextLayout::empty(px(0.)));
        self.editor.update(cx, |this, _| { this.layouts.insert(self.block, saved); });
        let _ = bounds;
    }
}
/// (font size px, line-height factor) per block kind; shared with the handle gutter.
pub(super) fn metrics(kind: Kind, base: f32) -> (f32, f32) {
    match kind { Kind::H1 => (32., 1.2), Kind::H2 => (22., 1.3), Kind::H3 => (18., 1.35), _ => (base, 1.5) }
}
pub(super) fn heading_metrics(kind: Kind, level: Option<u8>, fenced: bool, minimal: bool, base: f32) -> (f32, f32) {
    if fenced { return (14., 1.5); }
    match (kind, level) {
        (Kind::H3, Some(4)) => (16., 1.4), (Kind::H3, Some(5 | 6)) => (14., 1.45),
        (Kind::H1 | Kind::H2 | Kind::H3, _) => metrics(kind, base),
        _ if minimal => (16., 1.5), _ => (base, 1.5),
    }
}
thread_local! { static PLACEHOLDER: RefCell<Option<(u16, gpui::ShapedLine)>> = const { RefCell::new(None) }; }
/// Hint drawn over the zero-width placeholder glyph; never part of the text or hit testing.
fn paint_placeholder(layout: &TextLayout, window: &mut Window, cx: &mut App) {
    let dark = crate::theme::is_dark();
    let font_size = layout.lines.first().map_or(Theme::ocean().editor_font, |(_, l)| l.font_size());
    let key = (crate::theme::font_choice() as u16) | ((crate::theme::is_en() as u16) << 4)
        | ((dark as u16) << 5) | ((f32::from(font_size) as u16) << 8);
    let line = PLACEHOLDER.with(|slot| {
        let mut slot = slot.borrow_mut();
        if slot.as_ref().is_none_or(|(c, _)| *c != key) {
            let text = SharedString::from(crate::theme::tr("输入 / 或 、 唤出菜单，或开始输入…", "Type / to open menu, or start typing…"));
            let color = if dark { rgb(0x6e6d68) } else { rgb(0x9b9a95) };
            let mut run = plain(text.len(), color.into()); run.font.weight = FontWeight::NORMAL;
            *slot = Some((key, window.text_system().shape_line(text, font_size, &[run], None)));
        }
        slot.as_ref().map(|(_, l)| l.clone()).unwrap()
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
