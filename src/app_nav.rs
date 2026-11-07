//! In-place tab opening, explicit new-tab opening, and back/forward navigation history.
use std::path::{Path, PathBuf};
use gpui::{App, Context, KeyBinding, actions, div, prelude::*, px};
use crate::icons::Icon;
use crate::ui::icon_button::icon_button;
use super::{Workspace, tabs::{clear_tab_menu, record_tab_open}};

const MAX_NAV_HISTORY: usize = 64;

actions!(workspace_nav, [NavigateBack, NavigateForward]);

#[derive(Default)]
pub(crate) struct NavHistory {
    entries: Vec<PathBuf>,
    index: usize,
}

impl NavHistory {
    pub fn push(&mut self, path: PathBuf) {
        if !self.entries.is_empty() && self.entries.get(self.index) == Some(&path) {
            return;
        }
        if !self.entries.is_empty() && self.index + 1 < self.entries.len() {
            self.entries.truncate(self.index + 1);
        }
        self.entries.push(path);
        if self.entries.len() > MAX_NAV_HISTORY {
            self.entries.remove(0);
        }
        self.index = self.entries.len() - 1;
    }

    pub fn can_go_back(&self) -> bool {
        self.entries.len() > 1 && self.index > 0
    }

    pub fn can_go_forward(&self) -> bool {
        !self.entries.is_empty() && self.index + 1 < self.entries.len()
    }

    pub fn go_back(&mut self) -> Option<PathBuf> {
        if !self.can_go_back() { return None; }
        self.index -= 1;
        self.entries.get(self.index).cloned()
    }

    pub fn go_forward(&mut self) -> Option<PathBuf> {
        if !self.can_go_forward() { return None; }
        self.index += 1;
        self.entries.get(self.index).cloned()
    }

    pub fn clear(&mut self) {
        self.entries.clear();
        self.index = 0;
    }

    pub fn rebase(&mut self, from: &Path, to: &Path) {
        for entry in &mut self.entries {
            if let Some(next) = crate::files::ops::rebase(entry, from, to) {
                *entry = next;
            }
        }
        self.dedup_preserving_index();
    }

    pub fn remove_under(&mut self, prefix: &Path) {
        if self.entries.is_empty() { return; }
        let mut next_entries = Vec::with_capacity(self.entries.len());
        let mut next_index = 0usize;
        for (i, entry) in self.entries.drain(..).enumerate() {
            if entry.starts_with(prefix) {
                if i < self.index && next_index > 0 {
                    next_index -= 1;
                }
                continue;
            }
            if i == self.index {
                next_index = next_entries.len();
            } else if i < self.index {
                next_index = next_entries.len();
            }
            next_entries.push(entry);
        }
        self.entries = next_entries;
        self.index = if self.entries.is_empty() { 0 } else { next_index.min(self.entries.len() - 1) };
        self.dedup_preserving_index();
    }

    fn dedup_preserving_index(&mut self) {
        if self.entries.len() < 2 { return; }
        let mut deduped: Vec<PathBuf> = Vec::with_capacity(self.entries.len());
        let mut new_idx = 0usize;
        for (i, p) in self.entries.drain(..).enumerate() {
            if deduped.last() != Some(&p) {
                deduped.push(p);
            }
            if i == self.index {
                new_idx = deduped.len().saturating_sub(1);
            }
        }
        self.entries = deduped;
        self.index = new_idx.min(self.entries.len().saturating_sub(1));
    }
}

pub(crate) fn bind_keys(cx: &mut App) {
    crate::ui::app_shortcuts::bind(cx, [
        KeyBinding::new("cmd-[", NavigateBack, Some("Workspace")),
        KeyBinding::new("cmd-]", NavigateForward, Some("Workspace")),
    ]);
    #[cfg(not(target_os = "macos"))]
    crate::ui::app_shortcuts::bind(cx, [
        KeyBinding::new("alt-left", NavigateBack, Some("Workspace")),
        KeyBinding::new("alt-right", NavigateForward, Some("Workspace")),
    ]);
}

