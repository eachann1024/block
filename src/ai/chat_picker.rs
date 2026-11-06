//! Filesystem discovery never runs on the input/render thread.
use std::{path::PathBuf, time::{Duration, Instant}};
use gpui::{App, Context, Task};
use super::{AiChat, agent};

#[derive(Default)]
pub(in crate::editor::ai) struct PickerCache {
    root: Option<PathBuf>,
    pub pages: Vec<String>,
    pub skills: Vec<(String, String)>,
    pub loading: bool,
    refreshed: Option<Instant>,
    active: bool,
    task: Option<Task<()>>,
}

impl AiChat {
    /// Unlike agent_ctx, menu discovery needs no serialization of the open note.
    pub(in crate::editor::ai) fn picker_location(&self, cx: &App) -> (PathBuf, Option<String>) {
        let path = self.editor.as_ref().and_then(|e| e.upgrade()).and_then(|e| e.read(cx).path.clone());
        let root = path.as_ref().and_then(|p| p.parent()).map(|p| p.to_path_buf()).unwrap_or_default();
        let current = path.as_ref().and_then(|p| p.file_stem()).map(|s| s.to_string_lossy().into_owned());
        (root, current)
    }

    pub(in crate::editor::ai) fn refresh_picker(&mut self, cx: &mut Context<Self>) {
        let active = self.picker_token(cx).is_some();
        let opening = active && !self.picker_cache.active;
        self.picker_cache.active = active;
        if !active { return; }
        let (root, _) = self.picker_location(cx);
        let changed = self.picker_cache.root.as_ref() != Some(&root);
        let stale = self.picker_cache.refreshed.is_none_or(|time| time.elapsed() > Duration::from_secs(5));
        if !changed && (self.picker_cache.loading || !opening || !stale) { return; }
        if changed {
            self.picker_cache = PickerCache { root: Some(root.clone()), active, ..Default::default() };
        }
        self.picker_cache.loading = true;
        let load = cx.background_executor().spawn(async move {
            let ctx = agent::Ctx { root, current: None, current_md: String::new() };
            let pages = if ctx.root.as_os_str().is_empty() { Vec::new() } else { ctx.pages() };
            let skills = ctx.skills_from_pages(pages.iter().cloned()).into_iter().map(|(n, d, _)| (n, d)).collect();
            (pages, skills)
        });
        self.picker_cache.task = Some(cx.spawn(async move |this, cx| {
            let (pages, skills) = load.await;
            let _ = this.update(cx, |this, cx| {
                this.picker_cache.pages = pages;
                this.picker_cache.skills = skills;
                this.picker_cache.loading = false;
                this.picker_cache.refreshed = Some(Instant::now());
                cx.notify();
            });
        }));
    }
}
