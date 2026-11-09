//! Browser build of the export view's 「保存」: render the chosen format and hand it to the
//! browser as a file download (there is no filesystem or save dialog on wasm32).
use gpui::Context;
use wasm_bindgen::JsCast;

use super::{
    Cancellation, ExportError, ExportFormat,
    export_docx::render_docx, export_html::render_html, export_pdf::render_pdf,
    format::render_markdown, view::ExportView,
};
use crate::theme::tr;

fn mime(format: ExportFormat) -> &'static str {
    match format {
        ExportFormat::Png => "image/png",
        ExportFormat::Pdf => "application/pdf",
        ExportFormat::Word => "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        ExportFormat::Markdown => "text/markdown;charset=utf-8",
        ExportFormat::Html => "text/html;charset=utf-8",
    }
}

fn download(name: &str, mime: &str, bytes: &[u8]) -> Result<(), String> {
    let fail = |e: wasm_bindgen::JsValue| format!("{e:?}");
    let parts = js_sys::Array::new();
    parts.push(&js_sys::Uint8Array::from(bytes));
    let options = web_sys::BlobPropertyBag::new();
    options.set_type(mime);
    let blob = web_sys::Blob::new_with_u8_array_sequence_and_options(&parts, &options).map_err(fail)?;
    let url = web_sys::Url::create_object_url_with_blob(&blob).map_err(fail)?;
    let document = web_sys::window().and_then(|w| w.document()).ok_or_else(|| tr("无法访问页面文档", "no document").to_string())?;
    let anchor: web_sys::HtmlAnchorElement = document.create_element("a").map_err(fail)?
        .dyn_into().map_err(|_| tr("无法创建下载链接", "not an anchor").to_string())?;
    anchor.set_href(&url);
    anchor.set_download(name);
    anchor.click();
    let _ = web_sys::Url::revoke_object_url(&url);
    Ok(())
}

impl ExportView {
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
        self.saving = true;
        self.notice = Some(tr("正在生成导出文件", "Generating export file").into());
        cx.notify();
        cx.spawn(async move |this, cx| {
            let rendered = cx
                .background_executor()
                .spawn(async move {
                    let cancel = Cancellation::default();
                    Ok::<Vec<u8>, ExportError>(match format {
                        ExportFormat::Png => png.ok_or(ExportError::Cancelled)?.bytes().to_vec(),
                        ExportFormat::Pdf => render_pdf(&blocks, options, &cancel)?,
                        ExportFormat::Word => render_docx(&blocks, &options, &cancel)?,
                        ExportFormat::Markdown => render_markdown(&blocks, &options).into_bytes(),
                        ExportFormat::Html => render_html(&blocks, &options).into_bytes(),
                    })
                })
                .await;
            let (notice, failed) = match rendered {
                Ok(bytes) => match download(&name, mime(format), &bytes) {
                    Ok(()) => (format!("{} {name}", tr("已下载", "Downloaded")), false),
                    Err(error) => (error, true),
                },
                Err(error) => (error.to_string(), true),
            };
            let _ = this.update(cx, |this, cx| {
                if this.revision == revision || failed { this.notice = Some(notice); }
                this.finish_save(cx);
            });
        })
        .detach();
    }
}
