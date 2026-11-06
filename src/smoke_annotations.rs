//! Multi-block annotations via GPUI events in the isolated smoke window.
use gpui::{Modifiers, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, PlatformInput};
use super::{AsyncApp, Run, sleep};

impl Run {
    pub(super) async fn annotations(&self, cx: &AsyncApp) -> Result<(), String> {
        let editor = self.query(cx, |w, _| w.editor.clone());
        self.win(cx, |window, cx| editor.update(cx, |e, cx| e.prepare_annotation_smoke(window, cx)))?;
        sleep(cx, 250).await;
        self.annotation_drag(cx, (0, 3), (3, 6)).await?;
        let expected = self.editor(cx, |e| e.annotation_smoke_targets());
        self.check(9, expected.len() == 4 && expected[0].2 == 21 && expected[0].1 == 3 && expected[3].2 == 6, "跨四行拖选保留中文首尾边界")?;
        self.click_target(cx, crate::theme::tr("添加批注", "Add annotation"), "跨四行添加批注").await?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_pending()) == 4, "批注输入框保留全部四行")?;
        self.type_text(cx, "Review").await?;
        self.press(cx, "enter").await?;
        let notes = self.editor(cx, |e| e.annotation_smoke_notes());
        self.check(9, notes.len() == 1 && notes[0].5 == "Review" && notes[0].4 == expected.iter().map(|t| t.3.as_str()).collect::<Vec<_>>().join("\n"), "跨四行只保存一条批注和完整引用")?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_spans()) == expected, "同一条批注高亮全部四行实际选区")?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_hit_ids()).iter().all(|id| *id == Some(notes[0].0)), "四行点击均指向同一批注")?;
        self.check(9, self.editor(cx, |e| e.smoke_annotations_persisted()), "单条多行批注持久化完整")?;
        cx.update(|cx| editor.update(cx, |e, _| e.reload_annotation_smoke()));
        self.check(9, self.editor(cx, |e| e.annotation_smoke_notes()).len() == 1 && self.editor(cx, |e| e.annotation_smoke_spans()) == expected, "重载后仍为一条批注且保留全部锚点")?;
        self.annotation_drag(cx, (3, 6), (0, 3)).await?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_targets()) == expected, "反向拖选得到相同四行选区")?;
        self.annotation_emoji(cx, 0).await?;
        self.check_annotation_emoji(cx, "👍", 1)?;
        self.annotation_drag(cx, (0, 3), (3, 6)).await?;
        self.annotation_emoji(cx, 1).await?;
        self.check_annotation_emoji(cx, "❤️", 1)?;
        self.annotation_drag(cx, (0, 3), (3, 6)).await?;
        self.annotation_emoji(cx, 1).await?;
        self.check_annotation_emoji(cx, "❤️", 0)?;
        self.annotation_drag(cx, (0, 3), (3, 0)).await?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_targets()).len() == 3, "选区结束于下一行开头时不误包含该行")?;
        self.click_target(cx, crate::theme::tr("添加批注", "Add annotation"), "验证取消跨行批注").await?;
        self.press(cx, "escape").await?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_pending()) == 0 && self.editor(cx, |e| e.annotation_smoke_notes()).len() == 1, "取消批注不创建数据")?;
        self.press(cx, "right").await?;
        let id = notes[0].0;
        self.click_target(cx, &format!("annotation-edit-{id}"), "编辑整段批注").await?;
        self.press(cx, "cmd-a").await?;
        self.type_text(cx, "Updated").await?;
        self.press(cx, "enter").await?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_notes()).len() == 1
            && self.editor(cx, |e| e.annotation_smoke_notes())[0].5 == "Updated"
            && self.editor(cx, |e| e.annotation_smoke_spans()) == expected, "编辑后仍仅一条批注并关联四行")?;
        self.click_target(cx, &format!("annotation-delete-{id}"), "删除整段批注").await?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_notes()).is_empty()
            && self.editor(cx, |e| e.annotation_smoke_hit_ids()).iter().all(Option::is_none), "删除一次清除整段批注")?;
        self.annotation_drag(cx, (0, 3), (3, 6)).await?;
        self.annotation_emoji(cx, 0).await?;
        let reactions = self.editor(cx, |e| e.annotation_smoke_notes());
        self.check(9, reactions.len() == 1 && reactions[0].5.is_empty() && reactions[0].6 == ["👍"]
            && self.editor(cx, |e| e.annotation_smoke_spans()) == expected, "无批注时跨行表情也只创建一个记录")?;
        self.annotation_drag(cx, (0, 3), (3, 6)).await?;
        self.annotation_emoji(cx, 0).await?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_notes()).is_empty(), "重复选择相同表情移除整段回应")?;
        self.annotation_drag(cx, (0, 3), (3, 6)).await?;
        self.click_target(cx, crate::theme::tr("添加批注", "Add annotation"), "准备整段改写").await?;
        self.type_text(cx, "Rewrite").await?;
        self.press(cx, "enter").await?;
        let original = self.editor(cx, |e| e.markdown_text());
        let id = self.editor(cx, |e| e.annotation_smoke_notes())[0].0;
        cx.update(|cx| editor.update(cx, |e, cx| e.prepare_annotation_result_smoke(cx)));
        self.redraw(cx); sleep(cx, 250).await;
        self.click_target(cx, &format!("annotation-accept-{id}"), "应用整段改写").await?;
        let text = self.editor(cx, |e| e.markdown_text());
        self.check(9, text.contains("古Alpha") && text.contains("Beta晚上做什么")
            && !text.contains("怎么刷牙") && !text.contains("冰箱") && self.editor(cx, |e| e.block_count()) == 2,
            "一次改写替换完整跨行选区并保留首尾未选中文字")?;
        self.press(cx, "cmd-z").await?;
        self.check(9, self.editor(cx, |e| e.markdown_text()) == original, "一次撤销还原整段原文")?;
        Ok(())
    }

    fn check_annotation_emoji(&self, cx: &AsyncApp, emoji: &str, count: usize) -> Result<(), String> {
        let notes = self.editor(cx, |e| e.annotation_smoke_notes());
        self.check(9, notes.len() == 1 && notes.iter().all(|n| n.5 == "Review" && n.6.len() <= 1)
            && notes.iter().filter(|n| n.6.first().is_some_and(|r| r == emoji)).count() == count
            && self.editor(cx, |e| e.smoke_annotations_persisted()),
            &format!("选区仅有 {count} 个表情 {emoji}，保留单条批注并持久化"))
    }

    async fn annotation_emoji(&self, cx: &AsyncApp, index: usize) -> Result<(), String> {
        self.click_target(cx, crate::theme::tr("添加表情回应", "Add reaction"), "打开选区表情").await?;
        self.click_target(cx, &format!("pick-emoji-{index}"), "选择表情").await
    }

    async fn annotation_drag(&self, cx: &AsyncApp, from: (usize, usize), to: (usize, usize)) -> Result<(), String> {
        self.redraw(cx);
        let start = self.editor(cx, |e| e.annotation_smoke_point(from.0, from.1)).ok_or("缺少拖选起点布局")?;
        let end = self.editor(cx, |e| e.annotation_smoke_point(to.0, to.1)).ok_or("缺少拖选终点布局")?;
        self.win(cx, |window, cx| {
            window.dispatch_event(PlatformInput::MouseMove(MouseMoveEvent { position: start, modifiers: Modifiers::default(), pressed_button: None }), cx);
            window.dispatch_event(PlatformInput::MouseDown(MouseDownEvent { position: start, button: MouseButton::Left, modifiers: Modifiers::default(), click_count: 1, first_mouse: false }), cx);
        })?;
        for step in 1..=10 {
            let position = start + (end - start) * (step as f32 / 10.);
            self.win(cx, |window, cx| {
                window.dispatch_event(PlatformInput::MouseMove(MouseMoveEvent { position, modifiers: Modifiers::default(), pressed_button: Some(MouseButton::Left) }), cx);
            })?;
            sleep(cx, 25).await;
        }
        self.win(cx, |window, cx| {
            window.dispatch_event(PlatformInput::MouseUp(MouseUpEvent { position: end, button: MouseButton::Left, modifiers: Modifiers::default(), click_count: 1 }), cx);
        })?;
        sleep(cx, 200).await;
        self.redraw(cx);
        Ok(())
    }
}
