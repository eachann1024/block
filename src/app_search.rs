//! Global search palette (⌘K) and the workspace-level key bindings.
use std::path::PathBuf;
use gpui::{
    AnyElement, App, AppContext, Context, KeyBinding, MouseButton, Window, actions, prelude::*, px,
};
use gpui_component::input::{InputEvent, InputState};
use crate::search::{SearchState, notebook_folders, render_body, render_footer, render_header, render_scope_menu};
use crate::theme::{search_shortcut, tr};
use super::Workspace;

pub(super) use crate::search::kbd;

actions!(workspace, [
    OpenSearch, OpenSettings, NewNote, NewFolder, Dismiss, SearchUp, SearchDown,
    SearchNextScope, SearchPrevScope, SearchResetScopeAll,
]);

pub(crate) struct Search {
    pub(super) state: SearchState,
    _sub: gpui::Subscription,
}

/// Registered from `Workspace::new`, so `main.rs` needs no extra call.
pub(super) fn bind_keys(cx: &mut App) {
    let search_key = search_shortcut();
    if let Ok(binding) = KeyBinding::load(&search_key, Box::new(OpenSearch), None, false, None, cx) {
        cx.bind_keys([binding]);
    }
    cx.bind_keys([
        KeyBinding::new("cmd-k", OpenSearch, None),
        KeyBinding::new("cmd-shift-k", OpenSearch, None),
        KeyBinding::new("alt-cmd-k", OpenSearch, None),
        KeyBinding::new("cmd-p", OpenSearch, None),
        KeyBinding::new("cmd-,", OpenSettings, None),
        KeyBinding::new("cmd-n", NewNote, Some("Workspace")),
        KeyBinding::new("cmd-shift-n", NewFolder, Some("Workspace")),
        KeyBinding::new("escape", Dismiss, Some("Workspace")),
        KeyBinding::new("escape", Dismiss, Some("SearchPalette")),
        KeyBinding::new("cmd-w", Dismiss, Some("SearchPalette")),
        KeyBinding::new("up", SearchUp, Some("SearchPalette")),
        KeyBinding::new("down", SearchDown, Some("SearchPalette")),
        KeyBinding::new("tab", SearchNextScope, Some("SearchPalette")),
        KeyBinding::new("shift-tab", SearchPrevScope, Some("SearchPalette")),
        KeyBinding::new("cmd-0", SearchResetScopeAll, Some("SearchPalette")),
    ]);
    #[cfg(not(target_os = "macos"))]
    cx.bind_keys([
        KeyBinding::new("ctrl-k", OpenSearch, None),
        KeyBinding::new("ctrl-shift-k", OpenSearch, None),
        KeyBinding::new("ctrl-alt-k", OpenSearch, None),
        KeyBinding::new("ctrl-p", OpenSearch, None),
        KeyBinding::new("ctrl-,", OpenSettings, None),
        KeyBinding::new("ctrl-n", NewNote, Some("Workspace")),
        KeyBinding::new("ctrl-shift-n", NewFolder, Some("Workspace")),
        KeyBinding::new("ctrl-w", Dismiss, Some("SearchPalette")),
        KeyBinding::new("ctrl-0", SearchResetScopeAll, Some("SearchPalette")),
    ]);
}

impl Workspace {
    pub(crate) fn search_folders(&self) -> Vec<PathBuf> {
        notebook_folders(self.folder.clone(), &self.notebooks)
    }

    fn finish_scope(&mut self, changed: bool, cx: &mut Context<Self>) {
        if changed {
            if let Some(search) = &mut self.search {
                search.state.research(cx, |this| this.search.as_mut().map(|search| &mut search.state));
            }
        }
        cx.notify();
    }

    fn toggle_scope_menu(&mut self, cx: &mut Context<Self>) {
        let folders = self.search_folders();
        if let Some(search) = &mut self.search {
            let changed = search.state.sync_folders(folders);
            search.state.toggle_scope_menu();
            if changed && search.state.asked { search.state.research(cx, |this| this.search.as_mut().map(|search| &mut search.state)); }
        }
        cx.notify();
    }

    fn cycle_search_scope(&mut self, delta: isize, cx: &mut Context<Self>) {
        let folders = self.search_folders();
        let changed = self.search.as_mut().is_some_and(|search| {
            let list = search.state.sync_folders(folders);
            search.state.cycle_scope(delta) || (list && search.state.asked)
        });
        self.finish_scope(changed, cx);
    }

