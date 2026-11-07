use gpui::{Context, FontWeight, MouseButton, ParentElement, SharedString, Styled, Window, div, prelude::*, px};

use super::{AiSettings, TestNote};
use super::super::{backend::{BackendConfig, BackendKind}, backend_ui, chat};
use crate::{icons::Icon, theme::{Theme, tr}, ui::settings_controls::{action_button, agent_card, card_title, checkbox_switch, setting_row, settings_card, toggle_pill}};

impl AiSettings {
    pub(in crate::editor::ai) fn select_focused_backend(&mut self, kind: BackendKind, _window: &mut Window, cx: &mut Context<Self>) {
        self.focused_backend = kind;
        self.test_note = None;
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
        let full_permissions = config.full_permissions(kind);
        let title = div().text_size(px(15.)).font_weight(FontWeight(560.)).text_color(theme.text)
            .child(format!("{} {}", backend_ui::backend_name(kind), tr("权限设置", "Permissions")));
        let toggle = checkbox_switch("ai-agent-full-permissions", full_permissions, enabled)
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                let mut config = BackendConfig::load();
                if !config.is_enabled(kind) { return; }
                let next = !config.full_permissions(kind);
                if let Err(error) = config.set_full_permissions(kind, next) {
                    this.test_note = Some(TestNote::Err(error));
                }
                chat::sync_settings(cx);
                cx.notify();
            }));
        let card = settings_card(16.)
            .child(title)
            .child(setting_row(tr("最大权限", "Full Permissions"), toggle));
        vec![card]
    }
}
