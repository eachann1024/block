use gpui::{App, Context, Window};
use super::chat_draft_state::{self, ComposerDraft, DraftBox};
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
    fn take_box(&mut self, cx: &App) -> DraftBox {
        DraftBox {
            current: self.current,
            composer: ComposerDraft {
                text: self.input.read(cx).value().to_string(),
                refs: std::mem::take(&mut self.refs),
                skills: std::mem::take(&mut self.skills),
            },
            sent: self.sent_draft.take(),
            deferred: std::mem::take(&mut self.deferred_drafts),
            retry: self.retry_draft.take(),
        }
    }

    fn install_box(&mut self, state: DraftBox) {
        self.current = state.current;
        self.refs = state.composer.refs;
        self.skills = state.composer.skills;
        self.sent_draft = state.sent;
        self.deferred_drafts = state.deferred;
        self.retry_draft = state.retry;
    }

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
        if self.sent_draft.is_none() { return; }
        let message_len = self.sent_draft.as_ref().and_then(|draft| {
            self.convs.iter().find(|conversation| conversation.id == draft.conversation).map(|conversation| conversation.messages.len())
        });
        let outcome = match chat_draft_state::restore_failed(self.take_box(cx), message_len) {
            Ok(outcome) => outcome,
            Err(state) => { self.install_box(state); return; }
        };
        if let Some(id) = outcome.pop_conversation {
            if let Some(conversation) = self.convs.iter_mut().find(|conversation| conversation.id == id) {
                conversation.messages.pop();
            }
        }
        self.current = outcome.current;
        self.refs = outcome.refs;
        self.skills = outcome.skills;
        self.retry_draft = outcome.retry;
        self.deferred_drafts = outcome.deferred;
        self.sent_draft = None;
        if outcome.replace_text {
            let text = outcome.text;
            self.input.update(cx, |input, cx| input.set_value(text, window, cx));
        }
        if outcome.write_conversations { self.persist(); }
        if outcome.write_active { self.persist_active_draft(cx); }
        if outcome.write_failed { self.persist_failed_drafts(); }
    }

    pub(in crate::editor::ai) fn restore_deferred_draft(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !chat_draft_state::has_deferred(&self.deferred_drafts, self.current) { return; }
        let known: Vec<u64> = self.convs.iter().map(|conversation| conversation.id).collect();
        let parked = chat_draft_state::park_deferred(self.take_box(cx), |id| known.contains(&id));
        if !parked.applied {
            self.install_box(parked.state);
            return;
        }
        let clear_text = parked.clear_text;
        self.install_box(parked.state);
        if clear_text {
            self.input.update(cx, |input, cx| input.set_value("", window, cx));
        }
        self.restore_failed_draft(window, cx);
        self.persist_failed_drafts();
        self.persist_active_draft(cx);
        cx.notify();
    }

    pub(in crate::editor::ai) fn has_deferred_draft(&self) -> bool {
        chat_draft_state::has_deferred(&self.deferred_drafts, self.current)
    }
}
