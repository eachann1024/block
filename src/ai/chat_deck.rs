use gpui::{CursorStyle, FontWeight, MouseButton, SharedString, div, prelude::*, px};
use gpui_component::input::Input;
use super::{backend::{BackendKind, BackendStatus}, backend_ui, chat::AiChat};
use crate::{icons::{Icon, icon}, theme::tr, ui::ai_controls};

thread_local! {
    static TRIGGER_BOUNDS: std::rc::Rc<std::cell::Cell<Option<gpui::Bounds<gpui::Pixels>>>> = Default::default();
}

const THINKING_LEVELS: [(&str, &str); 3] = [("low", "Low"), ("medium", "Medium"), ("high", "High")];

pub(super) fn trigger(chat: &AiChat, cx: &mut gpui::Context<AiChat>) -> gpui_base::Button {
    let kind = chat.selected_backend();
    let model = chat.selected_model();
    let label = if model.is_empty() {
        backend_ui::backend_name(kind).to_owned()
    } else {
        chat.model_label(&model)
    };
    let thinking = backend_ui::thinking_label(chat.backend_config.thinking_level(kind));
    let open = backend_ui::DECK_OPEN.with(std::cell::Cell::get);
    ai_controls::deck_trigger_capsule(
        "ai-deck-trigger", backend_ui::backend_logo(kind), label, Some(thinking), chat.fullscreen, open,
        cx.listener(|chat, _, window, cx| {
            cx.stop_propagation();
            if !backend_ui::close_menu() { backend_ui::open_deck(chat, window, cx); }
            cx.notify();
        }),
    ).child(crate::ui::placement::track_bounds(TRIGGER_BOUNDS.with(Clone::clone), open))
}

