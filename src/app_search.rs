//! Global search palette (⌘K) and the workspace-level key bindings.
use std::path::PathBuf;
use gpui::{
    AnyElement, App, AppContext, Context, KeyBinding, MouseButton, Window, actions, prelude::*, px,
};
use gpui_component::input::{InputEvent, InputState};
use crate::search::{SearchState, render_body, render_footer, render_header};
use crate::theme::{search_shortcut, tr};
use super::Workspace;

pub(super) use crate::search::kbd;

actions!(workspace, [OpenSearch, OpenSettings, NewNote, NewFolder, Dismiss, SearchUp, SearchDown]);

pub(crate) struct Search {
    state: SearchState,
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
    ]);
}

impl Workspace {
    pub(crate) fn summon_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.open_search(window, cx);
    }

    pub(super) fn open_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.folder.is_none() { return; }
        if let Some(search) = &self.search {
            search.state.input.update(cx, |input, cx| input.focus(window, cx));
            return;
        }
        if self.renaming.is_some() { self.cancel_rename(window, cx); }
        if self.settings.is_some() { self.close_settings(window, cx); }
        self.close_overlays(cx);
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(tr("搜索标题与正文", "Search titles and content")));
        let sub = cx.subscribe_in(&input, window, |this, input, event: &InputEvent, window, cx| match event {
            InputEvent::Change => {
                let query = input.read(cx).value().to_string();
                let root = this.folder.clone();
                if let Some(search) = &mut this.search {
                    search.state.run_search(root, query, cx, |this| this.search.as_mut().map(|s| &mut s.state));
                }
            }
            InputEvent::PressEnter { .. } => this.open_hit(window, cx),
            _ => {}
        });
        input.update(cx, |input, cx| input.focus(window, cx));
        let focus_input = input.clone();
        cx.defer_in(window, move |_, window, cx| focus_input.update(cx, |input, cx| input.focus(window, cx)));
        self.search = Some(Search { state: SearchState::new(input), _sub: sub });
        cx.notify();
    }

    pub(super) fn close_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.search.take().is_none() { return; }
        self.restore_focus(window, cx);
        cx.notify();
    }

    fn open_hit(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(path) = self.search.as_ref().and_then(|s| s.state.selected_path()) else { return };
        self.open_from_search(path, window, cx);
    }

    pub(crate) fn open_from_search(&mut self, path: PathBuf, window: &mut Window, cx: &mut Context<Self>) {
        self.settings = None;
        self.close_search(window, cx);
        if self.busy() { return; }
        self.open_file(path, cx);
        self.restore_focus(window, cx);
    }

    pub(super) fn search_view(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let search = self.search.as_ref()?;
        let dialog = crate::ui::modal::card("search-dialog", px(640.))
            .key_context("SearchPalette")
            .on_action(cx.listener(|this, _: &SearchUp, _, cx| {
                if let Some(search) = &mut this.search {
                    search.state.move_selection(-1);
                    cx.notify();
                }
            }))
            .on_action(cx.listener(|this, _: &SearchDown, _, cx| {
                if let Some(search) = &mut this.search {
                    search.state.move_selection(1);
                    cx.notify();
                }
            }))
            .on_action(cx.listener(|this, _: &Dismiss, window, cx| this.close_search(window, cx)))
            .child(render_header(&search.state.input, cx.listener(|this, _, window, cx| this.close_search(window, cx))))
            .child(render_body(&search.state, self.folder.is_some(), false, cx, |this, ix, window, cx| {
                if let Some(search) = &mut this.search { search.state.selected = ix; }
                this.open_hit(window, cx);
            }))
            .child(render_footer());
        Some(crate::ui::modal::backdrop("search-overlay", dialog)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.close_search(window, cx)))
            .into_any_element())
    }
}
