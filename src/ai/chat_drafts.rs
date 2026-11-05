use gpui::{App, Context, Window};
use super::{AiChat, SentDraft};

fn path() -> std::path::PathBuf { crate::files::app_data_dir("Block").join("ai-failed-drafts.json") }
fn active_path() -> std::path::PathBuf { crate::files::app_data_dir("Block").join("ai-active-draft.json") }
#[derive(Default, serde::Serialize, serde::Deserialize)]
pub(super) struct ActiveDraft {
    pub conversation: Option<u64>, pub text: String, pub refs: Vec<String>, pub skills: Vec<String>,
    pub retry: Option<SentDraft>,
}
pub(super) fn load_active() -> ActiveDraft {
    std::fs::read(active_path()).ok().and_then(|data| serde_json::from_slice(&data).ok()).unwrap_or_default()
}
pub(super) fn load() -> Vec<SentDraft> {
    std::fs::read(path()).ok().and_then(|data| serde_json::from_slice(&data).ok()).unwrap_or_default()
}
impl AiChat {
    pub(in crate::editor::ai) fn persist_active_draft(&mut self, cx: &App) {
        let draft = ActiveDraft { conversation: self.current, text: self.input.read(cx).value().to_string(),
            refs: self.refs.clone(), skills: self.skills.clone(), retry: self.retry_draft.clone() };
        let result = (|| {
            let path = active_path();
            if let Some(parent) = path.parent() { std::fs::create_dir_all(parent)?; }
            let data = serde_json::to_vec(&draft).map_err(std::io::Error::other)?;
            std::fs::write(path, data)
        })();
        if let Err(error) = result { self.error = Some(format!("Failed to save draft: {error}")); }
    }
    fn persist_failed_drafts(&mut self) {
        let path = path();
        let result = (|| {
            if let Some(parent) = path.parent() { std::fs::create_dir_all(parent)?; }
            let data = serde_json::to_vec(&self.deferred_drafts).map_err(std::io::Error::other)?;
            std::fs::write(path, data)
        })();
        if let Err(error) = result { self.error = Some(format!("Failed to save draft: {error}")); }
    }

    pub(super) fn restore_failed_draft(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(draft) = self.sent_draft.take() else { return };
        if self.current == Some(draft.conversation) && self.input.read(cx).value().is_empty()
            && self.refs.is_empty() && self.skills.is_empty() {
            let mut removed = false;
            if let Some(conversation) = self.convs.iter_mut().find(|conversation| conversation.id == draft.conversation) {
                if draft.message_index != usize::MAX && conversation.messages.len() == draft.message_index + 1 {
                    conversation.messages.pop(); removed = true;
                }
            }
            self.retry_draft = (!removed && draft.message_index != usize::MAX).then_some(draft.clone());
            self.refs = draft.refs; self.skills = draft.skills;
            self.input.update(cx, |input, cx| input.set_value(draft.text, window, cx));
            self.persist();
            self.persist_active_draft(cx);
        } else {
            self.deferred_drafts.push(draft);
            self.persist_failed_drafts();
        }
    }

    pub(in crate::editor::ai) fn restore_deferred_draft(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(index) = self.deferred_drafts.iter().position(|draft| self.current.is_none() || Some(draft.conversation) == self.current) else { return };
        let draft = self.deferred_drafts.remove(index);
        let text = self.input.read(cx).value().to_string();
        if !text.is_empty() || !self.refs.is_empty() || !self.skills.is_empty() {
            let message_index = self.retry_draft.take().filter(|retry| retry.conversation == draft.conversation
                && retry.text == text && retry.refs == self.refs && retry.skills == self.skills)
                .map_or(usize::MAX, |retry| retry.message_index);
            self.deferred_drafts.push(SentDraft { text, refs: std::mem::take(&mut self.refs), skills: std::mem::take(&mut self.skills),
                conversation: draft.conversation, message_index });
            self.input.update(cx, |input, cx| input.set_value("", window, cx));
        }
        if self.current.is_none() && self.convs.iter().any(|conversation| conversation.id == draft.conversation) {
            self.current = Some(draft.conversation);
        }
        self.sent_draft = Some(draft);
        self.restore_failed_draft(window, cx);
        self.persist_failed_drafts();
        self.persist_active_draft(cx);
        cx.notify();
    }

    pub(in crate::editor::ai) fn has_deferred_draft(&self) -> bool {
        self.deferred_drafts.iter().any(|draft| self.current.is_none() || Some(draft.conversation) == self.current)
    }
}
