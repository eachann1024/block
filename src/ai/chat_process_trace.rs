use super::super::chat::{AiChat, Msg};

pub(super) fn now_ms() -> u64 { std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap_or_default().as_millis() as u64 }

pub(in crate::editor::ai) fn record_activity(chat: &mut AiChat, name: String) {
    let name: String = name.chars().filter(|c| c.is_alphanumeric() || matches!(c, '_' | '-' | '.')).take(80).collect();
    if name.is_empty() { return; }
    if let Some(conversation) = chat.current.and_then(|id| chat.convs.iter_mut().find(|conversation| conversation.id == id)) {
        if conversation.messages.last().is_some_and(|message| message.role == "activity" && message.content == name) { return; }
        conversation.messages.push(Msg { role: "activity".into(), content: name, ..Default::default() });
    }
}

pub(in crate::editor::ai) fn record_summary(chat: &mut AiChat, text: String) {
    if let Some(conversation) = chat.current.and_then(|id| chat.convs.iter_mut().find(|conversation| conversation.id == id)) {
        if !conversation.messages.last().is_some_and(|message| message.role == "public_summary") {
            conversation.messages.push(Msg { role: "public_summary".into(), ..Default::default() });
        }
        if let Some(message) = conversation.messages.last_mut() {
            let remaining = 4096usize.saturating_sub(message.content.chars().count());
            message.content.extend(text.chars().take(remaining));
        }
    }
}

pub(in crate::editor::ai) fn record_outcome(chat: &mut AiChat, outcome: &str) {
    if let Some(message) = chat.current.and_then(|id| chat.convs.iter_mut().find(|c| c.id == id)).and_then(|c| c.messages.iter_mut().rev().find(|m| m.role == "user")) {
        if let Some((_, end)) = &mut message.timing { *end = Some(now_ms()); }
    }
    chat.push_msg(Msg { role: "turn_outcome".into(), content: outcome.into(), ..Default::default() });
}

pub(in crate::editor::ai) fn start_turn(chat: &mut AiChat) {
    if let Some(message) = chat.current.and_then(|id| chat.convs.iter_mut().find(|c| c.id == id)).and_then(|c| c.messages.iter_mut().rev().find(|m| m.role == "user")) {
        message.timing = Some((now_ms(), None));
        chat.persist();
    }
}
