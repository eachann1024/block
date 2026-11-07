use gpui::Focusable;
use super::{AsyncApp, Run, sleep, smoke_overlay};

impl Run {
    pub(super) async fn check_plan_link(&self, cx: &AsyncApp) -> Result<(), String> {
        let root = self.query(cx, |workspace, _| workspace.folder.clone()).ok_or("Notebook missing")?;
        let note = root.join("潮州汕头行程.md");
        std::fs::write(&note, "| 日期 | 地点 |\n| --- | --- |\n| 1&#47;1 | 潮州&#58;老城 |\n| 1&#47;2 | 汕头 |\n").map_err(|error| error.to_string())?;
        let other = root.join("notes.txt");
        cx.update(|cx| self.workspace.update(cx, |workspace, cx| workspace.open_file_with_mode(other.clone(), false, true, cx)));
        let opened = self.wait_until(cx, 4000, |workspace, cx| workspace.editor.read(cx).current_path() == Some(other.as_path()) && !workspace.editor.read(cx).is_loading()).await;
        self.check(14, opened, "无法打开 notes.txt")?;
        self.win(cx, |window, cx| crate::editor::ai::plan_link::prepare_plan_link_smoke(root.clone(), window, cx))??;
        let switched = self.wait_until(cx, 4000, |workspace, cx| workspace.editor.read(cx).current_path() == Some(note.as_path()) && !workspace.editor.read(cx).is_loading()).await;
        self.check(14, switched, "计划未自动打开目标笔记")?;
        sleep(cx, 300).await; cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        let repeated = "重复的修改正文 MARKER\n```markdown\n# LARGE-PLAN-CONTENT\n```";
        self.win(cx, |window, cx| crate::editor::ai::chat::set_stream_smoke(repeated, true, window, cx))??;
        self.redraw(cx);
        self.check(14, cx.update(|_| crate::editor::ai::chat::smoke_visible_replies()).iter().all(|reply| !reply.contains("LARGE-PLAN-CONTENT")), "计划流式期间聊天不重复修改正文")?;
        self.win(cx, |window, cx| crate::editor::ai::chat::set_stream_smoke(repeated, false, window, cx))??;
        self.redraw(cx);
        self.check(14, cx.update(|_| crate::editor::ai::chat::smoke_visible_replies()).iter().all(|reply| !reply.contains("LARGE-PLAN-CONTENT")), "计划完成后聊天不重复修改正文")?;
        for id in ["ai-plan-card", "plan-op-1-0", "ai-editor-plan-card", "ai-plan-inline-dock", "ai-plan-inline-step", "ai-plan-inline-reject", "ai-plan-inline-accept"] {
            self.check(14, smoke_overlay::target_bounds(id).is_some(), &format!("Missing {id}"))?;
        }
        self.capture_ai(cx, "plan")?;
        self.click_target(cx, "ai-plan-mode-diff", "切换增删对比").await?;
        cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        self.check(14, smoke_overlay::target_bounds("ai-plan-diff-del-row").is_some(), "Missing deleted row")?;
        let link = crate::editor::ai::plan_link::current().ok_or("Missing active plan preview")?;
        self.check(14, link.diff_mode && !link.before_blocks.is_empty() && !link.after_blocks.is_empty()
            && link.before_blocks.iter().map(|block| &block.text).collect::<Vec<_>>() != link.after_blocks.iter().map(|block| &block.text).collect::<Vec<_>>(), "对比包含不同的修改前后内容")?;
        self.capture_ai(cx, "editor-diff")?;
        self.click_target(cx, "ai-plan-mode-clean", "切换修改后预览").await?;
        self.check(14, crate::editor::ai::plan_link::current().is_some_and(|link| !link.diff_mode), "切换回修改后预览")?;
        let before = self.editor(cx, |editor| editor.markdown_text());
        let disk_before = std::fs::read(&note).map_err(|error| error.to_string())?;
        self.click_target(cx, "ai-plan-inline-reject", "拒绝修改").await?;
        sleep(cx, 200).await; cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        self.check(14, self.editor(cx, |editor| editor.markdown_text()) == before
            && smoke_overlay::target_bounds("ai-editor-plan-card").is_none(), "拒绝后正文保持原样并关闭审阅")?;
        self.win(cx, |window, cx| crate::editor::ai::plan_link::prepare_plan_link_smoke(root, window, cx))??;
        sleep(cx, 300).await; cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        self.click_target(cx, "ai-plan-mode-diff", "选择部分表格修改").await?;
        let link = crate::editor::ai::plan_link::current().ok_or("Missing active plan preview")?;
        let lines = crate::editor::ai::plan_link::selection::diff(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text)?;
        let removed = lines.iter().find(|line| line.change < 0 && line.text.trim_start().starts_with('|')).ok_or("Missing removed table row")?;
        let added = lines.iter().find(|line| line.change > 0 && line.text.trim_start().starts_with('|')).ok_or("Missing added table row")?;
        self.click_target(cx, &format!("ai-plan-diff-remove-{}", removed.index), "保留原表格行").await?;
        self.click_target(cx, &format!("ai-plan-diff-add-{}", added.index), "排除新增表格行").await?;
        let selected = crate::editor::ai::plan_link::selection::selected(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text)?;
        self.check(14, selected.contains(removed.text.trim()) && !selected.contains(added.text.trim())
            && self.editor(cx, |editor| editor.markdown_text()) == before
            && std::fs::read(&note).map_err(|error| error.to_string())? == disk_before, "选择后原表格行保留、新行排除且正文与文件未变")?;
        self.click_target(cx, "ai-plan-inline-accept", "接受选中的修改").await?;
        sleep(cx, 200).await;
        let normalize = |text: &str| crate::editor::markdown::serialize(&crate::editor::markdown::parse(text));
        self.check(14, normalize(&self.editor(cx, |editor| editor.markdown_text())) == normalize(&selected), "接受后正文与选中的修改一致")?;
        self.press(cx, "cmd-z").await?;
        self.check(14, normalize(&self.editor(cx, |editor| editor.markdown_text())) == normalize(&before), "撤销部分修改恢复原文")?;
        let root = self.query(cx, |workspace, _| workspace.folder.clone()).ok_or("Notebook missing")?;
        self.win(cx, |window, cx| crate::editor::ai::plan_link::prepare_plan_link_smoke(root, window, cx))??;
        sleep(cx, 300).await; cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        self.click_target(cx, "ai-plan-inline-accept", "接受修改").await?;
        sleep(cx, 200).await; cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        self.check(14, cx.update(|cx| crate::editor::ai::plan_link::smoke_applied(cx)), "计划未应用")?;
        self.check(14, smoke_overlay::target_bounds("ai-editor-plan-card").is_none(), "审阅卡未关闭")?;
        let markdown = self.editor(cx, |editor| editor.markdown_text());
        let doc = crate::editor::markdown::parse(&markdown);
        let heading = doc.blocks.iter().any(|block| block.kind == crate::editor::model::Kind::H2 && block.text == "总路线");
        let table = doc.blocks.iter().filter_map(|block| block.meta.table.as_ref()).any(|table| table.rows.len() == 6 && table.rows.iter().flat_map(|row| &row.cells).any(|cell| cell.content.text.contains("头新溪谢肥记(君)")));
        self.check(14, heading && table, "标题或五行表格未写入")?;
        self.check_create_selection(cx).await
    }

