use std::{cell::Cell, rc::Rc};
use gpui::{App, IntoElement, Window, div, prelude::*, px};
use super::{backend::{ApprovalResponse, BackendKind}, chat::AiChat};
use crate::{theme::{Theme, tr}, ui::ai_controls};

thread_local! { pub(super) static DECK_OPEN: Cell<bool> = const { Cell::new(false) }; }
pub(super) fn close_menu() -> bool { DECK_OPEN.with(|open| open.replace(false)) }

pub(super) fn open_deck(chat: &mut AiChat, window: &mut Window, cx: &mut gpui::Context<AiChat>) {
    DECK_OPEN.with(|open| open.set(true));
    chat.history = false;
    chat.picker_dismissed = true;
    if chat.selected_backend() == BackendKind::Dsh && !super::dsh_ui::is_open() { super::dsh_ui::open(chat, window, cx); }
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

pub(super) fn approval_card(title: String, detail: String, on_response: impl Fn(ApprovalResponse, &mut Window, &mut App) + 'static) -> impl IntoElement {
    let theme = Theme::ocean();
    let on_response = Rc::new(on_response);
    let approve = on_response.clone();
    div().flex().flex_col().gap(px(10.)).p(px(14.)).rounded(crate::ui::radius::SURFACE)
        .border_1().border_color(theme.btn_border).bg(theme.paper).text_size(px(14.))
        .child(div().font_weight(gpui::FontWeight(600.)).child(title))
        .when(!detail.is_empty(), |card| card.child(div().text_color(theme.text_secondary).child(detail)))
        .child(div().flex().gap(px(8.))
            .child(ai_controls::action_chip("ai-approval-allow", None, tr("允许", "Allow"), true, false,
                move |_, window, cx| approve(ApprovalResponse::AllowOnce, window, cx)).h(px(32.)))
            .child(ai_controls::action_chip("ai-approval-deny", None, tr("拒绝", "Deny"), false, false,
                move |_, window, cx| on_response(ApprovalResponse::Deny, window, cx)).h(px(32.)).border_1().border_color(theme.btn_border)))
}
