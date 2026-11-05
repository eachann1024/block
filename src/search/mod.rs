//! Shared search state and debounced query runner used by the in-app search
//! overlay (`app_search.rs`) and the floating search window (`search/window.rs`).
#[cfg(not(target_family = "wasm"))]
pub mod window;
mod view;

use std::path::{Path, PathBuf};
use std::time::Duration;
use gpui::{Context, Entity, ScrollHandle};
use gpui_component::input::InputState;
use crate::files::search::{search_notes, search_notes_in_folders};

pub use view::*;

const LIMIT: usize = 50;
const DEBOUNCE: Duration = Duration::from_millis(150);

#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub enum SearchScope {
    #[default]
    All,
    Folder(PathBuf),
}

pub struct SearchState {
    pub input: Entity<InputState>,
    pub hits: Vec<crate::files::search::Hit>,
    pub selected: usize,
    pub epoch: u64,
    pub searching: bool,
    pub asked: bool,
    pub scroll: ScrollHandle,
    pub scope: SearchScope,
    pub folders: Vec<PathBuf>,
    pub scope_menu_open: bool,
}

impl SearchState {
    pub fn new(input: Entity<InputState>, folders: Vec<PathBuf>) -> Self {
        Self {
            input, hits: Vec::new(), selected: 0, epoch: 0, searching: false, asked: false,
            scroll: ScrollHandle::new(), scope: SearchScope::All, folders, scope_menu_open: false,
        }
    }

    pub fn selected_path(&self) -> Option<PathBuf> {
        self.hits.get(self.selected).map(|hit| hit.path.clone())
    }

    pub fn scope_label(&self) -> String {
        match &self.scope {
            SearchScope::All => crate::theme::tr("全部文件夹", "All Folders").to_string(),
            SearchScope::Folder(path) => folder_name(path),
        }
    }

    pub fn effective_roots(&self) -> Vec<PathBuf> {
        match &self.scope {
            SearchScope::All => self.folders.clone(),
            SearchScope::Folder(path) => vec![path.clone()],
        }
    }

    pub fn sync_folders(&mut self, folders: Vec<PathBuf>) -> bool {
        let scope_lost = matches!(&self.scope, SearchScope::Folder(path) if !folders.iter().any(|folder| folder == path));
        if self.folders == folders && !scope_lost { return false; }
        self.folders = folders;
        if scope_lost { self.scope = SearchScope::All; }
        true
    }

    pub fn toggle_scope_menu(&mut self) { self.scope_menu_open = !self.scope_menu_open; }

    pub fn cycle_scope(&mut self, delta: isize) -> bool {
        cycle_scope(&mut self.scope, &self.folders, &mut self.scope_menu_open, delta)
    }

    pub fn reset_scope_all(&mut self) -> bool {
        self.scope_menu_open = false;
        let changed = self.scope != SearchScope::All;
        self.scope = SearchScope::All;
        changed
    }

    /// `0` is every folder; later indexes are `folders`.
    pub fn select_scope_index(&mut self, index: usize) -> bool {
        let next = if index == 0 {
            SearchScope::All
        } else {
            match self.folders.get(index - 1) {
                Some(path) => SearchScope::Folder(path.clone()),
                None => return false,
            }
        };
        let changed = self.scope != next;
        self.scope = next;
        self.scope_menu_open = false;
        changed
    }

    pub fn move_selection(&mut self, delta: isize) {
        if self.hits.is_empty() { return; }
        let last = self.hits.len() - 1;
        self.selected = (self.selected as isize + delta).clamp(0, last as isize) as usize;
        let titles = self.hits.iter().filter(|hit| hit.title_match).count();
        let child = if self.selected < titles { self.selected + 1 } else { self.selected + 1 + (titles > 0) as usize };
        self.scroll.scroll_to_item(child);
    }

    pub fn research<V: 'static>(&mut self, cx: &mut Context<V>, get: fn(&mut V) -> Option<&mut SearchState>) {
        let query = self.input.read(cx).value().to_string();
        self.run_search(query, cx, get);
    }

    pub fn run_search<V: 'static>(&mut self, query: String, cx: &mut Context<V>, get: fn(&mut V) -> Option<&mut SearchState>) {
        let roots = self.effective_roots();
        if roots.is_empty() {
            self.hits.clear();
            self.searching = false;
            self.selected = 0;
            self.asked = false;
            cx.notify();
            return;
        }
        self.epoch += 1;
        let epoch = self.epoch;
        self.asked = !query.trim().is_empty();
        if !self.asked {
            self.hits.clear();
            self.searching = false;
            self.selected = 0;
            cx.notify();
            return;
        }
        let single = matches!(self.scope, SearchScope::Folder(_)) && roots.len() == 1;
        self.searching = true;
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(DEBOUNCE).await;
            let alive = this.update(cx, |this, _| get(this).is_some_and(|state| state.epoch == epoch)).unwrap_or(false);
            if !alive { return; }
            let hits = cx.background_executor().spawn(async move {
                if single { search_notes(&roots[0], &query, LIMIT) } else { search_notes_in_folders(&roots, &query, LIMIT) }
            }).await;
            let _ = this.update(cx, |this, cx| {
                let Some(search) = get(this).filter(|state| state.epoch == epoch) else { return };
                search.hits = hits;
                search.selected = 0;
                search.searching = false;
                search.scroll.scroll_to_item(0);
                cx.notify();
            });
        }).detach();
    }
}

pub(crate) fn notebook_folders(current: Option<PathBuf>, notebooks: &[PathBuf]) -> Vec<PathBuf> {
    let mut folders = Vec::new();
    if let Some(folder) = current {
        if !folder.as_os_str().is_empty() {
            folders.push(folder);
        }
    }
    for folder in notebooks {
        if !folder.as_os_str().is_empty() && !folders.iter().any(|known| known == folder) {
            folders.push(folder.clone());
        }
    }
    folders
}

fn cycle_scope(scope: &mut SearchScope, folders: &[PathBuf], menu_open: &mut bool, delta: isize) -> bool {
    *menu_open = false;
    let mut options = vec![SearchScope::All];
    options.extend(folders.iter().cloned().map(SearchScope::Folder));
    let len = options.len() as isize;
    if len == 0 || delta == 0 { return false; }
    let current = options.iter().position(|item| item == scope).unwrap_or(0) as isize;
    let next = options[(current + delta).rem_euclid(len) as usize].clone();
    let changed = *scope != next;
    *scope = next;
    changed
}

pub(crate) fn folder_name(path: &Path) -> String {
    path.file_name().map(|name| name.to_string_lossy().into_owned()).filter(|name| !name.is_empty())
        .unwrap_or_else(|| path.display().to_string())
}

pub(crate) use crate::ui::shortcut_label::kbd;
