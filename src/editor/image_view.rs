//! Image block rendering: aligned picture, corner resize handles and floating top-centered toolbar.
use std::{collections::HashMap, path::PathBuf};
use gpui::{App, Context, CursorStyle, Entity, Global, MouseButton, SharedString, Window, div, prelude::*, px};
use super::{
    image_edit::ImagePop,
    image_remote::RemoteImageState,
    image_source::{Align, ImageSpec, ImageTarget},
    view::Editor,
};

/// Smallest width an image can be dragged or stepped to, as a share of the content width.
const MIN_SHARE: f32 = 0.1;
const DEFAULT_MAX: f32 = 560.;

#[derive(Default)]
struct ImageUi {
    measured: HashMap<u64, f32>,
    natural: HashMap<PathBuf, f32>,
    paths: HashMap<u64, PathBuf>,
    avail: f32,
    drag: Option<Drag>,
}
impl Global for ImageUi {}
#[derive(Clone, Copy)]
struct Drag { id: u64, start_x: f32, start_w: f32, dir: f32, max: f32, width: f32 }
#[derive(Clone)]
struct Resize { id: u64, dir: f32 }
struct Ghost;
impl Render for Ghost {
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement { gpui::Empty }
}

fn base_of(ui: &ImageUi, id: u64) -> f32 {
    let avail = ui.avail;
    match ui.paths.get(&id).and_then(|p| ui.natural.get(p)).copied().filter(|&w| w > 0.) {
        Some(w) if avail > 0. => w.min(avail),
        Some(w) => w,
        None => avail,
    }
}

pub(super) fn rekey(cx: &mut App, old_id: u64, new_id: u64) {
    let ui = cx.default_global::<ImageUi>();
    if let Some(p) = ui.paths.get(&old_id).cloned() { ui.paths.insert(new_id, p); }
    if let Some(w) = ui.measured.get(&old_id).copied() { ui.measured.insert(new_id, w); }
}

fn begin(resize: &Resize, x: f32, cx: &mut App) {
    let ui = cx.default_global::<ImageUi>();
    let width = ui.measured.get(&resize.id).copied().unwrap_or(0.);
    let base = base_of(ui, resize.id);
    let max = if base > 0. { base } else { DEFAULT_MAX };
    ui.drag = Some(Drag { id: resize.id, start_x: x, start_w: width, dir: resize.dir, max, width });
}
fn resize_move(editor: &Entity<Editor>, id: u64, x: f32, cx: &mut App) {
    let Some(drag) = cx.default_global::<ImageUi>().drag.as_mut().filter(|d| d.id == id) else { return };
    drag.width = (drag.start_w + (x - drag.start_x) * drag.dir).clamp(drag.max * MIN_SHARE, drag.max);
    editor.update(cx, |this, cx| {
        if let Some(index) = this.doc.index_of(id) { this.list.splice(index..index + 1, 1); }
        cx.notify();
    });
}
fn resize_end(editor: &Entity<Editor>, id: u64, cx: &mut App) {
    let Some(drag) = cx.default_global::<ImageUi>().drag.take().filter(|d| d.id == id) else { return };
    editor.update(cx, |this, cx| {
        if (drag.width - drag.start_w).abs() >= 1. { this.edit_image(id, |s| s.width = Some(drag.width.round() as u32), cx); }
        else { cx.notify(); }
    });
}

