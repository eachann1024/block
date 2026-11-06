//! The 12 scripted smoke steps: editor CRUD, in-note find, global search, tabs and settings.
use gpui::{point, px};
use super::{AsyncApp, Run, sleep, smoke_overlay};

impl Run {
    /// Bold serializes as `<strong>` (inline HTML) today; accept the `**` form too.
    fn bold_on(&self, cx: &AsyncApp) -> bool {
        let md = self.editor(cx, |e| e.markdown_text());
        md.contains("**TaskOne**") || md.contains("<strong>TaskOne</strong>")
    }

    pub(super) async fn all_steps(&self, cx: &mut AsyncApp) -> Result<(), String> {
        let cx = &*cx;
        let ready = self.wait_until(cx, 4000, |w, _| !w.loading && !w.busy() && w.folder.is_some()).await;
        if !ready { return Err("工作区未能在 4 秒内就绪".into()); }
        sleep(cx, 350).await;
        self.new_note(cx).await?;
        self.heading(cx).await?;
        self.paragraph(cx).await?;
        self.todo(cx).await?;
        self.todo_toggle(cx).await?;
        self.bold(cx).await?;
        self.undo_redo(cx).await?;
        self.menu_positions(cx).await?;
        self.backspace(cx).await?;
        self.find(cx).await?;
        self.annotations(cx).await?;
        self.global_search(cx).await?;
        self.tabs_and_sidebar(cx).await?;
        self.settings(cx).await?;
        self.code_block_open(cx).await?;
        self.chaoshan_paste_and_hotkey(cx).await?;
        self.image_menu_positions(cx).await?;
        self.performance(cx).await?;
        self.ai_panel_and_deck(cx).await
    }

    async fn chaoshan_paste_and_hotkey(&self, cx: &AsyncApp) -> Result<(), String> {
        self.hud(cx, "Step 13 · 验证潮汕之旅.md 在编辑器与小窗完整粘贴及快捷键防抖");
        let editor = self.query(cx, |w, _| w.editor.clone());
        self.win(cx, move |window, cx| {
            editor.update(cx, |e, cx| e.verify_chaoshan_paste_smoke(window, cx))
        })??;
        // Restore code block note state for subsequent steps if needed
        let _ = crate::quick::mark_quick_triggered();
        let debounced_quick = !crate::quick::mark_quick_triggered();
        let _ = crate::quick::mark_search_triggered();
        let debounced_search = !crate::quick::mark_search_triggered();
        self.check(13, debounced_quick && debounced_search, "小窗与搜索快捷键在主窗口隐藏/激活态下防重复触发")
    }

