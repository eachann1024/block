//! Workspace transitions commit only after scanning and editor close succeed.
use std::path::PathBuf;
use gpui::{AppContext, Context, Focusable, Window};
use crate::editor::view::Editor;
use crate::theme::{Theme, tr};
use crate::files::{create_note_at, note_candidates, load_state, save_folder, scan, index::TreeIndex, tree::FileTree};

#[path = "app_view.rs"] mod view;
#[path = "app_shortcuts.rs"] mod shortcuts;
#[path = "app_tabs.rs"] mod tabs;
#[path = "app_nav.rs"] mod nav;
#[path = "app_folder.rs"] mod folder;
#[path = "app_sidebar.rs"] mod sidebar;
#[path = "app_files.rs"] mod file_ops;
#[path = "app_smoke.rs"] mod smoke_probe;
#[path = "app_menu.rs"] mod menu;
#[path = "app_search.rs"] mod search;
#[path = "app_settings.rs"] mod settings;
#[path = "app_settings_view.rs"] mod settings_view;
#[path = "app_settings_sidebar.rs"] mod settings_sidebar;
#[path = "app_settings_tabs.rs"] mod settings_tabs;
#[path = "app_settings_fonts.rs"] mod settings_fonts;
#[path = "app_settings_folders.rs"] mod settings_folders;
#[path = "app_settings_folders_view.rs"] mod settings_folders_view;
#[path = "app_history.rs"] mod history;
#[path = "app_history_view.rs"] mod history_view;

use file_ops::{FileOp, Renaming, TreeMenu}; use search::Search; use settings::SettingsModal; use history::{HistoryToast, PageHistoryState, UndoRestoreState};
pub use search::{Dismiss, NewFolder, NewNote, OpenSearch, OpenSettings, SearchDown, SearchNextScope, SearchPrevScope, SearchResetScopeAll, SearchUp};
pub use tabs::{CloseTab, NewTab, NextTab, PrevTab, ToggleSidebar}; pub use nav::{NavigateBack, NavigateForward};

struct PreparedFolder { path: PathBuf, index: TreeIndex, notice: String, tabs: Vec<PathBuf>, active_tab: Option<PathBuf> }

pub struct Workspace {
    pub(crate) folder: Option<PathBuf>,
    pub(crate) folder_error: Option<PathBuf>,
    pub(crate) tree: gpui::Entity<FileTree>,
    pub(crate) editor: gpui::Entity<Editor>,
    selected: Option<PathBuf>,
    tabs: Vec<PathBuf>,
    pub(super) closed_tabs: tabs::ClosedTabs,
    pub(super) nav: nav::NavHistory,
    startup_tabs: Option<(Vec<PathBuf>, Option<PathBuf>)>,
    pub(super) external_open: Vec<PathBuf>,
    dirty_bits: u64,
    pub(crate) loading: bool,
    choosing: bool,
    creating: bool,
    persisting: bool,
    pending: Option<PreparedFolder>,
    notice: String,
    sidebar_width: gpui::Pixels,
    pub(crate) sidebar_collapsed: bool,
    /// The sidebar was opened while the window had no column for it.
    sidebar_pinned: bool,
    /// Set once the chat entity exists, so a sash drag relayouts the shell.
    chat_watched: bool,
    /// Last macOS traffic-light row applied for the fullscreen session bar.
    traffic_lights_tall: Option<bool>,
    /// (pointer x at press, width at press) while the edge is dragged.
    resizing: Option<(gpui::Pixels, gpui::Pixels)>,
    notebooks: Vec<PathBuf>,
    switcher_open: bool,
    menu: Option<TreeMenu>,
    renaming: Option<Renaming>,
    search: Option<Search>,
    settings: Option<SettingsModal>,
    pub(super) history: Option<PageHistoryState>,
    pub(super) undo_restore: Option<UndoRestoreState>,
    pub(super) history_toast: Option<HistoryToast>,
    history_token: u64,
    history_opening: bool,
    history_restore_busy: bool,
    history_return_focus: bool,
    /// A disk change that waits for the editor to release the affected note.
    file_op: Option<FileOp>,
    working: bool,
    pub(crate) scan_generation: u64,
    /// Keeps the Workspace key context alive when nothing else has focus.
    root_focus: gpui::FocusHandle,
    bounds_task: Option<gpui::Task<()>>,
    _editor_subscription: gpui::Subscription,
}