/// 10px white rounded square on a corner; dragging it resizes the width in proportion (the height follows).
fn handle(id: u64, left: bool, top: bool, dir: f32, group: &SharedString, show: bool) -> impl IntoElement {
    let ring = if crate::theme::is_dark() { 0x2b6be6 } else { 0x2f6fe4 };
    div().id(("image-resize", id as usize * 4 + usize::from(left) + 2 * usize::from(top))).absolute().size(px(10.))
        .when(left, |s| s.left(px(-5.))).when(!left, |s| s.right(px(-5.)))
        .when(top, |s| s.top(px(-5.))).when(!top, |s| s.bottom(px(-5.)))
        .bg(gpui::white()).border(px(1.5)).border_color(gpui::rgb(ring)).rounded(px(3.))
        .cursor(if left == top { CursorStyle::ResizeUpLeftDownRight } else { CursorStyle::ResizeUpRightDownLeft })
        .invisible().when(show, |s| s.visible()).group_hover(group.clone(), |s| s.visible())
        .on_drag(Resize { id, dir }, |resize, _, window, cx| {
            begin(resize, f32::from(window.mouse_position().x), cx); cx.new(|_| Ghost)
        })
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

pub(super) fn image_block(editor: Entity<Editor>, id: u64, target: ImageTarget, spec: ImageSpec, active: bool, pop: Option<ImagePop>, cx: &mut App) -> impl IntoElement {
    let (ready_path, remote_pending) = match target {
        ImageTarget::Local(path) => (Some(path), None),
        ImageTarget::Remote(url) => match super::image_remote::request(editor.clone(), id, &url, cx) {
            RemoteImageState::Ready(path) => (Some(path), None),
            RemoteImageState::Loading => (None, Some((url, false))),
            RemoteImageState::Failed => (None, Some((url, true))),
        },
    };
    let (base, draft) = {
        let ui = cx.default_global::<ImageUi>();
        if let Some(p) = &ready_path {
            ui.paths.insert(id, p.clone());
        }
        (base_of(ui, id), ui.drag.filter(|d| d.id == id))
    };
    let dragging = draft.is_some() && cx.has_active_drag();
    let limit = if base > 0. { base } else { DEFAULT_MAX };
    let width = if dragging { draft.map(|d| d.width) } else { spec.width.map(|w| w as f32) }
        .map(|w| w.min(limit).max(limit * MIN_SHARE));
    let record_natural = spec.width.is_none() && !dragging;
    let show = active || dragging;
    let group: SharedString = format!("image-{id}").into();
    let scale = if spec.align == Align::Center { 2. } else { 1. };
    let focus = editor.clone();
    let ring = if crate::theme::is_dark() { 0x2b6be6 } else { 0x2f6fe4 };
    let pct = size_percent(draft.map(|d| d.width), spec.width, base);
    if let Some((url, failed)) = remote_pending {
        let theme = crate::theme::Theme::ocean();
        let card_w = width.unwrap_or(DEFAULT_MAX);
        let label = if failed {
            crate::theme::tr("图片加载失败，点击重试", "Failed to load image, click to retry")
        } else {
            crate::theme::tr("正在加载图片…", "Loading image…")
        };
        return div().relative().w_full().flex().flex_col()
            .when(spec.align == Align::Center, |s| s.items_center()).when(spec.align == Align::Right, |s| s.items_end())
            .child(gpui::canvas(|bounds, _, cx| { cx.default_global::<ImageUi>().avail = f32::from(bounds.size.width); }, |_, _, _, _| {})
                .absolute().size_full())
            .when(show, |s| s.child(div().absolute().top(px(-44.)).left_0().right_0().flex().justify_center()
                .child(super::image_size::image_toolbar(&editor, id, spec.align, pct, pop))))
            .child(div().id(("image", id as usize)).relative().w(px(card_w)).h(px(120.)).rounded(px(6.))
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
                }));
    }
    let path = ready_path.unwrap_or_default();
    let natural_path = path.clone();
    let picture = gpui::img(path).rounded(px(4.));
    let picture = match width { Some(w) => picture.w(px(w)), None => picture.max_w(px(DEFAULT_MAX)).max_h(px(420.)) };
    let (move_editor, up_editor, out_editor, canvas_editor) = (editor.clone(), editor.clone(), editor.clone(), editor.clone());
    div().relative().w_full().flex().flex_col()
        .when(spec.align == Align::Center, |s| s.items_center()).when(spec.align == Align::Right, |s| s.items_end())
        .child(gpui::canvas(|bounds, _, cx| { cx.default_global::<ImageUi>().avail = f32::from(bounds.size.width); }, |_, _, _, _| {})
            .absolute().size_full())
        .when(show, |s| s.child(div().absolute().top(px(-44.)).left_0().right_0().flex().justify_center()
            .child(super::image_size::image_toolbar(&editor, id, spec.align, pct, pop))))
        .child(div().id(("image", id as usize)).relative().flex_none().group(group.clone())
            .child(picture)
            .child(gpui::canvas(move |bounds, _, cx| {
                let w = f32::from(bounds.size.width);
                let ui = cx.default_global::<ImageUi>();
                let prev_w = ui.measured.insert(id, w).unwrap_or(0.);
                if record_natural && w > 0. { ui.natural.insert(natural_path.clone(), w); }
                if prev_w == 0. && w > 0. {
                    canvas_editor.update(cx, |this, cx| {
                        if let Some(idx) = this.doc.index_of(id) && idx < this.list.item_count() {
                            this.list.splice(idx..idx + 1, 1);
                            cx.notify();
                        }
                    });
                }
            }, |_, _, _, _| {}).absolute().size_full())
            .when(active, |s| s.child(div().absolute().inset_0().rounded(px(4.)).border(px(2.)).border_color(gpui::rgb(ring))))
            .child(handle(id, true, true, -scale, &group, show)).child(handle(id, false, true, scale, &group, show))
            .child(handle(id, true, false, -scale, &group, show)).child(handle(id, false, false, scale, &group, show))
            .on_drag_move::<Resize>(move |event, _, cx| resize_move(&move_editor, id, f32::from(event.event.position.x), cx))
            .on_mouse_up(MouseButton::Left, move |_, _, cx| resize_end(&up_editor, id, cx))
            .on_mouse_up_out(MouseButton::Left, move |_, _, cx| resize_end(&out_editor, id, cx))
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                focus.update(cx, |this, cx| { this.image_menu = None; this.focus_block(id, window, cx); });
                cx.stop_propagation();
            }))
}

/// (original width, rendered width) of an image from the last paint; 0 until it has been laid out.
pub(super) fn sizes(cx: &App, id: u64) -> (f32, f32) {
    cx.try_global::<ImageUi>().map_or((0., 0.), |ui| (base_of(ui, id), ui.measured.get(&id).copied().unwrap_or(0.)))
}
fn size_percent(draft: Option<f32>, width: Option<u32>, base: f32) -> u32 {
    let base = if base > 0. { base } else { DEFAULT_MAX };
    let Some(shown) = draft.or(width.map(|w| w as f32)) else { return 100 };
    ((shown / base * 100.).round() as u32).clamp(1, 100)
}
