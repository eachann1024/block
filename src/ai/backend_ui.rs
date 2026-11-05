use std::{cell::Cell, rc::Rc};

use gpui::{App, Div, IntoElement, MouseButton, SharedString, Window, div, prelude::*, px};

use super::backend::{ApprovalResponse, BackendKind, BackendStatus};
use super::chat::AiChat;
use crate::theme::{Theme, tr};
use crate::ui::{ai_controls, dropdown};

thread_local! { static MENU_OPEN: Cell<bool> = const { Cell::new(false) }; }
thread_local! { static SCOPE_OPEN: Cell<bool> = const { Cell::new(false) }; }

pub(super) fn close_menu() -> bool {
    let backend = MENU_OPEN.with(|open| open.replace(false));
    let scope = SCOPE_OPEN.with(|open| open.replace(false));
    backend || scope
}

pub(super) fn chat_selector(chat: &AiChat, cx: &mut gpui::Context<AiChat>) -> impl IntoElement {
    let selected = chat.selected_backend();
    let kinds = [BackendKind::Builtin, BackendKind::Codex, BackendKind::Claude, BackendKind::Dsh];
    let statuses: Vec<_> = kinds.into_iter().map(|kind| (kind, chat.backend_status(kind).cloned())).collect();
    let open = MENU_OPEN.with(Cell::get);
    let toggle = cx.entity().downgrade();
    let select = toggle.clone();
    let config = toggle.clone();
    selector(selected, &statuses, open, move |_, cx| {
        let is_open = MENU_OPEN.with(|state| { let next = !state.get(); state.set(next); next });
        let _ = toggle.update(cx, |chat, cx| {
            if is_open { for kind in kinds { chat.probe_backend(kind, cx); } }
            cx.notify();
        });
    }, move |kind, window, cx| {
        close_menu();
        let _ = select.update(cx, |chat, cx| { chat.select_backend(kind, window, cx); cx.notify(); });
    }, move |window, cx| {
        close_menu();
        let _ = config.update(cx, |chat, cx| { super::dsh_ui::open(chat, window, cx); cx.notify(); });
    })
}

pub(super) fn backend_name(backend: BackendKind) -> &'static str {
    match backend {
        BackendKind::Builtin => tr("内置 AI", "Built-in AI"),
        BackendKind::Codex => "Codex",
        BackendKind::Claude => "Claude",
        BackendKind::Dsh => "dsh",
    }
}

pub(super) fn status_label(_kind: BackendKind, status: Option<&BackendStatus>) -> String {
    match status {
        None => tr("未检测", "Not checked").into(),
        Some(BackendStatus::Ready { .. }) => tr("可用", "Available").into(),
        Some(BackendStatus::Installed { .. }) => tr("已安装 · 认证未知", "Installed · authentication unknown").into(),
        Some(BackendStatus::Missing) => tr("未安装", "Not installed").into(),
        Some(BackendStatus::NotAuthenticated { .. }) => tr("未登录", "Not signed in").into(),
        Some(BackendStatus::Unsupported { .. }) => tr("不支持", "Unsupported").into(),
        Some(BackendStatus::Error { .. }) => tr("不可用", "Unavailable").into(),
    }
}

pub(super) fn selector(
    selected: BackendKind,
    statuses: &[(BackendKind, Option<BackendStatus>)],
    open: bool,
    on_toggle: impl Fn(&mut Window, &mut App) + 'static,
    on_select: impl Fn(BackendKind, &mut Window, &mut App) + 'static,
    on_config: impl Fn(&mut Window, &mut App) + 'static,
) -> Div {
    let on_select = Rc::new(on_select);
    let trigger = ai_controls::action_chip("ai-backend-trigger", None, backend_name(selected), false, open,
        move |_, window, cx| { cx.stop_propagation(); on_toggle(window, cx); }).h(px(32.)).rounded(crate::ui::radius::CONTROL).border_1().border_color(Theme::ocean().btn_border)
        .child(crate::ui::dropdown::line_icon(crate::icons::Icon::ChevronDown, 14., Theme::ocean().text_secondary));
    let menu = open.then(|| {
        let menu = statuses.iter().fold(dropdown::shell("ai-backend-menu").w(px(260.)), |menu, (kind, status)| {
            let kind = *kind;
            let on_select = on_select.clone();
            let label = format!("{} · {}", backend_name(kind), status_label(kind, status.as_ref()));
            menu.child(dropdown::row_with(SharedString::from(format!("ai-backend-{kind:?}")), dropdown::RowSpec {
                label: label.into(), checked: kind == selected, ..Default::default()
            }).on_mouse_down(MouseButton::Left, move |_, window, cx| {
                cx.stop_propagation();
                on_select(kind, window, cx);
            }))
        });
        menu.child(dropdown::separator()).child(dropdown::row("ai-dsh-config", None, tr("dsh 配置", "dsh configuration"), false)
            .on_mouse_down(MouseButton::Left, move |_, window, cx| { cx.stop_propagation(); on_config(window, cx); }))
    });
    div().relative().child(trigger).children(menu.map(|menu| gpui::deferred(div().absolute().top(px(32.)).right_0().child(menu)).with_priority(10)))
}

