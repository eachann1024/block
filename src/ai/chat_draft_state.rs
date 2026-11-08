//! Pure draft transitions. The composer widget and JSON files stay in `chat_drafts`.
use super::SentDraft;

#[derive(Clone)]
pub(super) struct ComposerDraft {
    pub text: String,
    pub refs: Vec<String>,
    pub skills: Vec<String>,
}

impl ComposerDraft {
    fn is_blank(&self) -> bool {
        self.text.is_empty() && self.refs.is_empty() && self.skills.is_empty()
    }
}

#[derive(Clone)]
pub(super) struct DraftBox {
    pub current: Option<u64>,
    pub composer: ComposerDraft,
    pub sent: Option<SentDraft>,
    pub deferred: Vec<SentDraft>,
    pub retry: Option<SentDraft>,
}

pub(super) fn known_conversation(saved: Option<u64>, ids: impl IntoIterator<Item = u64>) -> Option<u64> {
    let saved = saved?;
    ids.into_iter().any(|id| id == saved).then_some(saved)
}

pub(super) fn retry_for_conversation(retry: Option<SentDraft>, current: Option<u64>) -> Option<SentDraft> {
    retry.filter(|draft| Some(draft.conversation) == current)
}

pub(super) fn has_deferred(deferred: &[SentDraft], current: Option<u64>) -> bool {
    deferred.iter().any(|draft| current.is_none() || Some(draft.conversation) == current)
}

pub(super) struct FailedRestore {
    pub current: Option<u64>,
    pub text: String,
    pub refs: Vec<String>,
    pub skills: Vec<String>,
    pub retry: Option<SentDraft>,
    pub deferred: Vec<SentDraft>,
    pub pop_conversation: Option<u64>,
    pub replace_text: bool,
    pub write_conversations: bool,
    pub write_active: bool,
    pub write_failed: bool,
}

pub(super) fn restore_failed(mut state: DraftBox, message_len: Option<usize>) -> Result<FailedRestore, DraftBox> {
    let Some(draft) = state.sent.take() else { return Err(state); };
    if state.current == Some(draft.conversation) && state.composer.is_blank() {
        let pop = draft.message_index != usize::MAX && message_len == Some(draft.message_index + 1);
        let retry = (!pop && draft.message_index != usize::MAX).then(|| draft.clone());
        Ok(FailedRestore {
            current: state.current,
            text: draft.text,
            refs: draft.refs,
            skills: draft.skills,
            retry,
            deferred: state.deferred,
            pop_conversation: pop.then_some(draft.conversation),
            replace_text: true,
            write_conversations: true,
            write_active: true,
            write_failed: false,
        })
    } else {
        state.deferred.push(draft);
        Ok(FailedRestore {
            current: state.current,
            text: state.composer.text,
            refs: state.composer.refs,
            skills: state.composer.skills,
            retry: state.retry,
            deferred: state.deferred,
            pop_conversation: None,
            replace_text: false,
            write_conversations: false,
            write_active: false,
            write_failed: true,
        })
    }
}

pub(super) struct ParkedDeferred {
    pub state: DraftBox,
    pub clear_text: bool,
    pub applied: bool,
}

pub(super) fn park_deferred(mut state: DraftBox, conversation_exists: impl Fn(u64) -> bool) -> ParkedDeferred {
    let Some(index) = state.deferred.iter().position(|draft| state.current.is_none() || Some(draft.conversation) == state.current) else {
        return ParkedDeferred { state, clear_text: false, applied: false };
    };
    let draft = state.deferred.remove(index);
    let mut clear_text = false;
    if !state.composer.is_blank() {
        let message_index = match state.retry.take() {
            Some(retry)
                if retry.conversation == draft.conversation
                    && retry.text == state.composer.text
                    && retry.refs == state.composer.refs
                    && retry.skills == state.composer.skills =>
            {
                retry.message_index
            }
            _ => usize::MAX,
        };
        state.deferred.push(SentDraft {
            text: std::mem::take(&mut state.composer.text),
            refs: std::mem::take(&mut state.composer.refs),
            skills: std::mem::take(&mut state.composer.skills),
            conversation: draft.conversation,
            message_index,
        });
        clear_text = true;
    }
    if state.current.is_none() && conversation_exists(draft.conversation) {
        state.current = Some(draft.conversation);
    }
    state.sent = Some(draft);
    ParkedDeferred { state, clear_text, applied: true }
}
