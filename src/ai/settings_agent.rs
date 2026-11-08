use gpui::{App, Context, FontWeight, MouseButton, ParentElement, SharedString, Styled, Window, div, prelude::*, px};

use super::{AiSettings, TestNote};
use super::super::{backend::{self, BackendConfig, BackendKind, BackendStatus}, backend_ui, chat, presets};
use crate::{
    icons::{Icon, icon},
    theme::{Theme, is_dark, tr},
    ui::settings_controls::{
        action_button, agent_card, brand_glyph, card_bg, card_title, mono_font,
        pill_switch, segmented, setting_row, static_badge, toggle_pill,
    },
};

fn backend_version_tag(kind: BackendKind, settings: &AiSettings, cx: &App) -> String {
    if kind == BackendKind::Builtin {
        return presets::find(&settings.provider).map_or_else(|| "DeepSeek".into(), |p| p.label().into());
    }
    match chat::backend_status(kind, cx) {
        Some(BackendStatus::Ready { version } | BackendStatus::Installed { version }) => {
            let token = version.split_whitespace().find(|part| part.chars().any(|c| c.is_ascii_digit())).unwrap_or(&version);
            if token.starts_with('v') { token.to_owned() } else { format!("v{token}") }
        }
        Some(BackendStatus::NotAuthenticated { .. }) => tr("未登录", "Unauthenticated").into(),
        Some(BackendStatus::Unsupported { .. } | BackendStatus::Error { .. }) => tr("异常", "Error").into(),
        _ => String::new(),
    }
}

impl AiSettings {
    pub(in crate::editor::ai) fn select_focused_backend(&mut self, kind: BackendKind, _window: &mut Window, cx: &mut Context<Self>) {
        self.focused_backend = kind;
        self.test_note = None;
        cx.notify();
    }

    pub(in crate::editor::ai) fn promote_backend(&mut self, kind: BackendKind, window: &mut Window, cx: &mut Context<Self>) {
        let recognized = chat::recognized_backends(cx);
        let mut config = BackendConfig::load();
        if !config.is_enabled(kind) {
            if let Err(error) = config.set_enabled(kind, true, &recognized) {
                self.test_note = Some(TestNote::Err(error));
                cx.notify();
                return;
            }
        }
        if let Err(error) = config.promote_priority(kind, &recognized) {
            self.test_note = Some(TestNote::Err(error));
            cx.notify();
            return;
        }
        self.focused_backend = kind;
        chat::sync_settings(cx);
        let _ = window;
        cx.notify();
    }

