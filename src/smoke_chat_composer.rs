use gpui::{ClipboardItem, px};
use super::{AsyncApp, Run, sleep, smoke_overlay};

impl Run {
    pub(super) async fn verify_backends_and_settings(&self, cx: &AsyncApp) -> Result<(), String> {
        use crate::editor::ai::{backend::BackendKind, chat::input_smoke};
        self.win(cx, input_smoke::seed_all_backends_ready)?;
        sleep(cx, 120).await;
        self.redraw(cx);
        self.click_target(cx, "ai-deck-trigger", "Step 14/14 · 打开后端选择舱验证单行卡片与 Pi").await?;
        let card = smoke_overlay::target_bounds("ai-backend-Builtin").ok_or("找不到 ai-backend-Builtin")?;
        self.check(14, card.size.height <= px(42.), "后端卡片已移除可用副标题并压缩为单行")?;
        self.check(14, smoke_overlay::target_center("ai-backend-Pi").is_some(), "默认展示已识别的 Pi 后端")?;
        self.click_target(cx, "ai-backend-Pi", "Step 14/14 · 切换到 Pi 后端").await?;
        self.check(14, self.win(cx, input_smoke::selected_backend)? == BackendKind::Pi, "已切换到 Pi 后端")?;
        self.win(cx, |w, cx| input_smoke::set_backend_missing(BackendKind::Pi, w, cx))?;
        sleep(cx, 120).await;
        self.redraw(cx);
        self.check(14, smoke_overlay::target_center("ai-backend-Pi").is_none() && self.win(cx, input_smoke::selected_backend)? != BackendKind::Pi, "未识别后端在命令舱中隐藏并自动回退选中项")?;
        self.click_target(cx, "ai-deck-settings", "Step 14/14 · 从命令舱进入 AI 设置").await?;
        self.check(14, self.query(cx, |w, _| w.settings_is_open()) && smoke_overlay::target_center("ai-backend-toggle-Pi").is_none(), "未识别后端在 AI 设置中也隐藏")?;
        self.win(cx, input_smoke::seed_all_backends_ready)?;
        sleep(cx, 120).await;
        self.redraw(cx);
        self.check(14, smoke_overlay::target_center("ai-backend-toggle-Pi").is_some(), "识别后的 Pi 后端在 AI 设置中显示")?;
        for (id, kind) in [
            ("ai-backend-toggle-Builtin", BackendKind::Builtin),
            ("ai-backend-toggle-Codex", BackendKind::Codex),
            ("ai-backend-toggle-Claude", BackendKind::Claude),
            ("ai-backend-toggle-Dsh", BackendKind::Dsh),
        ] {
            self.click_target(cx, id, "Step 14/14 · 禁用后端验证内置与外部后端均可关闭").await?;
            self.check(14, !input_smoke::is_backend_enabled(kind), "已在 AI 设置中禁用该后端")?;
        }
        self.click_target(cx, "ai-backend-toggle-Pi", "Step 14/14 · 尝试禁用仅剩的最后一个启用后端").await?;
        self.check(14, input_smoke::is_backend_enabled(BackendKind::Pi), "仅剩一个启用后端时禁止关闭")?;
        for id in ["ai-backend-toggle-Builtin", "ai-backend-toggle-Codex", "ai-backend-toggle-Claude", "ai-backend-toggle-Dsh"] {
            self.click_target(cx, id, "Step 14/14 · 重新启用全部已识别后端").await?;
        }
        self.click_target(cx, "settings-back", "Step 14/14 · 从 AI 设置返回编辑").await?;
        Ok(())
    }

    pub(super) async fn chat_composer(&self, cx: &AsyncApp) -> Result<(), String> {
        self.win(cx, crate::editor::ai::chat::prepare_selection_smoke)??;
        sleep(cx, 250).await;
        let clipboard = cx.update(|cx| cx.read_from_clipboard());
        let result = self.check_composer(cx).await;
        cx.update(|cx| cx.write_to_clipboard(clipboard.unwrap_or_else(|| ClipboardItem::new_string(String::new()))));
        result
    }

