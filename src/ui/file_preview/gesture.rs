//! Pointer, wheel and pinch navigation over one cached image.

use gpui::{ClickEvent, Context, DispatchPhase, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, PinchEvent, Point, Pixels, ScrollDelta, ScrollEvent, Window};

use super::view::{self, ViewTransform, ZoomMode};
use super::FilePreview;

impl FilePreview {
    pub(super) fn track_drag(&self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.dragging { return; }
        let entity = cx.entity();
        window.on_mouse_event(move |event: &MouseMoveEvent, phase, _, cx| {
            if phase != DispatchPhase::Capture || !event.dragging() { return; }
            entity.update(cx, |this, cx| this.drag_to(event.position, cx));
        });
        let entity = cx.entity();
        window.on_mouse_event(move |event: &MouseUpEvent, phase, _, cx| {
            if phase != DispatchPhase::Capture || event.button != MouseButton::Left { return; }
            entity.update(cx, |this, cx| this.end_drag(cx));
        });
    }

    pub(super) fn begin_drag(&mut self, event: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        if event.button != MouseButton::Left || self.natural().is_none() { return; }
        window.focus(&self.focus, cx);
        let (nat_w, nat_h) = self.natural().unwrap();
        self.drag = Some(super::DragOrigin {
            x: f32::from(event.position.x),
            y: f32::from(event.position.y),
            pan_x: self.view.pan_x,
            pan_y: self.view.pan_y,
            scale: view::effective_scale(self.view, nat_w, nat_h, self.stage.w, self.stage.h),
        });
        self.dragging = true;
        self.pointer_moved = false;
        cx.notify();
    }

    pub(super) fn drag_to(&mut self, position: Point<Pixels>, cx: &mut Context<Self>) {
        let Some(origin) = self.drag else { return };
        let dx = f32::from(position.x) - origin.x;
        let dy = f32::from(position.y) - origin.y;
        if !dx.is_finite() || !dy.is_finite() { return; }
        if dx.hypot(dy) > 3.0 { self.pointer_moved = true; }
        if !self.pointer_moved { return; }
        self.view = view::pan_by(
            ViewTransform { mode: ZoomMode::Custom, scale: origin.scale, pan_x: origin.pan_x, pan_y: origin.pan_y },
            dx,
            dy,
        );
        cx.notify();
    }

    pub(super) fn end_drag(&mut self, cx: &mut Context<Self>) {
        if !self.dragging { return; }
        self.dragging = false;
        cx.notify();
    }

    pub(super) fn on_click(&mut self, event: &ClickEvent, cx: &mut Context<Self>) {
        if event.click_count() >= 2 && !self.pointer_moved && self.natural().is_some() {
            self.view = if self.view.mode == ZoomMode::Fit { ViewTransform::actual() } else { ViewTransform::fit() };
            cx.notify();
        }
    }

    pub(super) fn on_scroll(&mut self, event: &ScrollEvent, window: &mut Window, cx: &mut Context<Self>) {
        cx.stop_propagation();
        window.prevent_default();
        let Some((nat_w, nat_h)) = self.natural() else { return; };
        let (dx, dy, precise) = match event.delta {
            ScrollDelta::Pixels(delta) => (f32::from(delta.x), f32::from(delta.y), true),
            ScrollDelta::Lines(delta) => (delta.x, delta.y, false),
        };
        if !dx.is_finite() || !dy.is_finite() { return; }
        let zoom_mod = event.modifiers.control || event.modifiers.secondary(cx);
        if precise && !zoom_mod {
            self.pan(nat_w, nat_h, dx, dy, cx);
        } else if !precise && event.modifiers.shift && !zoom_mod {
            let horizontal = if dx.abs() >= dy.abs() { dx } else { dy };
            self.pan(nat_w, nat_h, horizontal, 0.0, cx);
        } else {
            self.zoom_at(event.position, nat_w, nat_h, view::wheel_factor(dy, precise || zoom_mod), cx);
        }
    }

    pub(super) fn on_pinch(&mut self, event: &PinchEvent, window: &mut Window, cx: &mut Context<Self>) {
        cx.stop_propagation();
        window.prevent_default();
        let Some((nat_w, nat_h)) = self.natural() else { return; };
        if !event.delta.is_finite() || event.delta == 0.0 { return; }
        self.zoom_at(event.position, nat_w, nat_h, (1.0 + event.delta).clamp(0.5, 2.0), cx);
    }

    pub fn zoom_button(&mut self, factor: f32, cx: &mut Context<Self>) {
        let Some((nat_w, nat_h)) = self.natural() else { return; };
        let scale = view::effective_scale(self.view, nat_w, nat_h, self.stage.w, self.stage.h);
        self.view = view::zoom_at(ViewTransform { scale, ..self.view }, 0.0, 0.0, scale * factor);
        cx.notify();
    }

    pub fn zoom_actual(&mut self, cx: &mut Context<Self>) {
        self.view = ViewTransform::actual();
        cx.notify();
    }

    pub fn zoom_fit(&mut self, cx: &mut Context<Self>) {
        self.view = ViewTransform::fit();
        cx.notify();
    }

    fn pan(&mut self, nat_w: f32, nat_h: f32, dx: f32, dy: f32, cx: &mut Context<Self>) {
        let scale = view::effective_scale(self.view, nat_w, nat_h, self.stage.w, self.stage.h);
        self.view = view::pan_by(ViewTransform { mode: ZoomMode::Custom, scale, pan_x: self.view.pan_x, pan_y: self.view.pan_y }, dx, dy);
        cx.notify();
    }

    fn zoom_at(&mut self, position: Point<Pixels>, nat_w: f32, nat_h: f32, factor: f32, cx: &mut Context<Self>) {
        let scale = view::effective_scale(self.view, nat_w, nat_h, self.stage.w, self.stage.h);
        let ox = f32::from(position.x) - self.stage.x - self.stage.w * 0.5;
        let oy = f32::from(position.y) - self.stage.y - self.stage.h * 0.5;
        self.view = view::zoom_at(ViewTransform { scale, ..self.view }, ox, oy, scale * factor);
        cx.notify();
    }
}
