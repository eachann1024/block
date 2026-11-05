//! Plain source column: standard width, no block chrome, optional HTML/SVG header.
use gpui::{AnyElement, Context, FontWeight, Pixels, SharedString, div, list, prelude::*, px};
use crate::theme::{Theme, tr};
use crate::ui::file_fallback::{open_button, unsupported_file};
use crate::ui::segmented_control::{SegmentSpec, segmented_control};
use super::source_paint::{self, SourceLine};
use super::source_text::word_range;
use super::surface::Surface;
use super::view::Editor;

impl Editor {
    pub(super) fn prepare_source_view(&mut self) {
        self.ensure_source_runs();
        let reveal = self.source_doc_mut().and_then(|doc| {
            doc.sync_list();
            if !doc.reveal { return None; }
            doc.reveal = false;
            Some((doc.list.clone(), doc.text.line_of(doc.head)))
        });
        if let Some((list, line)) = reveal {
            crate::ui::scrollbar::reveal_item(&list, line, px(80.));
        }
    }
    fn ensure_source_runs(&mut self) {
        let Some(doc) = self.source_doc() else { return };
        let theme = (crate::theme::font_settings_hash(), crate::theme::is_dark());
        if !doc.highlight || doc.composing.is_some() || (doc.runs_rev == doc.rev && doc.runs_theme == theme) { return; }
        let (text, language, rev) = (doc.text.body().to_owned(), doc.language.clone(), doc.rev);
        let runs = source_paint::slice_full(&text, &language);
        if let Some(doc) = self.source_doc_mut() && doc.rev == rev {
            doc.runs = runs;
            doc.runs_rev = rev;
            doc.runs_theme = theme;
        }
    }
    pub(super) fn source_pointer_down(&mut self, event: &gpui::MouseDownEvent, window: &mut gpui::Window, cx: &mut Context<Self>) {
        self.finish_source_composition(cx);
        window.focus(&self.focus, cx);
        let Some(at) = self.source_hit(event.position) else { cx.notify(); return; };
        let clicks = event.click_count.max(1);
        let (anchor, head) = if clicks >= 3 { self.source_line_span(at) }
            else if clicks == 2 { self.source_double_click(at, event.modifiers.alt) }
            else { (at, at) };
        if let Some(doc) = self.source_doc_mut() {
            if !event.modifiers.shift { doc.anchor = anchor; }
            doc.head = head;
            doc.preferred_col = None;
            doc.reveal = false;
        }
        self.source_drag = clicks == 1;
        cx.notify();
    }
    pub(super) fn source_drag_to(&mut self, point: gpui::Point<Pixels>, cx: &mut Context<Self>) {
        let Some(at) = self.source_hit(point) else { return };
        if let Some(doc) = self.source_doc_mut() {
            doc.head = at;
            doc.reveal = false;
        }
        let list = self.source_doc().map(|doc| doc.list.clone());
        if let Some(list) = list {
            let bounds = list.viewport_bounds();
            if point.y < bounds.top() + px(24.) { list.scroll_by(px(-18.)); }
            if point.y > bounds.bottom() - px(24.) { list.scroll_by(px(18.)); }
        }
        cx.notify();
    }
    fn source_line_span(&self, at: usize) -> (usize, usize) {
        let Some(doc) = self.source_doc() else { return (at, at) };
        let index = doc.text.line_of(at);
        (doc.text.line_visible(index).0, doc.text.line_limit(index))
    }
    fn source_double_click(&self, at: usize, alt: bool) -> (usize, usize) {
        let Some(doc) = self.source_doc() else { return (at, at) };
        let text = doc.text.body();
        let range = crate::editor::code_selection::code_select(text, at, &doc.language, alt)
            .or_else(|| crate::editor::code_selection::match_identifier_at(text, at))
            .unwrap_or_else(|| word_range(text, at));
        (range.start, range.end)
    }
}

