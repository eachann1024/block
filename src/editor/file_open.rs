//! Install a classified file into the editor without parsing it as Markdown.
use std::path::{Path, PathBuf};
use gpui::{AppContext as _, Context};
use super::model::Doc;
use super::source_text::SourceDoc;
use super::surface::Surface;
use super::view::{storage::{self, SaveDoc}, Editor};

impl Editor {
    pub fn recover_latest_draft(&mut self, cx: &mut Context<Self>) {
        self.persistence.generation += 1;
        self.persistence.loading = false;
        self.persistence.pending = None;
        if self.path.is_some() || !self.persistence.saving.is_empty() { cx.notify(); return; }
        let generation = self.persistence.generation;
        self.persistence.loading = true;
        self.report_status(crate::theme::tr("正在读取恢复草稿…", "Recovering draft…").into(), cx);
        let task = cx.background_executor().spawn(async { storage::latest_draft() });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                if this.persistence.generation != generation { return; }
                this.persistence.loading = false;
                match result {
                    Ok((path, loaded)) => {
                        this.show_loaded(path, loaded, cx);
                        this.status = crate::theme::tr("已恢复草稿。原文件若已删除或被外部修改仍受保护，可复制或导出备份", "Draft recovered. Copy or export a backup if the original file changed").into();
                    }
                    Err(error) => this.status = error,
                }
                cx.notify();
            });
        }).detach();
    }
    pub(super) fn show_loaded(&mut self, path: PathBuf, loaded: storage::Loaded, cx: &mut Context<Self>) {
        use crate::files::classify::OpenKind;
        self.clear_transient_overlays();
        self.source_layouts.clear();
        self.source_drag = false;
        self.persistence.base = loaded.base;
        self.persistence.gate.lock().unwrap().saved.insert(path.clone(), loaded.base);
        self.path = Some(path.clone());
        self.folded.clear();
        let recovered = loaded.recovered;
        let note = crate::theme::tr("已恢复未保存草稿，请确认后保存", "Recovered unsaved draft; please review and save");
        match loaded.kind {
            OpenKind::Markdown(text) => {
                self.surface = Surface::Note;
                self.reset_document(super::markdown::parse(&text));
                self.folded = crate::editor::fold::restore_folds(&path, &self.doc);
                self.dirty = recovered;
                self.status = if recovered { note.into() } else { String::new() };
            }
            OpenKind::Source(file) => {
                self.reset_document(Doc::empty());
                self.surface = Surface::Source(SourceDoc::open(file));
                self.dirty = recovered;
                self.status = if recovered { note.into() } else { String::new() };
            }
            OpenKind::Preview { svg } => {
                self.reset_document(Doc::empty());
                let editable = svg.is_some();
                let text = svg.as_ref().map(|file| file.text.clone());
                let source = svg.map(SourceDoc::open);
                let preview = cx.new(|cx| crate::ui::file_preview::FilePreview::new(path.clone(), cx));
                cx.observe(&preview, |_, _, cx| cx.notify()).detach();
                // Editable SVG already shows filename + mode in the source header.
                if source.is_some() {
                    preview.update(cx, |preview, cx| {
                        if let Some(text) = text { preview.set_svg_source(text, cx); }
                        preview.set_show_filename(false, cx);
                    });
                }
                self.surface = Surface::Preview { preview, source, show_source: false };
                self.dirty = recovered && editable;
                self.status = if self.dirty { note.into() } else { String::new() };
            }
            OpenKind::Blocked { reason } => {
                self.reset_document(Doc::empty());
                self.surface = Surface::Blocked { reason };
                self.dirty = false;
                self.status.clear();
            }
        }
        self.refresh_find_matches(cx);
        cx.notify();
    }

    pub(super) fn current_save_doc(&self) -> Option<SaveDoc> {
        if let Some((text, bom)) = self.raw_save() { return Some(SaveDoc::Raw { text, bom }); }
        matches!(self.surface, Surface::Note).then(|| SaveDoc::Note(self.doc.clone()))
    }

    pub(super) fn save_payload(&self, path: &Path) -> Option<(SaveDoc, storage::Fingerprint, u64)> {
        if self.path.as_deref() == Some(path) {
            if !self.dirty { return None; }
            Some((self.current_save_doc()?, self.persistence.base, self.persistence.revision))
        } else {
            self.parked_save_payload(path)
        }
    }
}
