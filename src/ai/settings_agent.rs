use gpui::{Context, FontWeight, MouseButton, ParentElement, SharedString, Styled, Window, div, prelude::*, px};
use gpui_component::input::Input;

use super::{AiSettings, TestNote};
use super::super::{backend::{self, BackendConfig, BackendKind}, backend_ui, chat};
use crate::{icons::Icon, theme::{Theme, tr}, ui::settings_controls::{action_button, card_title, mono_font, agent_card, setting_row, settings_card, static_badge, status_action_button, tag_chip, toggle_pill}};

impl AiSettings {
    pub(in crate::editor::ai) fn select_focused_backend(&mut self, kind: BackendKind, window: &mut Window, cx: &mut Context<Self>) {
        self.focused_backend = kind;
        self.agent_fetching = false;
        let config = BackendConfig::load();
        let model = config.selected_model(kind);
        self.agent_models = if model.is_empty() { Vec::new() } else { vec![model] };
        self.syncing_launch_args = true;
        self.launch_args_input.update(cx, |input, cx| input.set_value(config.launch_args(kind), window, cx));
        self.syncing_launch_args = false;
        if kind != BackendKind::Builtin && config.is_enabled(kind) { self.refresh_agent_models(cx); }
        cx.notify();
    }

    pub(super) fn refresh_agent_models(&mut self, cx: &mut Context<Self>) {
        if self.agent_fetching { return; }
        let kind = self.focused_backend;
        self.agent_fetching = true;
        let task = cx.background_spawn(async move { backend::list_models(kind) });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                if this.focused_backend != kind { return; }
                this.agent_fetching = false;
                match result {
                    Ok(models) => {
                        this.agent_models = models.into_iter().map(|model| model.value).filter(|value| !value.is_empty()).collect();
                        let selected = BackendConfig::load().selected_model(kind);
                        if !selected.is_empty() && !this.agent_models.contains(&selected) { this.agent_models.insert(0, selected); }
                    }
                    Err(error) => this.test_note = Some(TestNote::Err(error.to_string())),
                }
                cx.notify();
            });
        }).detach();
        cx.notify();
    }

    pub(in crate::editor::ai) fn agent_overview(&self, cx: &mut Context<Self>) -> gpui::Div {
        let config = BackendConfig::load();
        let recognized = chat::recognized_backends(cx);

        let toggle = toggle_pill("ai-enabled-switch", Icon::Sparkles, self.enabled,
            if self.enabled { tr("已开启", "Enabled") } else { tr("已关闭", "Disabled") }, self.toggle_anim,
            cx.listener(|this, active, _, cx| { if this.toggle_anim.set_hovered(*active) { cx.notify(); } }),
            cx.listener(|this, _, _, cx| { this.enabled = !this.enabled; this.toggle_anim.record_toggle(); this.save(cx); }));
        let mut cards = div().flex().flex_wrap().gap(px(12.));
        for kind in recognized.iter().copied() {
            let active = self.pending_backends.iter().find(|(item, _)| *item == kind).map_or(config.is_enabled(kind), |(_, enabled)| *enabled);
            let card_id: gpui::ElementId = SharedString::from(format!("ai-backend-card-{kind:?}")).into();
            let toggle_id: gpui::ElementId = SharedString::from(format!("ai-backend-toggle-{kind:?}")).into();
            let logo = match kind {
                BackendKind::Builtin => None,
                BackendKind::Codex => Some("agents/codex.svg"),
                BackendKind::Claude => Some("agents/claude.svg"),
                BackendKind::Dsh => Some("agents/dsh.svg"),
                BackendKind::Pi => Some("agents/pi.svg"),
            };
            cards = cards.child(agent_card(card_id, toggle_id, backend_ui::backend_name(kind), logo,
                self.focused_backend == kind, active,
                cx.listener(move |this, _, window, cx| {
                    if this.focused_backend != kind { this.select_focused_backend(kind, window, cx); }
                }),
                cx.listener(move |this, _, window, cx| {
                    cx.stop_propagation();
                    if this.focused_backend != kind { this.select_focused_backend(kind, window, cx); }
                    this.toggle_backend(kind, cx);
                })));

        }
        settings_card(16.).child(div().flex().items_center().justify_between().child(card_title(tr("AI 与本机 Agent", "AI & Local Agents"))).child(toggle))
            .when(self.enabled, |d| d.child(cards).child(action_button("ai-save-backends", tr("保存", "Save"), true, !self.pending_backends.is_empty()).on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.save_backends(cx)))))
    }

    pub(in crate::editor::ai) fn agent_locked(&self, cx: &mut Context<Self>) -> gpui::Stateful<gpui::Div> {
        let kind = self.focused_backend;
        settings_card(8.).id("ai-focused-locked").relative().children(crate::ui::smoke_overlay::probe_for(&"ai-focused-locked".into())).flex_row().items_center().justify_between()
            .child(format!("{} {}", backend_ui::backend_name(kind), tr("当前未启用，勾选卡片右上角后可修改以下配置", "is disabled. Check the top-right box to edit settings below")))
            .child(action_button("ai-enable-focused", tr("选中", "Select"), false, true)
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| this.toggle_backend(kind, cx))))
    }

    pub(in crate::editor::ai) fn agent_config(&self, cx: &mut Context<Self>) -> Vec<gpui::Div> {
        let theme = Theme::ocean();
        let kind = self.focused_backend;
        let config = BackendConfig::load();
        let enabled = config.is_enabled(kind);
        let path = backend::cli_env::resolve(kind.key()).map(|path| path.display().to_string()).unwrap_or_default();
        let title = |suffix| div().text_size(px(15.)).font_weight(FontWeight(560.)).text_color(theme.text)
            .child(format!("{} {}", backend_ui::backend_name(kind), suffix));
        let launch = settings_card(16.)
            .child(div().flex().items_center().justify_between().gap(px(8.)).child(title(tr("启动配置", "Launch Config"))).child(static_badge(path, true)))
            .child(setting_row(tr("启动参数", "Launch Args"), div().id("ai-agent-launch-args").relative().children(crate::ui::smoke_overlay::probe_for(&"ai-agent-launch-args".into())).flex_1().min_w_0().h(px(34.))
                .rounded(crate::ui::radius::CONTROL).border_1().border_color(theme.btn_border).bg(theme.shell).font(mono_font())
                .child(Input::new(&self.launch_args_input).disabled(!enabled).appearance(false).bordered(false).focus_bordered(false).h(px(32.)))));
        let mut models = div().id("ai-agent-model-list").flex().flex_wrap().gap(px(8.));
        for model in &self.agent_models {
            let value = model.clone();
            models = models.child(tag_chip(SharedString::from(format!("ai-agent-model-{model}")), model.clone(), *model == config.selected_model(kind), true)
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                    if !BackendConfig::load().is_enabled(kind) { return; }
                    if let Err(error) = BackendConfig::load().set_model(kind, &value) { this.test_note = Some(TestNote::Err(error)); }
                    chat::sync_settings(cx); cx.notify();
                })));
        }
        let model_card = settings_card(16.)
            .child(div().flex().items_center().justify_between().child(title(tr("默认模型", "Default Model")))
                .child(status_action_button("ai-agent-fetch-models", Some(Icon::Refresh),
                    if self.agent_fetching { tr("获取中…", "Fetching…") } else { tr("重新获取模型", "Refresh Models") }, false, enabled && !self.agent_fetching)
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { if BackendConfig::load().is_enabled(this.focused_backend) { this.refresh_agent_models(cx); } }))))
            .child(models);
        vec![launch, model_card]
    }
}
