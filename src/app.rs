//! Workspace transitions commit only after scanning and editor close succeed.
use std::path::PathBuf;
use gpui::{AppContext, Context, Focusable, Window};
use crate::editor::view::Editor;
use crate::theme::{Theme, tr};
use crate::files::{create_note_at, note_candidates, load_state, save_folder, scan, index::TreeIndex, tree::FileTree};

#[path = "app_view.rs"]
mod view;
#[path = "app_tabs.rs"]
mod tabs;
#[path = "app_folder.rs"]
mod folder;
#[path = "app_sidebar.rs"]
mod sidebar;
#[path = "app_files.rs"]
mod file_ops;

#[path = "app_menu.rs"]
mod menu;
#[path = "app_search.rs"]
mod search;
#[path = "app_settings.rs"]
mod settings;
#[path = "app_settings_view.rs"]
mod settings_view;

use file_ops::{FileOp, Renaming, TreeMenu};
pub use search::{Dismiss, NewNote, OpenSearch, OpenSettings, SearchDown, SearchUp};
pub use tabs::{CloseTab, NewTab, NextTab, PrevTab};
use search::Search;
use settings::SettingsModal;

struct PreparedFolder { path: PathBuf, index: TreeIndex, notice: String, tabs: Vec<PathBuf>, active_tab: Option<PathBuf> }

pub struct Workspace {
    pub(crate) folder: Option<PathBuf>,
    pub(crate) folder_error: Option<PathBuf>,
    pub(crate) tree: gpui::Entity<FileTree>,
    pub(crate) editor: gpui::Entity<Editor>,
    selected: Option<PathBuf>,
    tabs: Vec<PathBuf>,
    startup_tabs: Option<(Vec<PathBuf>, Option<PathBuf>)>,
    dirty_bits: u64,
    pub(crate) loading: bool,
    choosing: bool,
    creating: bool,
    persisting: bool,
    pending: Option<PreparedFolder>,
    notice: String,
    sidebar_width: gpui::Pixels,
    pub(crate) sidebar_collapsed: bool,
    /// (pointer x at press, width at press) while the edge is dragged.
    resizing: Option<(gpui::Pixels, gpui::Pixels)>,
    notebooks: Vec<PathBuf>,
    switcher_open: bool,
    menu: Option<TreeMenu>,
    renaming: Option<Renaming>,
    search: Option<Search>,
    settings: Option<SettingsModal>,
    /// A disk change that waits for the editor to release the affected note.
    file_op: Option<FileOp>,
    working: bool,
    /// Keeps the Workspace key context alive when nothing else has focus.
    root_focus: gpui::FocusHandle,
    _editor_subscription: gpui::Subscription,
}

