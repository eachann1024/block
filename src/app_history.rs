//! Workspace integration for Page History: checkpoint, focus, and request tokens.
use std::path::PathBuf;
use std::time::Duration;
use gpui::{Context, FocusHandle, Window};
use crate::editor::view::history_bridge::{HistoryCapture, HistoryCapturePoll};
use crate::page_history::{DiffBlock, HistoryError, HistoryVersion, PreviewMode};
use super::Workspace;

#[path = "app_history_load.rs"]
mod load;
#[path = "app_history_restore.rs"]
mod restore;

pub struct PageHistoryState {
    pub path: PathBuf,
    pub versions: Vec<HistoryVersion>,
    pub selected_id: Option<String>,
    pub selected_content: Option<String>,
    pub current_content: String,
    pub current_base: u64,
    pub mode: PreviewMode,
    pub loading: bool,
    pub content_loading: bool,
    pub error: Option<HistoryError>,
    pub confirm_open: bool,
    pub restoring: bool,
    pub list_token: u64,
    pub content_token: u64,
    pub editor_generation: u64,
    pub is_readonly: bool,
    pub prev_sidebar_collapsed: bool,
    pub diff_blocks: Option<Vec<DiffBlock>>,
    pub preview_key: String,
    pub list_focus: FocusHandle,
    pub modal_focus: FocusHandle,
    pub focus_list: bool,
    pub focus_modal: bool,
}

#[derive(Clone)]
pub struct UndoRestoreState {
    pub path: PathBuf,
    pub pre_restore_content: String,
    pub expected_base: u64,
    pub anchor_content: String,
}

#[derive(Clone)]
pub struct HistoryToast {
    pub message: String,
    pub can_undo: bool,
}

impl Workspace {
    pub fn is_history_open(&self) -> bool { self.history.is_some() }

    fn next_history_token(&mut self) -> u64 {
        self.history_token = self.history_token.wrapping_add(1);
        self.history_token
    }

    pub fn open_page_history(&mut self, _window: &mut Window, cx: &mut Context<Self>) {
        if self.busy() || self.history.is_some() || self.history_opening || self.history_restore_busy { return; }
        let Some(capture) = self.editor.update(cx, |editor, cx| editor.begin_history_capture(cx)) else { return; };
        if !crate::files::ops::exists(&capture.path) { return; }
        match self.editor.read(cx).poll_history_capture(&capture) {
            HistoryCapturePoll::Ready { content, base } => self.install_history(capture, content, base, cx),
            HistoryCapturePoll::Failed(message) => { self.notice = message; cx.notify(); }
            HistoryCapturePoll::Superseded => {}
            HistoryCapturePoll::Pending => {
                self.history_opening = true;
                let token = self.next_history_token();
                self.spawn_history_open(capture, token, cx);
            }
        }
    }

    fn spawn_history_open(&mut self, capture: HistoryCapture, token: u64, cx: &mut Context<Self>) {
        cx.spawn(async move |this, cx| {
            loop {
                let poll = this.update(cx, |this, cx| {
                    if !this.history_opening || this.history_token != token { return HistoryCapturePoll::Superseded; }
                    this.editor.read(cx).poll_history_capture(&capture)
                });
                let Ok(poll) = poll else { return };
                match poll {
                    HistoryCapturePoll::Pending => {
                        let _ = this.update(cx, |this, cx| {
                            this.editor.update(cx, |editor, cx| editor.pump_history_save(cx));
                        });
                        cx.background_executor().timer(Duration::from_millis(40)).await;
                    }
                    HistoryCapturePoll::Ready { content, base } => {
                        let _ = this.update(cx, |this, cx| {
                            if !this.history_opening || this.history_token != token { return; }
                            this.history_opening = false;
                            this.install_history(capture.clone(), content, base, cx);
                        });
                        return;
                    }
                    HistoryCapturePoll::Failed(message) => {
                        let _ = this.update(cx, |this, cx| {
                            if this.history_token != token { return; }
                            this.history_opening = false;
                            this.notice = message;
                            cx.notify();
                        });
                        return;
                    }
                    HistoryCapturePoll::Superseded => {
                        let _ = this.update(cx, |this, _| { if this.history_token == token { this.history_opening = false; } });
                        return;
                    }
                }
            }
        }).detach();
    }

