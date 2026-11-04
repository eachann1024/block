//! Read-only probes for the `--smoke` run; they expose private workspace state to `crate::smoke`.
use gpui::App;
use super::Workspace;

fn file_name(path: &std::path::Path) -> String {
    path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default()
}

impl Workspace {
    pub(crate) fn search_is_open(&self) -> bool { self.search.is_some() }
    pub(crate) fn search_hit_count(&self) -> usize { self.search.as_ref().map_or(0, |s| s.state.hits.len()) }
    pub(crate) fn switcher_is_open(&self) -> bool { self.switcher_open }
    pub(crate) fn settings_is_open(&self) -> bool { self.settings.is_some() }
    pub(crate) fn tab_count(&self) -> usize { self.tabs.len() }
    pub(crate) fn first_tab_name(&self) -> String { self.tabs.first().map(|p| file_name(p)).unwrap_or_default() }
    pub(crate) fn active_tab_name(&self, cx: &App) -> String {
        self.editor.read(cx).current_path().map(file_name).unwrap_or_default()
    }
}