pub(super) fn editor_column(editor: &mut Editor, rows: AnyElement, std_w: Pixels, rail: Pixels, pad: (f32, f32), cx: &mut Context<Editor>) -> AnyElement {
    if editor.path.is_none() { return note_column(editor, rows, std_w, rail, pad, cx); }
    let kind = match &editor.surface {
        Surface::Blocked { .. } => 0,
        Surface::Preview { show_source: false, .. } => 1,
        Surface::Source(_) | Surface::Preview { show_source: true, .. } => 2,
        Surface::Note => 3,
    };
    match kind {
        0 => blocked_pane(editor),
        1 => preview_pane(editor, cx),
        2 => source_pane(editor, std_w, cx),
        _ => note_column(editor, rows, std_w, rail, pad, cx),
    }
}

fn blocked_pane(editor: &Editor) -> AnyElement {
    let reason = editor.blocked_reason().unwrap_or("").to_owned();
    let path = editor.path.clone();
    div().flex_1().min_h_0().size_full().child(unsupported_file(reason, move |_, _, _| {
        if let Some(path) = &path {
            if let Err(error) = crate::files::open_with::open_default(path) {
                crate::log::error(&format!("failed to open {} with the default app: {error}", path.display()));
            }
        }
    })).into_any_element()
}

fn note_column(editor: &Editor, rows: AnyElement, std_w: Pixels, rail: Pixels, pad: (f32, f32), _cx: &mut Context<Editor>) -> AnyElement {
    div().flex_1().min_h_0().min_w_0().w_full().relative().flex().pr(px(if editor.minimal { 6. } else { crate::ui::scrollbar::GUTTER }))
        .on_scroll(crate::ui::scrollbar::forward_wheel(&editor.list))
        .child(div().h_full().min_w_0().w_full().mx_auto().max_w(std_w + rail).flex().justify_start()
            .child(div().h_full().min_w_0().w_full().max_w(std_w)
                .when(editor.minimal, |paper| paper.flex())
                .when(editor.path.is_some(), |paper| paper.child(rows))
                .when(editor.path.is_none() && !editor.is_loading(), |paper| paper.child(crate::mascot::editor_empty(_cx)))))
        .when(editor.path.is_some(), |area| area.child(crate::ui::scrollbar::vertical("editor-scrollbar", &editor.list, px(pad.0 + pad.1))))
        .into_any_element()
}

fn source_pane(editor: &mut Editor, std_w: Pixels, cx: &mut Context<Editor>) -> AnyElement {
    editor.prepare_source_view();
    let theme = Theme::ocean();
    let list_state = editor.source_doc().map(|doc| doc.list.clone());
    let Some(list_state) = list_state else { return div().flex_1().into_any_element(); };
    let zoom = super::zoom::zoom_factor(editor.minimal);
    let gutter = gutter_width(editor, zoom);
    let entity = cx.entity();
    let rows = list(list_state.clone(), move |index, _, _| {
        let row_editor = entity.clone();
        div().id(("src-line", index)).w_full().flex().items_start()
            .on_mouse_down(gpui::MouseButton::Left, move |event, window, cx| {
                row_editor.update(cx, |this, cx| this.source_pointer_down(event, window, cx));
                cx.stop_propagation();
            })
            .child(line_no(index, gutter, zoom))
            .child(SourceLine { editor: entity.clone(), index })
            .into_any_element()
    }).size_full();
    div().flex_1().min_h_0().w_full().flex().flex_col().bg(theme.paper)
        .child(source_header(editor, cx))
        .child(div().flex_1().min_h_0().flex().pr(px(crate::ui::scrollbar::GUTTER))
            .on_scroll(crate::ui::scrollbar::forward_wheel(&list_state))
            .child(div().h_full().min_w_0().w_full().mx_auto().max_w(std_w)
                .pt(px(28.)).pb(px(64.)).child(rows))
            .child(crate::ui::scrollbar::vertical("source-scrollbar", &list_state, px(28.))))
        .into_any_element()
}

