//! "生成图片" view state: option changes re-render the PNG on the background executor,
//! save writes exactly the previewed bytes, copy puts the same PNG on the clipboard.
use std::{sync::Arc, time::Duration};

use gpui::{
    App, ClipboardItem, Context, Entity, FocusHandle, Focusable, Global, Image, ImageFormat, Window,
};

use super::{
    Cancellation, ExportError, ExportJob, ExportTheme, RenderOptions, render_with_cancel, write_png,
};
use crate::editor::model::Block;

/// A scope's snapshot, or why it cannot be exported.
pub type Snapshot = Result<Arc<Vec<Block>>, String>;

/// The open view, if any. `Workspace` observes this global and overlays the view.
#[derive(Default)]
pub struct ExportSlot(pub Option<Entity<ExportView>>);
impl Global for ExportSlot {}

pub fn active_view(cx: &App) -> Option<Entity<ExportView>> {
    cx.try_global::<ExportSlot>()
        .and_then(|slot| slot.0.clone())
}

pub struct ExportView {
    pub(super) title: String,
    pub(super) whole: Snapshot,
    pub(super) selection: Option<Snapshot>,
    pub(super) use_selection: bool,
    pub(super) style: ExportTheme,
    pub(super) watermark: bool,
    pub(super) png: Option<Arc<Image>>,
    pub(super) error: Option<String>,
    pub(super) notice: Option<String>,
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
            watermark: true,
            png: None,
            error: None,
            notice: None,
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
            self.style = ExportTheme::default_for(dark);
            self.request(cx);
        }
    }

    pub(super) fn set_style(&mut self, style: ExportTheme, cx: &mut Context<Self>) {
        if style != self.style {
            self.style = style;
            self.request(cx);
        }
    }

    pub(super) fn toggle_watermark(&mut self, cx: &mut Context<Self>) {
        self.watermark = !self.watermark;
        self.request(cx);
    }

    /// Re-render for the current options. A running render is cancelled and redone.
    pub fn request(&mut self, cx: &mut Context<Self>) {
        self.copied = false;
        self.notice = None;
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
        let job = match ExportJob::begin() {
            Ok(job) => job,
            Err(error) => {
                self.error = Some(error.to_string());
                self.dirty = false;
                return;
            }
        };
        let options = RenderOptions {
            theme: self.style,
            card: false,
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
                    let result = render_with_cancel(&blocks, options, &job.cancellation);
                    drop(job);
                    result
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
        let Some(png) = self.png.clone().filter(|_| self.ready()) else {
            return;
        };
        cx.write_to_clipboard(ClipboardItem::new_image(&png));
        self.copied = true;
        self.notice = Some("已复制到剪贴板".into());
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

    pub(super) fn save(&mut self, cx: &mut Context<Self>) {
        let Some(png) = self.png.clone().filter(|_| self.ready()) else {
            return;
        };
        let directory = std::env::var_os("HOME")
            .map(std::path::PathBuf::from)
            .unwrap_or_default();
        let name = format!("{}.png", self.title.replace(['/', ':'], "-"));
        let prompt = cx.prompt_for_new_path(&directory, Some(&name));
        self.saving = true;
        self.notice = Some("请选择保存位置".into());
        cx.notify();
        cx.spawn(async move |this, cx| {
            let message = match prompt.await {
                Ok(Ok(Some(path))) => {
                    let written = cx
                        .background_executor()
                        .spawn(async move {
                            write_png(&path, png.bytes(), &Cancellation::default()).map(|_| path)
                        })
                        .await;
                    match written {
                        Ok(path) => format!("已保存到 {}", path.display()),
                        Err(error) => error.to_string(),
                    }
                }
                Ok(Ok(None)) => ExportError::Cancelled.to_string(),
                Ok(Err(error)) => ExportError::Dialog(error.to_string()).to_string(),
                Err(error) => ExportError::Dialog(error.to_string()).to_string(),
            };
            let _ = this.update(cx, |this, cx| {
                this.saving = false;
                this.notice = Some(message);
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
