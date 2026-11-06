use gpui::{Div, MouseButton, SharedString, div, prelude::*, px};
use super::{backend::{BackendKind, BackendStatus}, backend_ui, chat::AiChat, dsh_ui};
use crate::{theme::{Theme, tr}, ui::{ai_controls, dropdown}};

pub(super) fn trigger(chat: &AiChat, cx: &mut gpui::Context<AiChat>) -> gpui::Stateful<Div> {
    let kind = chat.selected_backend();
    let model = chat.selected_model();
    let model_text = if model.is_empty() { tr("默认模型", "Default model").to_owned() } else { chat.model_label(&model) };
    let label = format!("{} · {}", backend_ui::backend_name(kind), model_text);
    let ready = kind == BackendKind::Builtin || matches!(chat.backend_status(kind), Some(BackendStatus::Ready { .. }));
    let open = backend_ui::DECK_OPEN.with(std::cell::Cell::get);
    ai_controls::deck_trigger_capsule("ai-deck-trigger", label, ready, open, cx.listener(|chat, _, window, cx| {
        cx.stop_propagation();
        if !backend_ui::close_menu() { backend_ui::open_deck(chat, window, cx); }
        cx.notify();
    }))
}

pub(super) fn view(chat: &AiChat, cx: &mut gpui::Context<AiChat>) -> Option<gpui::Stateful<Div>> {
    if !backend_ui::DECK_OPEN.with(std::cell::Cell::get) { return None; }
    let theme = Theme::ocean();
    let selected = chat.selected_backend();
    let mut grid = div().flex().flex_col().gap(px(8.));
    for kinds in [[BackendKind::Builtin, BackendKind::Codex], [BackendKind::Claude, BackendKind::Dsh]] {
        let mut row = div().flex().gap(px(8.));
        for kind in kinds {
            let ready = kind == BackendKind::Builtin || matches!(chat.backend_status(kind), Some(BackendStatus::Ready { .. }));
            row = row.child(ai_controls::backend_card(format!("ai-backend-{kind:?}"), backend_ui::backend_name(kind), backend_ui::status_label(kind, chat.backend_status(kind)), kind == selected, ready,
                cx.listener(move |chat, _, window, cx| {
                    cx.stop_propagation();
                    chat.select_backend(kind, window, cx);
                    if kind == BackendKind::Dsh { dsh_ui::open(chat, window, cx); } else { dsh_ui::close(); }
                    chat.refresh_models(cx); cx.notify();
                })));
        }
        grid = grid.child(row);
    }
    let selected_model = chat.selected_model();
    let mut models = chat.model_choices();
    if matches!(selected, BackendKind::Codex | BackendKind::Claude) { models.insert(0, String::new()); }
    let mut list = div().id("ai-deck-models").max_h(px(160.)).overflow_y_scroll().flex().flex_col();
    for (index, model) in models.into_iter().enumerate() {
        let label = chat.model_label(&model);
        let active = model == selected_model;
        list = list.child(dropdown::row_with(SharedString::from(format!("ai-model-{index}")), dropdown::RowSpec {
            label: label.into(), checked: active, ..Default::default()
        }).h(px(32.)).when(active, |row| row.bg(theme.selected)).on_mouse_down(MouseButton::Left, cx.listener(move |chat, _, _, cx| {
            cx.stop_propagation();
            match chat.select_model(&model, cx) { Ok(()) => { backend_ui::close_menu(); }, Err(error) => chat.error = Some(error) }
            cx.notify();
        })));
    }
    Some(ai_controls::panel_shell("ai-agent-deck").p(px(12.)).rounded(px(14.))
        .child(div().flex().items_center().justify_between().child(div().font_weight(gpui::FontWeight(560.)).child(tr("选择执行后端", "Choose backend")))
            .child(div().text_color(theme.text_secondary).child(tr("点击卡片即切换", "Select a card"))))
        .child(grid)
        .children((selected == BackendKind::Dsh).then(|| dsh_ui::inline_view(cx.entity().downgrade())).flatten())
        .child(div().border_t_1().border_color(theme.hairline).pt(px(8.)).flex().items_center().justify_between()
            .child(div().text_color(theme.text_secondary).child(tr("当前后端模型", "Current Backend Models")))
            .child(ai_controls::action_chip("ai-deck-settings", None, tr("AI 设置", "AI settings"), false, false, |_, window, cx| {
                cx.stop_propagation(); backend_ui::close_menu(); window.dispatch_action(Box::new(super::OpenAiSettings), cx);
            })))
        .child(list)
        .children(match chat.model_status() {
            super::chat::chat_models::ModelStatus::Loading => Some(tr("正在获取模型…", "Loading models…")),
            super::chat::chat_models::ModelStatus::Error(_) => Some(tr("无法获取模型", "Models unavailable")),
            _ => None,
        }.map(|status| div().text_color(theme.text_secondary).child(status))))
}
