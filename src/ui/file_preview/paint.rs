//! Flat preview chrome: filename, zoom cluster, and a borderless stage.

use gpui::{
    ClickEvent, Context, CursorStyle, FontWeight, IntoElement, MouseButton, MouseDownEvent, MouseMoveEvent,
    MouseUpEvent, ObjectFit, ParentElement, PinchEvent, RenderImage, ScrollEvent, SharedString, Styled,
    Window, canvas, div, img, prelude::*, px,
};
use crate::{
    icons::Icon,
    theme::{Theme, tr},
    ui::icon_button::dock_icon_button,
};

use super::view::{self, ZoomMode};
use super::FilePreview;

impl FilePreview {
    pub(super) fn paint(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.track_drag(window, cx);
        let theme = Theme::ocean();
        let ready = self.natural().is_some();
        div()
            .id(self.eid("root"))
            .track_focus(&self.focus)
            .size_full()
            .flex()
            .flex_col()
            .overflow_hidden()
            .bg(theme.paper)
            .text_color(theme.text)
            .occlude()
            .child(self.header(&theme, ready, cx))
            .child(self.stage(&theme, window, cx))
    }

    fn header(&self, theme: &Theme, ready: bool, cx: &mut Context<Self>) -> impl IntoElement {
        let name = file_label(&self.path);
        div()
            .h(px(40.))
            .w_full()
            .flex_none()
            .px(px(16.))
            .flex()
            .items_center()
            .justify_end()
            .gap(px(12.))
            .border_b_1()
            .border_color(theme.hairline)
            .bg(theme.paper)
            .when(self.show_filename, |row| {
                row.child(
                    div()
                        .min_w_0()
                        .flex_1()
                        .overflow_hidden()
                        .whitespace_nowrap()
                        .text_ellipsis()
                        .font(crate::ui::settings_controls::mono_font())
                        .text_size(px(14.))
                        .line_height(px(20.))
                        .font_weight(FontWeight(560.))
                        .child(name),
                )
            })
            .when(ready, |row| {
                let placed = self.placed();
                let fit = self.view.mode == ZoomMode::Fit;
                row.child(
                    div()
                        .flex_none()
                        .flex()
                        .items_center()
                        .gap(px(2.))
                        .child(dock_icon_button(self.eid("out"), Icon::Minus, false, tr("缩小", "Zoom out"), cx.listener(|this, _, _, cx| this.zoom_button(1.0 / 1.2, cx))))
                        .child(self.percent_button(theme, &view::percent_label(placed.scale), cx))
                        .child(dock_icon_button(self.eid("in"), Icon::Plus, false, tr("放大", "Zoom in"), cx.listener(|this, _, _, cx| this.zoom_button(1.2, cx))))
                        .child(dock_icon_button(self.eid("fit"), Icon::WindowMax, fit, tr("适应窗口", "Fit"), cx.listener(|this, _, _, cx| this.zoom_fit(cx)))),
                )
            })
    }