    async fn ai_panel_and_deck(&self, cx: &AsyncApp) -> Result<(), String> {
        self.click_target(cx, "titlebar-ai", "Step 14/14 · 点击顶栏 AI 图标打开面板").await?;
        self.check(14, self.query(cx, |_, cx| crate::editor::ai::chat::is_open(cx)), "顶栏 AI 图标已打开 AI 侧栏")?;
        self.check(14, smoke_overlay::target_center("ai-enable-cover").is_some() && smoke_overlay::target_center("ai-layout-toggle").is_none() && smoke_overlay::target_center("ai-deck-trigger").is_none(), "未启用 AI 时居中显示封面且顶栏与底栏控件已隐藏")?;
        self.click_target(cx, "ai-enable-cover", "Step 14/14 · 点击未启用 AI 封面跳转设置").await?;
        self.check(14, self.query(cx, |w, _| w.settings_is_open()), "点击封面已跳转到 AI 设置页")?;
        self.click_target(cx, "settings-back", "Step 14/14 · 从 AI 设置返回编辑").await?;
        self.win(cx, crate::editor::ai::chat::enable_smoke_builtin)??;
        sleep(cx, 250).await;
        self.redraw(cx);
        self.click_target(cx, "ai-empty-polish", "Step 14/14 · 点击页隼空状态快捷指令").await?;
        self.click_target(cx, "ai-layout-toggle", "Step 14/14 · 切换 AI 全屏沉浸模式").await?;
        self.check(14, self.query(cx, |_, cx| crate::editor::ai::chat::is_fullscreen(cx)), "已切换至 AI 全屏沉浸模式")?;
        self.click_target(cx, "ai-layout-toggle", "Step 14/14 · 切回 AI 侧栏模式").await?;
        self.check(14, self.query(cx, |_, cx| !crate::editor::ai::chat::is_fullscreen(cx)), "已切回 AI 侧栏模式")?;
        self.verify_backends_and_settings(cx).await?;
        self.click_target(cx, "ai-deck-trigger", "Step 14/14 · 打开底部合一 Agent 命令舱").await?;
        self.click_target(cx, "ai-backend-Dsh", "Step 14/14 · 切换 dsh 并展开原位配置").await?;
        self.click_target(cx, "dsh-auth-block", "Step 14/14 · 选择复用 Block 配置").await?;
        self.click_target(cx, "ai-backend-Builtin", "Step 14/14 · 切回内置 AI 后端").await?;
        self.click_target(cx, "ai-deck-trigger", "Step 14/14 · 收起合一 Agent 命令舱").await?;
        self.click_target(cx, "ai-chat-history", "Step 14/14 · 展开顶栏历史会话列表").await?;
        self.click_target(cx, "ai-chat-history", "Step 14/14 · 收起顶栏历史会话列表").await?;
        self.chat_selection(cx).await?;
        self.chat_composer(cx).await?;
        self.click_target(cx, "ai-chat-close", "Step 14/14 · 关闭 AI 面板").await?;
        self.check(14, self.query(cx, |_, cx| !crate::editor::ai::chat::is_open(cx)), "AI 面板已关闭")
    }

    /// Opening a note whose first paint includes a code block used to re-enter Editor and abort.
    async fn code_block_open(&self, cx: &AsyncApp) -> Result<(), String> {
        let bounds = smoke_overlay::target_bounds("editor").ok_or("找不到点击目标 editor")?;
        let spot = point(bounds.center().x - px(120.), bounds.top() + px(220.));
        self.click_at(cx, spot, "Step 13/13 · 打开带代码块的笔记").await?;
        self.press(cx, "enter").await?;
        self.press(cx, "/").await?;
        sleep(cx, 260).await;
        self.check(13, self.editor(cx, |e| e.slash_menu_open()), "代码块斜杠菜单已显示")?;
        self.type_text(cx, "code").await?;
        sleep(cx, 150).await;
        self.press(cx, "enter").await?;
        sleep(cx, 250).await;
        let md = self.editor(cx, |e| e.markdown_text());
        println!("[SMOKE13] {md:?}");
        self.check(13, md.contains("```"), "代码块笔记渲染后仍可读取")
    }

    async fn new_note(&self, cx: &AsyncApp) -> Result<(), String> {
        self.click_target(cx, "new-note", "Step 1/12 · 新建笔记").await?;
        let opened = self.wait_until(cx, 2000, |w, _| !w.busy() && w.tab_count() >= 2).await;
        self.check(1, opened, "新建笔记并打开新标签页")?;
        sleep(cx, 250).await;
        Ok(())
    }

    async fn heading(&self, cx: &AsyncApp) -> Result<(), String> {
        let bounds = smoke_overlay::target_bounds("editor").ok_or("找不到点击目标 editor")?;
        let spot = point(bounds.center().x - px(120.), bounds.top() + px(96.));
        self.click_at(cx, spot, "Step 2/12 · 斜杠菜单建一级标题").await?;
        self.press(cx, "/").await?;
        sleep(cx, 260).await;
        self.check(2, self.editor(cx, |e| e.slash_menu_open()), "斜杠菜单已显示")?;
        for key in ["down", "enter"] { self.press(cx, key).await?; }
        self.type_text(cx, "CRUD Title").await?;
        self.check(2, self.editor(cx, |e| e.markdown_text()).contains("# CRUD Title"), "一级标题已写入文档")
    }

