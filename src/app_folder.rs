use gpui::{Context, PathPromptOptions, Window};
use super::*;

impl Workspace {
    pub(super) fn use_folder(&mut self, folder: PathBuf, cx: &mut Context<Self>) {
        self.use_folder_guarded(folder, None, cx);
    }

    pub(crate) fn retry_folder(&mut self, cx: &mut Context<Self>) {
        if let Some(folder) = self.folder_error.clone() {
            self.use_folder(folder, cx);
        }
    }

    pub(super) fn use_folder_guarded(&mut self, folder: PathBuf, startup_epoch: Option<u64>, cx: &mut Context<Self>) {
        if self.busy() { return; }
        if self.restore_superseded(startup_epoch, cx) { return; }
        let startup_tabs = self.startup_tabs.take();
        let failed_folder = folder.clone();
        self.loading = true;
        self.notice = format!("{} {}…", tr("正在扫描", "Scanning"), folder.display());
        self.refresh(cx);
        // Only one request is admitted; switching/creating stays disabled until it settles.
        cx.spawn(async move |this, cx| {
            let result = cx.background_executor().spawn(async move {
                let scan = scan(&folder)?;
                let notice = if scan.warning_count > 0 {
                    if crate::theme::is_en() {
                        format!("Some directories could not be read ({} items): {}", scan.warning_count, scan.warnings.join("; "))
                    } else {
                        format!("部分目录未能读取（{} 项）：{}", scan.warning_count, scan.warnings.join("；"))
                    }
                } else if scan.nodes.is_empty() { tr("此文件夹没有 Markdown 笔记或可浏览的子目录", "No Markdown notes or folders found").into() }
                else { String::new() };
                let mut tabs = Vec::new();
                let mut active_tab = None;
                if let Some((saved_tabs, saved_active)) = startup_tabs {
                    for tab in saved_tabs {
                        if tab.starts_with(&folder) && crate::files::ops::is_file(&tab) && !tabs.contains(&tab) {
                            tabs.push(tab);
                        }
                    }
                    active_tab = saved_active.filter(|a| tabs.contains(a)).or_else(|| tabs.first().cloned());
                }
                Ok::<_, std::io::Error>(PreparedFolder {
                    path: folder, index: TreeIndex::new(scan.nodes), notice, tabs, active_tab,
                })
            }).await;
            let _ = this.update(cx, |this, cx| {
                this.loading = false;
                if this.restore_superseded(startup_epoch, cx) {
                    cx.background_executor().spawn(async move { drop(result); }).detach();
                    return;
                }
                match result {
                    Err(error) => {
                        this.folder_error = Some(failed_folder);
                        this.notice = format!("{}: {error}", tr("无法打开文件夹", "Failed to open folder"));
                        this.refresh(cx);
                    }
                    Ok(prepared) => {
                        this.folder_error = None;
                        this.pending = Some(prepared);
                        let closed = this.editor.update(cx, |editor, cx| editor.close(cx));
                        if closed { this.commit_folder(cx); }
                        else if !this.editor.read(cx).is_busy() {
                            this.discard_pending(cx);
                            this.notice = tr("目录未切换：当前笔记未能保存，请处理编辑器中的错误后重试", "Folder switch canceled: could not save current note").into();
                            this.refresh(cx);
                        } else {
                            this.notice = tr("正在保存当前笔记，完成后切换文件夹…", "Saving current note before switching folder…").into();
                            this.refresh(cx);
                        }
                    }
                }
            });
        }).detach();
    }

    fn restore_superseded(&mut self, startup_epoch: Option<u64>, cx: &mut Context<Self>) -> bool {
        let superseded = startup_epoch.is_some_and(|epoch| self.editor.read(cx).interaction_epoch() != epoch);
        if superseded {
            self.notice = tr("已保留当前编辑操作；需要时可重新选择文件夹", "Kept current editing state; choose a folder again if needed").into();
            self.refresh(cx);
        }
        superseded
    }

    pub(super) fn discard_pending(&mut self, cx: &mut Context<Self>) {
        let pending = self.pending.take();
        cx.background_executor().spawn(async move { drop(pending); }).detach();
    }

