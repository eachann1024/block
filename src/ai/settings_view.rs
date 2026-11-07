//! Rendering for 设置 › AI 助手: provider cards and connection fields.
use gpui::{
    Context, CursorStyle, Entity, FontWeight, IntoElement, MouseButton, ParentElement, Render,
    SharedString, Styled, Window, div, prelude::*, px,
};
use gpui_component::input::{Input, InputState};

use super::presets::PRESETS;
use super::settings::{AiSettings, TestNote};
use crate::theme::{Theme, is_dark, is_en, tr};
use crate::ui::settings_controls::{
    action_button, card_bg, card_title, selected_bg, setting_row, settings_card,
    status_action_button,
};

fn input_box(input: &Entity<InputState>) -> impl IntoElement {
    let disabled = !super::backend::BackendConfig::load().is_enabled(super::backend::BackendKind::Builtin);
    div().flex_1().min_w_0().child(Input::new(input).disabled(disabled).appearance(false).bordered(false).focus_bordered(false).h(px(28.)))
}

fn field_shell() -> gpui::Div {
    let theme = Theme::ocean();
    div().h(px(34.)).px(px(10.)).flex().items_center().rounded(crate::ui::radius::CONTROL).border_1().border_color(theme.btn_border)
        .bg(theme.shell).text_size(px(14.))
}

fn field_flex(input: &Entity<InputState>) -> gpui::Div {
    field_shell().flex_1().min_w_0().child(input_box(input))
}

impl AiSettings {
    fn provider_card(&self, ix: usize, cx: &mut Context<Self>) -> gpui::Stateful<gpui::Div> {
        let theme = Theme::ocean();
        let p = &PRESETS[ix];
        let id = p.id;
        let active = self.provider == id;
        div().id(("ai-pcard", ix)).flex_1().min_w_0().py(px(10.)).px(px(12.)).rounded(crate::ui::radius::CONTROL)
            .flex().items_center().gap(px(8.)).cursor(CursorStyle::PointingHand)
            .when(active, |d| d.bg(selected_bg()).border_1().border_color(theme.accent))
            .when(!active, |d| d.bg(theme.shell).border_1().border_color(theme.btn_border).hover(move |s| s.bg(theme.hover)))
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| this.select_provider(id, w, cx)))
            .child(div().size(px(28.)).rounded(crate::ui::radius::CONTROL).bg(card_bg()).border_1().border_color(theme.hairline)
                .flex().items_center().justify_center().flex_none()
                .child(crate::icons::brand_icon(p.icon(), px(16.), gpui::rgb(p.brand_rgb).into())))
            .child(div().flex_1().min_w_0().text_size(px(14.)).line_height(px(20.)).font_weight(FontWeight(500.)).text_color(theme.text)
                .overflow_hidden().whitespace_nowrap().text_ellipsis().child(p.label()))
    }

    fn connection_card(&self, cx: &mut Context<Self>) -> gpui::Div {
        let test_ok = matches!(self.test_note, Some(TestNote::Ok));
        let test_label = if self.testing { tr("测试中…", "Testing…") }
            else if test_ok { tr("连接正常", "Connected") } else { tr("连接测试", "Test Connection") };
        let test_icon = if self.testing {
            crate::icons::Icon::Spinner
        } else if test_ok {
            crate::icons::Icon::Check
        } else {
            crate::icons::Icon::Sparkles
        };
        let error = match &self.test_note {
            Some(TestNote::Err(e)) => Some(e.clone()),
            _ => None,
        };
        let key_toggle = action_button("ai-toggle-key", if self.show_key { tr("隐藏", "Hide") } else { tr("显示", "Show") }, false, true)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                this.show_key = !this.show_key;
                let masked = !this.show_key;
                this.api_key.update(cx, |i, cx| i.set_masked(masked, window, cx));
                cx.notify();
            }));
        let dark = is_dark();
        settings_card(16.)
            .child(div().flex().items_center().justify_between()
                .child(card_title(tr("服务商与连接", "Provider & Connection")))
                .child(status_action_button("ai-test", Some(test_icon), test_label, test_ok, !self.testing)
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.test(cx)))))
            .child(div().flex().gap(px(10.)).children((0..PRESETS.len()).map(|ix| self.provider_card(ix, cx))))
            .child(setting_row(tr("接口地址", "Base URL"), div().flex_1().min_w_0().max_w(px(380.)).child(field_flex(&self.base_url))))
            .child(setting_row(tr("API 密钥", "API Key"), div().flex_1().min_w_0().max_w(px(380.)).flex().items_center().gap(px(8.))
                .child(field_flex(&self.api_key)).child(key_toggle)))
            .children(error.map(|e| {
                div().px(px(12.)).py(px(8.)).rounded(crate::ui::radius::CONTROL).border_1()
                    .bg(if dark { gpui::rgb(0x3a1d1d) } else { gpui::rgb(0xfef3f2) })
                    .border_color(if dark { gpui::rgb(0x6e2c2c) } else { gpui::rgb(0xfecdca) })
                    .text_size(px(14.))
                    .text_color(if dark { gpui::rgb(0xf97066) } else { gpui::rgb(0xb42318) })
                    .child(SharedString::from(e))
            }))
    }
}

impl Render for AiSettings {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if self.last_en != is_en() { self.last_en = is_en(); self.sync_placeholders(window, cx); }
        let focused_enabled = super::backend::BackendConfig::load().is_enabled(self.focused_backend);
        div().id("ai-settings-section").flex().flex_col().gap(px(24.))
            .child(self.agent_overview(cx))
            .when(self.enabled, |d| d.children((!focused_enabled).then(|| self.agent_locked(cx)))
                .child(div().relative().flex().flex_col().gap(px(24.)).when(!focused_enabled, |d| d.opacity(0.45))
                    .when(self.focused_backend == super::backend::BackendKind::Builtin,
                        |d| d.child(self.connection_card(cx)))
                    .when(self.focused_backend != super::backend::BackendKind::Builtin, |d| d.children(self.agent_config(cx)))
                    .when(!focused_enabled, |d| d.child(div().absolute().inset_0().on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())))))
    }
}