fn preview_pane(editor: &mut Editor, cx: &mut Context<Editor>) -> AnyElement {
    let theme = Theme::ocean();
    let svg = matches!(&editor.surface, Surface::Preview { source: Some(_), .. });
    let preview = match &editor.surface {
        Surface::Preview { preview, .. } => Some(preview.clone()),
        _ => None,
    };
    div().flex_1().min_h_0().size_full().flex().flex_col().bg(theme.paper)
        .when(svg, |col| col.child(source_header(editor, cx)))
        .children(preview)
        .into_any_element()
}

fn source_header(editor: &Editor, cx: &mut Context<Editor>) -> gpui::Div {
    let theme = Theme::ocean();
    let name = editor.path.as_ref().and_then(|p| p.file_name()).map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
    let html = editor.source_doc().is_some_and(|doc| doc.html);
    let svg = editor.source_doc().is_some_and(|doc| doc.svg) || matches!(&editor.surface, Surface::Preview { source: Some(_), .. });
    let show_source = editor.editing_source();
    let path = editor.path.clone();
    let entity = cx.entity();
    div().h(px(40.)).px(px(16.)).flex_none().flex().items_center().justify_between().gap(px(12.))
        .border_b_1().border_color(theme.sidebar_border).bg(theme.paper)
        .child(div().min_w_0().flex().items_center().gap(px(10.))
            .child(div().min_w_0().overflow_hidden().text_ellipsis().whitespace_nowrap()
                .font_family(crate::theme::mono_font_family()).text_size(px(14.)).font_weight(FontWeight(560.)).child(name))
            .when(!editor.status.is_empty(), |row| row.child(div().flex_none().text_size(px(14.)).text_color(theme.text_secondary).child(editor.status.clone()))))
        .child(div().flex_none().flex().items_center().gap(px(8.))
            .when(svg, |row| {
                let to_preview = entity.clone();
                let to_source = entity.clone();
                row.child(segmented_control("svg-mode", vec![
                    SegmentSpec { label: SharedString::from(tr("预览", "Preview")), selected: !show_source, on_select: Box::new(move |_, _, cx| {
                        to_preview.update(cx, |this, cx| this.show_svg_source(false, cx));
                    }) },
                    SegmentSpec { label: SharedString::from(tr("源码", "Source")), selected: show_source, on_select: Box::new(move |_, _, cx| {
                        to_source.update(cx, |this, cx| this.show_svg_source(true, cx));
                    }) },
                ]))
            })
            .when(html, |row| row.child(open_button("html-browser", tr("浏览器打开", "Open in Browser"), move |_, _, cx| {
                let Some(path) = path.clone() else { return };
                let entity = entity.clone();
                cx.spawn(async move |cx| {
                    let opened = path.clone();
                    let result = cx.background_executor().spawn(async move {
                        crate::files::open_with::open_in_browser(&opened)
                    }).await;
                    if let Err(error) = result {
                        crate::log::error(&format!("failed to open {} in the browser: {error}", path.display()));
                        let _ = entity.update(cx, |this, cx| { this.status = error.to_string(); cx.notify(); });
                    }
                }).detach();
            }))))
}

fn gutter_width(editor: &Editor, zoom: f32) -> Pixels {
    let n = editor.source_doc().map(|doc| doc.text.line_count()).unwrap_or(1);
    let digits = n.to_string().len().max(2) as f32;
    px((digits * 8. + 16.) * zoom)
}

fn line_no(index: usize, width: Pixels, zoom: f32) -> gpui::Div {
    let theme = Theme::ocean();
    div().w(width).flex_none().pt(px(1. * zoom)).pr(px(10. * zoom)).text_right()
        .font_family(crate::theme::mono_font_family()).text_size(px(14. * zoom)).line_height(px(24. * zoom))
        .text_color(theme.text_secondary).child(format!("{}", index + 1))
}
