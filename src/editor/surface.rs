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
}
