//! Confirm, restore, and undo. Disk IO stays on the history bridge's DiskGate permit.
use gpui::{Context, Window};
use crate::editor::view::history_bridge::{canonical_markdown, HistoryCommit};
use crate::page_history::HistoryError;
use crate::theme::tr;
use super::super::Workspace;
use super::{HistoryToast, PageHistoryState, UndoRestoreState};

fn map_restore_error(err: String) -> HistoryError {
    match err.as_str() {
        "READ_ONLY" => HistoryError::ReadOnly,
        "EXTERNAL_CONFLICT" | "SUPERSEDED" => HistoryError::ExternalConflict,
        _ => HistoryError::RestoreFailed(err),
    }
}

fn restore_allowed(hist: &PageHistoryState, retry: bool) -> bool {
    if hist.restoring || hist.loading || hist.content_loading || hist.is_readonly { return false; }
    let blocked_error = match &hist.error {
        None => false,
        Some(HistoryError::RestoreFailed(_)) => !retry,
        Some(_) => true,
    };
    if blocked_error { return false; }
    let Some(id) = hist.selected_id.as_ref() else { return false; };
    let Some(content) = hist.selected_content.as_ref() else { return false; };
    if content == &hist.current_content || canonical_markdown(content) == canonical_markdown(&hist.current_content) { return false; }
    hist.versions.iter().find(|v| &v.id == id).is_some_and(|v| !v.is_current && !v.same_as_current)
}

impl Workspace {
    pub fn open_restore_confirm(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let allowed = self.history.as_ref().is_some_and(|hist| !hist.confirm_open && restore_allowed(hist, false));
        let Some(hist) = self.history.as_mut() else { return };
        if !allowed { return; }
        hist.confirm_open = true;
        hist.focus_modal = true;
        hist.focus_list = false;
        let focus = hist.modal_focus.clone();
        window.focus(&focus, cx);
        cx.notify();
    }

    pub fn cancel_restore_confirm(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_mut() else { return };
        if !hist.confirm_open { return; }
        hist.confirm_open = false;
        hist.focus_modal = false;
        hist.focus_list = true;
        let focus = hist.list_focus.clone();
        window.focus(&focus, cx);
        cx.notify();
    }

    pub fn confirm_restore(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_ref() else { return };
        if hist.restoring || self.history_restore_busy || !restore_allowed(hist, true) { return; }
        let path = hist.path.clone();
        let generation = hist.editor_generation;
        let version_id = hist.selected_id.clone().unwrap_or_default();
        let pre_restore_content = hist.current_content.clone();
        let checkpoint_base = hist.current_base;
        let label = hist.versions.iter().find(|v| v.id == version_id).map(|v| v.display_time.clone())
            .unwrap_or_else(|| tr("已选版本", "Selected version").to_string());
        if !self.editor.read(cx).history_anchor_matches(&path, &pre_restore_content, checkpoint_base) {
            self.fail_restore(HistoryError::ExternalConflict, cx);
            return;
        }
        let permit = match self.editor.update(cx, |editor, _| editor.prepare_history_disk(&path, generation)) {
            Ok(permit) => permit,
            Err(err) => { self.fail_restore(map_restore_error(err), cx); return; }
        };
        if let Some(hist) = self.history.as_mut() { hist.confirm_open = false; hist.restoring = true; hist.error = None; }
        self.history_restore_busy = true;
        cx.notify();
        let _ = window;
        cx.spawn(async move |this, cx| {
            let wrote = cx.background_executor().spawn(async move { permit.restore_version(&version_id) }).await;
            let _ = this.update(cx, |this, cx| this.finish_restore(wrote, pre_restore_content, label, cx));
        }).detach();
    }