    async fn paragraph(&self, cx: &AsyncApp) -> Result<(), String> {
        self.hud(cx, "Step 3/12 · 回车新建段落并输入正文");
        self.press(cx, "enter").await?;
        self.type_text(cx, "Alpha Beta").await?;
        let (blocks, text) = self.editor(cx, |e| (e.block_count(), e.markdown_text()));
        self.check(3, blocks >= 2 && text.contains("Alpha Beta"), "回车新建段落并输入正文")
    }

    async fn todo(&self, cx: &AsyncApp) -> Result<(), String> {
        self.hud(cx, "Step 4/12 · 斜杠菜单建待办清单");
        self.press(cx, "enter").await?;
        self.press(cx, "/").await?;
        sleep(cx, 220).await;
        self.type_text(cx, "todo").await?;
        sleep(cx, 150).await;
        self.press(cx, "enter").await?;
        self.type_text(cx, "TaskOne").await?;
        self.check(4, self.editor(cx, |e| e.has_todo_checked(false)), "待办块已创建且未勾选")
    }

    async fn todo_toggle(&self, cx: &AsyncApp) -> Result<(), String> {
        let label = "Step 5/12 · 点击待办复选框切换状态";
        sleep(cx, 150).await;
        self.click_target(cx, "todo-checkbox", label).await?;
        self.check(5, self.editor(cx, |e| e.has_todo_checked(true)), "点击后待办已勾选")?;
        self.click_target(cx, "todo-checkbox", label).await?;
        self.check(5, self.editor(cx, |e| e.has_todo_checked(false)), "再次点击后待办取消勾选")?;
        self.click_target(cx, "todo-checkbox", label).await?;
        self.check(5, self.editor(cx, |e| e.has_todo_checked(true)), "最后保持勾选")
    }

    async fn bold(&self, cx: &AsyncApp) -> Result<(), String> {
        self.hud(cx, "Step 6/12 · 选中文本并用浮动工具栏加粗");
        self.press(cx, "cmd-down").await?;
        for _ in 0..7 { self.press(cx, "shift-left").await?; }
        sleep(cx, 220).await;
        self.check(6, self.editor(cx, |e| e.has_selection_toolbar()), "选中文本后浮动工具栏出现")?;
        self.click_target(cx, "toolbar-bold", "Step 6/12 · 点击加粗").await?;
        self.check(6, self.bold_on(cx), "文本已加粗")
    }

    async fn undo_redo(&self, cx: &AsyncApp) -> Result<(), String> {
        self.hud(cx, "Step 7/12 · 撤销与重做");
        sleep(cx, 200).await;
        self.press(cx, "cmd-z").await?;
        sleep(cx, 200).await;
        self.check(7, !self.bold_on(cx), "撤销后加粗消失")?;
        self.press(cx, "cmd-shift-z").await?;
        sleep(cx, 200).await;
        self.check(7, self.bold_on(cx), "重做后加粗恢复")
    }

