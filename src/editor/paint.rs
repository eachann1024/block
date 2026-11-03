//! Visible block shaping supplies drawing, selection, hit testing and IME geometry.
use std::{cell::RefCell, rc::Rc};
use gpui::{App, AvailableSpace, Bounds, Element, ElementId, Font,
    FontFallbacks, FontFeatures, FontStyle, FontWeight, GlobalElementId, Hsla, IntoElement,
    LayoutId, Pixels, SharedString, StrikethroughStyle, Style, TextAlign, TextRun, Window,
    fill, point, px, relative, size};
use crate::theme::Theme;
use super::{model::{Kind, MarkKind, Span}, view::{Editor, layout::TextLayout}};

pub(super) struct BlockText { pub editor: gpui::Entity<Editor>, pub block: u64 }
type Measured = Rc<RefCell<TextLayout>>;
impl Element for BlockText {
    type RequestLayoutState = Measured;
    type PrepaintState = TextLayout;
    fn id(&self) -> Option<ElementId> { None }
    fn source_location(&self) -> Option<&'static core::panic::Location<'static>> { None }
    fn request_layout(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, window: &mut Window, cx: &mut App) -> (LayoutId, Measured) {
        let theme = Theme::ocean();
        let editor = self.editor.read(cx);
        let block = editor.doc.get(self.block).expect("visible block exists");
        let font_size = match block.kind { Kind::H1 => px(28.), Kind::H2 => px(22.), Kind::H3 => px(19.), _ => theme.editor_font };
        let text: SharedString = if block.text.is_empty() { "\u{200b}".into() } else { block.text.clone().into() };
        let mut runs = runs_for(&text, &block.spans, theme.text);
        if matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3) { for run in &mut runs { run.font.weight = FontWeight::BOLD; } }
        if let Some(language) = &block.meta.code_language {
            let mut base = plain(text.len(), theme.text); base.font.family = "Menlo".into();
            runs = super::view::syntax::runs(&text, language, base);
        }
        let line_height = font_size * 1.55;
        let measured = Rc::new(RefCell::new(TextLayout { bounds: Bounds::default(), lines: Vec::new(), line_height }));
        let output = measured.clone();
        let mut style = Style::default(); style.size.width = relative(1.).into();
        let id = window.request_measured_layout(style, move |known, available, window, _| {
            let width = known.width.unwrap_or_else(|| match available.width { AvailableSpace::Definite(w) => w, _ => px(640.) }).max(px(1.));
            let lines = window.text_system().shape_text(text.clone(), font_size, &runs, Some(width), None).unwrap_or_default();
            let mut start = 0;
            let mut data = output.borrow_mut();
            data.lines = lines.into_iter().map(|line| { let offset = start; start += line.len()+1; (offset, line) }).collect();
            size(width, data.height().max(line_height))
        });
        (id, measured)
    }
    fn prepaint(&mut self, _: Option<&GlobalElementId>, _: Option<&gpui::InspectorElementId>, bounds: Bounds<Pixels>, state: &mut Measured, window: &mut Window, cx: &mut App) -> TextLayout {
        let mut layout = state.borrow().clone(); layout.bounds = bounds;
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
            this.layouts.insert(self.block, layout.clone());
            if this.caret.head.block == self.block { this.reveal_caret = false; }
        });
        let editor = self.editor.read(cx);
        let focused = editor.focus.is_focused(window);
        if focused {
            if let Some(range) = editor.selected_range(self.block) {
                for rect in layout.rects(range) { window.paint_quad(fill(rect, Theme::ocean().selected)); }
            }
            if editor.caret.head.block == self.block {
                if editor.caret.is_empty() {
                    let p = layout.point(editor.caret.head.offset);
                    window.paint_quad(fill(Bounds::new(p, size(px(1.5), layout.line_height)), Theme::ocean().accent));
                }
                if let Some(range) = editor.caret.composing.clone() {
                    for rect in layout.rects(range) {
                        window.paint_quad(fill(Bounds::new(point(rect.left(), rect.bottom()-px(1.)), size(rect.size.width, px(1.))), Theme::ocean().accent));
                    }
                }
            }
        }
        let mut y = bounds.top();
        for (_, line) in &layout.lines {
            let _ = line.paint(point(bounds.left(), y), layout.line_height, TextAlign::Left, Some(bounds), window, cx);
            y += line.size(layout.line_height).height;
        }
    }
}
impl IntoElement for BlockText { type Element = Self; fn into_element(self) -> Self { self } }
fn vertical_offset(editor: &Editor, block: u64, layout: &TextLayout) -> Option<usize> {
    let (id, x, down, _) = editor.pending_vertical?;
    if id != block { return None; }
    let y = if down { layout.bounds.top() } else { layout.bounds.bottom()-px(1.) };
    Some(super::view::selection::boundary(editor.block_text(), layout.hit(point(x, y))))
}
fn runs_for(text: &str, spans: &[Span], color: Hsla) -> Vec<TextRun> {
    let mut runs = Vec::new(); let mut cursor = 0;
    for span in spans {
        let start = span.start.max(cursor).min(text.len()); let end = span.end.min(text.len());
        if start > cursor { runs.push(plain(start-cursor, color)); }
        if end > start {
            let mut run = plain(end-start, color);
            if span.marks.contains(&MarkKind::Bold) { run.font.weight = FontWeight::BOLD; }
            if span.marks.contains(&MarkKind::Italic) { run.font.style = FontStyle::Italic; }
            if span.marks.contains(&MarkKind::Code) { run.font.family = "Menlo".into(); run.background_color = Some(Theme::ocean().code_bg); }
            if span.marks.contains(&MarkKind::Strike) { run.strikethrough = Some(StrikethroughStyle { color: Some(color), thickness: px(1.) }); }
            runs.push(run); cursor = end;
        }
    }
    if cursor < text.len() { runs.push(plain(text.len()-cursor, color)); }
    runs
}
fn plain(len: usize, color: Hsla) -> TextRun {
    TextRun { len, font: Font { family: ".SystemUIFont".into(), weight: FontWeight::NORMAL,
        style: FontStyle::Normal, features: FontFeatures::default(), fallbacks: Some(FontFallbacks::from_fonts(vec!["PingFang SC".into()])) },
        color, background_color: None, underline: None, strikethrough: None }
}
