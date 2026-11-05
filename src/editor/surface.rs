//! What the editor pane is showing. Markdown stays on `Doc`; other files do not.
use gpui::Context;
use super::source_text::SourceDoc;
use super::view::Editor;

pub(super) enum Surface {
    Note,
    Source(SourceDoc),
    Preview {
        preview: gpui::Entity<crate::ui::file_preview::FilePreview>,
        source: Option<SourceDoc>,
        show_source: bool,
    },
    Blocked { reason: String },
}

impl Default for Surface { fn default() -> Self { Self::Note } }

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TitlebarSurface {
    Empty,
    Note,
    ImagePreview,
    Svg { show_source: bool },
    HtmlSource,
    CodeSource,
    Blocked,
}

impl Editor {
    pub fn is_markdown_note(&self) -> bool {
        self.path.is_some() && matches!(self.surface, Surface::Note)
    }
    pub(super) fn editing_source(&self) -> bool { self.source_doc().is_some() }
    pub(super) fn source_composing(&self) -> bool {
        self.source_doc().is_some_and(|doc| doc.composing.is_some())
    }
    pub(super) fn source_doc(&self) -> Option<&SourceDoc> {
        match &self.surface {
            Surface::Source(doc) => Some(doc),
            Surface::Preview { source: Some(doc), show_source: true, .. } => Some(doc),
            _ => None,
        }
    }
    pub(super) fn source_doc_mut(&mut self) -> Option<&mut SourceDoc> {
        match &mut self.surface {
            Surface::Source(doc) => Some(doc),
            Surface::Preview { source: Some(doc), show_source: true, .. } => Some(doc),
            _ => None,
        }
    }
    /// Savable raw text, including an SVG buffer hidden behind the preview.
    pub(super) fn raw_save(&self) -> Option<(String, bool)> {
        let doc = match &self.surface {
            Surface::Source(doc) => doc,
            Surface::Preview { source: Some(doc), .. } => doc,
            _ => return None,
        };
        Some((doc.text.body().to_owned(), doc.bom))
    }
    pub(super) fn blocked_reason(&self) -> Option<&str> {
        match &self.surface { Surface::Blocked { reason } => Some(reason), _ => None }
    }
    pub(super) fn show_svg_source(&mut self, show: bool, cx: &mut Context<Self>) {
        if !show { self.finish_source_composition(cx); }
        let text = if !show { self.raw_save().map(|(text, _)| text) } else { None };
        let Surface::Preview { preview, source, show_source } = &mut self.surface else { return };
        if source.is_none() { return; }
        *show_source = show;
        preview.update(cx, |preview, cx| {
            if let Some(text) = text { preview.set_svg_source(text, cx); }
            preview.set_show_filename(false, cx);
        });
        cx.notify();
    }

    pub fn titlebar_surface(&self) -> TitlebarSurface {
        if self.path.is_none() { return TitlebarSurface::Empty; }
        match &self.surface {
            Surface::Note => TitlebarSurface::Note,
            Surface::Blocked { .. } => TitlebarSurface::Blocked,
            Surface::Preview { source: Some(_), show_source, .. } => TitlebarSurface::Svg { show_source: *show_source },
            Surface::Preview { source: None, .. } => TitlebarSurface::ImagePreview,
            Surface::Source(doc) => if doc.html { TitlebarSurface::HtmlSource } else { TitlebarSurface::CodeSource },
        }
    }

    pub fn source_status(&self) -> &str { &self.status }

    pub fn set_source_status(&mut self, status: String, cx: &mut Context<Self>) {
        self.status = status;
        cx.notify();
    }

    pub fn select_svg_source(&mut self, show: bool, cx: &mut Context<Self>) {
        self.show_svg_source(show, cx);
    }

    pub fn preview_zoom_state(&self, cx: &gpui::App) -> Option<(String, bool)> {
        match &self.surface {
            Surface::Preview { preview, show_source: false, .. } => preview.read(cx).zoom_state(),
            _ => None,
        }
    }

    pub fn preview_zoom_step(&mut self, factor: f32, cx: &mut Context<Self>) {
        let Surface::Preview { preview, .. } = &self.surface else { return };
        let preview = preview.clone();
        preview.update(cx, |p, cx| p.zoom_button(factor, cx));
        cx.notify();
    }

    pub fn preview_zoom_actual(&mut self, cx: &mut Context<Self>) {
        let Surface::Preview { preview, .. } = &self.surface else { return };
        let preview = preview.clone();
        preview.update(cx, |p, cx| p.zoom_actual(cx));
        cx.notify();
    }

    pub fn preview_zoom_fit(&mut self, cx: &mut Context<Self>) {
        let Surface::Preview { preview, .. } = &self.surface else { return };
        let preview = preview.clone();
        preview.update(cx, |p, cx| p.zoom_fit(cx));
        cx.notify();
    }
}
