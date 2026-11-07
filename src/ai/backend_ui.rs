use std::{cell::{Cell, RefCell}, rc::Rc};
use gpui::{App, AppContext, Entity, IntoElement, Subscription, Window};
use gpui_component::input::{InputEvent, InputState};
use super::{backend::{ApprovalResponse, BackendKind}, chat::AiChat};
use crate::theme::tr;

thread_local! {
    pub(super) static DECK_OPEN: Cell<bool> = const { Cell::new(false) };
    static DECK_SEARCH: RefCell<Option<(Entity<InputState>, Subscription)>> = const { RefCell::new(None) };
}

pub(super) fn close_menu() -> bool { DECK_OPEN.with(|open| open.replace(false)) }

pub(super) fn open_deck(chat: &mut AiChat, window: &mut Window, cx: &mut gpui::Context<AiChat>) {
    DECK_OPEN.with(|open| open.set(true));
    let placeholder = tr("搜索模型…", "Search models…");
    DECK_SEARCH.with(|slot| {
        if let Some((input, _)) = slot.borrow().as_ref() {
            input.update(cx, |i, cx| { i.set_placeholder(placeholder, window, cx); i.set_value("", window, cx); });
        } else {
            let input = cx.new(|cx| InputState::new(window, cx).placeholder(placeholder));
            let sub = cx.subscribe(&input, |_, _, event: &InputEvent, cx| {
                if matches!(event, InputEvent::Change) { cx.notify(); }
            });
            *slot.borrow_mut() = Some((input, sub));
        }
    });
    chat.history = false;
    chat.picker_dismissed = true;
    for kind in BackendKind::ALL { chat.probe_backend(kind, cx); }
    chat.refresh_models(cx);
    cx.notify();
}

pub(super) fn search_input() -> Option<Entity<InputState>> {
    DECK_SEARCH.with(|slot| slot.borrow().as_ref().map(|(input, _)| input.clone()))
}

pub(super) fn search_query(cx: &App) -> String {
    search_input().map(|input| input.read(cx).value().trim().to_lowercase()).unwrap_or_default()
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

pub(super) fn backend_logo(backend: BackendKind) -> Option<&'static str> {
    match backend {
        BackendKind::Builtin => None,
        BackendKind::Codex => Some("agents/codex.svg"),
        BackendKind::Claude => Some("agents/claude.svg"),
        BackendKind::Dsh => Some("agents/dsh.svg"),
        BackendKind::Pi => Some("agents/pi.svg"),
    }
}

pub(super) fn thinking_label(level: &str) -> &'static str {
    match level {
        "low" => "Low",
        "medium" => "Medium",
        _ => "High",
    }
}

pub(super) fn builtin_ready() -> bool {
    super::provider::validate_request(&super::provider::config()).is_ok()
}

pub(super) fn approval_card(state: Rc<RefCell<crate::ui::ai_approval::ApprovalState>>, title: String, detail: String,
    on_response: impl Fn(ApprovalResponse, &mut Window, &mut App) -> bool + 'static) -> impl IntoElement {
    crate::ui::ai_approval::approval_card(state, title, detail, on_response)
}

pub(super) fn question_card(state: Rc<RefCell<crate::ui::ai_approval::ApprovalState>>, questions: Vec<crate::ui::ai_approval::ApprovalQuestion>,
    window: &mut Window, cx: &mut App, on_submit: impl Fn(serde_json::Value, &mut Window, &mut App) -> bool + 'static) -> gpui::AnyElement {
    crate::ui::ai_approval::question_card(state, questions, window, cx, on_submit)
}