    fn install_history(&mut self, capture: HistoryCapture, _content: String, _base: u64, cx: &mut Context<Self>) {
        let HistoryCapturePoll::Ready { content, base } = self.editor.read(cx).poll_history_capture(&capture) else { return; };
        let prev_sidebar_collapsed = self.sidebar_collapsed;
        self.sidebar_collapsed = true;
        let list_focus = cx.focus_handle();
        let modal_focus = cx.focus_handle();
        self.history = Some(PageHistoryState {
            path: capture.path, versions: Vec::new(), selected_id: None, selected_content: None,
            current_content: content, current_base: base, mode: PreviewMode::Clean, loading: true,
            content_loading: false, error: None, confirm_open: false, restoring: false,
            list_token: 0, content_token: 0, editor_generation: capture.generation, is_readonly: false,
            prev_sidebar_collapsed, diff_blocks: None, preview_key: String::new(),
            list_focus, modal_focus, focus_list: true, focus_modal: false,
        });
        self.align_history_disk(cx);
        cx.notify();
    }

    pub(super) fn prepare_close_tab(&mut self, path: &PathBuf, cx: &mut Context<Self>) -> bool {
        if self.history_restore_busy { return false; }
        let return_focus = self.history.as_ref().is_some_and(|hist| hist.path == *path);
        if return_focus || self.history_opening { self.close_page_history(cx); }
        if return_focus { self.history_return_focus = true; }
        if self.undo_restore.as_ref().is_some_and(|undo| undo.path == *path) {
            self.undo_restore = None;
            self.history_toast = None;
        }
        true
    }

    pub fn close_page_history(&mut self, cx: &mut Context<Self>) {
        if self.history_restore_busy || self.history.as_ref().is_some_and(|hist| hist.restoring) { return; }
        let opening = std::mem::replace(&mut self.history_opening, false);
        if opening { self.history_token = self.history_token.wrapping_add(1); }
        if let Some(hist) = self.history.take() {
            self.history_token = self.history_token.wrapping_add(1);
            self.sidebar_collapsed = hist.prev_sidebar_collapsed;
            cx.notify();
        } else if opening {
            cx.notify();
        }
    }

    pub fn close_page_history_focused(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let was = self.history.is_some();
        self.close_page_history(cx);
        if was && self.history.is_none() { self.restore_focus(window, cx); }
    }

    pub fn history_escape(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        if self.history.is_none() { return false; }
        if self.history.as_ref().is_some_and(|hist| hist.restoring) { return true; }
        if self.history.as_ref().is_some_and(|hist| hist.confirm_open) {
            self.cancel_restore_confirm(window, cx);
            return true;
        }
        self.close_page_history_focused(window, cx);
        true
    }

    pub(super) fn sync_history_focus(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.history_return_focus && self.history.is_none() {
            self.history_return_focus = false;
            self.restore_focus(window, cx);
            return;
        }
        let target = self.history.as_ref().and_then(|hist| {
            if hist.focus_modal && hist.confirm_open { Some(hist.modal_focus.clone()) }
            else if hist.focus_list { Some(hist.list_focus.clone()) }
            else { None }
        });
        let Some(target) = target else { return };
        if let Some(hist) = self.history.as_mut() { hist.focus_list = false; hist.focus_modal = false; }
        window.focus(&target, cx);
    }

    pub fn toggle_history_mode(&mut self, mode: PreviewMode, cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_mut() else { return };
        if hist.restoring { return; }
        hist.mode = mode;
        load::refresh_preview_key(hist);
        cx.notify();
    }

    pub fn navigate_history(&mut self, step: isize, cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_ref() else { return };
        if hist.confirm_open || hist.restoring || hist.versions.is_empty() { return; }
        let cur = hist.selected_id.as_ref().and_then(|id| hist.versions.iter().position(|v| &v.id == id)).unwrap_or(0);
        let next = if step < 0 { cur.saturating_sub(1) } else { (cur + 1).min(hist.versions.len() - 1) };
        let id = hist.versions[next].id.clone();
        self.select_history_version(&id, cx);
    }
}
