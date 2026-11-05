//! One source line: display-only highlighting, caret and hit bounds.
use std::{cell::RefCell, rc::Rc};
use gpui::{App, AvailableSpace, Bounds, Element, ElementId, GlobalElementId, IntoElement, LayoutId,
    Pixels, SharedString, Style, TextAlign, Window, fill, point, px, relative, size};
use crate::theme::Theme;
use super::text_runs::plain;
use super::view::{Editor, layout::TextLayout};

pub(super) struct SourceLine { pub editor: gpui::Entity<Editor>, pub index: usize }
type Measured = Rc<RefCell<Option<TextLayout>>>;

impl Element for SourceLine {
    type RequestLayoutState = Measured;
    type PrepaintState = ();
    fn id(&self) -> Option<ElementId> { None }
    fn source_location(&self) -> Option<&'static core::panic::Location<'static>> { None }
    fn request_layout(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, window: &mut Window, _: &mut App) -> (LayoutId, Measured) {
        let measured: Measured = Rc::new(RefCell::new(None));
        let output = measured.clone();
        let editor = self.editor.clone();
        let index = self.index;
        let mut style = Style::default();
        style.flex_grow = 1.;
        style.flex_shrink = 1.;
        style.flex_basis = relative(0.).into();
        style.min_size.width = px(0.).into();
        let id = window.request_measured_layout(style, move |known, available, window, cx| {
            let width = known.width.unwrap_or(match available.width {
                AvailableSpace::Definite(w) => w, _ => px(640.),
            }).max(px(1.));
            let editor_ref = editor.read(cx);
            let zoom = super::zoom::zoom_factor(editor_ref.minimal);
            let (font_size, line_height) = (px(14.) * zoom, px(24.) * zoom);
            let Some(doc) = editor_ref.source_doc() else { return size(width, line_height); };
            let (start, visible) = doc.text.line_visible(index);
            let shown: SharedString = if visible.is_empty() { "\u{200b}".into() } else { visible.to_owned().into() };
            let runs = line_runs(doc, start, visible.len(), shown.len());
            let shaped = window.text_system().shape_text(shown, font_size, &runs, Some(width), None).unwrap_or_default();
            let mut start_at = 0usize;
            let lines = shaped.into_iter().map(|line| { let offset = start_at; start_at += line.len(); (offset, line) }).collect();
            let mut layout = TextLayout { bounds: Bounds::new(point(px(0.), px(0.)), size(width, line_height)), lines, line_height, cells: Vec::new(), alignment: TextAlign::Left, code_spans: Vec::new() };
            let height = layout.height().max(line_height);
            layout.bounds.size = size(width, height);
            *output.borrow_mut() = Some(layout);
            size(width, height)
        });
        (id, measured)
    }
    fn prepaint(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, bounds: Bounds<Pixels>, state: &mut Measured, window: &mut Window, _: &mut App) {
        let mut slot = state.borrow_mut();
        if slot.is_none() { *slot = Some(TextLayout::empty(px(24.))); }
        if let Some(layout) = slot.as_mut() { layout.bounds = bounds; }
        let _ = window.insert_hitbox(bounds, gpui::HitboxBehavior::Normal);
    }
    fn paint(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, _: Bounds<Pixels>, state: &mut Measured, _: &mut (), window: &mut Window, cx: &mut App) {
        let Some(layout) = state.borrow_mut().take() else { return };
        paint_line(&self.editor, self.index, &layout, window, cx);
        self.editor.update(cx, |editor, _| { editor.source_layouts.insert(self.index, layout); });
    }
}
impl IntoElement for SourceLine { type Element = Self; fn into_element(self) -> Self { self } }

