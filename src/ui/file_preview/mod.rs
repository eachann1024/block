//! Image and static SVG preview. The parent registers this module and builds
//! `cx.new(|cx| FilePreview::new(path, cx))`. SVG source mode stays in core;
//! unsaved text arrives through [`FilePreview::set_svg_source`].

mod gesture;
pub(crate) mod load;
mod open_app;
mod paint;
mod raster;
mod svg_attr;
mod svg_scan;
mod view;

use std::{path::Path, path::PathBuf, sync::atomic::{AtomicU64, Ordering}, time::Duration};
use gpui::{Context, FocusHandle, Focusable, Render, RenderImage, Task};

pub use view::{Placed, ViewTransform, ZoomMode, effective_scale, fit_scale, pan_by, place, wheel_factor, zoom_at};

static SLOT: AtomicU64 = AtomicU64::new(1);

pub struct FilePreview {
    path: PathBuf,
    focus: FocusHandle,
    slot: u64,
    generation: u64,
    live_svg: bool,
    job: Option<Task<()>>,
    body: Body,
    view: ViewTransform,
    stage: StageBox,
    drag: Option<DragOrigin>,
    dragging: bool,
    pointer_moved: bool,
    open_note: String,
}

enum Body {
    Loading,
    Ready { image: std::sync::Arc<RenderImage>, nat_w: f32, nat_h: f32 },
    Failed { reason: String },
}

struct StageBox {
    x: f32,
    y: f32,
    w: f32,
    h: f32,
}

#[derive(Clone, Copy)]
struct DragOrigin {
    x: f32,
    y: f32,
    pan_x: f32,
    pan_y: f32,
    scale: f32,
}

impl FilePreview {
    pub fn new(path: PathBuf, cx: &mut Context<Self>) -> Self {
        let supported = is_image_path(&path);
        let mut preview = Self {
            path,
            focus: cx.focus_handle(),
            slot: SLOT.fetch_add(1, Ordering::Relaxed),
            generation: 1,
            live_svg: false,
            job: None,
            body: if supported { Body::Loading } else { Body::Failed { reason: copy("此文件类型暂不支持", "This file type is not supported") } },
            view: ViewTransform::fit(),
            stage: StageBox { x: 0.0, y: 0.0, w: 0.0, h: 0.0 },
            drag: None,
            dragging: false,
            pointer_moved: false,
            open_note: String::new(),
        };
        if supported { preview.start_disk(cx); }
        preview
    }

    /// Debounced preview of unsaved SVG text. Does not read the path again.
    pub fn set_svg_source(&mut self, source: String, cx: &mut Context<Self>) {
        self.live_svg = true;
        self.generation = self.generation.wrapping_add(1);
        let generation = self.generation;
        let renderer = cx.svg_renderer();
        self.job = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(Duration::from_millis(200)).await;
            if this.update(cx, |this, _| this.generation == generation).unwrap_or(false) == false { return; }
            let outcome = cx.background_executor().spawn(async move { load::render_svg(&renderer, &source) }).await;
            this.update(cx, |this, cx| {
                if this.generation == generation { this.apply(outcome, false, cx); }
            }).ok();
        }));
    }

    fn start_disk(&mut self, cx: &mut Context<Self>) {
        let generation = self.generation;
        let path = self.path.clone();
        let renderer = cx.svg_renderer();
        self.job = Some(cx.spawn(async move |this, cx| {
            let outcome = cx.background_executor().spawn(async move { load::load_path(&renderer, &path) }).await;
            this.update(cx, |this, cx| {
                if this.generation == generation && !this.live_svg { this.apply(outcome, true, cx); }
            }).ok();
        }));
    }

    fn apply(&mut self, outcome: Result<load::Decoded, String>, log_failure: bool, cx: &mut Context<Self>) {
        match outcome {
            Ok(decoded) => {
                self.open_note.clear();
                self.body = Body::Ready { image: decoded.image, nat_w: decoded.width, nat_h: decoded.height };
            }
            Err(reason) => {
                if log_failure {
                    crate::log::warn(&format!("preview failed for {}: {reason}", self.path.display()));
                }
                self.body = Body::Failed { reason };
            }
        }
        cx.notify();
    }

    fn natural(&self) -> Option<(f32, f32)> {
        match self.body {
            Body::Ready { nat_w, nat_h, .. } if nat_w.is_finite() && nat_h.is_finite() && nat_w > 0.0 && nat_h > 0.0 => Some((nat_w, nat_h)),
            _ => None,
        }
    }

    fn placed(&self) -> view::Placed {
        let (nat_w, nat_h) = self.natural().unwrap_or((1.0, 1.0));
        view::place(self.view, nat_w, nat_h, self.stage.w, self.stage.h)
    }

    /// Percent label and whether the image is in fit-to-window mode.
    pub fn zoom_state(&self) -> Option<(String, bool)> {
        if self.natural().is_none() { return None; }
        Some((view::percent_label(self.placed().scale), self.view.mode == ZoomMode::Fit))
    }

    fn image(&self) -> Option<&std::sync::Arc<RenderImage>> {
        match &self.body {
            Body::Ready { image, .. } => Some(image),
            _ => None,
        }
    }

    fn failure(&self) -> Option<String> {
        match &self.body {
            Body::Failed { reason } => Some(reason.clone()),
            _ => None,
        }
    }

    fn eid(&self, name: &str) -> gpui::SharedString {
        gpui::SharedString::from(format!("fp-{}-{name}", self.slot))
    }
}

impl Render for FilePreview {
    fn render(&mut self, window: &mut gpui::Window, cx: &mut Context<Self>) -> impl gpui::IntoElement {
        self.paint(window, cx)
    }
}

impl Focusable for FilePreview {
    fn focus_handle(&self, _: &gpui::App) -> FocusHandle { self.focus.clone() }
}

pub fn is_svg_path(path: &Path) -> bool {
    extension_is(path, &["svg"])
}

/// PNG, JPEG, GIF, WebP, BMP, and SVG. Other extensions are left to the default app.
pub fn is_image_path(path: &Path) -> bool {
    is_svg_path(path) || extension_is(path, &["png", "jpg", "jpeg", "gif", "webp", "bmp"])
}

fn extension_is(path: &Path, extensions: &[&str]) -> bool {
    path.extension().and_then(|ext| ext.to_str()).is_some_and(|ext| extensions.iter().any(|candidate| ext.eq_ignore_ascii_case(candidate)))
}

pub(super) fn copy(zh: &'static str, en: &'static str) -> String {
    crate::theme::tr(zh, en).to_string()
}
