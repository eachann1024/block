//! Filesystem discovery never runs on the input/render thread.
use std::{path::PathBuf, time::{Duration, Instant}};
use gpui::{App, Context, Task};
use super::{AiChat, agent};

#[derive(Default)]
pub(in crate::editor::ai) struct PickerCache {
    root: Option<PathBuf>,
    pub folder: String,
    pub folders: Vec<String>,
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
        if !active { self.picker_cache.folder.clear(); return; }
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
            let mut pages = Vec::new();
            let mut folders = Vec::new();
            if !ctx.root.as_os_str().is_empty() {
                collect(&ctx.root, &ctx.root, &mut pages, &mut folders);
            }
            pages.sort(); folders.sort();
            let skills = ctx.skills_from_pages(pages.iter().cloned()).into_iter().map(|(n, d, _)| (n, d)).collect();
            (pages, folders, skills)
        });
        self.picker_cache.task = Some(cx.spawn(async move |this, cx| {
            let (pages, folders, skills) = load.await;
            let _ = this.update(cx, |this, cx| {
                this.picker_cache.pages = pages;
                this.picker_cache.folders = folders;
                this.picker_cache.skills = skills;
                this.picker_cache.loading = false;
                this.picker_cache.refreshed = Some(Instant::now());
                cx.notify();
            });
        }));
    }
}

// Match the sidebar's directory exclusions without reading file contents.
fn collect(dir: &std::path::Path, root: &std::path::Path, pages: &mut Vec<String>, folders: &mut Vec<String>) {
    let Ok(entries) = std::fs::read_dir(dir) else { return };
    for entry in entries.flatten() {
        let Ok(kind) = entry.file_type() else { continue };
        let path = entry.path();
        let name = entry.file_name().to_string_lossy().into_owned();
        if kind.is_symlink() || crate::files::scan::ignored(&name) { continue; }
        if kind.is_dir() {
            if crate::files::scan::ignored_dir(&name) { continue; }
            if let Ok(relative) = path.strip_prefix(root) { folders.push(relative.to_string_lossy().into_owned()); }
            collect(&path, root, pages, folders);
        } else if kind.is_file() && path.extension().is_some_and(|ext| ext == "md") {
            if let Ok(relative) = path.with_extension("").strip_prefix(root) { pages.push(relative.to_string_lossy().into_owned()); }
        }
    }
}
