//! File-tree operations: menu state, inline rename, and disk changes that
//! first release the open note so the editor never writes a moved path.
use std::path::{Path, PathBuf};
use gpui::{AppContext, Context, Focusable, Pixels, Point, Window};
use gpui_component::input::{InputEvent, InputState};
use crate::files::{create_note, ops, scan, index::TreeIndex};
use crate::theme::tr;
use super::Workspace;

#[derive(Clone)]
pub(crate) struct TreeMenu { pub target: (PathBuf, bool), pub position: Point<Pixels>, pub moving: bool }
pub(crate) struct Renaming { pub path: PathBuf, pub dir: bool, pub input: gpui::Entity<InputState>, _sub: gpui::Subscription }
pub(crate) enum FileOp {
    Trash { path: PathBuf },
    Rename { path: PathBuf, name: String, dir: bool, reopen: Option<PathBuf> },
    Move { path: PathBuf, dest: PathBuf, reopen: Option<PathBuf> },
}

impl FileOp {
    pub(crate) fn source_path(&self) -> &Path {
        match self { Self::Trash { path, .. } | Self::Rename { path, .. } | Self::Move { path, .. } => path }
    }
}

pub(crate) fn display_name(path: &Path, dir: bool) -> String {
    let full = path.file_name().unwrap_or(path.as_os_str()).to_string_lossy().into_owned();
    if dir || !crate::files::scan::is_markdown(&full) { return full; }
    path.file_stem().unwrap_or(path.as_os_str()).to_string_lossy().into_owned()
}

impl Workspace {
    pub(super) fn open_tree_menu(&mut self, target: Option<(PathBuf, bool)>, position: Point<Pixels>, cx: &mut Context<Self>) {
        if self.busy() || self.folder.is_none() { return; }
        self.switcher_open = false;
        let Some(target) = target else { self.close_overlays(cx); return };
        self.tree.update(cx, |tree, cx| tree.set_menu_target(Some(target.0.clone()), cx));
        self.menu = Some(TreeMenu { target, position, moving: false });
        cx.notify();
    }

    pub(super) fn close_overlays(&mut self, cx: &mut Context<Self>) {
        self.menu = None; self.switcher_open = false;
        self.tree.update(cx, |tree, cx| tree.set_menu_target(None, cx));
        cx.notify();
    }

    pub(crate) fn dismiss_dropdowns(&mut self, cx: &mut Context<Self>) -> bool {
        let had_overlay = self.menu.is_some() || self.switcher_open;
        if had_overlay { self.close_overlays(cx); }
        let closed_tabs = self.dismiss_tab_menu(cx); let closed_font = self.close_font_picker(cx); had_overlay || closed_tabs || closed_font
    }

    /// Escape: close the innermost transient surface; false when none was open.
    pub(super) fn dismiss(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        if crate::performance::close() { cx.notify(); return true; }
        if crate::feedback::close(window, cx) || crate::about::close() {
            self.restore_focus(window, cx); cx.notify(); return true;
        }
        if crate::export::close_active(window, cx) || self.history_escape(window, cx) || self.close_font_picker(cx) { return true; }
        if self.dismiss_tab_menu(cx) { self.restore_focus(window, cx); return true; }
        if self.menu.is_some() || self.switcher_open { self.close_overlays(cx); self.restore_focus(window, cx); return true; }
        if self.renaming.is_some() { self.cancel_rename(window, cx); return true; }
        if let Some(s) = &mut self.search {
            if s.state.scope_menu_open { s.state.scope_menu_open = false; cx.notify(); } else { self.close_search(window, cx); }
            return true;
        }
        if self.shell_split(window, cx).sidebar_drawer {
            self.toggle_sidebar(window, cx);
            return true;
        }
        if self.settings.is_some() {
            if !self.end_shortcut_recording(cx) { self.close_settings(window, cx); }
            return true;
        }
        false
    }

    pub(crate) fn restore_focus(&self, window: &mut Window, cx: &mut Context<Self>) {
        if self.editor.read(cx).current_path().is_some() {
            let focus = self.editor.read(cx).focus_handle(cx);
            window.focus(&focus, cx);
        } else {
            window.focus(&self.root_focus, cx);
        }
    }