    fn finish_restore(&mut self, wrote: Result<crate::editor::view::history_bridge::HistoryWrite, String>, pre_restore_content: String, label: String, cx: &mut Context<Self>) {
        self.history_restore_busy = false;
        if let Some(hist) = self.history.as_mut() { hist.restoring = false; }
        let write = match wrote {
            Ok(write) => write,
            Err(err) => { self.fail_restore(map_restore_error(err), cx); return; }
        };
        let applied = self.editor.update(cx, |editor, cx| editor.commit_history_document(&write, cx));
        if !matches!(applied, Ok(HistoryCommit::Applied | HistoryCommit::Idle)) {
            self.notice = tr("恢复已写入磁盘，但当前编辑未能应用，请重新打开该页。", "Restore was written, but this editor could not apply it. Reopen the page.").into();
            self.fail_restore(HistoryError::ExternalConflict, cx);
            return;
        }
        self.undo_restore = Some(UndoRestoreState {
            path: write.path.clone(), pre_restore_content, expected_base: write.base, anchor_content: write.content.clone(),
        });
        self.history_toast = Some(HistoryToast {
            message: if crate::theme::is_en() { format!("Restored to \"{label}\"") } else { format!("已恢复到「{label}」") },
            can_undo: true,
        });
        self.history_return_focus = true;
        if self.history.as_ref().is_some_and(|hist| hist.path == write.path) { self.close_page_history(cx); }
        cx.notify();
    }

    fn fail_restore(&mut self, err: HistoryError, cx: &mut Context<Self>) {
        self.history_restore_busy = false;
        if let Some(hist) = self.history.as_mut() {
            hist.restoring = false;
            hist.confirm_open = false;
            if matches!(err, HistoryError::ReadOnly) { hist.is_readonly = true; }
            hist.error = Some(err);
        }
        cx.notify();
    }

    pub fn undo_restore_action(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.history_restore_busy { return; }
        let Some(undo) = self.undo_restore.clone() else { return };
        if self.editor.read(cx).current_path() != Some(undo.path.as_path()) {
            self.undo_restore = None;
            self.history_toast = None;
            cx.notify();
            return;
        }
        if !self.editor.read(cx).history_anchor_matches(&undo.path, &undo.anchor_content, undo.expected_base) {
            self.undo_restore = None;
            self.history_toast = Some(HistoryToast {
                message: tr("页面已修改，无法撤销恢复。", "Page modified; cannot undo restore.").into(),
                can_undo: false,
            });
            cx.notify();
            return;
        }
        let generation = self.editor.read(cx).interaction_epoch();
        let permit = match self.editor.update(cx, |editor, _| editor.prepare_history_disk(&undo.path, generation)) {
            Ok(permit) => permit,
            Err(_) => {
                self.notice = tr("恢复失败，当前内容未更改。", "Restore failed; current content untouched.").into();
                cx.notify();
                return;
            }
        };
        self.history_restore_busy = true;
        self.focus_root(window, cx);
        let content = undo.pre_restore_content.clone();
        cx.notify();
        cx.spawn(async move |this, cx| {
            let wrote = cx.background_executor().spawn(async move { permit.restore_content(&content) }).await;
            let _ = this.update(cx, |this, cx| this.finish_undo(undo, wrote, cx));
        }).detach();
    }

    fn finish_undo(&mut self, undo: UndoRestoreState, wrote: Result<crate::editor::view::history_bridge::HistoryWrite, String>, cx: &mut Context<Self>) {
        self.history_restore_busy = false;
        self.history_return_focus = true;
        let write = match wrote {
            Ok(write) => write,
            Err(_) => {
                self.notice = tr("恢复失败，当前内容未更改。", "Restore failed; current content untouched.").into();
                cx.notify();
                return;
            }
        };
        if write.path != undo.path {
            cx.notify();
            return;
        }
        let applied = self.editor.update(cx, |editor, cx| editor.commit_history_document(&write, cx));
        if !matches!(applied, Ok(HistoryCommit::Applied | HistoryCommit::Idle)) {
            self.notice = tr("撤销已写入磁盘，但当前编辑未能应用，请重新打开该页。", "Undo was written, but this editor could not apply it. Reopen the page.").into();
            cx.notify();
            return;
        }
        self.undo_restore = None;
        self.history_toast = Some(HistoryToast { message: tr("已撤销恢复", "Restore undone").into(), can_undo: false });
        cx.notify();
    }

    pub fn dismiss_history_toast(&mut self, cx: &mut Context<Self>) {
        self.history_toast = None;
        self.undo_restore = None;
        cx.notify();
    }
}
