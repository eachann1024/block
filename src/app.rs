//! Workspace transitions commit only after scanning and editor close succeed.
use std::path::PathBuf;
use gpui::{AppContext, Context, Focusable, Window};
use crate::editor::view::Editor;
use crate::files::{create_note_at, note_candidates, load_folder, save_folder, scan, index::TreeIndex, tree::FileTree};

#[path = "app_view.rs"]
mod view;
#[path = "app_folder.rs"]
mod folder;

struct PreparedFolder { path: PathBuf, index: TreeIndex, notice: String }

pub struct Workspace {
    folder: Option<PathBuf>,
    tree: gpui::Entity<FileTree>,
    pub(crate) editor: gpui::Entity<Editor>,
    selected: Option<PathBuf>,
    loading: bool,
    choosing: bool,
    creating: bool,
    persisting: bool,
    pending: Option<PreparedFolder>,
    notice: String,
    _editor_subscription: gpui::Subscription,
}

impl Workspace {
    pub fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let tree = cx.new(|_| FileTree::new());
        let editor = cx.new(|cx| Editor::new(cx));
        let startup_epoch = editor.read(cx).interaction_epoch();
        let workspace = cx.entity();
        tree.update(cx, |tree, _| tree.on_open(move |path, _, cx| {
            workspace.update(cx, |this, cx| {
                if !this.busy() {
                    this.editor.update(cx, |editor, cx| editor.open(path, cx));
                }
            });
        }));
        let subscription = cx.observe_in(&editor, window, |this, editor, window, cx| {
            let path = editor.read(cx).current_path().map(|path| path.to_owned());
            let busy = editor.read(cx).is_busy();
            if this.selected != path {
                this.selected = path.clone();
                this.tree.update(cx, |tree, cx| tree.select(path.clone(), cx));
                if path.is_some() { window.focus(&editor.focus_handle(cx)); }
            }
            if this.pending.is_some() && !busy {
                if path.is_none() { this.commit_folder(cx); }
                else {
                    this.discard_pending(cx);
                    this.notice = "目录未切换：当前笔记未能保存，请处理编辑器中的错误后重试".into();
                    this.refresh(cx);
                }
            }
        });
        let this = Self { folder: None, tree, editor, selected: None, loading: true,
            choosing: false, creating: false, persisting: false, pending: None,
            notice: "正在读取工作区偏好…".into(), _editor_subscription: subscription };
        cx.spawn(async move |this, cx| {
            let saved = cx.background_executor().spawn(async { load_folder() }).await;
            let _ = this.update(cx, |this, cx| {
                this.loading = false;
                match saved {
                    Ok(Some(folder)) => this.use_folder_guarded(folder, Some(startup_epoch), cx),
                    Ok(None) => { this.notice = "选择文件夹以浏览 Markdown 笔记".into(); this.refresh(cx); }
                    Err(error) => {
                        this.notice = format!("无法读取工作区偏好：{error}。请选择文件夹重试");
                        this.refresh(cx);
                    }
                }
            });
        }).detach();
        this
    }

    fn busy(&self) -> bool {
        self.loading || self.choosing || self.creating || self.persisting || self.pending.is_some()
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
        self.notice = "正在新建笔记…".into();
        self.refresh(cx);
        cx.spawn(async move |this, cx| {
            let mut result = Err(std::io::Error::new(std::io::ErrorKind::AlreadyExists, "无法分配唯一笔记名称，请重试"));
            for path in note_candidates(&folder) {
                // A deleted disk file can still own a row and an open draft.
                // Never allocate that identity to a new empty document.
                let available = this.update(cx, |this, cx| {
                    !this.tree.read(cx).contains_root_path(&path)
                        && this.editor.read(cx).current_path() != Some(path.as_path())
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
                        this.notice = "笔记已创建".into();
                        this.editor.update(cx, |editor, cx| editor.open(path, cx));
                    }
                    Err(error) => this.notice = format!("新建失败：{error}"),
                }
                this.refresh(cx);
            });
        }).detach();
    }
}
