//! Quick-note window root: five slots backed by Markdown files, edited by a minimal Editor.
use std::{fs, time::Duration};
use gpui::{Context, Entity, FocusHandle, Focusable, Subscription, Task, Window, div, prelude::*, px};
use gpui_component::input::{InputEvent, InputState};
use crate::editor::view::Editor;
use crate::theme::{Theme, tr};
use super::{HideQuickNote, SaveToNote, SlotFive, SlotFour, SlotOne, SlotThree, SlotTwo, ToggleHelp, save, store};

pub struct QuickView {
    pub(super) editor: Entity<Editor>,
    pub(super) meta: store::Meta,
    pub(super) help: bool,
    pub(super) renaming: Option<(Entity<InputState>, Vec<Subscription>)>,
    pub(super) notice: String,
    notice_task: Option<Task<()>>,
    saving: bool,
    focus: FocusHandle,
    _observe: Subscription,
}

impl QuickView {
    pub fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let meta = store::load_meta();
        let editor = cx.new(|cx| { let mut editor = Editor::new(cx); editor.set_minimal(true); editor });
        let observe = cx.observe(&editor, |_, _, cx| cx.notify());
        let mut view = Self { editor, meta, help: false, renaming: None, notice: String::new(), notice_task: None,
            saving: false, focus: cx.focus_handle(), _observe: observe };
        view.open_slot(view.meta.active, window, cx);
        view
    }

    pub(super) fn open_slot(&mut self, slot: usize, window: &mut Window, cx: &mut Context<Self>) {
        self.meta.active = slot;
        let _ = store::save_meta(&self.meta);
        match store::ensure_slot(slot) {
            Ok(path) => self.editor.update(cx, |editor, cx| editor.open(path, cx)),
            Err(error) => self.set_notice(format!("{}: {error}", tr("无法打开速记", "Cannot open quick note")), cx),
        }
        let focus = self.editor.focus_handle(cx);
        window.focus(&focus, cx);
        cx.notify();
    }

    pub(super) fn switch(&mut self, slot: usize, window: &mut Window, cx: &mut Context<Self>) {
        self.commit_rename(window, cx);
        self.help = false;
        if slot != self.meta.active { self.notice.clear(); self.open_slot(slot, window, cx); }
        else {
            let focus = self.editor.focus_handle(cx);
            window.focus(&focus, cx);
        }
    }

    pub(super) fn set_notice(&mut self, text: String, cx: &mut Context<Self>) {
        self.notice = text;
        self.notice_task = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(Duration::from_secs(4)).await;
            let _ = this.update(cx, |this, cx| { this.notice.clear(); cx.notify(); });
        }));
        cx.notify();
    }

    pub(super) fn begin_rename(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let name = store::display_name(self.meta.active, &self.meta.names[self.meta.active]);
        let input = cx.new(|cx| InputState::new(window, cx).default_value(name));
        let subs = vec![cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| {
            if matches!(event, InputEvent::PressEnter { .. } | InputEvent::Blur) { this.commit_rename(window, cx); }
        })];
        input.update(cx, |input, cx| input.focus(window, cx));
        self.renaming = Some((input, subs));
        cx.notify();
    }

    pub(super) fn commit_rename(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some((input, _)) = self.renaming.take() else { return };
        let slot = self.meta.active;
        self.meta.names[slot] = store::clean_name(slot, input.read(cx).value().as_ref());
        let _ = store::save_meta(&self.meta);
        let focus = self.editor.focus_handle(cx);
        window.focus(&focus, cx);
        cx.notify();
    }

    /// Flush the slot, then close the window once the file is on disk (or after a short wait).
    pub fn close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.editor.update(cx, |editor, cx| editor.save(cx));
        cx.spawn_in(window, async move |this, cx| {
            for _ in 0..40 {
                let busy = this.update(cx, |this, cx| { let e = this.editor.read(cx); e.is_dirty() || e.is_busy() }).unwrap_or(false);
                if !busy { break; }
                cx.background_executor().timer(Duration::from_millis(50)).await;
            }
            let _ = this.update_in(cx, |_, window, _| window.remove_window());
        }).detach();
    }

    pub(super) fn save_to_note(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.saving { return; }
        let Some(folder) = save::target_folder() else { return self.set_notice(tr("请先在主窗口选择文件夹", "Choose a folder in the main window first").into(), cx) };
        self.saving = true;
        self.editor.update(cx, |editor, cx| editor.save(cx));
        let slot = self.meta.active;
        let name = self.meta.names[slot].clone();
        cx.spawn_in(window, async move |this, cx| {
            for _ in 0..60 {
                let busy = this.update(cx, |this, cx| { let e = this.editor.read(cx); e.is_dirty() || e.is_busy() }).unwrap_or(false);
                if !busy { break; }
                cx.background_executor().timer(Duration::from_millis(50)).await;
            }
            let job = cx.background_executor().spawn(async move {
                let text = fs::read_to_string(store::slot_path(slot)).unwrap_or_default();
                if text.trim().is_empty() { return Ok(None); }
                save::write_note(&folder, slot, &name, &text).map(|path| Some((path, text)))
            });
            let result = job.await;
            let _ = this.update_in(cx, |this, _, cx| {
                this.saving = false;
                match result {
                    Ok(None) => this.set_notice(tr("速记为空，无需保存", "Quick note is empty").into(), cx),
                    Ok(Some((path, text))) => {
                        let file = path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
                        super::notify_saved_note(path, cx);
                        // Like the original app: clear the slot only if it was not edited meanwhile.
                        let unchanged = fs::read_to_string(store::slot_path(slot)).is_ok_and(|now| now == text);
                        let clean = this.editor.read(cx).is_dirty() == false;
                        if unchanged && clean && slot == this.meta.active {
                            let slot_file = store::slot_path(slot);
                            this.editor.update(cx, |editor, cx| {
                                editor.close(cx);
                                let _ = fs::write(&slot_file, "");
                                editor.open(slot_file, cx);
                            });
                            this.set_notice(format!("{} {file}", tr("已保存到", "Saved to")), cx);
                        } else if crate::theme::is_en() {
                            this.set_notice(format!("Saved to {file} (slot kept)"), cx);
                        } else {
                            this.set_notice(format!("已保存到 {file}（速记已保留）"), cx);
                        }
                    }
                    Err(error) => this.set_notice(format!("{}: {error}", tr("保存失败", "Failed to save")), cx),
                }
            });
        }).detach();
    }

    fn footer_status(&self, cx: &Context<Self>) -> String {
        if !self.notice.is_empty() { return self.notice.clone(); }
        let editor = self.editor.read(cx);
        match editor.status_text() {
            "" | "Saved" => tr("已保存", "Saved").into(),
            "Saving…" => tr("保存中…", "Saving…").into(),
            "Unsaved changes" => tr("未保存", "Unsaved").into(),
            "Opening document…" => tr("正在打开…", "Opening…").into(),
            other => other.to_owned(),
        }
    }
}

