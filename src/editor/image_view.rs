//! Image block rendering: aligned picture, side resize handles and floating top-centered toolbar.
use std::{collections::HashMap, path::PathBuf};
use gpui::{App, CursorStyle, Entity, EntityId, Global, MouseButton, ObjectFit, Resource, SharedString, div, prelude::*, px};
use super::{
    image_edit::ImagePop,
    image_probe::probe_dims,
    image_remote::RemoteImageState,
    image_resize::{self, MIN_SHARE},
    image_source::{Align, ImageSpec, ImageTarget},
    view::Editor,
};

#[derive(Default)]
struct ImageUi {
    measured: HashMap<(EntityId, u64), f32>,
    dims: HashMap<PathBuf, (f32, f32)>,
    avail: HashMap<(EntityId, u64), f32>,
}
impl Global for ImageUi {}

fn base_of(ui: &ImageUi, editor_id: EntityId, id: u64) -> f32 {
    ui.avail.get(&(editor_id, id)).copied().filter(|&w| w > 0.)
        .unwrap_or_else(|| f32::from(crate::theme::Theme::ocean().content_width))
}

pub(super) fn clear_editor_measured(editor_id: EntityId, cx: &mut App) {
    let ui = cx.default_global::<ImageUi>();
    ui.measured.retain(|(ed, _), _| *ed != editor_id);
    ui.avail.retain(|(ed, _), _| *ed != editor_id);
    image_resize::clear(editor_id, cx);
}
pub(super) fn rekey(cx: &mut App, editor_id: EntityId, old_id: u64, new_id: u64) {
    let ui = cx.default_global::<ImageUi>();
    if let Some(w) = ui.measured.get(&(editor_id, old_id)).copied() { ui.measured.insert((editor_id, new_id), w); }
    if let Some(w) = ui.avail.get(&(editor_id, old_id)).copied() { ui.avail.insert((editor_id, new_id), w); }
}

pub(super) fn relayout(editor: &Entity<Editor>, id: u64, cx: &mut App) {
    editor.update(cx, |this, cx| {
        if let Some(idx) = this.doc.index_of(id).filter(|&i| i < this.list.item_count()) {
            this.list.splice(idx..idx + 1, 1);
        }
        cx.notify();
    });
}
fn measure_available(editor: Entity<Editor>, id: u64, zoom: f32) -> impl IntoElement {
    let editor_id = editor.entity_id();
    gpui::canvas(|_, _, _| {}, move |bounds, _, _, cx| {
        let width = f32::from(bounds.size.width) / zoom;
        if width <= 0. { return; }
        let previous = cx.default_global::<ImageUi>().avail.insert((editor_id, id), width).unwrap_or(0.);
        if (previous - width).abs() > 0.5 { relayout(&editor, id, cx); }
    }).absolute().size_full()
}

fn handle(editor: Entity<Editor>, id: u64, left: bool, dir: f32, zoom: f32, img_h: Option<f32>, group: &SharedString, show: bool) -> impl IntoElement {
    crate::ui::resize_handle::side_pill(("image-resize", id as usize * 2 + usize::from(left)), left, img_h, zoom, group.clone(), show)
        .on_mouse_down(MouseButton::Left, move |event, window, cx| {
            image_resize::begin(&editor, id, dir, zoom, event.position.x, window, cx);
            cx.stop_propagation();
        })
}

