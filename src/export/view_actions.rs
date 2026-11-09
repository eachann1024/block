//! Format selection, clipboard copy, and multi-format file save actions for `ExportView`.
use std::{path::PathBuf, sync::Arc, time::Duration};

use gpui::{ClipboardItem, Context};

use super::{
    Cancellation, ExportError, ExportFormat, RenderOptions,
    export_docx::render_docx,
    export_html::render_html,
    export_pdf::render_pdf,
    format::render_markdown,
    view::ExportView,
    write_bytes, write_png,
};
use crate::editor::model::Block;
use crate::theme::tr;

impl ExportView {
    pub(super) fn set_format(&mut self, format: ExportFormat, cx: &mut Context<Self>) {
        if format != self.format {
            let watermark_changed = self.watermark
                && format.supports_watermark() != self.format.supports_watermark();
            self.format = format;
            self.save_preferences();
            self.revision = self.revision.wrapping_add(1);
            self.copied = false;
            self.notice = None;
            self.saved_path = None;
            if watermark_changed || (self.png.is_none() && !self.rendering) {
                self.request(cx);
            } else {
                cx.notify();
            }
        }
    }

    pub(super) fn active_blocks(&self) -> Result<Arc<Vec<Block>>, String> {
        let source = match (self.use_selection, &self.selection) {
            (true, Some(selection)) => selection,
            _ => &self.whole,
        };
        source.clone()
    }

    pub(super) fn render_options(&self) -> RenderOptions {
        RenderOptions {
            theme: self.style,
            card: self.card,
            show_title: self.show_title,
            footer: self.watermark && self.format.supports_watermark(),
            title: self.title.clone(),
            source: self.source.clone(),
        }
    }

    pub(super) fn ready(&self) -> bool {
        if self.saving {
            return false;
        }
        if self.format.is_visual() {
            self.png.is_some() && !self.rendering && !self.dirty
        } else {
            self.active_blocks().is_ok()
        }
    }

    pub(super) fn copy(&mut self, cx: &mut Context<Self>) {
        if !self.ready() {
            return;
        }
        match self.format {
            ExportFormat::Markdown => {
                let Ok(blocks) = self.active_blocks() else { return };
                let md = render_markdown(&blocks, &self.render_options());
                cx.write_to_clipboard(ClipboardItem::new_string_with_metadata(md, crate::editor::MARKDOWN_CLIPBOARD_V1.into()));
            }
            ExportFormat::Html => {
                let Ok(blocks) = self.active_blocks() else { return };
                let html = render_html(&blocks, &self.render_options());
                cx.write_to_clipboard(ClipboardItem::new_string(html));
            }
            ExportFormat::Png | ExportFormat::Pdf | ExportFormat::Word => {
                let Some(png) = self.png.clone() else { return };
                cx.write_to_clipboard(ClipboardItem::new_image(&png));
            }
        }
        self.copied = true;
        self.notice = Some(tr("已复制到剪贴板", "Copied to clipboard").into());
        cx.notify();
        cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(Duration::from_millis(1600))
                .await;
            let _ = this.update(cx, |this, cx| {
                this.copied = false;
                cx.notify();
            });
        })
        .detach();
    }

    #[cfg(not(target_family = "wasm"))]
    pub(super) fn save(&mut self, cx: &mut Context<Self>) {
        if !self.ready() {
            return;
        }
        let format = self.format;
        let png = self.png.clone();
        let Ok(blocks) = self.active_blocks() else { return };
        let options = self.render_options();
        let name = super::destination::suggested_name(&self.title, format);
        let revision = self.revision;
        let prior_notice = self.notice.clone();
        self.saving = true;
        self.notice = Some(tr("请选择保存位置", "Choose save location").into());
        cx.notify();
        cx.spawn(async move |this, cx| {
            let directory = cx.background_executor().spawn(async { super::destination::directory() }).await;
            let Ok(prompt) = this.update(cx, |_, cx| cx.prompt_for_new_path(&directory, Some(&name))) else { return };
            let outcome = match prompt.await {
                Ok(Ok(Some(path))) => {
                    let written = cx
                        .background_executor()
                        .spawn(async move {
                            let cancel = Cancellation::default();
                            match format {
                                ExportFormat::Png => {
                                    let png = png.ok_or(ExportError::Cancelled)?;
                                    write_png(&path, png.bytes(), &cancel)?;
                                }
                                ExportFormat::Pdf => {
                                    let bytes = render_pdf(&blocks, options, &cancel)?;
                                    write_bytes(&path, &bytes, &cancel)?;
                                }
                                ExportFormat::Word => {
                                    let bytes = render_docx(&blocks, &options, &cancel)?;
                                    write_bytes(&path, &bytes, &cancel)?;
                                }
                                ExportFormat::Markdown => {
                                    let md = render_markdown(&blocks, &options);
                                    write_bytes(&path, md.as_bytes(), &cancel)?;
                                }
                                ExportFormat::Html => {
                                    let html = render_html(&blocks, &options);
                                    write_bytes(&path, html.as_bytes(), &cancel)?;
                                }
                            }
                            let remembered = match super::destination::remember(&path) {
                                Ok(()) => true,
                                Err(error) => {
                                    crate::log::warn(&format!("failed to remember export directory: {error}"));
                                    false
                                }
                            };
                            Ok::<(PathBuf, bool), ExportError>((path, remembered))
                        })
                        .await;
                    match written {
                        Ok((path, remembered)) => {
                            let file = path
                                .file_name()
                                .and_then(|s| s.to_str())
                                .unwrap_or("export");
                            let mut notice = format!("{} {file}", tr("已保存", "Saved"));
                            if !remembered {
                                notice.push_str(tr("；未能记住保存位置", "; could not remember save location"));
                            }
                            Some((notice, Some(path)))
                        }
                        Err(error) => Some((error.to_string(), None)),
                    }
                }
                Ok(Ok(None)) => None,
                Ok(Err(error)) => Some((ExportError::Dialog(error.to_string()).to_string(), None)),
                Err(error) => Some((ExportError::Dialog(error.to_string()).to_string(), None)),
            };
            let _ = this.update(cx, |this, cx| {
                if this.revision == revision {
                    if let Some((notice, path)) = outcome {
                        this.notice = Some(notice);
                        this.saved_path = path;
                    } else {
                        this.notice = prior_notice;
                    }
                } else if let Some((notice, None)) = outcome {
                    // A write failure must remain visible even if options changed.
                    this.notice = Some(notice);
                }
                this.finish_save(cx);
            });
        })
        .detach();
    }
}
