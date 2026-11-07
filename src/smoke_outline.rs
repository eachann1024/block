use gpui::{Modifiers, MouseMoveEvent, PlatformInput, px};
use super::{AsyncApp, Run, sleep, smoke_overlay};

impl Run {
    pub(super) async fn outline_rail(&self, cx: &AsyncApp) -> Result<(), String> {
        let editor = self.query(cx, |workspace, _| workspace.editor.clone());
        self.win(cx, |window, cx| editor.update(cx, |editor, cx| editor.prepare_outline_smoke(window, cx)))?;
        sleep(cx, 200).await;
        self.redraw(cx);
        let entries = self.editor(cx, |editor| editor.outline_smoke_entries());
        self.check(9, entries.len() == 4 && entries.iter().map(|entry| entry.1).collect::<Vec<_>>() == [1, 2, 3, 2], "大纲包含四级正确的标题")?;
        self.check(9, !entries[0].4 && entries[1].3 && entries[1].4 && !entries[2].3 && entries[2].4 && !entries[3].4,
            "当前章节及其子标题高亮")?;
        let rail = smoke_overlay::target_bounds("outline-rail").ok_or("找不到大纲栏")?;
        let bounds = smoke_overlay::target_bounds("editor").ok_or("找不到编辑器")?;
        self.check(9, rail.left() >= bounds.left() && rail.left() - bounds.left() < px(60.)
            && rail.top() >= bounds.top() && rail.top() - bounds.top() < px(100.), "大纲栏位于编辑区左上方")?;
        self.check(9, (0..4).all(|index| smoke_overlay::target_bounds(&format!("outline-bar-{index}")).is_some()), "四个大纲横条可见")?;
        let position = smoke_overlay::target_center("outline-bar-1").ok_or("找不到大纲悬停目标")?;
        self.win(cx, |window, cx| window.dispatch_event(PlatformInput::MouseMove(MouseMoveEvent {
            position, modifiers: Modifiers::default(), pressed_button: None,
        }), cx))?;
        sleep(cx, 180).await;
        self.redraw(cx);
        self.check(9, smoke_overlay::target_bounds("outline-popover").is_some()
            && smoke_overlay::target_bounds("outline-item-3").is_some(), "悬停展开完整大纲")?;
        self.click_target(cx, "outline-item-3", "Step 9 · 点击大纲跳转到章节").await?;
        cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        self.check(9, self.editor(cx, |editor| editor.caret_block_id()) == entries[3].0
            && smoke_overlay::target_bounds("outline-popover").is_none(), "点击标题跳转并关闭浮层")?;
        cx.update(|cx| editor.update(cx, |editor, cx| editor.set_outline_force_narrow_smoke(true, cx)));
        sleep(cx, 150).await;
        cx.update(|_| smoke_overlay::reset()); self.redraw(cx);
        self.check(9, smoke_overlay::target_bounds("outline-rail").is_none(), "位置不够时隐藏大纲")?;
        cx.update(|cx| editor.update(cx, |editor, cx| editor.set_outline_force_narrow_smoke(false, cx)));
        sleep(cx, 150).await;
        self.redraw(cx);
        self.check(9, smoke_overlay::target_bounds("outline-rail").is_some(), "位置恢复后显示大纲")
    }
}
