//! "生成图片" view state: option changes re-render the PNG on the background executor,
//! save writes exactly the previewed bytes, copy puts the same PNG on the clipboard.
use std::{path::PathBuf, sync::Arc, time::Duration};

use gpui::{
    App, ClipboardItem, Context, Entity, FocusHandle, Focusable, Global, Image, ImageFormat, Window,
};

use super::{
    Cancellation, ExportError, ExportJob, ExportTheme, RenderOptions, render_with_cancel, write_png,
};
use crate::editor::model::Block;
use crate::theme::tr;

/// A scope's snapshot, or why it cannot be exported.
pub type Snapshot = Result<Arc<Vec<Block>>, String>;

/// The open view, if any. `Workspace` observes this global and overlays the view.
#[derive(Default)]
pub struct ExportSlot(pub Option<Entity<ExportView>>);
impl Global for ExportSlot {}

pub fn active_view(cx: &App) -> Option<Entity<ExportView>> {
    cx.try_global::<ExportSlot>().and_then(|slot| slot.0.clone())
}

pub struct ExportView {
    pub(super) title: String,
    pub(super) whole: Snapshot,
    pub(super) selection: Option<Snapshot>,
    pub(super) use_selection: bool,
    pub(super) style: ExportTheme,
    pub(super) light_style: ExportTheme,
    pub(super) dark_style: ExportTheme,
    pub(super) show_title: bool,
    pub(super) watermark: bool,
    pub(super) zoom: Option<f32>,
    pub(super) png: Option<Arc<Image>>,
    pub(super) error: Option<String>,
    pub(super) notice: Option<String>,
    pub(super) saved_path: Option<PathBuf>,
    pub(super) rendering: bool,
    pub(super) saving: bool,
    pub(super) copied: bool,
    dirty: bool,
    cancel: Cancellation,
    pub(super) focus: FocusHandle,
    restore: FocusHandle,
}

impl ExportView {
    pub fn new(
        title: String,
        whole: Snapshot,
        selection: Option<Snapshot>,
        use_selection: bool,
        restore: FocusHandle,
        cx: &mut Context<Self>,
    ) -> Self {
        Self {
            title,
            whole,
            use_selection: use_selection && selection.is_some(),
            selection,
            style: ExportTheme::default(),
            light_style: ExportTheme::default_for(false),
            dark_style: ExportTheme::default_for(true),
            show_title: true,
            watermark: true,
            zoom: None,
            png: None,
            error: None,
            notice: None,
            saved_path: None,
            rendering: false,
            saving: false,
            copied: false,
            dirty: false,
            cancel: Cancellation::default(),
            focus: cx.focus_handle(),
            restore,
        }
    }

    pub(super) fn ready(&self) -> bool {
        self.png.is_some() && !self.rendering && !self.dirty && !self.saving
    }

    pub(super) fn back(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.cancel.cancel();
        let restore = self.restore.clone();
        cx.set_global(ExportSlot(None));
        window.focus(&restore);
    }

    pub(super) fn set_scope(&mut self, selection: bool, cx: &mut Context<Self>) {
        if selection != self.use_selection && (!selection || self.selection.is_some()) {
            self.use_selection = selection;
            self.request(cx);
        }
    }

    pub(super) fn set_dark(&mut self, dark: bool, cx: &mut Context<Self>) {
        if dark != self.style.is_dark() {
            self.style = if dark { self.dark_style } else { self.light_style };
            self.request(cx);
        }
    }

    pub(super) fn set_style(&mut self, style: ExportTheme, cx: &mut Context<Self>) {
        if style != self.style {
            self.style = style;
            if style.is_dark() {
                self.dark_style = style;
            } else {
                self.light_style = style;
            }
            self.request(cx);
        }
    }

    pub(super) fn toggle_title(&mut self, cx: &mut Context<Self>) {
        self.show_title = !self.show_title;
        self.request(cx);
    }

    pub(super) fn toggle_watermark(&mut self, cx: &mut Context<Self>) {
        self.watermark = !self.watermark;
        self.request(cx);
    }

    pub(super) fn zoom_in(&mut self, base: f32, cx: &mut Context<Self>) {
        let next = ((self.zoom.unwrap_or(base) + 0.15) * 20.0).round() / 20.0;
        self.zoom = Some(next.clamp(0.5, 1.5));
        cx.notify();
    }

    pub(super) fn zoom_out(&mut self, base: f32, cx: &mut Context<Self>) {
        let next = ((self.zoom.unwrap_or(base) - 0.15) * 20.0).round() / 20.0;
        self.zoom = Some(next.clamp(0.5, 1.5));
        cx.notify();
    }

