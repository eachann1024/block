//! Image block rendering: aligned picture, side resize handles and floating top-centered toolbar.
use std::{collections::HashMap, path::PathBuf};
use gpui::{App, Context, CursorStyle, Entity, EntityId, Global, MouseButton, ObjectFit, Resource, SharedString, Window, div, prelude::*, px};
use super::{
    image_edit::ImagePop,
    image_probe::probe_dims,
    image_remote::RemoteImageState,
    image_source::{Align, ImageSpec, ImageTarget},
    view::Editor,
};

/// Smallest width an image can be dragged or stepped to, as a share of the content width.
const MIN_SHARE: f32 = 0.1;
const DEFAULT_MAX: f32 = 560.;
const DEFAULT_MAX_H: f32 = 420.;

#[derive(Default)]
struct ImageUi {
    measured: HashMap<(EntityId, u64), f32>,
    dims: HashMap<PathBuf, (f32, f32)>,
    paths: HashMap<(EntityId, u64), PathBuf>,
    avail: HashMap<EntityId, f32>,
    drag: Option<Drag>,
}
impl Global for ImageUi {}
#[derive(Clone, Copy)]
struct Drag { editor_id: EntityId, id: u64, start_x: f32, start_w: f32, dir: f32, max: f32, width: f32, zoom: f32 }
#[derive(Clone)]
struct Resize { editor_id: EntityId, id: u64, dir: f32, zoom: f32 }
struct Ghost;
impl Render for Ghost { fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement { gpui::Empty } }

fn base_of(ui: &ImageUi, editor_id: EntityId, id: u64) -> f32 {
    let avail = ui.avail.get(&editor_id).copied().unwrap_or(0.);
    if let Some(&(raw_w, raw_h)) = ui.paths.get(&(editor_id, id)).and_then(|p| ui.dims.get(p)).filter(|&&(w, h)| w > 0. && h > 0.) {
        let nat_w = raw_w.min(DEFAULT_MAX).min(raw_w * (DEFAULT_MAX_H / raw_h));
        return if avail > 0. { nat_w.min(avail) } else { nat_w };
    }
    if avail > 0. { avail.min(DEFAULT_MAX) } else { DEFAULT_MAX }
}

pub(super) fn clear_editor_measured(editor_id: EntityId, cx: &mut App) {
    let ui = cx.default_global::<ImageUi>();
    ui.measured.retain(|(ed, _), _| *ed != editor_id);
    if ui.drag.as_ref().is_some_and(|d| d.editor_id == editor_id) {
        ui.drag = None;
    }
}
pub(super) fn rekey(cx: &mut App, editor_id: EntityId, old_id: u64, new_id: u64) {
    let ui = cx.default_global::<ImageUi>();
    if let Some(p) = ui.paths.get(&(editor_id, old_id)).cloned() { ui.paths.insert((editor_id, new_id), p); }
    if let Some(w) = ui.measured.get(&(editor_id, old_id)).copied() { ui.measured.insert((editor_id, new_id), w); }
}

fn begin(resize: &Resize, x: f32, cx: &mut App) {
    let zoom = resize.zoom;
    let ui = cx.default_global::<ImageUi>();
    let base = base_of(ui, resize.editor_id, resize.id) * zoom;
    let max = if base > 0. { base } else { DEFAULT_MAX * zoom };
    let width = ui.measured.get(&(resize.editor_id, resize.id)).copied().filter(|&w| w > 0.).unwrap_or(max);
    ui.drag = Some(Drag {
        editor_id: resize.editor_id,
        id: resize.id,
        start_x: x,
        start_w: width,
        dir: resize.dir,
        max,
        width,
        zoom,
    });
}
fn resize_move(editor: &Entity<Editor>, id: u64, x: f32, cx: &mut App) {
    let editor_id = editor.entity_id();
    let Some(drag) = cx.default_global::<ImageUi>().drag.as_mut().filter(|d| d.editor_id == editor_id && d.id == id) else { return };
    drag.width = (drag.start_w + (x - drag.start_x) * drag.dir).clamp(drag.max * MIN_SHARE, drag.max);
    editor.update(cx, |this, cx| {
        if let Some(idx) = this.doc.index_of(id) {
            let row = this.column_group_range(idx).map_or(idx, |r| r.start);
            this.list.splice(row..row + 1, 1);
        }
        cx.notify();
    });
}
fn resize_end(editor: &Entity<Editor>, id: u64, cx: &mut App) {
    let editor_id = editor.entity_id();
    let Some(drag) = cx.default_global::<ImageUi>().drag.take().filter(|d| d.editor_id == editor_id && d.id == id) else { return };
    let zoom = drag.zoom.max(0.01);
    editor.update(cx, |this, cx| {
        if (drag.width - drag.start_w).abs() >= 1. {
            this.edit_image(id, |s| s.width = Some((drag.width / zoom).round().max(1.) as u32), cx);
        } else { cx.notify(); }
    });
}