    pub(super) fn commit_folder(&mut self, cx: &mut Context<Self>) {
        let Some(prepared) = self.pending.take() else { return };
        let folder = prepared.path;
        self.folder = Some(folder.clone());
        self.folder_error = None;
        self.tabs = prepared.tabs;
        self.nav.clear();
        self.selected = None;
        self.notice = prepared.notice;
        self.tree.update(cx, |tree, cx| tree.set_index(prepared.index, cx));
        if let Some(active) = prepared.active_tab {
            self.open_file(active, cx);
        } else {
            self.save_tabs_state(cx);
        }
        self.drain_external_open(cx);
        self.persisting = true;
        self.refresh(cx);
        cx.spawn(async move |this, cx| {
            let saved = cx.background_executor().spawn(async move { save_folder(&folder) }).await;
            let _ = this.update(cx, |this, cx| {
                this.persisting = false;
                match saved {
                    Ok(state) => this.notebooks = state.notebooks,
                    Err(error) => this.notice = format!("{}; {}: {error}", this.notice, tr("文件夹已打开，但偏好未能保存", "Folder opened, but preferences could not be saved")),
                }
                this.refresh(cx);
            });
        }).detach();
    }

    pub(crate) fn choose_folder(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.busy() { return; }
        #[cfg(target_family = "wasm")]
        {
            let _ = window;
            self.use_folder(PathBuf::from("/Block"), cx);
            return;
        }
        #[cfg(not(target_family = "wasm"))]
        {
        self.choosing = true;
        self.refresh(cx);
        let prompt = cx.prompt_for_paths(PathPromptOptions {
            files: false, directories: true, multiple: false, prompt: Some(tr("选择文件夹", "Choose Folder").into()),
        });
        cx.spawn_in(window, async move |this, cx| {
            let result = prompt.await;
            let _ = this.update(cx, |this, cx| {
                this.choosing = false;
                match result {
                    Ok(Ok(Some(paths))) => {
                        if let Some(folder) = paths.into_iter().next() { this.use_folder(folder, cx); }
                    }
                    Ok(Ok(None)) => {},
                    Ok(Err(error)) => this.notice = format!("{}: {error}", tr("无法选择文件夹", "Failed to choose folder")),
                    Err(error) => this.notice = format!("{}: {error}", tr("选择文件夹已中断", "Folder selection canceled")),
                }
                this.refresh(cx);
            });
        }).detach();
        }
    }

    pub(crate) fn open_external_paths(&mut self, paths: Vec<PathBuf>, cx: &mut Context<Self>) {
        for raw in paths {
            let path = std::fs::canonicalize(&raw).unwrap_or(raw);
            if (crate::files::ops::is_dir(&path) || (crate::files::ops::is_file(&path) && crate::files::scan::is_markdown(&path.to_string_lossy())))
                && !self.external_open.contains(&path)
            {
                self.external_open.push(path);
            }
        }
        if !self.loading && self.pending.is_none() {
            self.drain_external_open(cx);
        }
    }

    pub(super) fn drain_external_open(&mut self, cx: &mut Context<Self>) {
        if self.loading || self.pending.is_some() || self.external_open.is_empty() { return; }
        if self.folder.is_none() {
            if let Some(first) = self.external_open.first().cloned() {
                if crate::files::ops::is_dir(&first) {
                    self.external_open.remove(0);
                    self.use_folder(first, cx);
                    return;
                }
                if let Some(parent) = first.parent().map(std::path::Path::to_path_buf) {
                    self.use_folder(parent, cx);
                    return;
                }
            }
        }
        let queued = std::mem::take(&mut self.external_open);
        for path in queued {
            if crate::files::ops::is_dir(&path) {
                self.use_folder(path, cx);
            } else if crate::files::ops::is_file(&path) && crate::files::scan::is_markdown(&path.to_string_lossy()) {
                self.insert_external_note(path.clone(), cx);
                self.open_file_in_new_tab(path, cx);
            }
        }
    }
}