    pub(in crate::editor::ai) fn agent_overview(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let config = BackendConfig::load();
        let recognized = chat::recognized_backends(cx);

        let toggle = toggle_pill("ai-enabled-switch", Icon::Sparkles, self.enabled,
            if self.enabled { tr("已开启", "Enabled") } else { tr("已关闭", "Disabled") }, self.toggle_anim,
            cx.listener(|this, active, _, cx| { if this.toggle_anim.set_hovered(*active) { cx.notify(); } }),
            cx.listener(|this, _, _, cx| { this.enabled = !this.enabled; this.toggle_anim.record_toggle(); this.save(cx); }));

        let header = div().w_full().flex().items_center().justify_between()
            .child(card_title(tr("AI 与本机 Agent", "AI & Local Agents")))
            .child(toggle);

        if !self.enabled {
            return div().w_full().min_w_0().bg(card_bg()).border_1().border_color(theme.hairline)
                .rounded(crate::ui::radius::SURFACE).py(px(18.)).px(px(20.)).child(header);
        }

        let mut rail = div().w(px(260.)).flex_none().bg(theme.sidebar).border_r_1().border_color(theme.hairline)
            .flex().flex_col();
        let total = recognized.len();
        for (idx, kind) in recognized.iter().copied().enumerate() {
            let active = config.is_enabled(kind);
            let card_id: gpui::ElementId = SharedString::from(format!("ai-backend-card-{kind:?}")).into();
            let toggle_id: gpui::ElementId = SharedString::from(format!("ai-backend-toggle-{kind:?}")).into();
            let subtitle = backend_version_tag(kind, self, cx);
            let logo = backend_ui::backend_logo(kind);
            rail = rail.child(agent_card(
                card_id, toggle_id, backend_ui::backend_name(kind), subtitle, logo,
                self.focused_backend == kind, active, idx + 1 == total,
                cx.listener(move |this, _, window, cx| {
                    if this.focused_backend != kind { this.select_focused_backend(kind, window, cx); }
                }),
                cx.listener(move |this, _, window, cx| {
                    cx.stop_propagation();
                    if this.focused_backend != kind { this.select_focused_backend(kind, window, cx); }
                    this.toggle_backend(kind, cx);
                }),
            ));
        }

        let detail = if self.focused_backend == BackendKind::Builtin {
            self.connection_detail(cx)
        } else {
            self.agent_detail(cx)
        };

        let split_card = div().w_full().min_w_0().overflow_hidden().bg(card_bg())
            .border_1().border_color(theme.hairline).rounded(crate::ui::radius::SURFACE)
            .flex().items_stretch()
            .child(rail)
            .child(div().flex_1().min_w_0().py(px(16.)).px(px(20.)).flex().flex_col().gap(px(14.)).child(detail));

        let mut hidden_priority_probes = div().size(px(0.)).overflow_hidden();
        for kind in recognized.iter().copied() {
            let pill_id: gpui::ElementId = SharedString::from(format!("ai-priority-pill-{kind:?}")).into();
            hidden_priority_probes = hidden_priority_probes.child(
                div().id(pill_id.clone()).relative().children(crate::ui::smoke_overlay::probe_for(&pill_id))
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| this.promote_backend(kind, window, cx))),
            );
        }

        div().w_full().min_w_0().flex().flex_col().gap(px(12.))
            .child(header)
            .child(split_card)
            .child(hidden_priority_probes)
    }

    pub(in crate::editor::ai) fn inline_disabled_banner(&self, kind: BackendKind, cx: &mut Context<Self>) -> gpui::Stateful<gpui::Div> {
        let theme = Theme::ocean();
        let dark = is_dark();
        let bg = if dark { gpui::rgb(0x1f2b40) } else { gpui::rgb(0xeef4ff) };
        let border = if dark { gpui::rgb(0x31456a) } else { gpui::rgb(0xc9d8f3) };
        let fg: gpui::Hsla = if dark { gpui::rgb(0x9dbcf5).into() } else { gpui::rgb(0x345498).into() };
        div().id("ai-focused-locked").relative().children(crate::ui::smoke_overlay::probe_for(&"ai-focused-locked".into()))
            .w_full().px(px(12.)).py(px(9.)).rounded(crate::ui::radius::CONTROL)
            .bg(bg).border_1().border_color(border)
            .flex().items_center().justify_between().gap(px(10.))
            .child(div().flex().items_center().gap(px(8.)).min_w_0().flex_1()
                .child(icon(Icon::HelpCircle, px(15.), theme.mono_icon_colors(fg)))
                .child(div().text_size(px(14.)).line_height(px(20.)).text_color(fg)
                    .child(tr("当前未在对话栏启用，开启后可在模型切换器中选择", "Disabled in chat. Enable to select in the model switcher"))))
            .child(action_button("ai-enable-focused", tr("立即启用", "Enable"), true, true)
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| this.toggle_backend(kind, cx))))
    }

    fn agent_detail(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let kind = self.focused_backend;
        let config = BackendConfig::load();
        let enabled = config.is_enabled(kind);
        let full_permissions = config.full_permissions(kind);
        let thinking = config.thinking_level(kind);
        let version = backend_version_tag(kind, self, cx);
        let bin_path = backend::cli_env::resolve(kind.key())
            .map(|p| p.display().to_string())
            .unwrap_or_else(|| kind.key().to_owned());

        let head = div().w_full().pb(px(12.)).border_b_1().border_color(theme.hairline)
            .flex().items_center().justify_between().gap(px(10.))
            .child(div().flex().items_center().gap(px(8.))
                .child(brand_glyph(backend_ui::backend_logo(kind), 18., theme.text))
                .child(div().text_size(px(15.)).line_height(px(22.)).font_weight(FontWeight(560.)).text_color(theme.text)
                    .child(backend_ui::backend_name(kind))))
            .when(!version.is_empty(), |d| d.child(div().font(mono_font()).text_size(px(13.)).text_color(theme.text_secondary).child(version)));

        let thinking_ctrl = segmented(
            "ai-agent-thinking",
            [
                ("Low", thinking == "low", "low"),
                ("Medium", thinking == "medium", "medium"),
                ("High", thinking == "high", "high"),
            ],
            move |level, cx| {
                Box::new(cx.listener(move |this: &mut Self, _, _, cx| {
                    let mut cfg = BackendConfig::load();
                    if let Err(err) = cfg.set_thinking_level(kind, level) {
                        this.test_note = Some(TestNote::Err(err));
                    }
                    chat::sync_settings(cx);
                    cx.notify();
                }))
            },
        )(cx);

        let perm_toggle = pill_switch("ai-agent-full-permissions", full_permissions, true)
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                let mut cfg = BackendConfig::load();
                let next = !cfg.full_permissions(kind);
                if let Err(error) = cfg.set_full_permissions(kind, next) {
                    this.test_note = Some(TestNote::Err(error));
                }
                chat::sync_settings(cx);
                cx.notify();
            }));

        div().w_full().min_w_0().flex().flex_col().gap(px(12.))
            .child(head)
            .children((!enabled).then(|| self.inline_disabled_banner(kind, cx)))
            .child(div().w_full().pb(px(10.)).border_b_1().border_color(theme.hairline)
                .child(setting_row(tr("可执行文件路径", "Binary Path"), static_badge(bin_path, true))))
            .child(div().w_full().pb(px(10.)).border_b_1().border_color(theme.hairline)
                .child(setting_row(tr("思考强度", "Thinking Level"), thinking_ctrl)))
            .child(setting_row(tr("最大权限", "Full Permissions"), perm_toggle))
    }
}

