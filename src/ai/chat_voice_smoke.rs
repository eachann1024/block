use gpui::{App, Window};
use super::{Holder, chat, input_smoke};

pub(crate) fn prepare(window: &mut Window, cx: &mut App) -> (u64, u64) {
    assert!(crate::smoke::is_requested());
    let ids = input_smoke::seed_tab_conversations(window, cx);
    chat(window, cx).update(cx, |chat, cx| {
        chat.convs.retain(|conversation| conversation.id == ids.0 || conversation.id == ids.1);
        cx.notify();
    });
    ids
}

pub(crate) fn message_count(cx: &App) -> usize {
    assert!(crate::smoke::is_requested());
    cx.try_global::<Holder>().map_or(0, |holder| {
        holder.0.read(cx).convs.iter().map(|conversation| conversation.messages.len()).sum()
    })
}
