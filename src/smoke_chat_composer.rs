use gpui::{ClipboardItem, px};
use super::{AsyncApp, Run, sleep, smoke_overlay};

impl Run {
    pub(super) async fn chat_composer(&self, cx: &AsyncApp) -> Result<(), String> {
        self.win(cx, crate::editor::ai::chat::prepare_selection_smoke)??;
        sleep(cx, 250).await;
        let clipboard = cx.update(|cx| cx.read_from_clipboard());
        let result = self.check_composer(cx).await;
        cx.update(|cx| cx.write_to_clipboard(clipboard.unwrap_or_else(|| ClipboardItem::new_string(String::new()))));
        result
    }

    async fn check_composer(&self, cx: &AsyncApp) -> Result<(), String> {
        self.redraw(cx);
        let bounds = || smoke_overlay::target_bounds("ai-composer-input").ok_or("Missing composer");
        let initial = bounds()?.size.height;
        self.check(14, initial >= px(44.) && initial <= px(64.), &format!("默认输入区为两行: {initial:?}"))?;
        self.click_target(cx, "ai-composer-input", "验证输入框自动换行").await?;
        let text = "这些笔记提到的问题都是什么地方比如说长内容自动换行".repeat(20);
        cx.update(|cx| cx.write_to_clipboard(ClipboardItem::new_string(text.clone())));
        self.press(cx, "cmd-v").await?;
        sleep(cx, 250).await; self.redraw(cx);
        let expanded = bounds()?;
        let snapshot = self.win(cx, crate::editor::ai::chat::input_smoke::snapshot)?;
        self.check(14, snapshot.0 == text && snapshot.1.x == px(0.), "长中文保留完整且无横向滚动")?;
        self.check(14, expanded.size.height > initial && expanded.size.height <= initial + px(88.), "输入区最多六行")?;
        let actions = smoke_overlay::target_bounds("ai-composer-actions").ok_or("Missing actions")?;
        self.check(14, expanded.bottom() <= actions.top(), "文字区域不与模型和发送按钮重叠")?;
        self.press(cx, "cmd-a").await?;
        cx.update(|cx| cx.write_to_clipboard(ClipboardItem::new_string("第一行".into())));
        self.press(cx, "cmd-v").await?;
        self.press(cx, "shift-enter").await?;
        let snapshot = self.win(cx, crate::editor::ai::chat::input_smoke::snapshot)?;
        self.check(14, snapshot.0 == "第一行\n", "Shift+Enter 插入换行而不发送")?;
        self.win(cx, |window, cx| { window.dispatch_keystroke(gpui::Keystroke::parse("enter").unwrap(), cx); })?;
        // Observe reset before the intentionally offline fixture restores a failed draft.
        self.redraw(cx);
        let snapshot = self.win(cx, crate::editor::ai::chat::input_smoke::snapshot)?;
        self.check(14, snapshot.0.is_empty() && bounds()?.size.height == initial, "发送清空并恢复默认两行")?;
        Ok(())
    }
}
