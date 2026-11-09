//! Export view state: option changes re-render the preview on the background executor,
//! save writes the selected format (PNG / PDF / Word / Markdown / HTML), copy writes to clipboard.
use std::{path::PathBuf, sync::Arc};

use gpui::{App, Context, Entity, FocusHandle, Focusable, Global, Image, ImageFormat, Window};

use super::{
    Cancellation, ExportError, ExportFormat, ExportJob, ExportTheme, render_preview_with_cancel,
};
use crate::editor::model::Block;

/// A scope's snapshot, or why it cannot be exported.
pub type Snapshot = Result<Arc<Vec<Block>>, String>;

/// The open view, if any. `Workspace` observes this global and overlays the view.
#[derive(Default)]
pub struct ExportSlot(pub Option<Entity<ExportView>>);
impl Global for ExportSlot {}

pub fn active_view(cx: &App) -> Option<Entity<ExportView>> {
    cx.try_global::<ExportSlot>().and_then(|slot| slot.0.clone())
}

pub fn close_active(window: &mut Window, cx: &mut App) -> bool {
    let Some(view) = active_view(cx) else { return false };
    view.update(cx, |v, cx| v.back(window, cx));
    true
}

pub struct ExportView {
    pub(super) title: String,
    pub(super) source: super::SourceContext,
    pub(super) whole: Snapshot,
    pub(super) selection: Option<Snapshot>,
    pub(super) use_selection: bool,
    pub(super) format: ExportFormat,
    pub(super) style: ExportTheme,
    pub(super) light_style: ExportTheme,
    pub(super) dark_style: ExportTheme,
    pub(super) card: bool,
    pub(super) show_title: bool,
    pub(super) watermark: bool,
    pub(super) zoom: Option<f32>,
    pub(super) png: Option<Arc<Image>>,
    pub(super) preview_tiles: Vec<(gpui::ImageSource, u32, u32)>,
    pub(super) error: Option<String>,
    pub(super) notice: Option<String>,
    pub(super) saved_path: Option<PathBuf>,
    pub(super) rendering: bool,
    pub(super) saving: bool,
    pub(super) copied: bool,
    pub(super) dirty: bool,
    pub(super) cancel: Cancellation,
    pub(super) focus: FocusHandle,
    restore: FocusHandle,
}

impl ExportView {
    pub fn new(
        title: String,
        source: super::SourceContext,
        whole: Snapshot,
        selection: Option<Snapshot>,
        use_selection: bool,
        restore: FocusHandle,
        cx: &mut Context<Self>,
    ) -> Self {
        let preferences = super::ExportPreferences::load();
        Self {
            title,
            source,
            whole,
            use_selection: use_selection && selection.is_some(),
            selection,
            format: ExportFormat::default(),
            style: preferences.style,
            light_style: preferences.light_style,
            dark_style: preferences.dark_style,
            card: false,
            show_title: preferences.show_title,
            watermark: preferences.watermark,
            zoom: None,
            png: None,
            preview_tiles: Vec::new(),
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

    pub(super) fn back(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.cancel.cancel();
        let restore = self.restore.clone();
        cx.set_global(ExportSlot(None));
        window.focus(&restore, cx);
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
            self.save_preferences();
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
            self.save_preferences();
            self.request(cx);
        }
    }

    pub(super) fn toggle_title(&mut self, cx: &mut Context<Self>) {
        self.show_title = !self.show_title;
        self.save_preferences();
        self.request(cx);
    }

    pub(super) fn toggle_watermark(&mut self, cx: &mut Context<Self>) {
        self.watermark = !self.watermark;
        self.save_preferences();
        self.request(cx);
    }

    fn save_preferences(&self) {
        let preferences = super::ExportPreferences {
            style: self.style,
            light_style: self.light_style,
            dark_style: self.dark_style,
            show_title: self.show_title,
            watermark: self.watermark,
        };
        if let Err(error) = preferences.save() {
            crate::log::warn(&format!("failed to save export preferences: {error}"));
        }
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
            crate::files::ops::reveal_in_finder(path);
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
        let blocks = match self.active_blocks() {
            Ok(blocks) => blocks,
            Err(message) => {
                self.error = Some(message);
                self.png = None;
                self.preview_tiles.clear();
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
        let options = self.render_options();
        self.rendering = true;
        self.dirty = false;
        self.cancel = job.cancellation.clone();
        cx.spawn(async move |this, cx| {
            #[cfg(target_family = "wasm")]
            cx.background_executor().timer(std::time::Duration::from_millis(1)).await;
            let result = cx
                .background_executor()
                .spawn(async move {
                    let res = render_preview_with_cancel(&blocks, options, &job.cancellation);
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
                        Ok((bytes, tiles, _)) => {
                            let svg = cx.svg_renderer();
                            this.preview_tiles = tiles
                                .into_iter()
                                .map(|(tile_bytes, tw, th)| {
                                    let tile_img =
                                        Arc::new(Image::from_bytes(ImageFormat::Png, tile_bytes));
                                    let src: gpui::ImageSource = tile_img
                                        .to_image_data(svg.clone())
                                        .map(Into::into)
                                        .unwrap_or_else(|_| tile_img.into());
                                    (src, tw, th)
                                })
                                .collect();
                            this.png = Some(Arc::new(Image::from_bytes(ImageFormat::Png, bytes)));
                            this.error = None;
                        }
                        Err(ExportError::Cancelled) => {}
                        Err(error) => {
                            this.png = None;
                            this.preview_tiles.clear();
                            this.error = Some(error.to_string());
                        }
                    }
                }
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