impl Workspace {
    pub(crate) fn navigate_back(&mut self, cx: &mut Context<Self>) {
        if self.busy() { return; }
        while let Some(path) = self.nav.go_back() {
            if crate::files::ops::is_file(&path) {
                self.open_file_with_mode(path, false, false, cx);
                break;
            }
        }
    }

    pub(crate) fn navigate_forward(&mut self, cx: &mut Context<Self>) {
        if self.busy() { return; }
        while let Some(path) = self.nav.go_forward() {
            if crate::files::ops::is_file(&path) {
                self.open_file_with_mode(path, false, false, cx);
                break;
            }
        }
    }

    pub(crate) fn open_file_in_new_tab(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        self.open_file_with_mode(path, true, true, cx);
    }

    pub(crate) fn open_file_with_mode(&mut self, path: PathBuf, in_new_tab: bool, record_nav: bool, cx: &mut Context<Self>) {
        clear_tab_menu();
        self.settings = None;
        if self.history_restore_busy { return; }
        let return_focus = self.history.is_some();
        if return_focus || self.history_opening { self.close_page_history(cx); }
        if return_focus { self.history_return_focus = true; }
        if self.undo_restore.as_ref().is_some_and(|undo| undo.path != path) {
            self.undo_restore = None;
            self.history_toast = None;
        }
        if record_nav {
            self.nav.push(path.clone());
        }
        let tabs_state_changed = self.selected.as_ref() != Some(&path) || !self.tabs.contains(&path);
        let mut evicted: Option<PathBuf> = None;
        if self.tabs.contains(&path) {
            // Target note is already open in a tab: activate that tab without duplicating.
        } else if in_new_tab || self.tabs.is_empty() {
            self.tabs.push(path.clone());
            if self.tabs.len() > 1 {
                record_tab_open(path.clone());
            }
        } else {
            let active_ix = self.selected.as_ref()
                .and_then(|s| self.tabs.iter().position(|t| t == s))
                .or_else(|| self.editor.read(cx).current_path().and_then(|c| self.tabs.iter().position(|t| t == c)))
                .unwrap_or(self.tabs.len() - 1);
            let old = std::mem::replace(&mut self.tabs[active_ix], path.clone());
            if !self.tabs.contains(&old) {
                evicted = Some(old);
            }
        }
        self.selected = Some(path.clone());
        self.tree.update(cx, |tree, cx| tree.select(Some(path.clone()), cx));
        self.editor.update(cx, |editor, cx| {
            editor.open(path, cx);
            if let Some(old) = evicted {
                editor.close_session(&old, cx);
            }
        });
        if tabs_state_changed { self.save_tabs_state(cx); }
        cx.notify();
    }

    /// Back/forward buttons shown only when the matching history direction exists.
    pub(super) fn nav_button_count(&self) -> usize {
        if self.settings.is_some() || self.is_history_open() { return 0; }
        usize::from(self.nav.can_go_back()) + usize::from(self.nav.can_go_forward())
    }

    pub(super) fn render_nav_buttons(&self, cx: &mut Context<Self>) -> Option<gpui::Div> {
        if self.settings.is_some() || self.is_history_open() { return None; }
        let show_back = self.nav.can_go_back();
        let show_fwd = self.nav.can_go_forward();
        if !show_back && !show_fwd { return None; }
        let enabled = self.ui_enabled();
        Some(div().flex_none().flex().items_center().gap(px(2.))
            .when(show_back, |d| d.child(icon_button("nav-back", Icon::ChevronLeft, enabled, cx.listener(|this, _, _, cx| {
                cx.stop_propagation();
                this.navigate_back(cx);
            }))))
            .when(show_fwd, |d| d.child(icon_button("nav-forward", Icon::ChevronRight, enabled, cx.listener(|this, _, _, cx| {
                cx.stop_propagation();
                this.navigate_forward(cx);
            })))))
    }
}