    pub(crate) fn summon_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.open_search(window, cx);
    }

    pub(super) fn open_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.folder.is_none() || self.history_restore_busy { return; }
        if self.is_history_open() || self.history_opening { self.close_page_history_focused(window, cx); }
        if self.search.is_some() {
            let folders = self.search_folders();
            let requery = self.search.as_mut().is_some_and(|search| {
                let before = search.state.scope.clone();
                search.state.sync_folders(folders);
                search.state.reset_scope_all();
                search.state.asked && search.state.scope != before
            });
            if requery && let Some(search) = &mut self.search {
                search.state.research(cx, |this| this.search.as_mut().map(|search| &mut search.state));
            }
            if let Some(search) = &self.search {
                let focus_input = search.state.input.clone();
                cx.defer_in(window, move |_, window, cx| focus_input.update(cx, |input, cx| input.focus(window, cx)));
            }
            cx.notify();
            return;
        }
        if self.renaming.is_some() { self.cancel_rename(window, cx); }
        if self.settings.is_some() { self.close_settings(window, cx); }
        self.close_overlays(cx);
        crate::dropdown::dismiss_all(cx);
        let folders = self.search_folders();
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(tr("搜索标题与正文", "Search titles and content")));
        let sub = cx.subscribe_in(&input, window, |this, input, event: &InputEvent, window, cx| match event {
            InputEvent::Change => {
                let query = input.read(cx).value().to_string();
                let folders = this.search_folders();
                if let Some(search) = &mut this.search {
                    search.state.sync_folders(folders);
                    search.state.run_search(query, cx, |this| this.search.as_mut().map(|search| &mut search.state));
                }
            }
            InputEvent::PressEnter { .. } => this.open_hit(window, cx),
            _ => {}
        });
        let focus_input = input.clone();
        cx.defer_in(window, move |_, window, cx| focus_input.update(cx, |input, cx| input.focus(window, cx)));
        self.search = Some(Search { state: SearchState::new(input, folders), _sub: sub });
        cx.notify();
    }

    pub(super) fn close_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.search.take().is_none() { return; }
        self.restore_focus(window, cx);
        cx.notify();
    }

    fn open_hit(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(path) = self.search.as_ref().and_then(|search| search.state.selected_path()) else { return };
        self.open_from_search(path, window, cx);
    }

    pub(crate) fn open_from_search(&mut self, path: PathBuf, window: &mut Window, cx: &mut Context<Self>) {
        self.settings = None;
        self.close_search(window, cx);
        if self.busy() { return; }
        if self.folder.as_deref().is_some_and(|current| path.starts_with(current)) {
            self.open_file(path, cx);
            self.restore_focus(window, cx);
            return;
        }
        if let Some(target) = self.notebooks.iter().filter(|notebook| path.starts_with(notebook)).max_by_key(|notebook| notebook.as_os_str().len()).cloned() {
            self.startup_tabs = Some((vec![path.clone()], Some(path)));
            self.use_folder(target, cx);
            return;
        }
        self.open_file(path, cx);
        self.restore_focus(window, cx);
    }

    pub(super) fn search_view(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let search = self.search.as_ref()?;
        let menu_open = search.state.scope_menu_open;
        let dialog = crate::ui::modal::card("search-dialog", px(640.))
            .relative()
            .key_context("SearchPalette")
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                cx.stop_propagation();
                if let Some(search) = &mut this.search && search.state.scope_menu_open {
                    search.state.scope_menu_open = false;
                    cx.notify();
                }
            }))
            .on_action(cx.listener(|this, _: &SearchUp, _, cx| {
                if let Some(search) = &mut this.search { search.state.move_selection(-1); cx.notify(); }
            }))
            .on_action(cx.listener(|this, _: &SearchDown, _, cx| {
                if let Some(search) = &mut this.search { search.state.move_selection(1); cx.notify(); }
            }))
            .on_action(cx.listener(|this, _: &SearchNextScope, _, cx| this.cycle_search_scope(1, cx)))
            .on_action(cx.listener(|this, _: &SearchPrevScope, _, cx| this.cycle_search_scope(-1, cx)))
            .on_action(cx.listener(|this, _: &SearchResetScopeAll, _, cx| {
                let changed = this.search.as_mut().is_some_and(|search| search.state.reset_scope_all());
                this.finish_scope(changed, cx);
            }))
            .capture_action(cx.listener(|this, _: &gpui_component::input::Escape, window, cx| {
                cx.stop_propagation();
                if this.search.as_ref().is_some_and(|search| search.state.scope_menu_open) {
                    if let Some(search) = &mut this.search { search.state.scope_menu_open = false; }
                    cx.notify();
                    return;
                }
                this.close_search(window, cx);
            }))
            .on_action(cx.listener(|this, _: &Dismiss, window, cx| {
                if this.search.as_ref().is_some_and(|search| search.state.scope_menu_open) {
                    if let Some(search) = &mut this.search { search.state.scope_menu_open = false; }
                    cx.notify();
                    return;
                }
                this.close_search(window, cx);
            }))
            .child(render_header(
                &search.state,
                cx.listener(|this, _, _, cx| this.toggle_scope_menu(cx)),
            ))
            .when(menu_open, |card| card.child(render_scope_menu(&search.state, cx, |this, index, _, cx| {
                let changed = this.search.as_mut().is_some_and(|search| search.state.select_scope_index(index));
                this.finish_scope(changed, cx);
            })))
            .child(render_body(&search.state, false, cx, |this, ix, window, cx| {
                if let Some(search) = &mut this.search { search.state.selected = ix; }
                this.open_hit(window, cx);
            }))
            .child(render_footer());
        Some(gpui::deferred(crate::ui::modal::backdrop("search-overlay", dialog)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.close_search(window, cx))))
            .with_priority(5)
            .into_any_element())
    }
}
