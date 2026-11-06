//! Quick-note window root: three independent slots backed by Markdown files, edited by a minimal Editor.
use std::{
    fs,
    time::{Duration, Instant},
};
use gpui::{
    Context, DispatchPhase, Entity, FocusHandle, Focusable, ScrollEvent, Subscription, Task,
    Window, div, prelude::*, px,
};
use crate::{
    editor::view::Editor,
    theme::{Theme, tr},
};
use super::{HideQuickNote, SaveToNote, SlotOne, SlotThree, SlotTwo, save, store};

pub struct QuickView {
    pub(super) editor: Entity<Editor>,
    pub(super) meta: store::Meta,
    pub(super) notice: String,
    pub(super) saved_flash: bool,
    notice_task: Option<Task<()>>,
    bounds_task: Option<Task<()>>,
    saving: bool,
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
        window.observe_window_appearance(|window, cx| {
            if crate::theme::sync_system_appearance(window.appearance(), cx) {
                cx.defer(|cx| {
                    if let Some((_, ws)) = crate::search::window::main_window(cx) {
                        ws.update(cx, |w, cx| {
                            w.editor.update(cx, |e, cx| { e.reset_layouts(); cx.notify(); });
                            w.tree.update(cx, |_, cx| cx.notify());
                            cx.notify();
                        });
                    }
                    crate::quick::refresh_style(cx);
                });
            }
        }).detach();
        cx.observe_window_bounds(window, |this, window, cx| {
            let bounds = crate::window_bounds::capture_window_bounds(window, cx);
            super::remember_bounds(bounds, cx);
            this.bounds_task = Some(cx.spawn(async move |_, cx| {
                cx.background_executor().timer(Duration::from_millis(250)).await;
                cx.background_executor().spawn(async move {
                    let _ = crate::files::update_state(move |s| s.quick_window_bounds = Some(bounds));
                }).await;
            }));
        }).detach();
        let mut view = Self {
            editor,
            meta,
            notice: String::new(),
            saved_flash: false,
            notice_task: None,
            bounds_task: None,
            saving: false,
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
                self.set_notice(format!("{}: {error}", tr("无法打开速记", "Cannot open quick note")), false, cx);
            }
        }
        let focus = self.editor.focus_handle(cx);
        window.focus(&focus, cx);
        cx.notify();
    }

    pub(super) fn switch(&mut self, slot: usize, window: &mut Window, cx: &mut Context<Self>) {
        if slot != self.meta.active {
            self.notice.clear();
            self.saved_flash = false;
            self.open_slot(slot, window, cx);
        } else {
            let focus = self.editor.focus_handle(cx);
            window.focus(&focus, cx);
        }
    }

    pub(super) fn on_shift_scroll(&mut self, axis: f32, window: &mut Window, cx: &mut Context<Self>) {
        let now = Instant::now();
        let expired = self.last_wheel_event.is_some_and(|t| now.duration_since(t) > Duration::from_millis(180));
        if expired || (self.wheel_accum != 0. && axis * self.wheel_accum < 0.) {
            self.wheel_accum = 0.;
        }
        self.last_wheel_event = Some(now);
        if self.last_wheel_switch.is_some_and(|t| now.duration_since(t) < Duration::from_millis(160)) {
            return;
        }
        self.wheel_accum += axis;
        if self.wheel_accum.abs() < 16. { return; }
        // Negative scroll delta moves down (next note); positive moves up (previous note).
        let step: isize = if self.wheel_accum < 0. { 1 } else { -1 };
        self.wheel_accum = 0.;
        self.last_wheel_switch = Some(now);
        let next = (self.meta.active as isize + step).rem_euclid(store::SLOTS as isize) as usize;
        self.switch(next, window, cx);
    }

    pub(super) fn set_notice(&mut self, text: String, flash: bool, cx: &mut Context<Self>) {
        self.notice = text;
        self.saved_flash = flash;
        self.notice_task = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(Duration::from_secs(3)).await;
            let _ = this.update(cx, |this, cx| {
                this.notice.clear();
                this.saved_flash = false;
                cx.notify();
            });
        }));
        cx.notify();
    }

    /// Flush the slot, then close the window once the file is on disk (or after a short wait).
    pub fn close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let bounds = crate::window_bounds::capture_window_bounds(window, cx);
        super::remember_bounds(bounds, cx);
        cx.background_executor().spawn(async move {
            let _ = crate::files::update_state(move |s| s.quick_window_bounds = Some(bounds));
        }).detach();
        self.editor.update(cx, |editor, cx| { let _ = editor.prepare_to_quit(); editor.save(cx); });
        super::clear_window_state(cx);
        window.remove_window();
    }

    pub(super) fn save_to_note(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.saving {
            return;
        }
        let Some(folder) = save::target_folder() else {
            return self.set_notice(
                tr("请先在主窗口选择文件夹", "Choose a folder in the main window first").into(),
                false,
                cx,
            );
        };
        self.saving = true;
        self.editor.update(cx, |editor, cx| editor.save(cx));
        let slot = self.meta.active;
        let name = self.meta.names[slot].clone();
        cx.spawn_in(window, async move |this, cx| {
            for _ in 0..60 {
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
            let job = cx.background_executor().spawn(async move {
                let text = fs::read_to_string(store::slot_path(slot)).unwrap_or_default();
                if text.trim().is_empty() {
                    return Ok(None);
                }
                save::write_note(&folder, slot, &name, &text).map(|path| Some((path, text)))
            });
            let result = job.await;
            let _ = this.update_in(cx, |this, _, cx| {
                this.saving = false;
                match result {
                    Ok(None) => this.set_notice(
                        tr("速记为空，无需保存", "Quick note is empty").into(),
                        false,
                        cx,
                    ),
                    Ok(Some((path, text))) => {
                        let file = path
                            .file_name()
                            .map(|n| n.to_string_lossy().into_owned())
                            .unwrap_or_default();
                        crate::search::window::notify_saved_note(path, cx);
                        let unchanged =
                            fs::read_to_string(store::slot_path(slot)).is_ok_and(|now| now == text);
                        let clean = !this.editor.read(cx).is_dirty();
                        if unchanged && clean && slot == this.meta.active {
                            let slot_file = store::slot_path(slot);
                            this.editor.update(cx, |editor, cx| {
                                editor.close(cx);
                                let _ = fs::write(&slot_file, "");
                                editor.open(slot_file, cx);
                            });
                            this.set_notice(
                                format!("{} {file}", tr("已存入", "Saved to")),
                                true,
                                cx,
                            );
                        } else if crate::theme::is_en() {
                            this.set_notice(format!("Saved to {file}"), true, cx);
                        } else {
                            this.set_notice(format!("已存入 {file}（当前内容已修改，未清空）"), true, cx);
                        }
                    }
                    Err(error) => this.set_notice(
                        format!("{}: {error}", tr("保存失败", "Failed to save")),
                        false,
                        cx,
                    ),
                }
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
            if phase != DispatchPhase::Capture || !event.modifiers.shift { return; }
            if event.modifiers.control || event.modifiers.alt || event.modifiers.platform { return; }
            let delta = event.delta.pixel_delta(px(20.));
            let axis = if delta.y.abs() >= delta.x.abs() { f32::from(delta.y) } else { f32::from(delta.x) };
            if axis.abs() < 0.5 { return; }
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
            .on_action(cx.listener(|this, _: &SaveToNote, w, cx| this.save_to_note(w, cx)))
            .on_action(cx.listener(|this, _: &HideQuickNote, w, cx| this.close(w, cx)))
            .on_action(cx.listener(|this, _: &super::ToggleQuickNote, w, cx| if super::mark_quick_triggered() { this.close(w, cx); }))
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
