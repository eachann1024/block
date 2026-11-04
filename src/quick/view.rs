//! Quick-note window root: three independent slots backed by Markdown files, edited by a minimal Editor.
use std::time::{Duration, Instant};
use gpui::{
    Context, DispatchPhase, Entity, FocusHandle, Focusable, ScrollEvent, Subscription, Task,
    Window, div, prelude::*, px,
};
use crate::{
    editor::view::Editor,
    theme::{Theme, tr},
};
use super::{HideQuickNote, SlotOne, SlotThree, SlotTwo, store};

pub struct QuickView {
    pub(super) editor: Entity<Editor>,
    pub(super) meta: store::Meta,
    pub(super) notice: String,
    notice_task: Option<Task<()>>,
    wheel_accum: f32,
    last_wheel_event: Option<Instant>,
    last_wheel_switch: Option<Instant>,
    focus: FocusHandle,
    _observe: Subscription,
}

impl QuickView {
    pub fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let meta = store::load_meta();
        let editor = cx.new(|cx| {
            let mut editor = Editor::new(cx);
            editor.set_minimal(true);
            editor
        });
        let observe = cx.observe(&editor, |_, _, cx| cx.notify());
        let mut view = Self {
            editor,
            meta,
            notice: String::new(),
            notice_task: None,
            wheel_accum: 0.,
            last_wheel_event: None,
            last_wheel_switch: None,
            focus: cx.focus_handle(),
            _observe: observe,
        };
        view.open_slot(view.meta.active, window, cx);
        view
    }

    pub(super) fn open_slot(&mut self, slot: usize, window: &mut Window, cx: &mut Context<Self>) {
        self.meta.active = slot;
        let _ = store::save_meta(&self.meta);
        match store::ensure_slot(slot) {
            Ok(path) => self.editor.update(cx, |editor, cx| editor.open(path, cx)),
            Err(error) => {
                self.set_notice(format!("{}: {error}", tr("无法打开速记", "Cannot open quick note")), cx);
            }
        }
        let focus = self.editor.focus_handle(cx);
        window.focus(&focus, cx);
        cx.notify();
    }

    pub(super) fn switch(&mut self, slot: usize, window: &mut Window, cx: &mut Context<Self>) {
        if slot != self.meta.active {
            self.notice.clear();
            self.open_slot(slot, window, cx);
        } else {
            let focus = self.editor.focus_handle(cx);
            window.focus(&focus, cx);
        }
    }

    pub(super) fn on_shift_scroll(&mut self, axis: f32, window: &mut Window, cx: &mut Context<Self>) {
        let now = Instant::now();
        let expired = self
            .last_wheel_event
            .is_some_and(|t| now.duration_since(t) > Duration::from_millis(180));
        if expired || (self.wheel_accum != 0. && axis * self.wheel_accum < 0.) {
            self.wheel_accum = 0.;
        }
        self.last_wheel_event = Some(now);
        if self
            .last_wheel_switch
            .is_some_and(|t| now.duration_since(t) < Duration::from_millis(160))
        {
            return;
        }
        self.wheel_accum += axis;
        if self.wheel_accum.abs() < 16. {
            return;
        }
        // Negative scroll delta moves down (next note); positive moves up (previous note).
        let step: isize = if self.wheel_accum < 0. { 1 } else { -1 };
        self.wheel_accum = 0.;
        self.last_wheel_switch = Some(now);
        let next = (self.meta.active as isize + step).rem_euclid(store::SLOTS as isize) as usize;
        self.switch(next, window, cx);
    }

    pub(super) fn set_notice(&mut self, text: String, cx: &mut Context<Self>) {
        self.notice = text;
        self.notice_task = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(Duration::from_secs(3)).await;
            let _ = this.update(cx, |this, cx| {
                this.notice.clear();
                cx.notify();
            });
        }));
        cx.notify();
    }

    /// Flush the slot, then close the window once the file is on disk (or after a short wait).
    pub fn close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.editor.update(cx, |editor, cx| editor.save(cx));
        cx.spawn_in(window, async move |this, cx| {
            for _ in 0..40 {
                let busy = this
                    .update(cx, |this, cx| {
                        let e = this.editor.read(cx);
                        e.is_dirty() || e.is_busy()
                    })
                    .unwrap_or(false);
                if !busy {
                    break;
                }
                cx.background_executor().timer(Duration::from_millis(50)).await;
            }
            let _ = this.update_in(cx, |_, window, cx| {
                super::clear_window_state(cx);
                window.remove_window();
            });
        })
        .detach();
    }
}

impl Focusable for QuickView {
    fn focus_handle(&self, _: &gpui::App) -> FocusHandle {
        self.focus.clone()
    }
}

impl Render for QuickView {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let entity = cx.entity().clone();
        window.on_mouse_event(move |event: &ScrollEvent, phase, window, cx| {
            if phase != DispatchPhase::Capture || !event.modifiers.shift {
                return;
            }
            if event.modifiers.control || event.modifiers.alt || event.modifiers.platform {
                return;
            }
            let delta = event.delta.pixel_delta(px(20.));
            let axis = if delta.y.abs() >= delta.x.abs() {
                f32::from(delta.y)
            } else {
                f32::from(delta.x)
            };
            if axis.abs() < 0.5 {
                return;
            }
            cx.stop_propagation();
            entity.update(cx, |this, cx| this.on_shift_scroll(axis, window, cx));
        });

        div()
            .id("quick-note")
            .key_context("QuickNote")
            .track_focus(&self.focus)
            .size_full()
            .relative()
            .flex()
            .flex_col()
            .overflow_hidden()
            .bg(theme.paper)
            .text_color(theme.text)
            .text_size(px(14.))
            .on_action(cx.listener(|this, _: &SlotOne, w, cx| this.switch(0, w, cx)))
            .on_action(cx.listener(|this, _: &SlotTwo, w, cx| this.switch(1, w, cx)))
            .on_action(cx.listener(|this, _: &SlotThree, w, cx| this.switch(2, w, cx)))
            .on_action(cx.listener(|this, _: &HideQuickNote, w, cx| this.close(w, cx)))
            // Esc is an editor action (block selection) when the editor has focus; the window hides instead.
            .capture_action(cx.listener(|this, _: &crate::editor::view::Cancel, w, cx| {
                this.close(w, cx);
                cx.stop_propagation();
            }))
            .child(self.header(window, cx))
            .child(div().flex_1().min_h_0().child(self.editor.clone()))
            .when(!self.notice.is_empty(), |root| root.child(self.notice_pill()))
    }
}
