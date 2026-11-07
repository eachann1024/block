use gpui::{Modifiers, MouseButton, MouseDownEvent, MouseUpEvent, PlatformInput};
use super::{AsyncApp, Run, sleep, smoke_overlay};
use crate::editor::ai::chat::{input_smoke, voice_smoke};

impl Run {
    pub(super) async fn beautiful_voice(&self, cx: &AsyncApp) -> Result<(), String> {
        self.win(cx, crate::editor::ai::chat::prepare_selection_smoke)??;
        let (first, second) = self.win(cx, voice_smoke::prepare)?;
        self.redraw(cx);
        let messages = cx.update(|cx| voice_smoke::message_count(cx));

        self.click_target(cx, "ai-dictation", "开始模拟语音输入").await?;
        let old = cx.update(|cx| input_smoke::dictation_generation(cx)).ok_or("语音未开始")?;
        self.check(14, cx.update(|cx| input_smoke::dictation_recording(cx)), "语音按钮进入录音状态")?;
        self.check(14, input_smoke::dictation_text(old, "voice"), "注入语音识别结果")?;
        sleep(cx, 120).await;
        self.check(14, self.win(cx, input_smoke::snapshot)?.0 == "voice" && cx.update(|cx| voice_smoke::message_count(cx)) == messages, "识别结果写入草稿且未发送")?;

        self.redraw(cx);
        let center = smoke_overlay::target_center("ai-dictation").ok_or("找不到语音按钮")?;
        let new = self.win(cx, |window, cx| {
            for _ in 0..2 {
                window.dispatch_event(PlatformInput::MouseDown(MouseDownEvent { position: center, button: MouseButton::Left, modifiers: Modifiers::default(), click_count: 1, first_mouse: false }), cx);
                window.dispatch_event(PlatformInput::MouseUp(MouseUpEvent { position: center, button: MouseButton::Left, modifiers: Modifiers::default(), click_count: 1 }), cx);
            }
            let generation = input_smoke::dictation_generation(cx);
            (generation, input_smoke::dictation_text(old, "stale"), input_smoke::dictation_done(old))
        })?;
        let generation = new.0.ok_or("快速重新开始失败")?;
        self.check(14, generation != old && new.1 && new.2, "快速停止并重启后注入旧会话事件")?;
        sleep(cx, 120).await;
        self.check(14, cx.update(|cx| input_smoke::dictation_recording(cx)) && self.win(cx, input_smoke::snapshot)?.0 == "voice", "旧会话事件不影响新录音和草稿")?;
        self.check(14, input_smoke::dictation_text(generation, " new") && input_smoke::dictation_done(generation), "注入新会话结果")?;
        sleep(cx, 120).await;
        self.check(14, !cx.update(|cx| input_smoke::dictation_recording(cx)) && self.win(cx, input_smoke::snapshot)?.0 == "voice new"
            && cx.update(|cx| voice_smoke::message_count(cx)) == messages, "完成识别后保留草稿且不发送")?;

        self.click_target(cx, "ai-dictation", "再次开始录音").await?;
        self.check(14, cx.update(|cx| input_smoke::dictation_recording(cx)), "手改前正在录音")?;
        self.click_target(cx, "ai-composer-input", "手动修改草稿").await?;
        self.type_text(cx, "x").await?;
        let edited = self.win(cx, input_smoke::snapshot)?.0;
        sleep(cx, 120).await;
        self.check(14, !cx.update(|cx| input_smoke::dictation_recording(cx)) && self.win(cx, input_smoke::snapshot)?.0 == edited, "手改草稿后停止识别并保留修改")?;

        self.click_target(cx, "ai-dictation", "验证切换会话停录").await?;
        self.click_target(cx, "ai-chat-history", "打开历史会话").await?;
        self.click_target(cx, &format!("conv-{second}"), "切换会话").await?;
        let switched = cx.update(|cx| input_smoke::tab_observation(cx)).ok_or("缺少会话状态")?;
        self.check(14, !cx.update(|cx| input_smoke::dictation_recording(cx)) && switched.0 == Some(second) && switched.1.is_empty(), "切换会话释放录音并保留独立草稿")?;
        self.click_target(cx, "ai-chat-history", "再次打开历史会话").await?;
        self.click_target(cx, &format!("conv-{first}"), "切回原会话").await?;
        self.check(14, self.win(cx, input_smoke::snapshot)?.0 == edited, "切回后恢复原草稿")?;
        self.click_target(cx, "ai-dictation", "验证关闭面板停录").await?;
        self.click_target(cx, "ai-chat-close", "关闭 AI 面板").await?;
        self.check(14, !cx.update(|cx| input_smoke::dictation_recording(cx)) && !cx.update(|cx| crate::editor::ai::chat::is_open(cx))
            && cx.update(|cx| voice_smoke::message_count(cx)) == messages, "关闭面板释放录音且没有发送")?;
        Ok(())
    }
}
