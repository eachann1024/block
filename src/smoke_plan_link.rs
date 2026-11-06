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
        self.win(cx, |window, cx| crate::editor::ai::plan_link::prepare_plan_link_smoke(root, window, cx))??;
        let switched = self.wait_until(cx, 4000, |workspace, cx| workspace.editor.read(cx).current_path() == Some(note.as_path()) && !workspace.editor.read(cx).is_loading()).await;
        self.check(14, switched, "计划未自动打开目标笔记")?;
        sleep(cx, 300).await; cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        for id in ["ai-plan-card", "plan-op-1-0", "plan-toggle-diff-1", "ai-editor-plan-card", "ai-plan-inline-dock", "ai-plan-inline-step", "ai-plan-inline-reject", "ai-plan-inline-accept"] {
            self.check(14, smoke_overlay::target_bounds(id).is_some(), &format!("Missing {id}"))?;
        }
        self.click_target(cx, "ai-plan-mode-diff", "切换增删对比").await?;
        cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        self.check(14, smoke_overlay::target_bounds("ai-plan-diff-del-row").is_some(), "Missing deleted row")?;
        self.click_target(cx, "ai-plan-mode-clean", "切换修改后预览").await?;
        self.click_target(cx, "ai-plan-inline-accept", "接受修改").await?;
        sleep(cx, 200).await; cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        self.check(14, cx.update(|cx| crate::editor::ai::plan_link::smoke_applied(cx)), "计划未应用")?;
        self.check(14, smoke_overlay::target_bounds("ai-editor-plan-card").is_none(), "审阅卡未关闭")?;
        let markdown = self.editor(cx, |editor| editor.markdown_text());
        let doc = crate::editor::markdown::parse(&markdown);
        let heading = doc.blocks.iter().any(|block| block.kind == crate::editor::model::Kind::H2 && block.text == "总路线");
        let table = doc.blocks.iter().filter_map(|block| block.meta.table.as_ref()).any(|table| table.rows.len() == 6 && table.rows.iter().flat_map(|row| &row.cells).any(|cell| cell.content.text.contains("头新溪谢肥记(君)")));
        self.check(14, heading && table, "标题或五行表格未写入")
    }
}
