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
        let result = match self.check_chat_selection(cx).await {
            Ok(()) => match self.check_process_card(cx).await {
                Ok(()) => match self.check_source(cx).await { Ok(()) => self.check_plan_link(cx).await, Err(error) => Err(error) },
                Err(error) => Err(error),
            },
            Err(error) => Err(error),
        };
        cx.update(|cx| {
            if let Some(item) = clipboard { cx.write_to_clipboard(item); }
            else { cx.write_to_clipboard(gpui::ClipboardItem::new_string(String::new())); }
        });
        crate::editor::ai::provider::save(&config)?;
        result
    }

    async fn check_process_card(&self, cx: &AsyncApp) -> Result<(), String> {
        self.win(cx, crate::editor::ai::chat::prepare_process_card_smoke)??;
        sleep(cx, 300).await; self.redraw(cx);
        for status in ["running", "failed", "stopped", "completed"] {
            self.win(cx, |window, cx| crate::editor::ai::chat::set_process_smoke_status(status, window, cx))??;
            cx.update(|_| smoke_overlay::clear_targets()); self.redraw(cx);
            self.check(14, smoke_overlay::target_bounds(&format!("ai-process-status-{status}")).is_some(), &format!("过程状态应为 {status}"))?;
            if status == "running" {
                self.capture_ai(cx, "trace-running-a")?;
                self.capture_loader_bounds(cx)?;
                sleep(cx, 180).await;
                self.capture_ai(cx, "trace-running-b")?;
            } else if status == "completed" { self.capture_ai(cx, "trace-completed")?; }
        }
        let (steps, notes) = cx.update(|_| crate::editor::ai::chat::process_card_smoke_data());
        self.check(14, steps.len() == 7 && steps[2].merged_count == 5 && steps[1].label == steps[5].label, "Process step order or merge incorrect")?;
        self.check(14, notes.len() == 6 && notes.iter().any(|note| note == "Claude"), "Used notes missing")?;
        let markdown = crate::editor::ai::md_view::normalize_markdown("- **提示词**: **11 篇**\n\n---\n\n共计 **29 篇** 笔记。");
        self.check(14, markdown.contains("<strong>提示词</strong>") && markdown.contains("<strong>11 篇</strong>") && markdown.contains("<strong>29 篇</strong>") && markdown.contains("---"), "CJK Markdown formatting missing")?;
        for id in ["ai-process-card", "ai-process-stage-badge", "ai-process-toggle"] {
            self.check(14, smoke_overlay::target_bounds(id).is_some(), &format!("Missing {id}"))?;
        }
        self.check(14, smoke_overlay::target_bounds("ai-process-steps").is_none(), "Process should start collapsed")?;
        self.click_target(cx, "ai-process-toggle", "展开过程").await?;
        sleep(cx, 220).await; self.refresh_process_targets(cx);
        self.check(14, smoke_overlay::target_bounds("ai-process-notes-more").is_some(), "Missing expand notes")?;
        let bounds = smoke_overlay::target_bounds("ai-process-steps").ok_or("Missing process steps")?;
        self.check(14, bounds.size.height <= px(185.), "Process viewport exceeds four steps")?;
        let visible: Vec<_> = (0..7).filter(|index| smoke_overlay::target_bounds(&format!("ai-process-tool-chip-{index}"))
            .is_some_and(|chip| chip.center().y >= bounds.top() && chip.center().y < bounds.bottom())).collect();
        self.check(14, visible.len() >= 2, "至少两个工具摘要可交互")?;
        let first = visible[0];
        let chip_before = smoke_overlay::target_bounds(&format!("ai-process-tool-chip-{first}"));
        let scroll_before = cx.update(|_| crate::ui::ai_process_card::smoke_scroll_offset());
        self.click_target(cx, &format!("ai-process-tool-chip-{first}"), "展开第一条工具摘要").await?;
        self.refresh_process_targets(cx);
        let detail = smoke_overlay::target_bounds(&format!("ai-process-tool-detail-{first}"));
        if detail.is_none() {
            self.capture_ai(cx, "trace-tool-failed")?;
            let expanded = cx.update(|_| crate::ui::ai_process_card::smoke_expanded_details());
            let scroll_after = cx.update(|_| crate::ui::ai_process_card::smoke_scroll_offset());
            return Err(format!("Step 14/14: 第一条工具摘要已展开: first={first}, chip_before={chip_before:?}, viewport_before={bounds:?}, scroll_before={scroll_before:?}, chip_after={:?}, viewport_after={:?}, scroll_after={scroll_after:?}, expanded={expanded:?}", smoke_overlay::target_bounds(&format!("ai-process-tool-chip-{first}")), smoke_overlay::target_bounds("ai-process-steps")));
        }
        let bounds = smoke_overlay::target_bounds("ai-process-steps").ok_or("Missing process steps")?;
        let second = (0..7).find(|index| *index != first && smoke_overlay::target_bounds(&format!("ai-process-tool-chip-{index}"))
            .is_some_and(|chip| chip.center().y >= bounds.top() && chip.center().y + px(30.) < bounds.bottom()))
            .ok_or("展开后没有可见的第二条工具摘要")?;
        self.click_target(cx, &format!("ai-process-tool-chip-{second}"), "展开第二条工具摘要").await?;
        self.refresh_process_targets(cx);
        self.check(14, smoke_overlay::target_bounds(&format!("ai-process-tool-detail-{first}")).is_some()
            && smoke_overlay::target_bounds(&format!("ai-process-tool-detail-{second}")).is_some(), "工具摘要独立展开")?;
        self.capture_ai(cx, "trace-chips-expanded")?;
        self.click_target(cx, &format!("ai-process-tool-chip-{first}"), "收起第一条工具摘要").await?;
        self.refresh_process_targets(cx);
        self.check(14, smoke_overlay::target_bounds(&format!("ai-process-tool-detail-{first}")).is_none()
            && smoke_overlay::target_bounds(&format!("ai-process-tool-detail-{second}")).is_some(), "收起单条不影响其他摘要")?;
        self.click_target(cx, "ai-process-notes-more", "展开全部笔记").await?;
        self.redraw(cx); self.check(14, smoke_overlay::target_bounds("ai-process-notes-less").is_some(), "Missing collapse notes")?;
        self.capture_ai(cx, "context-expanded")?;
        self.capture_ai_theme(cx, "context-expanded-light", crate::files::state::ThemeMode::Light)?;
        self.capture_ai_theme(cx, "context-expanded-dark", crate::files::state::ThemeMode::Dark)?;
        self.click_target(cx, "ai-process-notes-less", "收起笔记").await?;
        self.redraw(cx); self.check(14, smoke_overlay::target_bounds("ai-process-notes-more").is_some(), "Missing expand notes")?;
        self.click_target(cx, "ai-process-toggle", "收起过程").await?;
        cx.update(|_| smoke_overlay::reset()); sleep(cx, 220).await; self.redraw(cx);
        self.check(14, smoke_overlay::target_bounds("ai-process-steps").is_none() && smoke_overlay::target_bounds("ai-process-stage-badge").is_some(), "Process did not collapse")?;
        Ok(())
    }

    fn refresh_process_targets(&self, cx: &AsyncApp) {
        cx.update(|cx| { crate::editor::ai::chat::notify(cx); smoke_overlay::clear_targets(); });
        self.redraw(cx);
    }

    async fn check_source(&self, cx: &AsyncApp) -> Result<(), String> {
        let root = self.query(cx, |workspace, _| workspace.folder.clone()).ok_or("Notebook missing")?;
        let source = root.join("source-smoke.md");
        std::fs::write(&source, "# Source fixture\n\nSandbox source.\n").map_err(|error| error.to_string())?;
        self.win(cx, |window, cx| crate::editor::ai::chat::seed_source_smoke(&root, "source-smoke", window, cx))??;
        sleep(cx, 250).await; cx.update(|_| smoke_overlay::clear_targets()); self.redraw(cx);
        self.click_target(cx, "ai-sources-toggle", "展开真实来源").await?;
        self.redraw(cx);
        self.check(14, smoke_overlay::target_bounds("ai-source-0-0").is_some(), "真实来源可定位")?;
        self.capture_ai(cx, "source")?;
        self.click_target(cx, "ai-source-0-0", "打开来源笔记").await?;
        let opened = self.wait_until(cx, 4000, |workspace, cx| workspace.editor.read(cx).current_path() == Some(source.as_path()) && !workspace.editor.read(cx).is_loading()).await;
        self.check(14, opened, "来源点击后打开沙箱笔记")?;
        let code = "普通代码回复\n```rust\nfn main() { println!(\"hello\"); }\n```";
        self.win(cx, |window, cx| crate::editor::ai::chat::set_stream_smoke(code, true, window, cx))??;
        self.redraw(cx);
        self.check(14, cx.update(|_| crate::editor::ai::chat::smoke_visible_replies()).iter().any(|reply| reply == code), "普通代码流式回复完整显示")?;
        self.win(cx, |window, cx| crate::editor::ai::chat::set_stream_smoke(code, false, window, cx))??;
        self.redraw(cx);
        self.check(14, cx.update(|_| crate::editor::ai::chat::smoke_visible_replies()).iter().any(|reply| reply == code), "普通代码完成后仍完整显示")?;
        self.click_target(cx, "ai-follow-up-0", "点击追问建议").await?;
        let draft = self.win(cx, crate::editor::ai::chat::input_smoke::snapshot)?.0;
        self.check(14, !draft.is_empty(), "追问建议填入输入框")
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
        self.redraw(cx);
        let message = smoke_overlay::target_bounds("ai-assistant-message").ok_or("Missing reply")?;
        let button = smoke_overlay::target_bounds("ai-reply-copy").ok_or("Missing reply copy")?;
        self.check(14, (button.right() - message.right()).abs() < px(1.) && button.top() >= message.bottom(), "复制按钮在回复右下角")?;
        for (position, visible) in [(point(px(1.), px(1.)), false), (message.center(), true), (point(px(1.), px(1.)), false)] {
            self.win(cx, |window, cx| {
                window.dispatch_event(PlatformInput::MouseMove(MouseMoveEvent { position, modifiers: Modifiers::default(), pressed_button: None }), cx);
            })?;
            cx.update(|_| smoke_overlay::clear_targets());
            self.redraw(cx);
            self.check(14, smoke_overlay::target_bounds("ai-reply-copy-visible").is_some() == visible, "复制按钮仅在悬停回复时绘制，移出后隐藏")?;
        }
        self.click_target(cx, "ai-reply-copy", "悬停回复并点击复制图标").await?;
        let copied = cx.update(|cx| cx.read_from_clipboard().and_then(|item| item.text()).unwrap_or_default());
        self.check(14, copied == "回复 **加粗** 中文 Alpha\n\n第二段 Beta 结束", "复制按钮保留完整 Markdown")?;
        Ok(())
    }
}