fn line_runs(doc: &super::source_text::SourceDoc, start: usize, vis_len: usize, shown: usize) -> Vec<gpui::TextRun> {
    let theme = Theme::ocean();
    let fresh = doc.highlight && doc.runs_rev == doc.rev && !doc.runs.is_empty()
        && doc.runs_theme == (crate::theme::font_settings_hash(), crate::theme::is_dark());
    if vis_len == 0 || !fresh {
        return vec![mono(shown.max(1), theme.text)];
    }
    let mut out = Vec::new();
    let mut cursor = 0usize;
    let mut filled = 0usize;
    let end = start + vis_len;
    for run in &doc.runs {
        let run_end = cursor + run.len;
        let a = start.max(cursor);
        let b = end.min(run_end);
        if a < b {
            let gap = a - start;
            if gap > filled { out.push(mono(gap - filled, theme.text)); filled = gap; }
            let mut piece = run.clone();
            piece.len = b - a;
            out.push(piece);
            filled = b - start;
        }
        cursor = run_end;
        if cursor >= end { break; }
    }
    if filled < vis_len { out.push(mono(vis_len - filled, theme.text)); }
    if out.is_empty() { out.push(mono(shown.max(1), theme.text)); }
    out
}

pub(super) fn mono(len: usize, color: gpui::Hsla) -> gpui::TextRun {
    let mut run = plain(len, color);
    run.font.family = crate::theme::mono_font_family().into();
    run.font.fallbacks = None;
    run
}

fn paint_line(editor: &gpui::Entity<Editor>, index: usize, layout: &TextLayout, window: &mut Window, cx: &mut App) {
    let theme = Theme::ocean();
    let paint = {
        let editor_ref = editor.read(cx);
        let Some(doc) = editor_ref.source_doc() else { return };
        let (start, visible) = doc.text.line_visible(index);
        let vis_end = start + visible.len();
        let focused = editor_ref.focus.is_focused(window) && window.is_window_active();
        let sel = doc.selection();
        let selection = (!sel.is_empty()).then(|| {
            let a = sel.start.max(start).min(vis_end) - start;
            let b = sel.end.max(start).min(vis_end) - start;
            (a < b).then_some(a..b)
        }).flatten();
        let mut finds = Vec::new();
        if let Some(find) = &editor_ref.find {
            for (i, (_, range)) in find.matches.iter().enumerate() {
                let a = range.start.max(start).min(vis_end).saturating_sub(start);
                let b = range.end.max(start).min(vis_end).saturating_sub(start);
                if a < b { finds.push((a..b, i == find.current)); }
            }
        }
        let caret = (focused && doc.head >= start && doc.head <= vis_end && doc.anchor == doc.head)
            .then(|| doc.head - start);
        let composing = doc.composing.clone().filter(|_| focused).map(|mark| {
            let a = mark.start.max(start).min(vis_end).saturating_sub(start);
            let b = mark.end.max(start).min(vis_end).saturating_sub(start);
            a..b
        }).filter(|r| !r.is_empty());
        (selection, finds, caret, composing, visible.len())
    };
    let (selection, finds, caret, composing, vis_len) = paint;
    if let Some(range) = selection {
        for rect in layout.rects(range) { window.paint_quad(fill(rect, theme.selection)); }
    }
    for (range, current) in finds {
        let color = if current { theme.selected } else { theme.hover };
        for rect in layout.rects(range) { window.paint_quad(fill(rect, color)); }
    }
    let mut y = layout.bounds.top();
    for (_, line) in &layout.lines {
        let _ = line.paint(point(layout.bounds.left(), y), layout.line_height, TextAlign::Left, None, window, cx);
        y += line.size(layout.line_height).height;
    }
    if let Some(local) = caret {
        let p = layout.point(local.min(vis_len));
        window.paint_quad(fill(Bounds::new(p, size(px(1.5), layout.line_height)), theme.accent));
        crate::sync_ime_cursor_area(f32::from(p.x), f32::from(p.y), f32::from(layout.line_height));
    }
    if let Some(mark) = composing {
        for rect in layout.rects(mark) {
            window.paint_quad(fill(Bounds::new(point(rect.left(), rect.bottom() - px(1.)), size(rect.size.width, px(1.))), theme.accent));
        }
    }
}

pub(super) fn slice_full(text: &str, language: &str) -> Vec<gpui::TextRun> {
    let theme = Theme::ocean();
    let base = mono(text.len().max(1), theme.text);
    if text.is_empty() { return vec![base]; }
    let mut runs = super::view::syntax::runs(text, language, base);
    let family = crate::theme::mono_font_family();
    for run in &mut runs {
        run.font.family = family.clone();
        run.font.fallbacks = None;
    }
    let covered: usize = runs.iter().map(|run| run.len).sum();
    if covered < text.len() { runs.push(mono(text.len() - covered, theme.text)); }
    runs
}
