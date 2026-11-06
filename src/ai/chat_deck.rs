use gpui::{Div, SharedString, div, prelude::*, px};
use super::{backend::{BackendKind, BackendStatus}, backend_ui, chat::AiChat, dsh_ui};
use crate::{theme::{Theme, tr}, ui::ai_controls};

thread_local! {
    static TRIGGER_BOUNDS: std::rc::Rc<std::cell::Cell<Option<gpui::Bounds<gpui::Pixels>>>> = Default::default();
}

pub(super) fn trigger(chat: &AiChat, cx: &mut gpui::Context<AiChat>) -> gpui::Stateful<Div> {
    let kind = chat.selected_backend();
    let model = chat.selected_model();
    let label = if model.is_empty() {
        backend_ui::backend_name(kind).to_owned()
    } else {
        chat.model_label(&model)
    };
    let open = backend_ui::DECK_OPEN.with(std::cell::Cell::get);
    ai_controls::deck_trigger_capsule("ai-deck-trigger", label, open, cx.listener(|chat, _, window, cx| {
        cx.stop_propagation();
        if !backend_ui::close_menu() { backend_ui::open_deck(chat, window, cx); }
        cx.notify();
    })).child(crate::ui::placement::track_bounds(TRIGGER_BOUNDS.with(Clone::clone), open))
}

pub(super) fn view(chat: &AiChat, window: &gpui::Window, cx: &mut gpui::Context<AiChat>) -> Option<gpui::Deferred> {
    if !backend_ui::DECK_OPEN.with(std::cell::Cell::get) { return None; }
    let bounds = TRIGGER_BOUNDS.with(Clone::clone);
    let trigger = bounds.get()?;
    let spec = crate::ui::placement::PlacementSpec::above(trigger);
    let max_height = crate::ui::placement::available_height_for_spec(&spec, window.viewport_size(), window.viewport_size().height);
    let width = px(ai_controls::PANEL_WIDTH).min((window.viewport_size().width - px(16.)).max(px(0.)));
    let theme = Theme::ocean();
    let selected = chat.selected_backend();
    let mut grid = div().flex().flex_col().gap(px(8.));
    for kinds in [[BackendKind::Builtin, BackendKind::Codex], [BackendKind::Claude, BackendKind::Dsh]] {
        let mut row = div().flex().gap(px(8.));
        for kind in kinds {
            let ready = if kind == BackendKind::Builtin { backend_ui::builtin_ready() } else { matches!(chat.backend_status(kind), Some(BackendStatus::Ready { .. })) };
            row = row.child(ai_controls::backend_card(format!("ai-backend-{kind:?}"), backend_ui::backend_name(kind), backend_ui::status_label(kind, chat.backend_status(kind)), kind == selected, ready,
                cx.listener(move |chat, _, window, cx| {
                    cx.stop_propagation();
                    let changed = chat.selected_backend() != kind;
                    chat.select_backend(kind, window, cx);
                    if kind == BackendKind::Dsh { dsh_ui::open(chat, window, cx); } else { dsh_ui::close(); }
                    if changed { chat.refresh_models_for(kind, cx); }
                    cx.notify();
                })));
        }
        grid = grid.child(row);
    }
    let selected_model = chat.selected_model();
    let mut models = chat.model_choices();
    if matches!(selected, BackendKind::Codex | BackendKind::Claude) { models.insert(0, String::new()); }
    let list_h = if selected == BackendKind::Dsh { 64. } else { 176. };
    let mut list = div().id("ai-deck-models").h(px(list_h)).flex_none().overflow_y_scroll().track_scroll(&chat.deck_scroll).flex().flex_col();
    if models.is_empty() {
        let status = match chat.model_status() {
            super::chat::chat_models::ModelStatus::Loading => tr("正在获取模型…", "Loading models…"),
            super::chat::chat_models::ModelStatus::Error(_) => tr("无法获取模型", "Models unavailable"),
            _ => tr("暂无可选模型", "No models available"),
        };
        list = list.child(div().h(px(32.)).px(px(10.)).flex().items_center().text_color(theme.text_secondary).child(status));
    }
    for (index, model) in models.into_iter().enumerate() {
        let label = chat.model_label(&model);
        let active = model == selected_model;
        list = list.child(ai_controls::model_row(SharedString::from(format!("ai-model-{index}")), label, active, cx.listener(move |chat, _, _, cx| {
            cx.stop_propagation();
            match chat.select_model(&model, cx) { Ok(()) => { backend_ui::close_menu(); }, Err(error) => chat.error = Some(error) }
            cx.notify();
        })));
    }
    let deck = ai_controls::panel_shell("ai-agent-deck").w(width).max_h(max_height).overflow_y_scroll().p(px(12.)).rounded(px(14.))
        .child(div().flex().items_center().justify_between().child(div().font_weight(gpui::FontWeight(560.)).child(tr("选择执行后端", "Choose backend")))
            .child(div().text_color(theme.text_secondary).child(tr("点击卡片即切换", "Select a card"))))
        .child(grid.flex_none())
        .children((selected == BackendKind::Dsh).then(|| dsh_ui::inline_view(cx.entity().downgrade())).flatten())
        .child(div().border_t_1().border_color(theme.hairline).pt(px(8.)).flex().items_center().justify_between()
            .child(div().text_color(theme.text_secondary).child(tr("当前后端模型", "Current Backend Models")))
            .child(ai_controls::action_chip("ai-deck-settings", None, tr("AI 设置", "AI settings"), false, false, cx.listener(|chat, _, window, cx| {
                cx.stop_propagation(); backend_ui::close_menu(); chat.input.update(cx, |i, cx| i.focus(window, cx)); window.dispatch_action(Box::new(super::OpenAiSettings), cx);
            }))))
        .child(list);
    Some(crate::ui::placement::smart_popover_with(move |_, _| {
        crate::ui::placement::PlacementSpec::above(bounds.get().unwrap_or(trigger))
    }, deck, 20))
}