impl Workspace {
    pub fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let tree = cx.new(FileTree::new);
        let editor = cx.new(|cx| Editor::new(cx));
        let startup_epoch = editor.read(cx).interaction_epoch();
        let workspace = cx.entity();
        let (context_target, rename_target, trash_target, move_target) = (workspace.clone(), workspace.clone(), workspace.clone(), workspace.clone());
        tree.update(cx, |tree, _| {
            tree.on_context(move |target, position, _, cx| {
                let context_target = context_target.clone();
                cx.defer(move |cx| context_target.update(cx, |this, cx| this.open_tree_menu(target, position, cx)));
            });
            tree.on_open(move |path, in_new_tab, window, cx| {
                let workspace = workspace.clone();
                window.defer(cx, move |window, cx| {
                    workspace.update(cx, |this, cx| if !this.busy() {
                        this.close_settings(window, cx);
                        if in_new_tab { this.open_file_in_new_tab(path, cx); } else { this.open_file(path, cx); }
                    });
                });
            });
            tree.on_rename(move |(path, dir), window, cx| {
                let rename_target = rename_target.clone();
                window.defer(cx, move |window, cx| rename_target.update(cx, |this, cx| if !this.busy() { this.start_rename(path, dir, window, cx); }));
            });
            tree.on_trash(move |path, _, cx| {
                let trash_target = trash_target.clone();
                cx.defer(move |cx| trash_target.update(cx, |this, cx| if !this.busy() { this.trash(path, cx); }));
            });
            tree.on_move(move |from, dest, _, cx| {
                let move_target = move_target.clone();
                cx.defer(move |cx| move_target.update(cx, |this, cx| if !this.busy() { this.move_item(from, dest, cx); }));
            });
        });
        let subscription = cx.observe_in(&editor, window, |this, editor, window, cx| {
            let path = editor.read(cx).current_path().map(|path| path.to_owned());
            let loading = editor.read(cx).is_loading();
            let busy = editor.read(cx).is_busy();
            if !loading && this.pending.is_none() && this.file_op.is_none()
                && (this.selected != path || this.tree.read(cx).selected() != path.as_deref()) {
                if let Some(p) = &path {
                    if !this.tabs.contains(p) { this.tabs.push(p.clone()); }
                } else if let Some(prev) = &this.selected {
                    if !editor.read(cx).has_open_session(prev) {
                        this.tabs.retain(|t| t != prev);
                        if let Some(next) = this.tabs.last().cloned() {
                            this.open_file(next, cx);
                            return;
                        }
                    }
                }
                this.selected = path.clone();
                this.tree.update(cx, |tree, cx| tree.select(path.clone(), cx));
                if path.is_some() && !this.tree.read(cx).is_focused(window) {
                    let focus = editor.focus_handle(cx);
                    window.focus(&focus, cx);
                }
                this.save_tabs_state(cx);
                cx.notify();
            }
            let mask = this.dirty_mask(cx);
            if this.dirty_bits != mask {
                this.dirty_bits = mask;
                cx.notify();
            }
            if this.pending.is_some() && !busy {
                if path.is_none() { this.commit_folder(cx); }
                else {
                    this.discard_pending(cx);
                    this.notice = tr("目录未切换：当前笔记未能保存，请处理编辑器中的错误后重试", "Folder switch canceled: could not save current note").into();
                    this.refresh(cx);
                }
            }
            if this.file_op.is_some() && !busy {
                let blocked = this.file_op.as_ref().is_some_and(|op| {
                    let p = op.source_path();
                    let ed = editor.read(cx);
                    match op {
                        FileOp::Trash { .. } => ed.current_path().is_some_and(|cur| cur.starts_with(p)) || ed.has_dirty_under(p),
                        FileOp::Rename { .. } | FileOp::Move { .. } => ed.has_dirty_under(p),
                    }
                });
                if !blocked { this.run_file_op(cx); }
                else {
                    this.file_op = None;
                    this.notice = tr("操作已取消：当前笔记未能保存，请处理编辑器中的错误后重试", "Operation canceled: could not save current note").into();
                    this.refresh(cx);
                }
            }
            cx.notify();
        });
        cx.observe_global::<crate::export::ExportSlot>(|_, cx| cx.notify()).detach();
        crate::dropdown::register_workspace(&cx.entity());
        cx.observe_window_activation(window, |this, window, cx| {
            if !window.is_window_active() {
                this.dismiss_dropdowns(cx);
                crate::dropdown::dismiss_all(cx);
                // Autosave is debounced. Flush before another app edits the same file.
                this.editor.update(cx, |editor, cx| editor.save(cx));
            } else {
                this.editor.update(cx, |editor, cx| editor.refresh_from_disk(cx));
            }
        }).detach();
        cx.observe_window_appearance(window, |this, window, cx| {
            if crate::theme::sync_system_appearance(window.appearance(), cx) {
                this.editor.update(cx, |e, cx| { e.reset_layouts(); cx.notify(); });
                this.tree.update(cx, |_, cx| cx.notify());
                crate::quick::refresh_style(cx);
                cx.notify();
            }
        }).detach();
        cx.observe_window_bounds(window, |this, window, cx| {
            let bounds = crate::window_bounds::capture_window_bounds(window, cx);
            this.bounds_task = Some(cx.spawn(async move |_, cx| {
                cx.background_executor().timer(std::time::Duration::from_millis(250)).await;
                cx.background_executor().spawn(async move { let _ = crate::files::update_state(move |s| s.main_window_bounds = Some(bounds)); }).await;
            }));
        }).detach();
        search::bind_keys(cx);
        tabs::bind_keys(cx);
        nav::bind_keys(cx);
        crate::files::tree::bind_keys(cx);
        let this = Self { folder: None, folder_error: None, tree, editor, selected: None, tabs: Vec::new(), closed_tabs: tabs::ClosedTabs::default(), nav: nav::NavHistory::default(), startup_tabs: None, external_open: Vec::new(), dirty_bits: 0, loading: true,
            choosing: false, creating: false, persisting: false, pending: None,
            notice: tr("正在读取工作区偏好…", "Loading workspace preferences…").into(), sidebar_width: Theme::ocean().sidebar_width,
            sidebar_collapsed: false, sidebar_pinned: true, chat_watched: false, traffic_lights_tall: None, resizing: None, notebooks: Vec::new(), switcher_open: false, menu: None, renaming: None, search: None,
            settings: None, history: None, undo_restore: None, history_toast: None, history_token: 0, history_opening: false, history_restore_busy: false, history_return_focus: false, file_op: None, working: false, scan_generation: 0, root_focus: cx.focus_handle(),
            bounds_task: None, _editor_subscription: subscription };
        Self::install_shortcut_router(window, cx);
        window.focus(&this.root_focus, cx);
        cx.spawn(async move |this, cx| {
            let saved = cx.background_executor().spawn(async { load_state() }).await;
            let _ = this.update(cx, |this, cx| {
                this.loading = false;
                if let Ok(state) = &saved {
                    this.notebooks = state.notebooks.clone();
                    this.startup_tabs = Some((state.open_tabs.clone(), state.active_tab.clone()));
                    if let Some(width) = state.sidebar_width { this.sidebar_width = sidebar::clamp_width(gpui::px(width)); }
                    // Every launch opens the sidebar. A narrow window draws it over the page.
                    this.present_sidebar();
                }
                match saved.map(|state| state.folder) {
                    Ok(Some(folder)) => this.use_folder_guarded(folder, Some(startup_epoch), cx),
                    Ok(None) => {
                        this.notice = tr("选择文件夹以浏览 Markdown 笔记", "Select a folder to browse Markdown notes").into();
                        this.refresh(cx);
                        this.drain_external_open(cx);
                    }
                    Err(error) => {
                        this.notice = format!("{}: {error}", tr("无法读取工作区偏好", "Failed to read workspace preferences"));
                        this.refresh(cx);
                        this.drain_external_open(cx);
                    }
                }
            });
        }).detach();
        this
    }

    pub(crate) fn busy(&self) -> bool {
        self.loading || self.choosing || self.creating || self.persisting || self.pending.is_some()
            || self.file_op.is_some() || self.working || self.history_restore_busy
    }

    pub(crate) fn ui_enabled(&self) -> bool {
        self.folder.is_some() && !self.loading && self.pending.is_none()
    }

    fn refresh(&mut self, cx: &mut Context<Self>) {
        let enabled = self.ui_enabled();
        self.tree.update(cx, |tree, cx| tree.set_enabled(enabled, cx));
        cx.notify();
    }

    fn new_note(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.busy() { return; }
        let Some(folder) = self.folder.clone() else { return };
        self.creating = true;
        self.notice = tr("正在新建笔记…", "Creating note…").into();
        self.refresh(cx);
        cx.spawn_in(window, async move |this, cx| {
            let mut result = Err(std::io::Error::new(std::io::ErrorKind::AlreadyExists, tr("无法分配唯一笔记名称，请重试", "Could not allocate a unique note name, please retry")));
            for path in note_candidates(&folder) {
                // A deleted disk file can still own a row and an open draft.
                // Never allocate that identity to a new empty document.
                let available = this.update(cx, |this, cx| {
                    !this.tree.read(cx).contains_root_path(&path)
                        && !this.editor.read(cx).has_open_session(&path)
                }).unwrap_or(false);
                if !available { continue; }
                let candidate = path.clone();
                match cx.background_executor().spawn(async move { create_note_at(&candidate) }).await {
                    Ok(()) => { result = Ok(path); break; }
                    Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => continue,
                    Err(error) => { result = Err(error); break; }
                }
            }
            let _ = this.update_in(cx, |this, window, cx| {
                this.creating = false;
                match result {
                    Ok(path) => {
                        this.notice.clear();
                        this.tree.update(cx, |tree, cx| {
                            tree.insert_note(path.clone(), cx);
                            tree.scroll_to(&path);
                        });
                        this.open_file_in_new_tab(path.clone(), cx);
                        this.start_blank_rename(path, window, cx);
                    }
                    Err(error) => this.notice = format!("{}: {error}", tr("新建失败", "Failed to create")),
                }
                this.refresh(cx);
            });
        }).detach();
    }
}

/// Width persistence is fire-and-forget; a failure only loses the preference.
fn update_state_width(width: Option<f32>, cx: &mut Context<Workspace>) {
    cx.background_executor().spawn(async move { let _ = crate::files::update_state(|state| state.sidebar_width = width); }).detach();
}
