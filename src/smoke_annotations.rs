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
        self.check(9, notes.len() == 4 && notes.iter().zip(&expected).all(|(n, t)| (n.1, n.2, n.3, &n.4) == (t.0, t.1, t.2, &t.3) && n.5 == "Review"), "四行批注分别保存实际选区")?;
        let ids: std::collections::HashSet<_> = notes.iter().map(|n| n.0).collect();
        self.check(9, ids.len() == 4 && self.editor(cx, |e| e.smoke_annotations_persisted()), "批量批注 ID 唯一且持久化完整")?;
        self.annotation_drag(cx, (3, 6), (0, 3)).await?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_targets()) == expected, "反向拖选得到相同四行选区")?;
        self.annotation_emoji(cx, 0).await?;
        self.check_annotation_emoji(cx, "👍", 4)?;
        self.annotation_drag(cx, (0, 3), (3, 6)).await?;
        self.annotation_emoji(cx, 1).await?;
        self.check_annotation_emoji(cx, "❤️", 4)?;
        self.annotation_drag(cx, (0, 3), (3, 6)).await?;
        self.annotation_emoji(cx, 1).await?;
        self.check_annotation_emoji(cx, "❤️", 0)?;
        // A single-line reaction then a multi-line selection must converge, not invert each line.
        self.annotation_drag(cx, (0, 3), (0, 21)).await?;
        self.annotation_emoji(cx, 0).await?;
        self.check_annotation_emoji(cx, "👍", 1)?;
        self.annotation_drag(cx, (0, 3), (3, 6)).await?;
        self.annotation_emoji(cx, 0).await?;
        self.check_annotation_emoji(cx, "👍", 4)?;
        self.annotation_drag(cx, (0, 3), (3, 0)).await?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_targets()).len() == 3, "选区结束于下一行开头时不误包含该行")?;
        self.click_target(cx, crate::theme::tr("添加批注", "Add annotation"), "验证取消跨行批注").await?;
        self.press(cx, "escape").await?;
        self.check(9, self.editor(cx, |e| e.annotation_smoke_pending()) == 0 && self.editor(cx, |e| e.annotation_smoke_notes()).len() == 4, "取消批注不创建数据")?;
        self.press(cx, "right").await?;
        Ok(())
    }

    fn check_annotation_emoji(&self, cx: &AsyncApp, emoji: &str, count: usize) -> Result<(), String> {
        let notes = self.editor(cx, |e| e.annotation_smoke_notes());
        self.check(9, notes.len() == 4 && notes.iter().all(|n| n.5 == "Review" && n.6.len() <= 1)
            && notes.iter().filter(|n| n.6.first().is_some_and(|r| r == emoji)).count() == count
            && self.editor(cx, |e| e.smoke_annotations_persisted()),
            &format!("表情 {emoji} 覆盖 {count} 行，保留批注并持久化"))
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