    async fn check_create_selection(&self, cx: &AsyncApp) -> Result<(), String> {
        let root = self.query(cx, |workspace, _| workspace.folder.clone()).ok_or("Notebook missing")?;
        self.win(cx, |window, cx| {
            let focus = self.workspace.read(cx).editor.focus_handle(cx);
            window.focus(&focus, cx);
        })?;
        self.check(14, self.win(cx, |window, cx| self.workspace.read(cx).editor.focus_handle(cx).is_focused(window))?, "新建预览前编辑器已聚焦")?;
        let original = self.editor(cx, |editor| editor.markdown_text());
        let original_path = self.editor(cx, |editor| editor.current_path().map(|path| path.to_owned()));
        let path = self.win(cx, |window, cx| crate::editor::ai::plan_link::prepare_create_plan_smoke(root, window, cx))??;
        sleep(cx, 300).await; self.redraw(cx);
        self.check(14, smoke_overlay::target_bounds("ai-create-preview").is_some() && !path.exists(), "新建预览不写文件")?;
        self.type_text(cx, "abc").await?;
        self.check(14, self.editor(cx, |editor| editor.markdown_text()) == original && self.editor(cx, |editor| editor.current_path().map(|path| path.to_owned())) == original_path, "预览期间输入不修改底层正文和路径")?;
        self.press(cx, "backspace").await?;
        self.check(14, self.editor(cx, |editor| editor.markdown_text()) == original && self.editor(cx, |editor| editor.current_path().map(|path| path.to_owned())) == original_path, "预览期间退格不修改底层正文和路径")?;
        self.press(cx, "delete").await?;
        self.check(14, self.editor(cx, |editor| editor.markdown_text()) == original && self.editor(cx, |editor| editor.current_path().map(|path| path.to_owned())) == original_path, "预览期间删除不修改底层正文和路径")?;
        self.click_target(cx, "ai-plan-mode-diff", "查看新建内容对比").await?;
        let link = crate::editor::ai::plan_link::current().ok_or("Missing create preview")?;
        let lines = crate::editor::ai::plan_link::selection::diff(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text)?;
        let excluded = lines.iter().find(|line| line.change > 0 && line.text.contains("排除")).ok_or("Missing create row")?;
        self.click_target(cx, &format!("ai-plan-diff-add-{}", excluded.index), "排除新建内容的一行").await?;
        let selected = || crate::editor::ai::plan_link::selection::selected(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text);
        let before_close = selected()?;
        self.check(14, before_close.contains("保留") && !before_close.contains("排除") && !path.exists(), "选择后仍未写入新文件")?;
        self.click_target(cx, "ai-plan-inline-reject", "关闭新建预览").await?;
        self.redraw(cx);
        self.check(14, smoke_overlay::target_bounds("ai-create-preview").is_none() && !path.exists(), "关闭预览不写文件")?;
        self.click_target(cx, "plan-op-1-0", "重开新建预览").await?;
        self.redraw(cx);
        self.check(14, smoke_overlay::target_bounds("ai-create-preview").is_some() && selected()? == before_close, "重开预览保留行选择")?;
        self.click_target(cx, "ai-plan-inline-accept", "接受选中的新建内容").await?;
        let opened = self.wait_until(cx, 4000, |workspace, cx| workspace.editor.read(cx).current_path() == Some(path.as_path()) && !workspace.editor.read(cx).is_loading()).await;
        let written = std::fs::read_to_string(&path).map_err(|error| error.to_string())?;
        self.check(14, opened && written.contains("保留") && !written.contains("排除"), "新建文件仅写入选中内容并打开")
    }
}