    fn percent_button(&self, theme: &Theme, label: &str, cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .id(self.eid("pct"))
            .h(px(30.))
            .min_w(px(52.))
            .px(px(8.))
            .flex_none()
            .flex()
            .items_center()
            .justify_center()
            .rounded(crate::ui::radius::CONTROL)
            .cursor(CursorStyle::PointingHand)
            .font(crate::ui::settings_controls::mono_font())
            .text_size(px(14.))
            .line_height(px(20.))
            .text_color(theme.text)
            .hover(|style| style.bg(theme.hover))
            .tooltip(crate::ui::tooltip::tooltip(tr("重置为 100%", "Reset to 100%")))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.zoom_actual(cx)))
            .child(label.to_string())
    }

    fn stage(&mut self, theme: &Theme, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let weak = cx.entity().downgrade();
        let placed = self.placed();
        let image = self.image().cloned();
        let failed = self.failure();
        let loading = matches!(self.body, super::Body::Loading);
        let cursor = if self.natural().is_some() {
            if self.dragging { CursorStyle::ClosedHand } else { CursorStyle::OpenHand }
        } else {
            CursorStyle::Arrow
        };
        div()
            .id(self.eid("stage"))
            .relative()
            .flex_1()
            .min_h_0()
            .overflow_hidden()
            .bg(theme.paper)
            .cursor(cursor)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, event: &MouseDownEvent, window, cx| this.begin_drag(event, window, cx)))
            .on_mouse_up(MouseButton::Left, cx.listener(|this, _: &MouseUpEvent, _, cx| this.end_drag(cx)))
            .on_mouse_up_out(MouseButton::Left, cx.listener(|this, _: &MouseUpEvent, _, cx| this.end_drag(cx)))
            .on_mouse_move(cx.listener(|this, event: &MouseMoveEvent, _, cx| if this.dragging { this.drag_to(event.position, cx); }))
            .on_click(cx.listener(|this, event: &ClickEvent, _, cx| this.on_click(event, cx)))
            .on_scroll(cx.listener(|this, event: &ScrollEvent, window, cx| this.on_scroll(event, window, cx)))
            .on_pinch(cx.listener(|this, event: &PinchEvent, window, cx| this.on_pinch(event, window, cx)))
            .child(canvas(
                move |bounds, _, cx| {
                    weak.update(cx, |this, cx| {
                        let next = super::StageBox {
                            x: f32::from(bounds.origin.x),
                            y: f32::from(bounds.origin.y),
                            w: f32::from(bounds.size.width),
                            h: f32::from(bounds.size.height),
                        };
                        if (this.stage.x - next.x).abs() > 0.5 || (this.stage.y - next.y).abs() > 0.5 || (this.stage.w - next.w).abs() > 0.5 || (this.stage.h - next.h).abs() > 0.5 {
                            this.stage = next;
                            cx.notify();
                        }
                    }).ok();
                },
                |_, _, _, _| {},
            ).absolute().top_0().left_0().size_full())
            .children(image.filter(|_| placed.width > 0.0 && placed.height > 0.0).map(|image| positioned_image(self.eid("image"), image, placed)))
            .when(loading, |stage| stage.child(center_copy(theme, tr("正在载入…", "Loading…"))))
            .when(!loading, |stage| stage.children(failed.map(|reason| fallback(theme, &self.path, reason, &self.open_note, self.eid("open"), cx))))
    }
}

fn positioned_image(id: SharedString, image: std::sync::Arc<RenderImage>, placed: view::Placed) -> impl IntoElement {
    div()
        .absolute()
        .left(px(placed.x))
        .top(px(placed.y))
        .w(px(placed.width))
        .h(px(placed.height))
        .overflow_hidden()
        .child(img(image).id(id).size_full().object_fit(ObjectFit::Fill))
}

fn fallback(theme: &Theme, path: &std::path::Path, reason: String, note: &str, id: SharedString, cx: &mut Context<FilePreview>) -> impl IntoElement {
    let path = path.to_path_buf();
    div()
        .absolute()
        .top_0()
        .left_0()
        .size_full()
        .flex()
        .flex_col()
        .items_center()
        .justify_center()
        .px(px(32.))
        .gap(px(8.))
        .child(div().text_size(px(16.)).line_height(px(24.)).font_weight(FontWeight(560.)).text_color(theme.text).child(tr("无法预览此文件", "Cannot preview this file")))
        .child(div().max_w(px(420.)).text_size(px(14.)).line_height(px(20.)).text_color(theme.text_secondary).text_center().child(reason))
        .child(
            div()
                .id(id)
                .mt(px(8.))
                .h(px(32.))
                .px(px(14.))
                .flex()
                .items_center()
                .rounded(crate::ui::radius::CONTROL)
                .border_1()
                .border_color(theme.btn_border)
                .bg(theme.paper)
                .text_size(px(14.))
                .text_color(theme.text)
                .cursor(CursorStyle::PointingHand)
                .hover(|style| style.bg(theme.hover))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    match super::open_app::open_default(&path) {
                        Ok(()) => this.open_note.clear(),
                        Err(message) => {
                            crate::log::warn(&format!("default app open failed for {}: {message}", this.path.display()));
                            this.open_note = message;
                        }
                    }
                    cx.notify();
                }))
                .child(tr("使用默认应用打开", "Open in Default App")),
        )
        .when(!note.is_empty(), |col| col.child(div().text_size(px(14.)).line_height(px(20.)).text_color(theme.text_secondary).child(note.to_string())))
}

fn center_copy(theme: &Theme, text: &'static str) -> impl IntoElement {
    div().size_full().flex().items_center().justify_center().text_size(px(14.)).line_height(px(20.)).text_color(theme.text_secondary).child(text)
}

fn file_label(path: &std::path::Path) -> String {
    path.file_name().map(|name| name.to_string_lossy().into_owned()).filter(|name| !name.is_empty()).unwrap_or_else(|| tr("图片", "Image").to_string())
}
