//! Version list, selection, diff cache, retry, and external re-compare.
use gpui::Context;
use crate::editor::view::history_bridge::fingerprint_of;
use crate::page_history::{self, HistoryError, HistoryVersion};
use super::super::Workspace;
use super::PageHistoryState;

pub(super) fn refresh_preview_key(hist: &mut PageHistoryState) {
    let id = hist.selected_id.as_deref().unwrap_or("-");
    let mode = match hist.mode { page_history::PreviewMode::Clean => "c", page_history::PreviewMode::Diff => "d" };
    hist.preview_key = format!("{}:{id}:{}:{mode}", hist.content_token, hist.path.display());
}

impl Workspace {
    pub(super) fn spawn_history_list(&mut self, prefer: Option<String>, cx: &mut Context<Self>) {
        if self.history.is_none() { return; }
        let token = self.next_history_token();
        let Some(hist) = self.history.as_mut() else { return };
        hist.list_token = token;
        hist.loading = true;
        let path = hist.path.clone();
        let content = hist.current_content.clone();
        let prefer = prefer.or_else(|| hist.selected_id.clone());
        cx.spawn(async move |this, cx| {
            let loaded = cx.background_executor().spawn(async move {
                let readonly = page_history::is_file_readonly(&path);
                let versions = page_history::load_versions(&path, &content);
                (readonly, versions)
            }).await;
            let _ = this.update(cx, |this, cx| this.finish_history_list(token, prefer, loaded, cx));
        }).detach();
    }

    fn finish_history_list(&mut self, token: u64, prefer: Option<String>, loaded: (bool, Result<Vec<HistoryVersion>, String>), cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_mut() else { return };
        if hist.list_token != token { return; }
        hist.loading = false;
        let (readonly, versions) = loaded;
        hist.is_readonly = readonly;
        match versions {
            Ok(list) => {
                if !matches!(hist.error, Some(HistoryError::ExternalConflict) | Some(HistoryError::ReadOnly)) {
                    hist.error = None;
                }
                hist.versions = list;
                let pick = prefer.filter(|id| hist.versions.iter().any(|v| &v.id == id)).unwrap_or_else(|| default_version(&hist.versions));
                let confirm = hist.confirm_open;
                cx.notify();
                if !confirm { self.select_history_version(&pick, cx); }
            }
            Err(err) => {
                hist.error = Some(HistoryError::LoadFailed(err));
                hist.selected_content = None;
                hist.diff_blocks = None;
                cx.notify();
            }
        }
    }

    pub fn retry_history_load(&mut self, cx: &mut Context<Self>) {
        if self.history_restore_busy || self.history.as_ref().is_none_or(|hist| hist.restoring || hist.confirm_open) { return; }
        if let Some(hist) = self.history.as_mut() {
            if matches!(hist.error, Some(HistoryError::ExternalConflict)) { self.recompare_history(cx); return; }
            hist.error = None;
            hist.selected_content = None;
            hist.diff_blocks = None;
        }
        self.spawn_history_list(None, cx);
    }

    pub fn select_history_version(&mut self, id: &str, cx: &mut Context<Self>) {
        let blocked = self.history.as_ref().is_none_or(|hist| {
            hist.restoring || hist.confirm_open || matches!(hist.error, Some(HistoryError::ExternalConflict))
        });
        if blocked { return; }
        let token = self.next_history_token();
        let Some(hist) = self.history.as_mut() else { return };
        let path = hist.path.clone();
        let checkpoint = hist.current_content.clone();
        hist.selected_id = Some(id.to_string());
        hist.selected_content = None;
        hist.diff_blocks = None;
        hist.content_loading = true;
        hist.content_token = token;
        if !matches!(hist.error, Some(HistoryError::ReadOnly)) { hist.error = None; }
        refresh_preview_key(hist);
        if id == "v-current" {
            self.finish_version_content(token, id.to_string(), Ok(checkpoint), cx);
            return;
        }
        let vid = id.to_string();
        let vid_load = vid.clone();
        cx.spawn(async move |this, cx| {
            let loaded = cx.background_executor().spawn(async move { page_history::load_version_content(&path, &vid_load) }).await;
            let _ = this.update(cx, |this, cx| this.finish_version_content(token, vid, loaded, cx));
        }).detach();
        cx.notify();
    }

    fn finish_version_content(&mut self, token: u64, id: String, loaded: Result<String, String>, cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_mut() else { return };
        if hist.content_token != token || hist.selected_id.as_deref() != Some(id.as_str()) { return; }
        hist.content_loading = false;
        match loaded {
            Ok(content) => {
                hist.selected_content = Some(content);
                if !matches!(hist.error, Some(HistoryError::ReadOnly)) { hist.error = None; }
                refresh_preview_key(hist);
                cx.notify();
                self.schedule_history_diff(cx);
            }
            Err(err) => {
                hist.selected_content = None;
                hist.diff_blocks = None;
                if hist.error.is_none() { hist.error = Some(HistoryError::LoadFailed(err)); }
                cx.notify();
            }
        }
    }

    pub(super) fn schedule_history_diff(&mut self, cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_ref() else { return };
        let Some(selected) = hist.selected_content.clone() else { return };
        let current = hist.current_content.clone();
        let token = hist.content_token;
        let id = hist.selected_id.clone();
        cx.spawn(async move |this, cx| {
            let blocks = cx.background_executor().spawn(async move { page_history::diff::compute_block_diff(&selected, &current) }).await;
            let _ = this.update(cx, |this, cx| {
                let Some(hist) = this.history.as_mut() else { return };
                if hist.content_token != token || hist.selected_id != id { return; }
                hist.diff_blocks = Some(blocks);
                cx.notify();
            });
        }).detach();
    }

