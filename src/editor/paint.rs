//! One block's text, shaped with the system UI font and PingFang SC fallback.

use gpui::{
    App, Bounds, Element, ElementId, ElementInputHandler, Font, FontFallbacks, FontFeatures,
    FontStyle, FontWeight, GlobalElementId, Hsla, IntoElement, LayoutId, PaintQuad, Pixels,
    ShapedLine, SharedString, StrikethroughStyle, Style, TextRun, Window, fill, point, px,
    relative, size,
};

use crate::theme::Theme;

use super::model::{MarkKind, Span};
use super::view::Editor;

pub(super) struct BlockText {
    pub editor: gpui::Entity<Editor>,
    pub block: u64,
}

impl Element for BlockText {
    type RequestLayoutState = ();
    type PrepaintState = Option<ShapedLine>;

    fn id(&self) -> Option<ElementId> {
        None
    }

    fn source_location(&self) -> Option<&'static core::panic::Location<'static>> {
        None
    }

    fn request_layout(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&gpui::InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (LayoutId, ()) {
        let mut style = Style::default();
        style.size.width = relative(1.).into();
        style.size.height = px(26.).into();
        (window.request_layout(style, [], cx), ())
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&gpui::InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) -> Option<ShapedLine> {
        let editor = self.editor.read(cx);
        let block = editor.doc.blocks.iter().find(|block| block.id == self.block)?;
        let theme = Theme::ocean();
        let display: SharedString =
            if block.text.is_empty() { "\u{200b}".into() } else { block.text.clone().into() };
        let runs = runs_for(&display, &block.spans, theme.text);
        Some(window.text_system().shape_line(display, theme.editor_font, &runs, None))
    }

    fn paint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&gpui::InspectorElementId>,
        bounds: Bounds<Pixels>,
        _: &mut (),
        line: &mut Option<ShapedLine>,
        window: &mut Window,
        cx: &mut App,
    ) {
        let Some(line) = line.take() else { return };
        let editor = self.editor.read(cx);
        if editor.caret.block == self.block && editor.focus.is_focused(window) {
            let range = editor.caret.range.clone();
            if range.is_empty() {
                window.paint_quad(cursor(bounds, line.x_for_index(range.start)));
            } else {
                window.paint_quad(selection(
                    bounds,
                    line.x_for_index(range.start),
                    line.x_for_index(range.end),
                ));
            }
            window.handle_input(
                &editor.focus,
                ElementInputHandler::new(bounds, self.editor.clone()),
                cx,
            );
        }
        let _ = line.paint(bounds.origin, px(26.), window, cx);
    }
}

impl IntoElement for BlockText {
    type Element = Self;
    fn into_element(self) -> Self {
        self
    }
}

fn runs_for(text: &str, spans: &[Span], color: Hsla) -> Vec<TextRun> {
    let mut runs = Vec::new();
    let mut cursor = 0;
    for span in spans {
        if span.start > cursor {
            runs.push(plain(span.start - cursor, color));
        }
        let len = span.end.min(text.len()).saturating_sub(span.start);
        runs.push(TextRun {
            len,
            font: marked_font(span),
            color,
            background_color: span.marks.contains(&MarkKind::Code).then_some(Theme::ocean().code_bg),
            underline: None,
            strikethrough: span.marks.contains(&MarkKind::Strike).then_some(StrikethroughStyle {
                color: Some(color),
                thickness: px(1.),
            }),
        });
        cursor = span.end;
    }
    if cursor < text.len() {
        runs.push(plain(text.len() - cursor, color));
    }
    runs.retain(|run| run.len > 0);
    if runs.is_empty() {
        runs.push(plain(text.len().max(1), color));
    }
    runs
}

fn marked_font(span: &Span) -> Font {
    let mut font = ui_font();
    if span.marks.contains(&MarkKind::Bold) {
        font.weight = FontWeight::BOLD;
    }
    if span.marks.contains(&MarkKind::Italic) {
        font.style = FontStyle::Italic;
    }
    font
}

fn plain(len: usize, color: Hsla) -> TextRun {
    TextRun {
        len,
        font: ui_font(),
        color,
        background_color: None,
        underline: None,
        strikethrough: None,
    }
}

fn ui_font() -> Font {
    Font {
        family: ".SystemUIFont".into(),
        weight: FontWeight::NORMAL,
        style: FontStyle::Normal,
        features: FontFeatures::default(),
        fallbacks: Some(FontFallbacks::from_fonts(vec!["PingFang SC".into()])),
    }
}

fn cursor(bounds: Bounds<Pixels>, x: Pixels) -> PaintQuad {
    fill(
        Bounds::new(point(bounds.left() + x, bounds.top()), size(px(1.5), bounds.size.height)),
        Theme::ocean().accent,
    )
}

fn selection(bounds: Bounds<Pixels>, start: Pixels, end: Pixels) -> PaintQuad {
    fill(
        Bounds::from_corners(
            point(bounds.left() + start, bounds.top()),
            point(bounds.left() + end, bounds.bottom()),
        ),
        Theme::ocean().selected,
    )
}
