//! Read-only editor content: shared document model, typography and selectable rich text.
use gpui::{HighlightStyle, Overflow, SharedString, StyleRefinement, Styled, px, rems};
use gpui_component::text::{SelectionFormat, TextView, TextViewStyle};
use crate::{editor::{markdown, model::Kind}, theme::Theme};

pub fn markdown(id: String, source: &str) -> TextView {
    let doc = markdown::parse(source);
    let blocks: Vec<_> = doc.blocks.iter().cloned().collect();
    let html = markdown::render::render_blocks(&blocks);
    let theme = Theme::ocean();
    let body = f32::from(theme.editor_font);
    let mut table = StyleRefinement::default();
    table.overflow.x = Some(Overflow::Scroll);
    let style = TextViewStyle::default().paragraph_gap(rems(0.6))
        .heading_font_size(move |level, _| {
            let kind = match level { 1 => Kind::H1, 2 => Kind::H2, _ => Kind::H3 };
            px(heading_metrics(kind, Some(level), false, false, body, 1.).0)
        })
        .code_block(StyleRefinement::default().bg(theme.code_bg).font_family(crate::theme::mono_font_family()))
        .inline_code(HighlightStyle { background_color: Some(theme.code_bg), ..Default::default() })
        .table(table)
        .table_head(StyleRefinement::default().bg(theme.shell).text_color(theme.text_secondary))
        .table_cell(StyleRefinement::default().px(px(10.)).py(px(6.)).text_size(px(15.04)).line_height(px(21.808)));
    TextView::html(SharedString::from(id), html)
        .selectable(true).selection_format(SelectionFormat::Plain)
        .scrollable(false).stream_fade(false).style(style)
        .font_family(crate::theme::editor_font().0)
        .text_size(theme.editor_font).line_height(theme.editor_font * crate::theme::body_line_factor())
        .text_color(theme.text)
        .on_link_click(|url, _, _, _| crate::editor::open_external_url(url, None))
}

/// (font size px, line-height factor) per block kind; shared with the handle gutter.
pub(crate) fn metrics(kind: Kind, base: f32, zoom: f32) -> (f32, f32) {
    match kind {
        Kind::H1 => (32. * zoom, 1.2),
        Kind::H2 => (22. * zoom, 1.3),
        Kind::H3 => (18. * zoom, 1.35),
        _ => (base * zoom, 1.5),
    }
}
pub(crate) fn heading_metrics(kind: Kind, level: Option<u8>, fenced: bool, minimal: bool, base: f32, zoom: f32) -> (f32, f32) {
    if fenced { return (14. * zoom, 1.5); }
    match (kind, level) {
        (Kind::H3, Some(4)) => (16. * zoom, 1.4),
        (Kind::H3, Some(5 | 6)) => (14. * zoom, 1.45),
        (Kind::H1 | Kind::H2 | Kind::H3, _) => metrics(kind, base, zoom),
        _ => ((if minimal { 16. } else { base }) * zoom, crate::theme::body_line_factor()),
    }
}