    pub(super) fn start_rename(&mut self, path: PathBuf, dir: bool, window: &mut Window, cx: &mut Context<Self>) {
        self.close_overlays(cx);
        self.renaming = None;
        let name = display_name(&path, dir);
        let input = cx.new(|cx| {
            let mut s = InputState::new(window, cx).default_value(name);
            s.focus(window, cx);
            s.select_all(window, cx);
            s
        });
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| match event {
            InputEvent::Change => this.tree.update(cx, |_, cx| cx.notify()),
            InputEvent::PressEnter { .. } => this.commit_rename(window, cx),
            InputEvent::Blur if this.renaming.is_some() => {
                this.renaming = None;
                this.tree.update(cx, |tree, cx| tree.set_renaming(None, cx));
                cx.notify();
            }
            _ => {}
        });
        self.tree.update(cx, |tree, cx| tree.set_renaming(Some((path.clone(), input.clone())), cx));
        self.renaming = Some(Renaming { path, dir, input, _sub: sub });
        cx.notify();
    }

    pub(super) fn cancel_rename(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.renaming.take().is_none() { return; }
        self.tree.update(cx, |tree, cx| tree.set_renaming(None, cx));
        let focus = self.tree.read(cx).focus_handle().clone();
        window.focus(&focus, cx);
        cx.notify();
    }

    pub(super) fn commit_rename(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(state) = &self.renaming else { return };
        let (path, dir) = (state.path.clone(), state.dir);
        let name = state.input.read(cx).value().trim().to_string();
        if name == display_name(&path, dir) { self.cancel_rename(window, cx); return; }
        // Reject empty or duplicate names here so the input stays open for a fix.
        if let Err(error) = ops::rename_target(&path, &name, dir) {
            self.flash(format!("{}: {error}", tr("无法重命名", "Cannot rename")), cx);
            return;
        }
        self.renaming = None;
        self.tree.update(cx, |tree, cx| tree.finish_rename(&path, name.clone().into(), cx));
        let focus = self.tree.read(cx).focus_handle().clone();
        window.focus(&focus, cx);
        let reopen = self.open_under(&path, cx);
        self.start_file_op(FileOp::Rename { path, name, dir, reopen }, cx);
    }

    fn open_under(&self, path: &Path, cx: &Context<Self>) -> Option<PathBuf> {
        self.editor.read(cx).current_path().filter(|open| open.starts_with(path)).map(Path::to_path_buf)
    }

    fn has_tab_under(&self, path: &Path, cx: &Context<Self>) -> bool {
        self.tabs.iter().any(|t| t.starts_with(path))
            || self.editor.read(cx).current_path().is_some_and(|p| p.starts_with(path))
    }

    pub(super) fn trash(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        self.close_overlays(cx);
        self.start_file_op(FileOp::Trash { path }, cx);
    }

    pub(super) fn move_item(&mut self, path: PathBuf, dest: PathBuf, cx: &mut Context<Self>) {
        self.close_overlays(cx);
        let reopen = self.open_under(&path, cx);
        self.start_file_op(FileOp::Move { path, dest, reopen }, cx);
    }

    fn start_file_op(&mut self, op: FileOp, cx: &mut Context<Self>) {
        if self.busy() { return; }
        let target = op.source_path().to_path_buf();
        let has_tabs = self.has_tab_under(&target, cx);
        let is_trash = matches!(op, FileOp::Trash { .. });
        self.file_op = Some(op);
        if !has_tabs { self.run_file_op(cx); return; }
        let ready = self.editor.update(cx, |editor, cx| {
            if is_trash { editor.release_under(&target, cx) } else { editor.flush_under(&target, cx) }
        });
        if ready { self.run_file_op(cx); }
        else if !self.editor.read(cx).is_busy() {
            self.file_op = None;
            self.flash(tr("操作已取消：当前笔记未能保存，请处理编辑器中的错误后重试", "Operation canceled: could not save current note").into(), cx);
        } else {
            self.notice = tr("正在保存当前笔记…", "Saving current note…").into();
        }
        self.refresh(cx);
    }

    pub(super) fn run_file_op(&mut self, cx: &mut Context<Self>) {
        let Some(op) = self.file_op.take() else { return };
        let Some(folder) = self.folder.clone() else { return };
        if let FileOp::Trash { path } = &op {
            if let Some(open) = self.remove_tabs_under(path, cx)
                && self.editor.read(cx).current_path() != Some(open.as_path())
            {
                self.open_file(open, cx);
            }
        }
        self.working = true;
        self.refresh(cx);
        cx.spawn(async move |this, cx| {
            let result = cx.background_executor().spawn(async move {
                let (notice, renamed, open, expand) = match op {
                    FileOp::Trash { path, .. } => {
                        ops::move_to_trash(&path)?;
                        (ops::trashed_notice(), None, None, None)
                    }
                    FileOp::Rename { path, name, dir, reopen } => {
                        let target = ops::rename(&path, &name, dir)?;
                        let open = reopen.and_then(|open| ops::rebase(&open, &path, &target));
                        (tr("已重命名", "Renamed"), Some((path, target)), open, None)
                    }
                    FileOp::Move { path, dest, reopen } => {
                        let target = ops::move_to(&path, &dest)?;
                        let expand = (dest != folder).then_some(dest);
                        let open = reopen.and_then(|open| ops::rebase(&open, &path, &target));
                        (tr("已移动", "Moved"), Some((path, target)), open, expand)
                    }
                };
                let index = TreeIndex::new(scan(&folder)?.nodes);
                Ok::<_, std::io::Error>((notice, renamed, open, expand, index))
            }).await;
            let _ = this.update(cx, |this, cx| {
                this.working = false;
                match result {
                    Ok((notice, renamed, open, expand, index)) => {
                        let reb = renamed.as_ref().map(|(f, t)| (f.as_path(), t.as_path()));
                        this.tree.update(cx, |tree, cx| tree.replace_index(index, expand, reb, cx));
                        if let Some((from, to)) = renamed {
                            this.reconcile_tabs_on_rename(&from, &to, cx);
                        }
                        if let Some(open) = open
                            && this.editor.read(cx).current_path() != Some(open.as_path())
                        {
                            this.open_file(open, cx);
                        }
                        this.flash(notice.into(), cx);
                    }
                    Err(error) => { crate::log::error(&format!("file operation failed: {error}")); this.flash(format!("{}: {error}", tr("操作失败", "Operation failed")), cx); }
                }
                this.refresh(cx);
            });
        }).detach();
    }

    /// Folder for new items: the selected folder row, else the open note's
    /// folder, else the notebook root (None).
    fn target_dir(&self, cx: &Context<Self>) -> Option<PathBuf> {
        let root = self.folder.as_ref()?;
        if let Some(dir) = self.tree.read(cx).focus_dir() { return Some(dir.to_path_buf()); }
        let parent = self.selected.as_ref()?.parent()?;
        (parent != root.as_path()).then(|| parent.to_path_buf())
    }

    pub(crate) fn new_item(&mut self, folder_kind: bool, window: &mut Window, cx: &mut Context<Self>) {
        let parent = self.target_dir(cx);
        self.create_in(parent, folder_kind, window, cx);
    }

    /// Create inside `parent` (a folder row) or at the notebook root.
    pub(super) fn create_in(&mut self, parent: Option<PathBuf>, folder_kind: bool, window: &mut Window, cx: &mut Context<Self>) {
        self.close_overlays(cx);
        if self.busy() { return; }
        let Some(root) = self.folder.clone() else { return };
        if parent.is_none() && !folder_kind { self.new_note(cx); return; }
        let dir = parent.clone().unwrap_or_else(|| root.clone());
        self.working = true;
        self.refresh(cx);
        cx.spawn_in(window, async move |this, cx| {
            let result = cx.background_executor().spawn(async move {
                let created = if folder_kind { ops::create_folder(&dir)? } else { create_note(&dir)? };
                let index = TreeIndex::new(scan(&root)?.nodes);
                Ok::<_, std::io::Error>((created, index))
            }).await;
            let _ = this.update_in(cx, |this, window, cx| {
                this.working = false;
                match result {
                    Ok((created, index)) => {
                        this.tree.update(cx, |tree, cx| tree.replace_index(index, parent, None, cx));
                        this.refresh(cx);
                        if folder_kind {
                            this.tree.update(cx, |tree, _| tree.scroll_to(&created));
                            this.start_rename(created, true, window, cx);
                        } else {
                            this.open_file_in_new_tab(created, cx);
                            this.flash(tr("笔记已创建", "Note created").into(), cx);
                        }
                    }
                    Err(error) => { crate::log::error(&format!("failed to create item: {error}")); this.flash(format!("{}: {error}", tr("新建失败", "Failed to create")), cx); this.refresh(cx); }
                }
            });
        }).detach();
    }

    pub(crate) fn insert_external_note(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        if crate::theme::hide_non_markdown_files()
            && !path.file_name().is_some_and(|n| crate::files::scan::is_markdown(&n.to_string_lossy()))
        {
            return;
        }
        if self.folder.as_deref().is_some_and(|root| path.starts_with(root)) {
            self.tree.update(cx, |tree, cx| tree.insert_note(path, cx));
        }
    }

    pub(super) fn flash(&mut self, notice: String, cx: &mut Context<Self>) {
        self.notice = notice.clone();
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(std::time::Duration::from_secs(3)).await;
            let _ = this.update(cx, |this, cx| {
                if this.notice == notice { this.notice.clear(); this.refresh(cx); }
            });
        }).detach();
        cx.notify();
    }
}
