//! Workspace probes and isolated reset hooks for smoke runs.
use std::path::PathBuf;
use gpui::{App, Context, Window};
use super::Workspace;

fn file_name(path: &std::path::Path) -> String {
    path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default()
}

impl Workspace {
    /// Keep the registered entities and observers; release all notes before deleting the sandbox.
    pub(crate) fn smoke_close_workspace(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.close_search(window, cx);
        self.dismiss_dropdowns(cx);
        self.close_settings(window, cx);
        self.close_page_history(cx);
        self.cancel_rename(window, cx);
        crate::dropdown::dismiss_all(cx);
        crate::feedback::close(window, cx);
        crate::about::close();
        crate::export::close_active(window, cx);
        self.undo_restore = None;
        self.history_toast = None;
        self.history_return_focus = false;
        self.sidebar_collapsed = false;
        self.sidebar_width = crate::theme::Theme::ocean().sidebar_width;
        self.resizing = None;
        // The editor observer must not reopen another tab when close completes.
        self.tabs.clear();
        self.closed_tabs.clear();
        self.nav.clear();
        self.startup_tabs = None;
        self.external_open.clear();
        self.editor.update(cx, |editor, cx| { editor.close(cx); });
        window.focus(&self.root_focus, cx);
        cx.notify();
    }

    pub(crate) fn smoke_open_notebook(&mut self, notebook: PathBuf, cx: &mut Context<Self>) {
        crate::theme::set_ignored_search_folders(Vec::new());
        crate::theme::set_hide_non_markdown_files(true);
        let welcome = notebook.join("Welcome.md");
        self.notebooks = vec![notebook.clone()];
        self.startup_tabs = Some((vec![welcome.clone()], Some(welcome)));
        self.editor.update(cx, |editor, cx| { editor.reset_layouts(); cx.notify(); });
        self.use_folder(notebook, cx);
    }

    pub(crate) fn search_is_open(&self) -> bool { self.search.is_some() }
    pub(crate) fn search_hit_count(&self) -> usize { self.search.as_ref().map_or(0, |s| s.state.hits.len()) }
    pub(crate) fn settings_is_open(&self) -> bool { self.settings.is_some() }
    pub(crate) fn tab_count(&self) -> usize { self.tabs.len() }
    pub(crate) fn first_tab_name(&self) -> String { self.tabs.first().map(|p| file_name(p)).unwrap_or_default() }
    pub(crate) fn active_tab_name(&self, cx: &App) -> String {
        self.editor.read(cx).current_path().map(file_name).unwrap_or_default()
    }
}

impl Workspace {
    pub(crate) fn prepare_menu_position_smoke(&mut self, right: bool, window: &Window, cx: &mut Context<Self>) {
        assert!(crate::ui::smoke_overlay::is_smoke_active());
        let target = self.folder.as_ref().map(|f| (f.join("Welcome.md"), false));
        let size = window.viewport_size();
        let at = gpui::point(if right { size.width - gpui::px(12.) } else { gpui::px(12.) }, size.height - gpui::px(16.));
        self.open_tree_menu(target, at, cx);
    }
}