impl Workspace {
    pub fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let tree = cx.new(FileTree::new);
        let editor = cx.new(|cx| Editor::new(cx));
        let startup_epoch = editor.read(cx).interaction_epoch();
        let workspace = cx.entity();
        let context_target = cx.entity();
        let rename_target = cx.entity();
        let trash_target = cx.entity();
        tree.update(cx, |tree, _| tree.on_context(move |target, position, _, cx| {
            // The tree is mid-update while dispatching; defer so the menu may mark its row.
            let context_target = context_target.clone();
            cx.defer(move |cx| context_target.update(cx, |this, cx| this.open_tree_menu(target, position, cx)));
        }));
        tree.update(cx, |tree, _| tree.on_open(move |path, window, cx| {
            workspace.update(cx, |this, cx| {
                if !this.busy() {
                    this.close_settings(window, cx);
                    this.open_file(path, cx);
                }
            });
        }));
        tree.update(cx, |tree, _| tree.on_rename(move |(path, dir), window, cx| {
            rename_target.update(cx, |this, cx| if !this.busy() { this.start_rename(path, dir, window, cx); });
        }));
        tree.update(cx, |tree, _| tree.on_trash(move |path, _, cx| {
            trash_target.update(cx, |this, cx| if !this.busy() { this.trash(path, cx); });
        }));
        let subscription = cx.observe_in(&editor, window, |this, editor, window, cx| {
            let path = editor.read(cx).current_path().map(|path| path.to_owned());
            let loading = editor.read(cx).is_loading();
            let busy = editor.read(cx).is_busy();
            if !loading && this.pending.is_none() && this.file_op.is_none() && this.selected != path {
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
                    editor.read(cx).current_path().is_some_and(|cur| cur.starts_with(p))
                        || this.tabs.iter().any(|t| t.starts_with(p) && editor.read(cx).is_path_dirty(t))
                });
                if !blocked { this.run_file_op(cx); }
                else {
                    this.file_op = None;
                    this.notice = tr("操作已取消：当前笔记未能保存，请处理编辑器中的错误后重试", "Operation canceled: could not save current note").into();
                    this.refresh(cx);
                }
            }
        });
        // The export view lives in a global slot (opened from the editor); re-render on open/close.
        cx.observe_global::<crate::export::ExportSlot>(|_, cx| cx.notify()).detach();
        crate::dropdown::register_workspace(&cx.entity());
        cx.observe_window_activation(window, |this, window, cx| {
            if !window.is_window_active() {
                this.dismiss_dropdowns(cx);
                crate::dropdown::dismiss_all(cx);
            }
        }).detach();
        search::bind_keys(cx);
        tabs::bind_keys(cx);
        crate::files::tree::bind_keys(cx);
        let this = Self { folder: None, folder_error: None, tree, editor, selected: None, tabs: Vec::new(), startup_tabs: None, dirty_bits: 0, loading: true,
            choosing: false, creating: false, persisting: false, pending: None,
            notice: tr("正在读取工作区偏好…", "Loading workspace preferences…").into(), sidebar_width: Theme::ocean().sidebar_width,
            sidebar_collapsed: false, resizing: None, notebooks: Vec::new(), switcher_open: false, menu: None, renaming: None, search: None,
            settings: None, file_op: None, working: false, root_focus: cx.focus_handle(),
            _editor_subscription: subscription };
        window.focus(&this.root_focus, cx);
        cx.spawn(async move |this, cx| {
            let saved = cx.background_executor().spawn(async { load_state() }).await;
            let _ = this.update(cx, |this, cx| {
                this.loading = false;
                if let Ok(state) = &saved {
                    this.notebooks = state.notebooks.clone();
                    this.startup_tabs = Some((state.open_tabs.clone(), state.active_tab.clone()));
                    if let Some(width) = state.sidebar_width { this.sidebar_width = sidebar::clamp_width(gpui::px(width)); }
                }
                match saved.map(|state| state.folder) {
                    Ok(Some(folder)) => this.use_folder_guarded(folder, Some(startup_epoch), cx),
                    Ok(None) => { this.notice = tr("选择文件夹以浏览 Markdown 笔记", "Select a folder to browse Markdown notes").into(); this.refresh(cx); }
                    Err(error) => {
                        this.notice = format!("{}: {error}", tr("无法读取工作区偏好", "Failed to read workspace preferences"));
                        this.refresh(cx);
                    }
                }
            });
        }).detach();
        this
    }

    pub(crate) fn busy(&self) -> bool {
        self.loading || self.choosing || self.creating || self.persisting || self.pending.is_some()
            || self.file_op.is_some() || self.working
    }

    fn refresh(&mut self, cx: &mut Context<Self>) {
        let enabled = !self.busy();
        self.tree.update(cx, |tree, cx| tree.set_enabled(enabled, cx));
        cx.notify();
    }

    fn new_note(&mut self, cx: &mut Context<Self>) {
        if self.busy() { return; }
        let Some(folder) = self.folder.clone() else { return };
        self.creating = true;
        self.notice = tr("正在新建笔记…", "Creating note…").into();
        self.refresh(cx);
        cx.spawn(async move |this, cx| {
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
            let _ = this.update(cx, |this, cx| {
                this.creating = false;
                match result {
                    Ok(path) => {
                        this.tree.update(cx, |tree, cx| tree.insert_note(path.clone(), cx));
                        let created_msg = tr("笔记已创建", "Note created");
                        this.notice = created_msg.into();
                        this.open_file(path, cx);
                        // Success is transient; errors and folder warnings stay until replaced.
                        cx.spawn(async move |this, cx| {
                            cx.background_executor().timer(std::time::Duration::from_secs(3)).await;
                            let _ = this.update(cx, |this, cx| {
                                if this.notice == created_msg { this.notice.clear(); this.refresh(cx); }
                            });
                        }).detach();
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
    cx.background_executor().spawn(async move {
        let _ = crate::files::update_state(|state| state.sidebar_width = width);
    }).detach();
}