pub(super) fn image_block(editor: Entity<Editor>, id: u64, target: ImageTarget, spec: ImageSpec, active: bool, pop: Option<ImagePop>, zoom: f32, has_overlay: bool, cx: &mut App) -> impl IntoElement {
    let (ready_path, remote_pending) = match target {
        ImageTarget::Local(path) => (Some(path), None),
        ImageTarget::Remote(url) => match super::image_remote::request(editor.clone(), id, &url, cx) {
            RemoteImageState::Ready(path) => (Some(path), None),
            RemoteImageState::Loading => (None, Some((url, false))),
            RemoteImageState::Failed => (None, Some((url, true))),
        },
    };
    let fetched_dims = ready_path.as_ref().and_then(|p| {
        let data = cx.fetch_asset::<gpui::ImgResourceLoader>(&Resource::from(p.clone()))?.ok()?;
        (data.frame_count() > 0).then(|| {
            let sz = data.size(0);
            (i32::from(sz.width) as f32, i32::from(sz.height) as f32)
        }).filter(|&(w, h)| w > 0. && h > 0.)
    });
    let editor_id = editor.entity_id();
    let zoom = zoom.max(0.01);
    let (base, raw_dims) = {
        let ui = cx.default_global::<ImageUi>();
        if let Some(p) = &ready_path {
            if let Some(d) = fetched_dims {
                ui.dims.insert(p.clone(), d);
            } else if !ui.dims.contains_key(p) && let Some(d) = probe_dims(p) {
                ui.dims.insert(p.clone(), d);
            }
        }
        let dims = ready_path.as_ref().and_then(|p| ui.dims.get(p).copied());
        (base_of(ui, editor_id, id), dims)
    };
    let draft = image_resize::width(editor_id, id, cx);
    let dragging = draft.is_some();
    let width = draft.or(spec.width.map(|w| w as f32)).unwrap_or(base).clamp(base * MIN_SHARE, base);
    let show = active || dragging;
    let show_bar = active && !dragging && !has_overlay;
    if !show_bar { crate::ui::motion::popup_reset(&format!("image-pop-{id}")); }
    let group: SharedString = format!("image-{id}").into();
    let scale = if spec.align == Align::Center { 2. } else { 1. };
    let focus = editor.clone();
    let ring = 0x547dd0;
    let pct = (width / base * 100.).round() as u32;
    if let Some((url, failed)) = remote_pending {
        let theme = crate::theme::Theme::ocean();
        let card_w = width * zoom;
        let label = if failed {
            crate::theme::tr("图片加载失败，点击重试", "Failed to load image, click to retry")
        } else {
            crate::theme::tr("正在加载图片…", "Loading image…")
        };
        return div().relative().w_full().flex().flex_col().items_start()
            .when(spec.align == Align::Center, |s| s.items_center()).when(spec.align == Align::Right, |s| s.items_end())
            .child(measure_available(editor.clone(), id, zoom))
            .child(div().id(("image", id as usize)).relative().w(px(card_w)).h(px(120. * zoom)).rounded(px(6. * zoom))
                .bg(theme.hover).border_1().border_color(if active { gpui::rgb(ring).into() } else { theme.hairline })
                .flex().items_center().justify_center().cursor(CursorStyle::PointingHand)
                .text_size(px(14. * zoom)).text_color(theme.text_secondary)
                .child(label)
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    if failed { super::image_remote::retry(&url); }
                    focus.update(cx, |this, cx| {
                        this.image_menu = None;
                        this.focus_block(id, window, cx);
                        cx.notify();
                    });
                    cx.stop_propagation();
                }))
            .when(show_bar, |s| s.child(div().absolute().top(px(-54.)).left_0().right_0().flex().justify_center()
                .child(super::image_size::image_toolbar(&editor, id, spec.align, pct, pop, cx))));
    }
    let path = ready_path.unwrap_or_default();
    let target_size = raw_dims.filter(|&(rw, rh)| rw > 0. && rh > 0.).map(|(rw, rh)| {
        let tw = (width * zoom).round().max(1.);
        let th = (tw * rh / rw).round().max(1.);
        (tw, th)
    });
    let picture = gpui::img(path).rounded(px(4. * zoom));
    let picture = match target_size {
        Some((tw, th)) => picture.w(px(tw)).h(px(th)).object_fit(ObjectFit::Fill),
        None => picture.w(px(width * zoom)),
    };
    let img_h = target_size.map(|(_, th)| th);
    let canvas_editor = editor.clone();
    div().relative().w_full().flex().flex_col().items_start()
        .when(spec.align == Align::Center, |s| s.items_center()).when(spec.align == Align::Right, |s| s.items_end())
        .child(measure_available(editor.clone(), id, zoom))
        .child(div().id(("image", id as usize)).relative().flex_none().group(group.clone())
            .when_some(target_size, |s, (tw, th)| s.w(px(tw)).h(px(th)))
            .child(picture)
            .child(gpui::canvas(|_, _, _| {}, move |bounds, _, _, cx| {
                let w = f32::from(bounds.size.width);
                let prev_w = cx.default_global::<ImageUi>().measured.insert((editor_id, id), w).unwrap_or(0.);
                if (prev_w - w).abs() > 0.5 && w > 0. {
                    relayout(&canvas_editor, id, cx);
                }
            }).absolute().size_full())
            .when(active, |s| s.child(div().absolute().inset_0().rounded(px(4. * zoom)).border(px(2.)).border_color(gpui::rgb(ring))))
            .child(handle(editor.clone(), id, true, -scale, zoom, img_h, &group, show))
            .child(handle(editor.clone(), id, false, scale, zoom, img_h, &group, show))
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                focus.update(cx, |this, cx| { this.image_menu = None; this.focus_block(id, window, cx); });
                cx.stop_propagation();
            }))
        .when(show_bar, |s| s.child(div().absolute().top(px(-54.)).left_0().right_0().flex().justify_center()
            .child(super::image_size::image_toolbar(&editor, id, spec.align, pct, pop, cx))))
}

/// (maximum content width, rendered width) of an image from the last paint.
pub(super) fn sizes(cx: &App, editor_id: EntityId, id: u64, minimal: bool) -> (f32, f32) {
    let zoom = super::zoom::zoom_factor(minimal).max(0.01);
    let fallback = f32::from(crate::theme::Theme::ocean().content_width);
    cx.try_global::<ImageUi>().map_or((fallback, 0.), |ui| (base_of(ui, editor_id, id), ui.measured.get(&(editor_id, id)).copied().map(|m| m / zoom).unwrap_or(0.)))
}