    pub(super) fn align_history_disk(&mut self, cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_ref() else { return };
        let path = hist.path.clone();
        let generation = hist.editor_generation;
        let token = self.next_history_token();
        if let Some(hist) = self.history.as_mut() { hist.list_token = token; }
        cx.spawn(async move |this, cx| {
            let read = cx.background_executor().spawn(async move { std::fs::read(&path) }).await;
            let _ = this.update(cx, |this, cx| this.finish_align_disk(token, generation, read, cx));
        }).detach();
    }

    fn finish_align_disk(&mut self, token: u64, generation: u64, read: Result<Vec<u8>, std::io::Error>, cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_ref() else { return };
        if hist.list_token != token { return; }
        if hist.confirm_open || hist.restoring {
            if let Some(hist) = self.history.as_mut() { hist.loading = false; }
            cx.notify();
            return;
        }
        let path = hist.path.clone();
        let bytes = match read {
            Ok(bytes) => bytes,
            Err(err) => {
                if let Some(hist) = self.history.as_mut() { hist.loading = false; hist.error = Some(HistoryError::LoadFailed(err.to_string())); }
                cx.notify();
                return;
            }
        };
        let Ok(text) = String::from_utf8(bytes.clone()) else {
            if let Some(hist) = self.history.as_mut() { hist.loading = false; hist.error = Some(HistoryError::LoadFailed("UTF8".into())); }
            cx.notify();
            return;
        };
        let fp = fingerprint_of(&bytes);
        if self.editor.read(cx).history_anchor_matches(&path, &text, fp) {
            if let Some(hist) = self.history.as_mut() { hist.current_content = text; hist.current_base = fp; }
            self.spawn_history_list(None, cx);
            return;
        }
        if self.editor.read(cx).is_path_dirty(&path) {
            if let Some(hist) = self.history.as_mut() { hist.loading = false; hist.error = Some(HistoryError::ExternalConflict); }
            self.notice = crate::theme::tr("页面已修改，无法重新对比。", "Page was edited; cannot re-diff.").into();
            cx.notify();
            return;
        }
        match self.editor.update(cx, |editor, cx| editor.adopt_saved_baseline(&path, &text, generation, cx)) {
            Ok(base) => {
                if let Some(hist) = self.history.as_mut() {
                    hist.current_content = text;
                    hist.current_base = base;
                    hist.error = None;
                }
                self.spawn_history_list(None, cx);
            }
            Err(_) => {
                if let Some(hist) = self.history.as_mut() { hist.loading = false; hist.error = Some(HistoryError::ExternalConflict); }
                cx.notify();
            }
        }
    }

    pub fn recompare_history(&mut self, cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_ref() else { return };
        if hist.restoring || hist.confirm_open || self.history_restore_busy { return; }
        let path = hist.path.clone();
        let generation = hist.editor_generation;
        let token = self.next_history_token();
        if let Some(hist) = self.history.as_mut() {
            hist.list_token = token;
            hist.loading = true;
            hist.selected_content = None;
            hist.diff_blocks = None;
            hist.content_loading = false;
            refresh_preview_key(hist);
        }
        cx.notify();
        cx.spawn(async move |this, cx| {
            let read = cx.background_executor().spawn(async move { std::fs::read_to_string(&path) }).await;
            let _ = this.update(cx, |this, cx| this.finish_recompare(token, generation, read, cx));
        }).detach();
    }

    fn finish_recompare(&mut self, token: u64, generation: u64, read: Result<String, std::io::Error>, cx: &mut Context<Self>) {
        let Some(hist) = self.history.as_ref() else { return };
        if hist.list_token != token { return; }
        if hist.restoring || hist.confirm_open {
            if let Some(hist) = self.history.as_mut() { hist.loading = false; }
            cx.notify();
            return;
        }
        let path = hist.path.clone();
        let checkpoint = hist.current_content.clone();
        let checkpoint_base = hist.current_base;
        let disk = match read {
            Ok(text) => text,
            Err(_) => {
                if let Some(hist) = self.history.as_mut() { hist.loading = false; hist.error = Some(HistoryError::ExternalConflict); }
                cx.notify();
                return;
            }
        };
        let unchanged = self.editor.read(cx).history_anchor_matches(&path, &checkpoint, checkpoint_base);
        if !unchanged {
            if let Some(hist) = self.history.as_mut() { hist.loading = false; }
            self.notice = crate::theme::tr("页面已修改，无法重新对比。", "Page was edited; cannot re-diff.").into();
            cx.notify();
            return;
        }
        match self.editor.update(cx, |editor, cx| editor.adopt_saved_baseline(&path, &disk, generation, cx)) {
            Ok(base) => {
                if let Some(hist) = self.history.as_mut() {
                    hist.current_content = disk;
                    hist.current_base = base;
                    hist.editor_generation = generation;
                    hist.error = None;
                    hist.loading = false;
                }
                self.spawn_history_list(None, cx);
            }
            Err(_) => {
                if let Some(hist) = self.history.as_mut() { hist.loading = false; hist.error = Some(HistoryError::ExternalConflict); }
                self.notice = crate::theme::tr("页面已修改，无法重新对比。", "Page was edited; cannot re-diff.").into();
                cx.notify();
            }
        }
    }
}

fn default_version(versions: &[HistoryVersion]) -> String {
    if versions.len() > 1 { versions[1].id.clone() }
    else { versions.first().map(|v| v.id.clone()).unwrap_or_else(|| "v-current".into()) }
}
