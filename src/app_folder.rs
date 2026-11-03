use gpui::{Context, PathPromptOptions, Window};
use super::*;

impl Workspace {
    pub(super) fn use_folder(&mut self, folder: PathBuf, cx: &mut Context<Self>) {
        self.use_folder_guarded(folder, None, cx);
    }

    pub(super) fn use_folder_guarded(&mut self, folder: PathBuf, startup_epoch: Option<u64>, cx: &mut Context<Self>) {
        if self.busy() { return; }
        if self.restore_superseded(startup_epoch, cx) { return; }
        self.loading = true;
        self.notice = format!("正在扫描 {}…", folder.display());
        self.refresh(cx);
        // Only one request is admitted; switching/creating stays disabled until it settles.
        cx.spawn(async move |this, cx| {
            let result = cx.background_executor().spawn(async move {
                let scan = scan(&folder)?;
                let notice = if scan.warning_count > 0 {
                    format!("部分目录未能读取（{} 项）：{}", scan.warning_count, scan.warnings.join("；"))
                } else if scan.nodes.is_empty() { "此文件夹没有 Markdown 笔记或可浏览的子目录".into() }
                else { "选择笔记开始编辑".into() };
                Ok::<_, std::io::Error>(PreparedFolder { path: folder, index: TreeIndex::new(scan.nodes), notice })
            }).await;
            let _ = this.update(cx, |this, cx| {
                this.loading = false;
                if this.restore_superseded(startup_epoch, cx) {
                    cx.background_executor().spawn(async move { drop(result); }).detach();
                    return;
                }
                match result {
                    Err(error) => { this.notice = format!("无法打开文件夹：{error}"); this.refresh(cx); }
                    Ok(prepared) => {
                        this.pending = Some(prepared);
                        let closed = this.editor.update(cx, |editor, cx| editor.close(cx));
                        if closed { this.commit_folder(cx); }
                        else if !this.editor.read(cx).is_busy() {
                            this.discard_pending(cx);
                            this.notice = "目录未切换：当前笔记未能保存，请处理编辑器中的错误后重试".into();
                            this.refresh(cx);
                        } else {
                            this.notice = "正在保存当前笔记，完成后切换文件夹…".into();
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
            self.notice = "已保留当前编辑操作；需要时可重新选择文件夹".into();
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
        self.selected = None;
        self.notice = prepared.notice;
        self.tree.update(cx, |tree, cx| tree.set_index(prepared.index, cx));
        self.persisting = true;
        self.refresh(cx);
        cx.spawn(async move |this, cx| {
            let saved = cx.background_executor().spawn(async move { save_folder(&folder) }).await;
            let _ = this.update(cx, |this, cx| {
                this.persisting = false;
                if let Err(error) = saved {
                    this.notice = format!("{}；文件夹已打开，但偏好未能保存：{error}", this.notice);
                }
                this.refresh(cx);
            });
        }).detach();
    }

    pub(super) fn choose_folder(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.busy() { return; }
        self.choosing = true;
        self.refresh(cx);
        let prompt = cx.prompt_for_paths(PathPromptOptions {
            files: false, directories: true, multiple: false, prompt: Some("选择文件夹".into()),
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
                    Ok(Err(error)) => this.notice = format!("无法选择文件夹：{error}"),
                    Err(error) => this.notice = format!("选择文件夹已中断：{error}"),
                }
                this.refresh(cx);
            });
        }).detach();
    }
}
