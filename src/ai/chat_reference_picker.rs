//! Folder navigation over the background reference index.
use gpui::App;
use super::{chat::{AiChat, Pick}, chat_composer::PICKER_SCROLL};
use crate::theme::tr;

impl AiChat {
    pub(super) fn reference_items(&self, query: &str, cx: &App) -> Vec<(Pick, String, String)> {
        let q = query.to_lowercase();
        let folder = &self.picker_cache.folder;
        let (_, current) = self.picker_location(cx);
        let mut items = Vec::new();
        if q.is_empty() && !folder.is_empty() {
            let parent = std::path::Path::new(folder).parent().unwrap_or(std::path::Path::new("")).to_string_lossy().into_owned();
            items.push((Pick::Folder(parent), tr("返回上级", "Parent folder").into(), String::new()));
        }
        let in_folder = |path: &str| std::path::Path::new(path).parent().unwrap_or(std::path::Path::new("")) == std::path::Path::new(folder);
        for path in &self.picker_cache.folders {
            if (q.is_empty() && in_folder(path)) || (!q.is_empty() && path.to_lowercase().contains(&q)) {
                let label = if q.is_empty() { path.rsplit('/').next().unwrap_or(path) } else { path };
                items.push((Pick::Folder(path.clone()), label.into(), String::new()));
            }
        }
        let pages = current.iter().chain(self.picker_cache.pages.iter().filter(|p| Some(*p) != current.as_ref()));
        for path in pages {
            if self.refs.contains(path) { continue; }
            if q.is_empty() && !in_folder(path) { continue; }
            if !q.is_empty() && !path.to_lowercase().contains(&q) { continue; }
            let label = if q.is_empty() { path.rsplit('/').next().unwrap_or(path) } else { path };
            let hint = if Some(path) == current.as_ref() { tr("当前文件", "Current file") } else { "" };
            items.push((Pick::Page(path.clone()), label.into(), hint.into()));
        }
        items
    }

    pub(super) fn open_reference_folder(&mut self, folder: String, rest: String, window: &mut gpui::Window, cx: &mut gpui::Context<Self>) {
        self.picker_cache.folder = folder;
        self.pick_index = 0;
        PICKER_SCROLL.with(|scroll| scroll.scroll_to_item(0, gpui::ScrollStrategy::Top));
        self.input.update(cx, |i, cx| {
            let value = format!("{rest}@"); let end = value.len();
            i.set_value(value, window, cx); i.set_selected_range(end..end, cx); i.focus(window, cx);
        });
        cx.defer_in(window, |this, window, cx| this.input.update(cx, |input, cx| input.focus(window, cx)));
        self.persist_active_draft(cx);
        cx.notify();
    }
}
