use std::{cell::{Cell, RefCell}, rc::Rc};
use gpui::{App, IntoElement, Window};
use super::{backend::{ApprovalResponse, BackendKind}, chat::AiChat};
use crate::theme::tr;

thread_local! { pub(super) static DECK_OPEN: Cell<bool> = const { Cell::new(false) }; }
pub(super) fn close_menu() -> bool { DECK_OPEN.with(|open| open.replace(false)) }

pub(super) fn open_deck(chat: &mut AiChat, _window: &mut Window, cx: &mut gpui::Context<AiChat>) {
    DECK_OPEN.with(|open| open.set(true));
    chat.history = false;
    chat.picker_dismissed = true;
    for kind in BackendKind::ALL { chat.probe_backend(kind, cx); }
    chat.refresh_models(cx);
    cx.notify();
}

pub(super) fn backend_name(backend: BackendKind) -> &'static str {
    match backend {
        BackendKind::Builtin => tr("内置 AI", "Built-in AI"),
        BackendKind::Codex => "Codex",
        BackendKind::Claude => "Claude",
        BackendKind::Dsh => "dsh",
        BackendKind::Pi => "Pi",
    }
}

pub(super) fn builtin_ready() -> bool {
    super::provider::validate_request(&super::provider::config()).is_ok()
}

pub(super) fn approval_card(state: Rc<RefCell<crate::ui::ai_approval::ApprovalState>>, title: String, detail: String,
    on_response: impl Fn(ApprovalResponse, &mut Window, &mut App) -> bool + 'static) -> impl IntoElement {
    crate::ui::ai_approval::approval_card(state, title, detail, on_response)
}
