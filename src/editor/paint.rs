//! Visible block shaping supplies drawing, selection, hit testing and IME geometry.
use std::{cell::RefCell, rc::Rc};
use gpui::{App, AvailableSpace, BorderStyle, Bounds, Element, ElementId, FontWeight, GlobalElementId,
    IntoElement, LayoutId, Pixels, SharedString, Style, TextAlign, Window,
    fill, point, px, quad, relative, rgb, size, transparent_black};
use crate::theme::Theme;
use super::{model::Kind, text_runs::{runs, plain}, table_layout,
    view::{Editor, layout::TextLayout}};

pub(super) struct BlockText { pub editor: gpui::Entity<Editor>, pub block: u64 }
type Measured = Rc<RefCell<Option<TextLayout>>>;
impl Element for BlockText {
    type RequestLayoutState = Measured;
    type PrepaintState = TextLayout;
    fn id(&self) -> Option<ElementId> { None }
    fn source_location(&self) -> Option<&'static core::panic::Location<'static>> { None }
    fn request_layout(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, window: &mut Window, cx: &mut App) -> (LayoutId, Measured) {
        let theme = Theme::ocean();
        let (kind, fenced, nowrap, composing, content) = {
            let editor = self.editor.read(cx);
            let block = editor.doc.get(self.block).expect("visible block exists");
            let nowrap = block.meta.code_language.is_some() && editor.nowrap.contains(&self.block);
            let composing = editor.caret.composing.is_some() && editor.caret.head.block == self.block;
            (block.kind, block.meta.code_language.clone(), nowrap, composing, super::view::layout::block_hash(block) ^ editor.ai_preview_key(self.block))
        };
        let (size_px, factor) = metrics(kind, f32::from(theme.editor_font));
        let font_size = px(size_px);
        let line_height = font_size * factor;
        let measured: Measured = Rc::new(RefCell::new(None));
        let output = measured.clone();
        // The row owns the gutters; measure text only in its remaining flex space.
        let mut style = Style::default();
        style.flex_grow = 1.; style.flex_shrink = 1.;
        style.flex_basis = relative(0.).into();
        if let Some(language) = &fenced { style.margin = super::code_block::margin(language); }
        style.min_size.width = if nowrap { gpui::Length::Auto } else { px(0.).into() };
        let _ = line_height;
        let (mut size_px, factor) = block_metrics(kind, fenced.is_some(), self.editor.read(cx).minimal, f32::from(theme.editor_font));
        if kind == Kind::H3 && fenced.is_none() {
            let level = self.editor.read(cx).doc.get(self.block).and_then(|b| b.meta.heading_level);
            size_px = match level { Some(4) => 17., Some(5) => 16., Some(6) => 15., _ => size_px };
        }
        let (font_size, line_height) = (px(size_px), px(size_px) * factor);
        let editor_entity = self.editor.clone();
        let block_id = self.block;
        let id = window.request_measured_layout(style, move |known, available, window, cx| {
            let room = known.width.or(match available.width { AvailableSpace::Definite(w) => Some(w), _ => None }).unwrap_or(px(0.));
            let width = known.width.unwrap_or_else(|| match available.width { AvailableSpace::Definite(w) => w, _ => px(640.) }).max(px(1.));
            let selected = {
                let editor = editor_entity.read(cx);
                let whole = editor.table_sel == Some((block_id, super::table_actions::TableSel::Whole));
                (editor.focus.is_focused(window) && !whole).then(|| editor.selected_range(block_id)).flatten()
            };
            let sel_hash = selected.as_ref().map_or(0, |r| ((r.start as u64).wrapping_add(1) << 32) ^ (r.end as u64).wrapping_add(1));
            let key = super::view::layout::LayoutKey {
                content: content ^ sel_hash,
                width_bits: if nowrap { 0 } else { f32::from(width).to_bits() },
                nowrap,
            };
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
            let (text, runs, table) = {
                let editor = editor_entity.read(cx);
                let block = editor.doc.get(block_id).expect("visible block exists");
                let text: SharedString = if block.text.is_empty() { "\u{200b}".into() } else { block.text.clone().into() };
                let mut runs = runs(block, &text, theme.text);
                if matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3) {
                    for run in &mut runs { run.font.weight = FontWeight(560.); }
                }
                let language = block.meta.code_language.as_deref().or_else(|| {
                    block.meta.raw_source.as_ref().map(|_| if text.trim_start().starts_with('<') { "html" } else { "markdown" })
                });
                if let Some(language) = language {
                    let mut base = plain(text.len(), theme.text); base.font.family = crate::theme::mono_font_family().into(); base.font.fallbacks = None;
                    runs = super::view::syntax::runs(&text, language, base);
                }
                // Inline AI review: the rewrite is diffed in place (thinkly inlinePreview).
                let (text, runs) = match editor.ai_preview(block_id, &runs) { Some((t, r)) => (t.into(), r), None => (text, runs) };
                (text, runs, block.meta.table.clone())
            };
            if nowrap {
                let runs = super::text_runs::tint_selection(&text, runs, selected, theme.selection_text);
                let shaped = window.text_system().shape_text(text, font_size, &runs, None, None).unwrap_or_default();
                let widest = shaped.iter().map(|l| l.width()).fold(px(0.), |a, b| a.max(b)) + px(2.);
                let mut start = 0;
                let lines = shaped.into_iter().map(|line| { let offset = start; start += line.len() + 1; (offset, line) }).collect();
                let layout = TextLayout { bounds: Bounds::default(), lines, line_height, cells: Vec::new(), alignment: TextAlign::Left };
                let height = layout.height().max(line_height);
                *output.borrow_mut() = Some(layout);
                super::view::layout::set_key(editor_id, block_id, key);
                return size(widest.max(room).max(px(1.)), height);
            }
            if let Some(table) = &table {
                let mut layout = table_layout::measure(table, width, selected, window);
                let height = layout.height() + px(table_layout::EXTEND_ROOM);
                layout.bounds.size = size(width, height);
                *output.borrow_mut() = Some(layout);
                super::view::layout::set_key(editor_id, block_id, key);
                return size(width, height);
            }
            let runs = super::text_runs::tint_selection(&text, runs, selected, theme.selection_text);
            let shaped = window.text_system().shape_text(text, font_size, &runs, Some(width), None).unwrap_or_default();
            let mut start = 0;
            let lines = shaped.into_iter().map(|line| { let offset = start; start += line.len() + 1; (offset, line) }).collect();
            let mut layout = TextLayout { bounds: Bounds::default(), lines, line_height, cells: Vec::new(), alignment: TextAlign::Left };
            let height = layout.height().max(line_height);
            layout.bounds.size = size(width, height);
            *output.borrow_mut() = Some(layout);
            super::view::layout::set_key(editor_id, block_id, key);
            size(width, height)
        });
        (id, measured)
    }
    fn prepaint(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, bounds: Bounds<Pixels>, state: &mut Measured, window: &mut Window, cx: &mut App) -> TextLayout {
        let mut layout = state.borrow_mut().take()
            .or_else(|| self.editor.update(cx, |this, _| this.layouts.remove(&self.block)))
            .unwrap_or_else(|| TextLayout { bounds: Bounds::default(), lines: Vec::new(), line_height: px(21.), cells: Vec::new(), alignment: TextAlign::Left });
        let delta = bounds.origin - layout.bounds.origin;
        if delta.x != px(0.) || delta.y != px(0.) { layout.translate(delta); }
        layout.bounds = bounds;
        let editor = self.editor.read(cx);
        if editor.reveal_caret && editor.caret.head.block == self.block {
            let offset = vertical_offset(editor, self.block, &layout).unwrap_or(editor.caret.head.offset);
            window.request_autoscroll(Bounds::new(layout.point(offset), size(px(2.), layout.line_height)));
        }
        layout
    }
    fn paint(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, bounds: Bounds<Pixels>, _: &mut Measured, layout: &mut TextLayout, window: &mut Window, cx: &mut App) {
        self.editor.update(cx, |this, _| {
            if let Some(offset) = vertical_offset(this, self.block, layout) {
                let extend = this.pending_vertical.take().unwrap().3;
                this.caret.head.offset = offset;
                if !extend { this.caret.anchor = this.caret.head; }
            }
            if this.caret.head.block == self.block { this.reveal_caret = false; }
        });
        let (focused, selected, head, empty, composing, placeholder) = {
            let editor = self.editor.read(cx);
            let blank = editor.doc.get(self.block).is_some_and(|b| b.text.is_empty() && b.kind == Kind::Paragraph
                && b.meta.table.is_none() && b.meta.raw_source.is_none());
            (editor.focus.is_focused(window), editor.selected_range(self.block), editor.caret.head,
                editor.caret.is_empty(), editor.caret.composing.clone(),
                blank && editor.path.is_some() && editor.caret.head.block == self.block && editor.caret.composing.is_none())
        };
        let fenced = self.editor.read(cx).doc.get(self.block).and_then(|b| b.meta.code_language.clone());
        if let Some(language) = &fenced { super::code_block::paint_card(layout.bounds, language, window, cx); }
        super::find_bar::paint_matches(self.editor.read(cx), self.block, layout, window);
        super::annotate::paint_highlights(self.editor.read(cx), self.block, layout, window);
        table_layout::paint_background(layout, window);
        super::ai::preview::paint_outline(self.editor.read(cx), self.block, layout, window);
        if !layout.cells.is_empty() { super::table_actions::track_hover(self.editor.clone(), self.block, layout, window); }
        let tint = {
            let editor = self.editor.read(cx);
            match (editor.table_sel, editor.doc.get(self.block).and_then(|b| b.meta.table.as_ref())) {
                (Some((id, sel)), Some(table)) if id == self.block && table.columns > 0
                    && layout.cells.len() == table.rows.len() * table.columns => Some((sel, table.columns)),
                _ => None,
            }
        };
        if let Some((sel, columns)) = tint {
            use super::table_actions::TableSel;
            let dark = crate::theme::is_dark();
            let (head_color, body_color): (gpui::Hsla, gpui::Hsla) = match (dark, sel) {
                (true, _) => (gpui::rgba(0x4c8bf533).into(), gpui::rgba(0x4c8bf522).into()),
                (false, TableSel::Whole) => (gpui::rgb(0xdee4ed).into(), gpui::rgb(0xeef3fb).into()),
                (false, _) => (gpui::rgb(0xdde3ec).into(), gpui::rgb(0xecf2fa).into()),
            };
            for (i, cell) in layout.cells.iter().enumerate() {
                let (r, c) = (i / columns, i % columns);
                let hit = match sel { TableSel::Row(x) => r == x, TableSel::Column(x) => c == x, TableSel::Whole => true, TableSel::Cell(..) => false };
                if hit { window.paint_quad(fill(cell.frame, if r == 0 { head_color } else { body_color })); }
            }
            table_layout::paint_grid_lines(layout, window);
        }
        paint_lines(layout, true, window, cx);
        if focused {
            if !tint.is_some_and(|(s, _)| matches!(s, super::table_actions::TableSel::Whole)) {
                if let Some(range) = selected {
                    for rect in layout.rects(range) { window.paint_quad(quad(rect, px(2.), Theme::ocean().selection, 0., transparent_black(), BorderStyle::default())); }
                }
            }
            if head.block == self.block {
                if empty {
                    let p = layout.point(head.offset);
                    window.paint_quad(fill(Bounds::new(p, size(px(1.5), layout.line_height)), Theme::ocean().accent));
                }
                if let Some(range) = composing {
                    for rect in layout.rects(range) {
                        window.paint_quad(fill(Bounds::new(point(rect.left(), rect.bottom()-px(1.)), size(rect.size.width, px(1.))), Theme::ocean().accent));
                    }
                }
            }
        }
        paint_lines(layout, false, window, cx);
        if placeholder { paint_placeholder(layout, window, cx); }
        let saved = std::mem::replace(layout, TextLayout {
            bounds: Bounds::default(), lines: Vec::new(), line_height: px(0.), cells: Vec::new(), alignment: TextAlign::Left,
        });
        self.editor.update(cx, |this, _| { this.layouts.insert(self.block, saved); });
        let _ = bounds;
    }
}
/// (font size px, line-height factor) per block kind; shared with the handle gutter.
pub(super) fn metrics(kind: Kind, base: f32) -> (f32, f32) {
    match kind { Kind::H1 => (28., 1.35), Kind::H2 => (22., 1.4), Kind::H3 => (19., 1.5), _ => (base, 1.5) }
}
pub(super) fn block_metrics(kind: Kind, fenced: bool, minimal: bool, base: f32) -> (f32, f32) {
    if fenced { return (14., 1.6); }
    match kind {
        Kind::H1 | Kind::H2 | Kind::H3 => metrics(kind, base),
        _ if minimal => (16., 1.55),
        _ => (base, 1.5),
    }
}
thread_local! {
    static PLACEHOLDER: RefCell<Option<(u8, gpui::ShapedLine)>> = const { RefCell::new(None) };
}
/// Hint drawn over the zero-width placeholder glyph; never part of the text or hit testing.
fn paint_placeholder(layout: &TextLayout, window: &mut Window, cx: &mut App) {
    let key = (crate::theme::font_choice() as u8) | ((crate::theme::is_en() as u8) << 4);
    let line = PLACEHOLDER.with(|slot| {
        let mut slot = slot.borrow_mut();
        if slot.as_ref().is_none_or(|(c, _)| *c != key) {
            let text = SharedString::from(crate::theme::tr("输入文字…", "Type something…"));
            let mut run = plain(text.len(), rgb(0x6d7784).into());
            run.font.weight = FontWeight::NORMAL;
            let line = window.text_system().shape_line(text, Theme::ocean().editor_font, &[run], None);
            *slot = Some((key, line));
        }
        slot.as_ref().map(|(_, l)| l.clone()).unwrap()
    });
    let _ = line.paint(layout.bounds.origin, layout.line_height, window, cx);
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
        if backgrounds {
            let _ = line.paint_background(origin,layout.line_height,layout.alignment,Some(layout.bounds),window,cx);
        } else {
            let _ = line.paint(origin,layout.line_height,layout.alignment,Some(layout.bounds),window,cx);
        }
        y += line.size(layout.line_height).height;
    }
}
