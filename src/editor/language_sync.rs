//! Refresh cached editor status strings and input placeholders after a language switch.
use crate::theme::tr;
use super::Editor;

fn localized_status(status: &str) -> Option<&'static str> {
    Some(match status {
        "未保存更改" | "Unsaved changes" => tr("未保存更改", "Unsaved changes"),
        "正在保存…" | "Saving…" => tr("正在保存…", "Saving…"),
        "已保存" | "Saved" => tr("已保存", "Saved"),
        "正在打开…" | "Opening…" => tr("正在打开…", "Opening…"),
        _ => return None,
    })
}

impl Editor {
    pub(crate) fn sync_language(&mut self, window: &mut gpui::Window, cx: &mut gpui::Context<Self>) {
        if let Some(next) = localized_status(&self.status) { self.status = next.into(); }
        for session in self.sessions.values_mut() {
            if let Some(next) = localized_status(&session.status) { session.status = next.into(); }
        }
        let ai = self.ai_session().map(|s| {
            let review = matches!(s.status, super::super::ai::session::AiStatus::Review { .. });
            (s.input.clone(), review)
        });
        if let Some((input, review)) = ai {
            let ph = if review { super::super::ai::session::review_placeholder() } else { super::super::ai::session::input_placeholder() };
            input.update(cx, |i, cx| i.set_placeholder(ph, window, cx));
        }
        if let Some(input) = self.annotate.composer.as_ref().map(|c| c.input.clone()) {
            input.update(cx, |i, cx| i.set_placeholder(tr("输入批注或修改指令…", "Add an annotation or edit instruction…"), window, cx));
        }
        if let Some(input) = self.annotate.followup.as_ref().map(|f| f.input.clone()) {
            input.update(cx, |i, cx| i.set_placeholder(tr("哪里不满意？", "What needs improvement?"), window, cx));
        }
        let link = match &self.format {
            Some(super::super::format_cmds::FormatPop::Link { input, .. }) => Some(input.clone()),
            _ => None,
        };
        if let Some(input) = link {
            input.update(cx, |i, cx| i.set_placeholder(tr("粘贴或输入链接", "Paste or enter a link"), window, cx));
        }
        let alt = match &self.image_menu {
            Some((_, super::super::image_edit::ImagePop::AltText(input))) => Some(input.clone()),
            _ => None,
        };
        if let Some(input) = alt {
            input.update(cx, |i, cx| i.set_placeholder(tr("输入替代文本…", "Enter alt text…"), window, cx));
        }
    }
}
