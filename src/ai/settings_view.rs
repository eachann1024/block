//! Rendering for 设置 › AI 助手: provider cards, connection fields and model picker.
use gpui::{
    Context, CursorStyle, Entity, FontWeight, IntoElement, MouseButton, ParentElement, Render,
    SharedString, Styled, Window, div, prelude::*, px,
};
use gpui_component::input::{Input, InputState};

use super::presets::PRESETS;
use super::settings::{AiSettings, TestNote};
use crate::theme::{Theme, is_en, tr};
use crate::ui::settings_controls::{
    action_button, card_bg, card_title, mono_font, setting_row, settings_card, toggle_pill,
};

fn input_box(input: &Entity<InputState>) -> impl IntoElement {
    div().flex_1().min_w_0().child(Input::new(input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)))
}

fn field_shell() -> gpui::Div {
    let theme = Theme::ocean();
    div().h(px(34.)).px(px(10.)).flex().items_center().rounded(px(8.)).border_1().border_color(theme.hairline)
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
        div().id(("ai-pcard", ix)).flex_1().min_w_0().py(px(10.)).px(px(12.)).rounded(px(8.))
            .flex().items_center().gap(px(8.)).cursor(CursorStyle::PointingHand)
            .when(active, |d| d.bg(theme.selected).border_1().border_color(theme.accent))
            .when(!active, |d| d.bg(theme.shell).border_1().border_color(theme.hairline).hover(move |s| s.bg(theme.hover)))
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| this.select_provider(id, w, cx)))
            .child(div().size(px(28.)).rounded(px(6.)).bg(card_bg()).border_1().border_color(theme.hairline)
                .flex().items_center().justify_center().flex_none()
                .child(crate::icons::brand_icon(p.icon(), px(16.), gpui::rgb(p.brand_rgb).into())))
            .child(div().flex_1().min_w_0().text_size(px(14.)).line_height(px(20.)).font_weight(FontWeight(500.)).text_color(theme.text)
                .overflow_hidden().whitespace_nowrap().text_ellipsis().child(p.label()))
    }

    fn connection_card(&self, cx: &mut Context<Self>) -> gpui::Div {
        let on = self.enabled;
        let toggle = toggle_pill("ai-enabled-switch", on, if on { tr("已开启", "Enabled") } else { tr("已关闭", "Disabled") })
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.enabled = !this.enabled; this.save(cx); }));
        let key_toggle = action_button("ai-toggle-key", if self.show_key { tr("隐藏", "Hide") } else { tr("显示", "Show") }, false, true)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                this.show_key = !this.show_key;
                let masked = !this.show_key;
                this.api_key.update(cx, |i, cx| i.set_masked(masked, window, cx));
                cx.notify();
            }));
        settings_card(16.)
            .child(div().flex().items_center().justify_between().gap(px(16.))
                .child(card_title(tr("服务商与连接", "Provider & Connection"))).child(toggle))
            .child(div().flex().gap(px(10.)).children((0..PRESETS.len()).map(|ix| self.provider_card(ix, cx))))
            .child(setting_row(tr("接口地址", "Base URL"), div().flex_1().min_w_0().max_w(px(380.)).child(field_flex(&self.base_url))))
            .child(setting_row(tr("API 密钥", "API Key"), div().flex_1().min_w_0().max_w(px(380.)).flex().items_center().gap(px(8.))
                .child(field_flex(&self.api_key)).child(key_toggle)))
    }

    fn model_card(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let current_model = self.model.read(cx).value().to_string();
        let test_ok = matches!(self.test_note, Some(TestNote::Ok));
        let fetch_label = if self.fetching { tr("获取中…", "Fetching…") }
            else if self.models.is_empty() { tr("获取模型", "Fetch Models") } else { tr("重新获取模型", "Refresh Models") };
        let test_label = if self.testing { tr("测试中…", "Testing…") }
            else if test_ok { tr("连接正常", "Connected") } else { tr("连接测试", "Test Connection") };
        let error = self.models_err.clone().or_else(|| match &self.test_note {
            Some(TestNote::Err(e)) => Some(e.clone()),
            _ => None,
        });
        let chips = self.models.iter().take(40).cloned().fold(div().flex().flex_wrap().gap(px(6.)), |d, id| {
            let active = id == current_model;
            let pick = id.clone();
            d.child(div().id(SharedString::from(format!("ai-model-{id}"))).h(px(28.)).px(px(10.)).flex().items_center()
                .rounded(px(6.)).text_size(px(13.)).font(mono_font()).border_1()
                .border_color(if active { theme.accent } else { theme.hairline })
                .text_color(if active { theme.accent } else { theme.text_secondary })
                .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                    this.model.update(cx, |i, cx| i.set_value(pick.clone(), window, cx));
                    this.save(cx);
                }))
                .child(id))
        });
        settings_card(16.)
            .child(card_title(tr("模型与连接", "Model & Connection")))
            .child(setting_row(tr("默认模型", "Default Model"), div().flex_1().min_w_0().max_w(px(380.)).flex().items_center().gap(px(8.))
                .child(field_flex(&self.model))
                .child(action_button("ai-fetch-models", fetch_label, false, !self.fetching)
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.fetch_models(cx))))
                .child(action_button("ai-test", test_label, test_ok, !self.testing)
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.test(cx))))))
            .when(!self.models.is_empty(), |d| d.child(div().id("ai-model-list").max_h(px(88.)).overflow_y_scroll().child(chips)))
            .children(error.map(|e| div().text_size(px(13.)).text_color(gpui::rgb(0xb42318)).child(SharedString::from(e))))
    }
}

impl Render for AiSettings {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if self.last_en != is_en() { self.last_en = is_en(); self.sync_placeholders(window, cx); }
        div().id("ai-settings-section").flex().flex_col().gap(px(24.))
            .child(self.connection_card(cx))
            .child(self.model_card(cx))
    }
}
