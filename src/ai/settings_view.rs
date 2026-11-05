//! Rendering for 设置 › AI 助手: provider cards, connection fields and model picker.
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
    status_action_button, tag_chip, toggle_pill,
};

fn input_box(input: &Entity<InputState>) -> impl IntoElement {
    div().flex_1().min_w_0().child(Input::new(input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)))
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
        let on = self.enabled;
        let toggle = toggle_pill(
            "ai-enabled-switch",
            crate::icons::Icon::Sparkles,
            on,
            if on { tr("已开启", "Enabled") } else { tr("已关闭", "Disabled") },
            self.toggle_anim,
            cx.listener(|this, active, _, cx| {
                if this.toggle_anim.set_hovered(*active) { cx.notify(); }
            }),
            cx.listener(|this, _, _, cx| {
                this.enabled = !this.enabled;
                this.toggle_anim.record_toggle();
                this.save(cx);
            }),
        );
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
        let current_model = self.model.clone();
        let test_ok = matches!(self.test_note, Some(TestNote::Ok));
        let fetch_label = if self.fetching { tr("获取中…", "Fetching…") }
            else if self.models.is_empty() { tr("获取模型", "Fetch Models") } else { tr("重新获取模型", "Refresh Models") };
        let test_label = if self.testing { tr("测试中…", "Testing…") }
            else if test_ok { tr("连接正常", "Connected") } else { tr("连接测试", "Test Connection") };
        let fetch_icon = if self.fetching { crate::icons::Icon::Spinner } else { crate::icons::Icon::Refresh };
        let test_icon = if self.testing {
            crate::icons::Icon::Spinner
        } else if test_ok {
            crate::icons::Icon::Check
        } else {
            crate::icons::Icon::Sparkles
        };
        let error = self.models_err.clone().or_else(|| match &self.test_note {
            Some(TestNote::Err(e)) => Some(e.clone()),
            _ => None,
        });
        let filter_query = self.model_filter.read(cx).value().trim().to_lowercase();
        let filter_box = field_shell().flex_1().min_w_0().gap(px(6.))
            .child(crate::icons::icon(crate::icons::Icon::FindSearch, px(15.), theme.mono_icon_colors(theme.text_secondary)))
            .child(input_box(&self.model_filter))
            .when(!filter_query.is_empty(), |d| {
                d.child(
                    div().id("ai-filter-clear").group("ai-filter-clear").size(px(20.)).rounded(px(4.)).flex().items_center().justify_center()
                        .cursor(CursorStyle::PointingHand)
                        .hover(move |s| s.bg(theme.hover))
                        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, w, cx| {
                            this.model_filter.update(cx, |i, cx| i.set_value(String::new(), w, cx));
                            cx.notify();
                        }))
                        .child(crate::icons::stateful_icon("ai-filter-clear", crate::icons::Icon::Close, px(12.), false, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text))),
                )
            });
        let toolbar = div().w_full().min_w_0().flex().items_center().gap(px(8.))
            .child(filter_box)
            .child(status_action_button("ai-fetch-models", Some(fetch_icon), fetch_label, false, !self.fetching)
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.fetch_models(cx))))
            .child(status_action_button("ai-test", Some(test_icon), test_label, test_ok, !self.testing)
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.test(cx))));
        let model_list = if self.models.is_empty() {
            let hint = if self.fetching {
                tr("正在获取模型列表…", "Fetching models…")
            } else {
                tr("点击「获取模型」加载可用模型", "Click \"Fetch Models\" to load available models")
            };
            div().id("ai-model-list").text_size(px(14.)).text_color(theme.text_secondary).child(hint)
        } else {
            let mut filtered: Vec<String> = self.models.iter()
                .filter(|m| filter_query.is_empty() || m.to_lowercase().contains(&filter_query))
                .cloned()
                .collect();
            if !filter_query.is_empty() {
                filtered.sort_by(|a, b| {
                    let (la, lb) = (a.to_lowercase(), b.to_lowercase());
                    let key = |s: &str| {
                        let pos = s.find(&filter_query).unwrap_or(usize::MAX);
                        let tier: u8 = if s == filter_query { 0 } else if pos == 0 { 1 } else { 2 };
                        (tier, pos)
                    };
                    key(&la).cmp(&key(&lb)).then_with(|| la.cmp(&lb))
                });
            }
            if filtered.is_empty() {
                div().id("ai-model-list").text_size(px(14.)).text_color(theme.text_secondary)
                    .child(tr("无匹配的模型", "No matching models"))
            } else {
                filtered.into_iter().fold(div().id("ai-model-list").flex().flex_wrap().gap(px(8.)), |d, id| {
                    let active = id == current_model;
                    let pick = id.clone();
                    d.child(
                        tag_chip(SharedString::from(format!("ai-model-{id}")), id, active, true)
                            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                                this.select_model(pick.clone(), cx);
                            })),
                    )
                })
            }
        };
        let dark = is_dark();
        settings_card(16.)
            .child(card_title(tr("模型与连接", "Model & Connection")))
            .child(toolbar)
            .child(model_list)
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
        div().id("ai-settings-section").flex().flex_col().gap(px(24.))
            .child(self.connection_card(cx))
            .child(self.model_card(cx))
    }
}