    pub(super) fn zoom_reset(&mut self, cx: &mut Context<Self>) {
        self.zoom = None;
        cx.notify();
    }

    pub(super) fn reveal_saved(&self) {
        if let Some(path) = &self.saved_path {
            let _ = std::process::Command::new("open").arg("-R").arg(path).spawn();
        }
    }

    pub fn request(&mut self, cx: &mut Context<Self>) {
        self.copied = false;
        self.notice = None;
        self.saved_path = None;
        cx.notify();
        if self.rendering {
            self.dirty = true;
            self.cancel.cancel();
        } else {
            self.start(cx);
        }
    }

    fn start(&mut self, cx: &mut Context<Self>) {
        let source = match (self.use_selection, &self.selection) {
            (true, Some(selection)) => selection,
            _ => &self.whole,
        };
        let blocks = match source {
            Ok(blocks) => blocks.clone(),
            Err(message) => {
                self.error = Some(message.clone());
                self.png = None;
                self.dirty = false;
                return;
            }
        };
        let Ok(job) = ExportJob::begin().map_err(|e| {
            self.error = Some(e.to_string());
            self.dirty = false;
        }) else {
            return;
        };
        let options = RenderOptions {
            theme: self.style,
            card: false,
            show_title: self.show_title,
            footer: self.watermark,
            title: self.title.clone(),
        };
        self.rendering = true;
        self.dirty = false;
        self.cancel = job.cancellation.clone();
        cx.spawn(async move |this, cx| {
            let result = cx
                .background_executor()
                .spawn(async move {
                    let res = render_with_cancel(&blocks, options, &job.cancellation);
                    drop(job);
                    res
                })
                .await;
            let _ = this.update(cx, |this, cx| {
                this.rendering = false;
                if this.dirty {
                    this.start(cx);
                } else {
                    match result {
                        Ok((bytes, _)) => {
                            this.png = Some(Arc::new(Image::from_bytes(ImageFormat::Png, bytes)));
                            this.error = None;
                        }
                        Err(ExportError::Cancelled) => {}
                        Err(error) => {
                            this.png = None;
                            this.error = Some(error.to_string());
                        }
                    }
                }
                cx.notify();
            });
        })
        .detach();
    }

    pub(super) fn copy(&mut self, cx: &mut Context<Self>) {
        let Some(png) = self.png.clone().filter(|_| self.ready()) else { return };
        cx.write_to_clipboard(ClipboardItem::new_image(&png));
        self.copied = true;
        self.notice = Some(tr("已复制到剪贴板", "Copied to clipboard").into());
        cx.notify();
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(Duration::from_millis(1600)).await;
            let _ = this.update(cx, |this, cx| {
                this.copied = false;
                cx.notify();
            });
        })
        .detach();
    }

    pub(super) fn save(&mut self, cx: &mut Context<Self>) {
        let Some(png) = self.png.clone().filter(|_| self.ready()) else { return };
        let directory = std::env::var_os("HOME").map(PathBuf::from).unwrap_or_default();
        let name = format!("{}.png", self.title.replace(['/', ':'], "-"));
        let prompt = cx.prompt_for_new_path(&directory, Some(&name));
        self.saving = true;
        self.notice = Some(tr("请选择保存位置", "Choose save location").into());
        cx.notify();
        cx.spawn(async move |this, cx| {
            let outcome = match prompt.await {
                Ok(Ok(Some(path))) => {
                    let written = cx
                        .background_executor()
                        .spawn(async move {
                            write_png(&path, png.bytes(), &Cancellation::default()).map(|_| path)
                        })
                        .await;
                    match written {
                        Ok(path) => {
                            let file = path.file_name().and_then(|s| s.to_str()).unwrap_or("image.png");
                            (format!("{} {file}", tr("已保存", "Saved")), Some(path))
                        }
                        Err(error) => (error.to_string(), None),
                    }
                }
                Ok(Ok(None)) => (ExportError::Cancelled.to_string(), None),
                Ok(Err(error)) => (ExportError::Dialog(error.to_string()).to_string(), None),
                Err(error) => (ExportError::Dialog(error.to_string()).to_string(), None),
            };
            let _ = this.update(cx, |this, cx| {
                this.saving = false;
                this.notice = Some(outcome.0);
                this.saved_path = outcome.1;
                cx.notify();
            });
        })
        .detach();
    }
}

impl Drop for ExportView {
    fn drop(&mut self) {
        self.cancel.cancel();
    }
}

impl Focusable for ExportView {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}
