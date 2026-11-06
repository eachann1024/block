//! AI message selection via events in the hidden smoke window, never OS input.
use gpui::{Modifiers, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, PlatformInput, point, px};
use super::{AsyncApp, Run, sleep, smoke_overlay};

impl Run {
    pub(super) async fn chat_selection(&self, cx: &AsyncApp) -> Result<(), String> {
        let config = crate::editor::ai::provider::stored();
        self.win(cx, crate::editor::ai::chat::prepare_selection_smoke)??;
        sleep(cx, 300).await;
        // Preserve the user's clipboard even if an assertion fails.
        let clipboard = cx.update(|cx| cx.read_from_clipboard());
        let result = self.check_chat_selection(cx).await;
        cx.update(|cx| {
            if let Some(item) = clipboard { cx.write_to_clipboard(item); }
            else { cx.write_to_clipboard(gpui::ClipboardItem::new_string(String::new())); }
        });
        crate::editor::ai::provider::save(&config)?;
        result
    }

    async fn check_chat_selection(&self, cx: &AsyncApp) -> Result<(), String> {
        for (id, expected, inset) in [
            ("ai-user-message", "用户拖选 Alpha Beta", 13.),
            ("ai-assistant-message", "回复 加粗 中文 Alpha\n\n第二段 Beta 结束", 1.),
        ] {
            self.redraw(cx);
            let bounds = smoke_overlay::target_bounds(id).ok_or_else(|| format!("Missing {id}"))?;
            let start = point(bounds.left() + px(inset), bounds.top() + px(if inset > 1. { 15. } else { 9. }));
            let end = point(bounds.right() - px(inset), bounds.bottom() - px(if inset > 1. { 15. } else { 9. }));
            self.hud(cx, "Step 14/14 · 消息拖选与复制");
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
            cx.update(|cx| cx.write_to_clipboard(gpui::ClipboardItem::new_string("smoke: no selection".into())));
            self.press(cx, "cmd-c").await?;
            let copied = cx.update(|cx| cx.read_from_clipboard().and_then(|item| item.text()).unwrap_or_default());
            // Paragraph separators are renderer-owned; exact words and ordering must survive.
            let normalize = |text: &str| text.split_whitespace().collect::<Vec<_>>().join(" ");
            self.check(14, normalize(&copied) == normalize(expected), &format!("{id} 拖选复制：{copied:?}"))?;
        }
        Ok(())
    }
}