pub(super) fn session_status(backend: BackendKind, status: Option<&BackendStatus>) -> Div {
    let theme = Theme::ocean();
    let label = format!("{} · {}", backend_name(backend), status_label(backend, status));
    div().text_size(px(14.)).text_color(theme.text_secondary)
        .child(if crate::theme::is_en() { format!("Current conversation · {label}") } else { format!("当前会话 · {label}") })
}

pub(super) fn approval_card(
    title: String,
    detail: String,
    on_response: impl Fn(ApprovalResponse, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
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

pub(super) fn chat_model(chat: &AiChat, cx: &mut gpui::Context<AiChat>) -> Div {
    use super::chat::chat_models::ModelStatus;
    let selected = chat.selected_model();
    let backend = chat.selected_backend();
    let choices = chat.model_choices();
    let status = chat.model_status();
    let open = SCOPE_OPEN.with(Cell::get);
    let entity = cx.entity().downgrade();
    let toggle = entity.clone();
    let label = if selected.is_empty() { tr("默认模型", "Default model").to_owned() } else { chat.model_label(&selected) };
    let labels: Vec<_> = choices.iter().map(|model| (model.clone(), chat.model_label(model))).collect();
    let trigger = ai_controls::action_chip("ai-model-trigger", None, label, false, open, move |_, _, cx| {
        cx.stop_propagation();
        let opened = SCOPE_OPEN.with(|state| { let next = !state.get(); state.set(next); next });
        let _ = toggle.update(cx, |chat, cx| { if opened { chat.picker_dismissed = true; chat.refresh_models(cx); } cx.notify(); });
    }).h(px(30.)).max_w(px(180.)).rounded(crate::ui::radius::CONTROL).bg(Theme::ocean().shell)
        .child(dropdown::line_icon(crate::icons::Icon::ChevronDown, 14., Theme::ocean().text_secondary));
    let menu = open.then(|| {
        let mut options = choices;
        if matches!(backend, BackendKind::Codex | BackendKind::Claude) { options.insert(0, String::new()); }
        let mut menu = dropdown::shell("ai-model-menu").w(px(260.));
        for (i, model) in options.into_iter().enumerate() {
            let select = entity.clone();
            let text = if model.is_empty() { tr("默认模型", "Default model").to_owned() } else { labels.iter().find(|(value, _)| value == &model).map(|(_, label)| label.clone()).unwrap_or_else(|| model.clone()) };
            menu = menu.child(dropdown::row_with(SharedString::from(format!("ai-model-{i}")), dropdown::RowSpec {
                label: text.into(), checked: model == selected, ..Default::default()
            }).on_mouse_down(MouseButton::Left, move |_, _, cx| {
                cx.stop_propagation();
                SCOPE_OPEN.with(|state| state.set(false));
                let _ = select.update(cx, |chat, cx| {
                    if let Err(error) = chat.select_model(&model, cx) { chat.error = Some(error); }
                    cx.notify();
                });
            }));
        }
        let note = match status {
            ModelStatus::Loading => Some(tr("正在获取模型…", "Loading models…")),
            ModelStatus::Error(_) => Some(tr("无法获取模型", "Models unavailable")),
            ModelStatus::Ready if backend == BackendKind::Dsh => Some(tr("模型可在 dsh 配置中自定义", "Customize models in dsh settings")),
            ModelStatus::Idle | ModelStatus::Ready => None,
        };
        if let Some(note) = note { menu = menu.child(div().px(px(10.)).py(px(8.)).text_size(px(14.)).text_color(Theme::ocean().text_secondary).child(note)); }
        menu
    });
    div().relative().child(trigger).children(menu.map(|menu| gpui::deferred(div().absolute().bottom(px(34.)).left_0().child(menu)).with_priority(10)))
}