    async fn check_composer(&self, cx: &AsyncApp) -> Result<(), String> {
        for fullscreen in [false, true] {
            if fullscreen { self.click_target(cx, "ai-layout-toggle", "切换全屏验证新会话焦点").await?; }
            self.click_target(cx, "ai-chat-history", "将焦点移出输入框").await?;
            self.click_target(cx, "ai-chat-new", "点击新会话后直接输入").await?;
            self.check(14, self.win(cx, crate::editor::ai::chat::input_smoke::is_focused)?, "新会话自动聚焦输入框")?;
            self.type_text(cx, "New chat focus").await?;
            self.check(14, self.win(cx, crate::editor::ai::chat::input_smoke::snapshot)?.0 == "New chat focus", "无需再次点击即可输入")?;
            self.click_target(cx, "ai-chat-new", "重复点击新会话清空并保持焦点").await?;
            self.check(14, self.win(cx, crate::editor::ai::chat::input_smoke::is_focused)? && self.win(cx, crate::editor::ai::chat::input_smoke::snapshot)?.0.is_empty(), "重复新建后为空且聚焦")?;
        }
        self.check_picker(cx).await?;
        self.click_target(cx, "ai-layout-toggle", "返回侧栏").await?;
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
    async fn check_picker(&self, cx: &AsyncApp) -> Result<(), String> {
        use crate::editor::ai::chat::input_smoke;
        self.win(cx, input_smoke::prepare_picker)??;
        let result = self.exercise_picker(cx).await;
        self.win(cx, input_smoke::cleanup_picker)?;
        result
    }

    async fn exercise_picker(&self, cx: &AsyncApp) -> Result<(), String> {
        use crate::editor::ai::chat::input_smoke;
        self.type_text(cx, "@").await?;
        for _ in 0..100 {
            self.redraw(cx);
            let state = self.win(cx, input_smoke::picker_state)?;
            if !state.0 && self.win(cx, input_smoke::picker_folder_state)?.1 >= 1200 { break; }
            sleep(cx, 50).await;
        }
        let state = self.win(cx, input_smoke::picker_state)?;
        let folder = self.win(cx, input_smoke::picker_folder_state)?;
        self.check(14, !state.0 && folder.1 >= 1200 && state.4 < 100, "1200 篇文件后台加载，候选过滤小于 100ms")?;
        let index = folder.2.ok_or("Missing reference folder")?;
        self.click_target(cx, &format!("ai-pick-{index}"), "点击文件夹浏览引用文件").await?;
        self.check(14, self.win(cx, input_smoke::picker_folder_state)?.0 == "picker-smoke" && self.win(cx, input_smoke::picker_state)?.2.is_empty(), "文件夹只导航，不添加引用")?;
        self.check(14, self.win(cx, input_smoke::picker_state)?.1 >= 1202, "文件夹显示文件及返回上级")?;
        self.click_target(cx, "ai-pick-0", "返回上级文件夹").await?;
        self.check(14, self.win(cx, input_smoke::picker_folder_state)?.0.is_empty(), "返回上级成功")?;
        self.type_text(cx, "picker-smoke/note-1199").await?;
        self.check(14, self.win(cx, input_smoke::picker_state)?.1 == 1, "虚拟列表仍可搜索末尾笔记")?;
        self.press(cx, "enter").await?;
        let state = self.win(cx, input_smoke::picker_state)?;
        let input = self.win(cx, input_smoke::snapshot)?.0;
        self.check(14, state.2.contains(&"picker-smoke/note-1199".to_string()), &format!("回车选中引用: {state:?}, input={input:?}"))?;
        self.type_text(cx, "/picker-fixture").await?;
        self.check(14, self.win(cx, input_smoke::picker_state)?.1 == 1, "斜杠筛选后台加载的技能")?;
        self.press(cx, "down").await?;
        self.press(cx, "enter").await?;
        self.check(14, self.win(cx, input_smoke::picker_state)?.3.contains(&"picker-fixture".to_string()), "方向键与回车选中技能")?;
        self.type_text(cx, "@").await?;
        self.press(cx, "escape").await?;
        self.check(14, self.win(cx, input_smoke::snapshot)?.0 == "@" && self.win(cx, input_smoke::picker_state)?.1 == 0, "Escape 关闭候选并保留输入")?;
        Ok(())
    }

}