impl Focusable for QuickView { fn focus_handle(&self, _: &gpui::App) -> FocusHandle { self.focus.clone() } }

impl Render for QuickView {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let status = self.footer_status(cx);
        div().id("quick-note").key_context("QuickNote").track_focus(&self.focus).size_full().relative().flex().flex_col()
            .overflow_hidden().bg(theme.paper).text_color(theme.text).text_size(px(14.))
            .on_action(cx.listener(|this, _: &SlotOne, w, cx| this.switch(0, w, cx)))
            .on_action(cx.listener(|this, _: &SlotTwo, w, cx| this.switch(1, w, cx)))
            .on_action(cx.listener(|this, _: &SlotThree, w, cx| this.switch(2, w, cx)))
            .on_action(cx.listener(|this, _: &SlotFour, w, cx| this.switch(3, w, cx)))
            .on_action(cx.listener(|this, _: &SlotFive, w, cx| this.switch(4, w, cx)))
            .on_action(cx.listener(|this, _: &SaveToNote, w, cx| this.save_to_note(w, cx)))
            .on_action(cx.listener(|this, _: &ToggleHelp, _, cx| { this.help = !this.help; cx.notify(); }))
            .on_action(cx.listener(|this, _: &HideQuickNote, w, cx| {
                if this.renaming.is_some() { this.commit_rename(w, cx); }
                else if this.help { this.help = false; cx.notify(); }
                else { this.close(w, cx); }
            }))
            // Esc is an editor action (block selection) when the editor has focus; the window hides instead.
            .capture_action(cx.listener(|this, _: &crate::editor::view::Cancel, w, cx| {
                if this.help { this.help = false; cx.notify(); } else { this.close(w, cx); }
                cx.stop_propagation();
            }))
            .child(self.header(window, cx))
            .child(div().flex_1().min_h_0().child(self.editor.clone()))
            .child(self.footer(status))
            .when(self.help, |root| root.child(self.help_popover()))
    }
}