/// Centered side pill handle with a 12px hit area; dragging it resizes the width in proportion (the height follows).
fn handle(editor_id: EntityId, id: u64, left: bool, dir: f32, zoom: f32, img_h: Option<f32>, group: &SharedString, show: bool) -> impl IntoElement {
    let bar_h = match img_h {
        Some(h) if h < 64. => (h * 0.6).round().max(16.),
        _ => 48.,
    };
    let bar_color = if crate::theme::is_dark() { 0x707070 } else { 0x9a9a9a };
    div().absolute().top_0().bottom_0()
        .when(left, |s| s.left(px(-12.5))).when(!left, |s| s.right(px(-12.5)))
        .flex().items_center()
        .child(div().id(("image-resize", id as usize * 2 + usize::from(left)))
            .w(px(12.)).h(px(bar_h + 16.)).flex().items_center().justify_center()
            .cursor(CursorStyle::ResizeLeftRight)
            .invisible().when(show, |s| s.visible()).group_hover(group.clone(), |s| s.visible())
            .on_drag(Resize { editor_id, id, dir, zoom }, |resize, _, window, cx| {
                begin(resize, f32::from(window.mouse_position().x), cx); cx.new(|_| Ghost)
            })
            .child(div().w(px(3.)).h(px(bar_h)).rounded(px(3.)).bg(gpui::rgb(bar_color))))
}