pub(super) fn view(chat: &AiChat, window: &gpui::Window, cx: &mut gpui::Context<AiChat>) -> Option<gpui::Deferred> {
    let open = backend_ui::DECK_OPEN.with(std::cell::Cell::get);
    let (phase, pop_gen) = crate::ui::motion::popup_motion("ai-agent-deck", open)?;
    crate::ui::motion::schedule_popup_finish("ai-agent-deck", pop_gen, cx);
    let bounds = TRIGGER_BOUNDS.with(Clone::clone);
    let trigger = bounds.get()?;
    // Keep the menu on the trigger's left edge so a provider switch cannot
    // slide it when the capsule width changes.
    let spec = crate::ui::placement::PlacementSpec::above(trigger)
        .align_x(crate::ui::placement::AlignX::PinLeft);
    let max_height = crate::ui::placement::available_height_for_spec(&spec, window.viewport_size(), window.viewport_size().height);
    let wide = chat.fullscreen || chat.panel_width >= px(500.);
    let target_w = if chat.fullscreen {
        px(ai_controls::FULLSCREEN_DECK_WIDTH)
    } else if wide {
        (chat.panel_width - px(24.)).clamp(px(460.), px(640.))
    } else {
        (chat.panel_width - px(24.)).clamp(px(308.), px(420.))
    };
    let width = target_w.min((window.viewport_size().width - px(16.)).max(px(0.)));
    let theme = crate::ui::ai_theme::theme();
    let selected = chat.selected_backend();
    let selected_logo = backend_ui::backend_logo(selected);
    let header_h = if wide { 44. } else { 38. };
    let header_px = if wide { 14. } else { 10. };

    let mut rail = div().w(px(if wide { 54. } else { 42. })).flex_none().py(px(if wide { 10. } else { 8. })).px(px(if wide { 6. } else { 4. }))
        .border_r_1().border_color(theme.hairline).bg(theme.shell).flex().flex_col().items_center().gap(px(if wide { 8. } else { 6. }));
    for kind in chat.visible_backends() {
        let ready = if kind == BackendKind::Builtin {
            backend_ui::builtin_ready()
        } else {
            matches!(chat.backend_status(kind), Some(BackendStatus::Ready { .. } | BackendStatus::Installed { .. }))
        };
        rail = rail.child(ai_controls::backend_rail_item(
            format!("ai-backend-{kind:?}"), backend_ui::backend_name(kind), backend_ui::backend_logo(kind),
            kind == selected, ready, wide,
            cx.listener(move |chat, _, window, cx| {
                cx.stop_propagation();
                let changed = chat.selected_backend() != kind;
                chat.select_backend(kind, window, cx);
                if changed { chat.refresh_models_for(kind, cx); }
                cx.notify();
            }),
        ));
    }

    let search_bar = div().h(px(header_h)).px(px(header_px)).flex_none().border_b_1().border_color(theme.hairline)
        .flex().items_center().gap(px(8.))
        .child(icon(Icon::FindSearch, px(if wide { 15. } else { 14. }), theme.mono_icon_colors(theme.text_secondary)))
        .when_some(backend_ui::search_input(), |bar, input| {
            bar.child(div().flex_1().min_w_0().child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(30.))))
        });

    let query = backend_ui::search_query(cx);
    let selected_model = chat.selected_model();
    let all_models = chat.model_choices();
    let filtered: Vec<(usize, String, String)> = all_models.into_iter().enumerate().filter_map(|(idx, model)| {
        let label = chat.model_label(&model);
        let hit = query.is_empty() || model.to_lowercase().contains(&query) || label.to_lowercase().contains(&query);
        hit.then_some((idx, model, label))
    }).collect();

    let list_h = if chat.fullscreen { 276. } else if wide { 220. } else { 172. };
    let mut list = div().id("ai-deck-models").h(px(list_h)).min_h(px(list_h)).max_h(px(list_h)).p(px(if wide { 8. } else { 6. })).flex_none().overflow_y_scroll().track_scroll(&chat.deck_scroll).flex().flex_col().gap(px(if wide { 3. } else { 2. }));
    if filtered.is_empty() {
        let status = if !query.is_empty() {
            tr("无匹配模型", "No matching models")
        } else {
            match chat.model_status() {
                super::chat::chat_models::ModelStatus::Loading => tr("正在获取模型…", "Loading models…"),
                super::chat::chat_models::ModelStatus::Error(_) => tr("无法获取模型", "Models unavailable"),
                _ => tr("暂无可选模型", "No models available"),
            }
        };
        list = list.child(div().h(px(36.)).px(px(12.)).flex().items_center().text_color(theme.text_secondary).child(status));
    }
    let prefix = if cfg!(target_os = "macos") { "⌘" } else { "Ctrl+" };
    for (index, model, label) in filtered {
        let active = model == selected_model;
        let shortcut = (index < 9).then(|| SharedString::from(format!("{prefix}{}", index + 1)));
        list = list.child(ai_controls::model_row(
            SharedString::from(format!("ai-model-{index}")), selected_logo, label, shortcut, active, wide,
            cx.listener(move |chat, _, _, cx| {
                cx.stop_propagation();
                match chat.select_model(&model, cx) { Ok(()) => { backend_ui::close_menu(); }, Err(error) => chat.error = Some(error) }
                cx.notify();
            }),
        ));
    }

    let current_thinking = chat.backend_config.thinking_level(selected);
    let model_col = div().flex_1().min_w_0().flex().flex_col().child(search_bar).child(list)
        .when(!wide, |col| {
            let mut pills = div().flex_1().flex().items_center().gap(px(4.));
            for (level, label) in THINKING_LEVELS {
                pills = pills.child(ai_controls::thinking_level_item(
                    format!("ai-thinking-{level}"), label, current_thinking == level, true,
                    cx.listener(move |chat, _, _, cx| {
                        cx.stop_propagation();
                        if let Err(err) = chat.backend_config.set_thinking_level(selected, level) { chat.error = Some(err); }
                        cx.notify();
                    }),
                ));
            }
            col.child(div().px(px(10.)).py(px(6.)).border_t_1().border_color(theme.hairline).flex().items_center().justify_between().gap(px(8.))
                .child(div().flex_none().text_size(px(12.5)).text_color(theme.text_secondary).child("Thinking Level"))
                .child(pills))
        });

    let upper = div().flex_1().min_w_0().flex()
        .child(model_col)
        .when(wide, |row| {
            let mut levels = div().p(px(8.)).flex().flex_col().gap(px(4.));
            for (level, label) in THINKING_LEVELS {
                levels = levels.child(ai_controls::thinking_level_item(
                    format!("ai-thinking-{level}"), label, current_thinking == level, false,
                    cx.listener(move |chat, _, _, cx| {
                        cx.stop_propagation();
                        if let Err(err) = chat.backend_config.set_thinking_level(selected, level) { chat.error = Some(err); }
                        cx.notify();
                    }),
                ));
            }
            row.child(div().w(px(if chat.fullscreen { 216. } else { 176. })).flex_none().border_l_1().border_color(theme.hairline).flex().flex_col()
                .child(div().h(px(header_h)).px(px(header_px)).flex_none().border_b_1().border_color(theme.hairline).flex().items_center()
                    .child(div().text_size(px(13.5)).font_weight(FontWeight(500.)).text_color(theme.text_secondary).child("Thinking Level")))
                .child(levels))
        });

    let full_access = selected == BackendKind::Builtin || chat.backend_config.full_permissions(selected);
    let perm_label = if full_access { tr("Full access", "Full access") } else { tr("标准权限", "Standard access") };
    let footer = div().h(px(if wide { 40. } else { 36. })).px(px(if wide { 12. } else { 10. })).flex_none().border_t_1().border_color(theme.hairline)
        .flex().items_center().justify_between().gap(px(8.))
        .child(ai_controls::action_chip("ai-deck-settings", Some(Icon::Sliders), tr("AI 设置", "AI settings"), false, false, cx.listener(|chat, _, window, cx| {
            cx.stop_propagation(); backend_ui::close_menu(); chat.input.update(cx, |i, cx| i.focus(window, cx)); window.dispatch_action(Box::new(super::OpenAiSettings), cx);
        })))
        .child(div().id("ai-deck-permissions").relative().children(crate::ui::smoke_overlay::probe("ai-deck-permissions"))
            .h(px(28.)).px(px(8.)).rounded(px(6.)).flex().items_center().gap(px(6.)).cursor(CursorStyle::PointingHand)
            .hover(move |s| s.bg(theme.hover))
            .child(div().size(px(6.)).rounded_full().bg(if full_access { theme.accent } else { theme.text_secondary }))
            .child(div().text_size(px(13.5)).text_color(if full_access { theme.text } else { theme.text_secondary }).child(perm_label))
            .child(crate::dropdown::line_icon(Icon::ChevronUp, 11., theme.text_secondary))
            .on_mouse_down(MouseButton::Left, cx.listener(move |chat, _, window, cx| {
                cx.stop_propagation();
                if selected == BackendKind::Builtin {
                    backend_ui::close_menu(); chat.input.update(cx, |i, cx| i.focus(window, cx)); window.dispatch_action(Box::new(super::OpenAiSettings), cx);
                } else {
                    let next = !chat.backend_config.full_permissions(selected);
                    if let Err(err) = chat.backend_config.set_full_permissions(selected, next) { chat.error = Some(err); }
                    cx.notify();
                }
            })));

    let deck = ai_controls::panel_shell("ai-agent-deck").w(width).min_w(width).max_w(width).max_h(max_height)
        .child(div().flex().w_full().min_w_0().child(rail).child(div().flex_1().min_w_0().flex().flex_col().child(upper).child(footer)));
    let shell = div().child(deck);
    let animated = match phase {
        crate::ui::motion::PopupMotion::Enter => crate::ui::motion::popup_enter(shell, ("ai-deck-pop", pop_gen)).into_any_element(),
        crate::ui::motion::PopupMotion::Exit => crate::ui::motion::popup_exit(shell, ("ai-deck-pop", pop_gen)).into_any_element(),
    };
    Some(gpui::deferred(crate::ui::placement::smart_anchored_with(move |_, _| {
        crate::ui::placement::PlacementSpec::above(bounds.get().unwrap_or(trigger))
            .align_x(crate::ui::placement::AlignX::PinLeft)
    }).child(animated)).with_priority(20))
}