    async fn backspace(&self, cx: &AsyncApp) -> Result<(), String> {
        self.hud(cx, "Step 8/12 · 退格删字与合并删块");
        self.press(cx, "cmd-down").await?;
        let before = self.editor(cx, |e| e.block_count());
        self.press(cx, "enter").await?;
        self.type_text(cx, "TempDel").await?;
        self.check(8, self.editor(cx, |e| e.markdown_text()).contains("TempDel"), "临时行已输入")?;
        // 7 letters, then the empty block may first turn into a paragraph before it merges back.
        for _ in 0..7 { self.press(cx, "backspace").await?; }
        for _ in 0..3 {
            if self.editor(cx, |e| e.block_count()) == before { break; }
            self.press(cx, "backspace").await?;
        }
        let (blocks, text) = self.editor(cx, |e| (e.block_count(), e.markdown_text()));
        self.check(8, blocks == before && !text.contains("TempDel"), "退格删除文字并合并回上一块")?;
        self.press(cx, "cmd-up").await?;
        sleep(cx, 120).await;
        let blocks_before_insert = self.editor(cx, |e| e.block_count());
        self.press(cx, "enter").await?;
        sleep(cx, 120).await;
        self.press(cx, "up").await?;
        sleep(cx, 150).await;
        self.check(8, self.editor(cx, |e| e.block_count() == blocks_before_insert + 1 && e.first_block_text().is_empty() && !e.first_line_delete_armed()), "首行已插入空段落且初始未进入待删状态")?;
        self.press(cx, "backspace").await?;
        sleep(cx, 180).await;
        self.check(8, self.editor(cx, |e| e.block_count() == blocks_before_insert + 1 && e.first_block_text().is_empty() && e.first_line_delete_armed() && e.first_line_placeholder() == crate::ui::reveal::delete_arm_placeholder_text()) && smoke_overlay::target_center("first-line-delete-badge").is_some(), "首行首次按删除键不删内容并显示删除标识及提示")?;
        self.press(cx, "backspace").await?;
        sleep(cx, 180).await;
        self.check(8, self.editor(cx, |e| e.block_count() == blocks_before_insert && e.first_block_text() == "CRUD Title" && !e.first_line_delete_armed()), "首行再次按删除键删除空行并将下方内容顶上来")
    }

    async fn find(&self, cx: &AsyncApp) -> Result<(), String> {
        self.hud(cx, "Step 9/12 · 笔记内查找");
        self.press(cx, "cmd-f").await?;
        sleep(cx, 250).await;
        self.check(9, self.editor(cx, |e| e.find_is_open()), "查找栏已打开")?;
        self.type_text(cx, "a").await?;
        sleep(cx, 200).await;
        self.check(9, self.editor(cx, |e| e.find_match_count()) >= 2, "查找命中多处")?;
        self.click_target(cx, "find-next", "Step 9/12 · 点击下一个").await?;
        self.check(9, self.editor(cx, |e| e.find_current_index()) == 1, "下一个定位到第 2 处")?;
        self.click_target(cx, "find-prev", "Step 9/12 · 点击上一个").await?;
        self.check(9, self.editor(cx, |e| e.find_current_index()) == 0, "上一个回到第 1 处")?;
        self.click_target(cx, "find-close", "Step 9/12 · 点击关闭查找栏").await?;
        self.check(9, !self.editor(cx, |e| e.find_is_open()), "查找栏已关闭")
    }

    async fn global_search(&self, cx: &AsyncApp) -> Result<(), String> {
        self.click_target(cx, "search-button", "Step 10/12 · 点击打开全局搜索").await?;
        self.check(10, self.query(cx, |w, _| w.search_is_open()), "搜索弹窗已打开")?;
        sleep(cx, 250).await;
        self.type_text(cx, "Welcome").await?;
        let found = self.wait_until(cx, 1500, |w, _| w.search_hit_count() >= 1).await;
        self.check(10, found, "搜索出结果")?;
        sleep(cx, 300).await;
        self.click_target(cx, "search-hit-0", "Step 10/12 · 点击第一条结果").await?;
        let jumped = self.wait_until(cx, 1500, |w, cx| !w.search_is_open() && w.active_tab_name(cx) == "Welcome.md").await;
        self.check(10, jumped, "点击结果后跳转到 Welcome.md")
    }