/// The block under `position`, preferring rendered list geometry so static images are found too.
impl Editor {
    pub(super) fn block_at(&self, position: gpui::Point<gpui::Pixels>) -> Option<(u64, gpui::Bounds<gpui::Pixels>)> {
        let start = self.list.logical_scroll_top().item_ix;
        for index in start..self.doc.blocks.len() {
            let Some(bounds) = self.list.bounds_for_item(index) else { continue };
            if bounds.top() > position.y { break; }
            if bounds.contains(&position) { return Some((self.doc.blocks[index].id, bounds)); }
        }
        None
    }
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
    let (base, draft, raw_dims) = {
        let ui = cx.default_global::<ImageUi>();
        if let Some(p) = &ready_path {
            ui.paths.insert((editor_id, id), p.clone());
            if let Some(d) = fetched_dims {
                ui.dims.insert(p.clone(), d);
            } else if !ui.dims.contains_key(p) && let Some(d) = probe_dims(p) {
                ui.dims.insert(p.clone(), d);
            }
        }
        let dims = ready_path.as_ref().and_then(|p| ui.dims.get(p).copied());
        (base_of(ui, editor_id, id), ui.drag.filter(|d| d.editor_id == editor_id && d.id == id), dims)
    };
    let drag_logical = draft.map(|d| d.width / zoom);
    let dragging = draft.is_some() && cx.has_active_drag();
    let limit = if base > 0. { base } else { DEFAULT_MAX };
    let width = if dragging { drag_logical } else { spec.width.map(|w| w as f32) }
        .map(|w| w.min(limit).max(limit * MIN_SHARE));
    let show = active || dragging;
    let show_bar = show && !has_overlay;
    let group: SharedString = format!("image-{id}").into();
    let scale = if spec.align == Align::Center { 2. } else { 1. };
    let focus = editor.clone();
    let ring = 0x547dd0;
    let pct = size_percent(drag_logical, spec.width, base);
    if let Some((url, failed)) = remote_pending {
        let theme = crate::theme::Theme::ocean();
        let card_w = width.unwrap_or(limit) * zoom;
        let label = if failed {
            crate::theme::tr("图片加载失败，点击重试", "Failed to load image, click to retry")
        } else {
            crate::theme::tr("正在加载图片…", "Loading image…")
        };
        return div().relative().w_full().flex().flex_col().items_start()
            .when(spec.align == Align::Center, |s| s.items_center()).when(spec.align == Align::Right, |s| s.items_end())
            .child(gpui::canvas(move |bounds, _, cx| {
                cx.default_global::<ImageUi>().avail.insert(editor_id, f32::from(bounds.size.width) / zoom);
            }, |_, _, _, _| {}).absolute().size_full())
            .child(div().id(("image", id as usize)).relative().w(px(card_w)).h(px(120. * zoom)).rounded(px(6.))
                .bg(theme.hover).border_1().border_color(if active { gpui::rgb(ring).into() } else { theme.hairline })
                .flex().items_center().justify_center().cursor(CursorStyle::PointingHand)
                .text_size(px(14.)).text_color(theme.text_secondary)
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
            .when(show_bar, |s| s.child(div().absolute().top(px(-44.)).left_0().right_0().flex().justify_center()
                .child(super::image_size::image_toolbar(&editor, id, spec.align, pct, pop))));
    }
    let path = ready_path.unwrap_or_default();
    let target_size = raw_dims.filter(|&(rw, rh)| rw > 0. && rh > 0.).map(|(rw, rh)| {
        let tw = (width.unwrap_or(limit) * zoom).round().max(1.);
        let th = (tw * rh / rw).round().max(1.);
        (tw, th)
    });
    let picture = gpui::img(path).rounded(px(4.));
    let picture = match target_size {
        Some((tw, th)) => picture.w(px(tw)).h(px(th)).object_fit(ObjectFit::Fill),
        None => match width { Some(w) => picture.w(px(w * zoom)), None => picture.max_w(px(DEFAULT_MAX * zoom)).max_h(px(DEFAULT_MAX_H * zoom)) },
    };
    let img_h = target_size.map(|(_, th)| th);
    let (move_editor, up_editor, out_editor, canvas_editor) = (editor.clone(), editor.clone(), editor.clone(), editor.clone());
    div().relative().w_full().flex().flex_col().items_start()
        .when(spec.align == Align::Center, |s| s.items_center()).when(spec.align == Align::Right, |s| s.items_end())
        .child(gpui::canvas(move |bounds, _, cx| {
            cx.default_global::<ImageUi>().avail.insert(editor_id, f32::from(bounds.size.width) / zoom);
        }, |_, _, _, _| {}).absolute().size_full())
        .child(div().id(("image", id as usize)).relative().flex_none().group(group.clone())
            .when_some(target_size, |s, (tw, th)| s.w(px(tw)).h(px(th)))
            .child(picture)
            .child(gpui::canvas(|_, _, _| {}, move |bounds, _, _, cx| {
                let w = f32::from(bounds.size.width);
                let prev_w = cx.default_global::<ImageUi>().measured.insert((editor_id, id), w).unwrap_or(0.);
                if (prev_w - w).abs() > 0.5 && w > 0. {
                    canvas_editor.update(cx, |this, cx| {
                        if let Some(idx) = this.doc.index_of(id) {
                            let row = this.column_group_range(idx).map_or(idx, |r| r.start);
                            if row < this.list.item_count() {
                                this.list.splice(row..row + 1, 1);
                                cx.notify();
                            }
                        }
                    });
                }
            }).absolute().size_full())
            .when(active, |s| s.child(div().absolute().top(px(-6.)).left(px(-6.)).right(px(-6.)).bottom(px(-6.)).rounded(px(8.)).border(px(2.)).border_color(gpui::rgb(ring))))
            .child(handle(editor_id, id, true, -scale, zoom, img_h, &group, show))
            .child(handle(editor_id, id, false, scale, zoom, img_h, &group, show))
            .on_drag_move::<Resize>(move |event, _, cx| resize_move(&move_editor, id, f32::from(event.event.position.x), cx))
            .on_mouse_up(MouseButton::Left, move |_, _, cx| resize_end(&up_editor, id, cx))
            .on_mouse_up_out(MouseButton::Left, move |_, _, cx| resize_end(&out_editor, id, cx))
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                focus.update(cx, |this, cx| { this.image_menu = None; this.focus_block(id, window, cx); });
                cx.stop_propagation();
            }))
        .when(show_bar, |s| s.child(div().absolute().top(px(-44.)).left_0().right_0().flex().justify_center()
            .child(super::image_size::image_toolbar(&editor, id, spec.align, pct, pop))))
}

/// (original width, rendered width) of an image from the last paint; 0 until it has been laid out.
pub(super) fn sizes(cx: &App, editor_id: EntityId, id: u64, minimal: bool) -> (f32, f32) {
    let zoom = super::zoom::zoom_factor(minimal).max(0.01);
    cx.try_global::<ImageUi>().map_or((0., 0.), |ui| (base_of(ui, editor_id, id), ui.measured.get(&(editor_id, id)).copied().map(|m| m / zoom).unwrap_or(0.)))
}
fn size_percent(draft: Option<f32>, width: Option<u32>, base: f32) -> u32 {
    let base = if base > 0. { base } else { DEFAULT_MAX };
    let Some(shown) = draft.or(width.map(|w| w as f32)) else { return 100 };
    ((shown / base * 100.).round() as u32).clamp(1, 100)
}