    async fn tabs_and_sidebar(&self, cx: &AsyncApp) -> Result<(), String> {
        self.click_target(cx, "sidebar-toggle", "Step 11/12 · 点击收起侧边栏").await?;
        self.check(11, self.query(cx, |w, _| w.sidebar_collapsed), "侧边栏已收起")?;
        self.click_target(cx, "sidebar-toggle", "Step 11/12 · 点击展开侧边栏").await?;
        self.check(11, self.query(cx, |w, _| !w.sidebar_collapsed), "侧边栏已展开")?;
        // Move off the first tab so the tab click below has something to switch.
        self.press(cx, "ctrl-tab").await?;
        sleep(cx, 300).await;
        let moved = self.wait_until(cx, 1500, |w, cx| w.active_tab_name(cx) != w.first_tab_name()).await;
        self.check(11, moved, "已切换到第二个标签页")?;
        self.click_target(cx, "tab-0", "Step 11/12 · 点击第一个标签页").await?;
        let back = self.wait_until(cx, 1500, |w, cx| w.active_tab_name(cx) == w.first_tab_name()).await;
        self.check(11, back, "点击后回到第一个标签页")
    }

    async fn settings(&self, cx: &AsyncApp) -> Result<(), String> {
        self.click_target(cx, "settings-button", "Step 12/12 · 点击打开设置页").await?;
        self.check(12, self.query(cx, |w, _| w.settings_is_open()), "设置页已打开")?;
        self.check(12, smoke_overlay::target_center("titlebar-feedback").is_none() && smoke_overlay::target_center("titlebar-ai").is_none(), "设置页顶栏已移除反馈与 AI 按钮")?;
        self.check(12, smoke_overlay::target_center("sidebar-toggle").is_some() && smoke_overlay::target_center("settings-back").is_some_and(|p| f32::from(p.y) >= 40.), "设置页顶栏保留侧栏折叠按钮且内容区右上角显示关闭按钮")?;
        self.click_target(cx, "settings-tab-folders", "Step 12/12 · 切换到文件夹设置").await?;
        sleep(cx, 200).await;
        self.check(12, crate::theme::hide_non_markdown_files(), "新用户默认勾选『不显示 MD 格式以外的文件』")?;
        let txt_hidden_by_default = self.query(cx, |w, cx| {
            w.folder.as_ref().is_some_and(|f| !w.tree.read(cx).contains_root_path(&f.join("notes.txt")) && w.tree.read(cx).contains_root_path(&f.join("Welcome.md")))
        });
        self.check(12, txt_hidden_by_default, "默认勾选时侧栏隐藏非 MD 文件并保留 MD 文件")?;
        self.click_target(cx, "folder-hide-non-md", "Step 12/12 · 取消勾选恢复显示所有文件").await?;
        let txt_shown = self.wait_until(cx, 1500, |w, cx| {
            !crate::theme::hide_non_markdown_files() && w.folder.as_ref().is_some_and(|f| w.tree.read(cx).contains_root_path(&f.join("notes.txt")))
        }).await;
        self.check(12, txt_shown, "取消勾选后侧栏显示非 MD 文件")?;
        self.click_target(cx, "folder-hide-non-md", "Step 12/12 · 重新勾选不显示 MD 格式以外的文件").await?;
        let txt_hidden_again = self.wait_until(cx, 1500, |w, cx| {
            crate::theme::hide_non_markdown_files() && w.folder.as_ref().is_some_and(|f| !w.tree.read(cx).contains_root_path(&f.join("notes.txt")))
        }).await;
        self.check(12, txt_hidden_again, "重新勾选后侧栏隐藏非 MD 文件")?;
        self.click_target(cx, "folder-search-ignore-0", "Step 12/12 · 勾选当前文件夹不参与全局搜索").await?;
        self.check(12, self.query(cx, |w, _| w.search_folders().is_empty()), "勾选后当前文件夹已从全局搜索排除")?;
        self.click_target(cx, "folder-search-ignore-0", "Step 12/12 · 取消勾选恢复参与全局搜索").await?;
        self.check(12, self.query(cx, |w, _| !w.search_folders().is_empty()), "取消勾选后当前文件夹恢复参与全局搜索")?;
        self.click_target(cx, "settings-back", "Step 12/12 · 点击返回编辑").await?;
        self.check(12, self.query(cx, |w, _| !w.settings_is_open()), "返回编辑，设置页已关闭")
    }
}
