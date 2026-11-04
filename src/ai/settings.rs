//! 偏好设置 › AI 助手, modeled on Cherry Studio's provider settings: 服务商预设、接口协议、
//! Base URL / API Key、AI 模型 (fetch + 默认模型) and 连接测试. Every edit is saved
//! straight to `ai.json`, which inline AI and the chat panel read per request.
use std::collections::BTreeMap;
use gpui::{App, AppContext, Context, CursorStyle, Entity, FontWeight, Global, IntoElement, MouseButton,
    ParentElement, Render, SharedString, Styled, Subscription, Window, deferred, div, prelude::*, px};
use gpui_component::input::{Input, InputEvent, InputState};

use super::presets::{self, PRESETS, Protocol, ProviderProfile};
use super::provider::{self, Config};
use crate::dropdown;
use crate::icons::Icon;
use crate::theme::{Theme, is_en, tr};

enum TestNote { Ok, Err(String) }

pub(crate) struct AiSettings {
    enabled: bool, provider: String, protocol: Protocol,
    providers: BTreeMap<String, ProviderProfile>, menu_open: bool, last_en: bool,
    base_url: Entity<InputState>, api_key: Entity<InputState>, model: Entity<InputState>,
    models: Vec<String>, models_err: Option<String>,
    fetching: bool, testing: bool, test_note: Option<TestNote>, _subs: Vec<Subscription>,
}

struct Holder(Entity<AiSettings>);
impl Global for Holder {}

/// Creates the form the first time 偏好设置 opens (inputs need a window).
pub(crate) fn ensure(window: &mut Window, cx: &mut App) {
    if cx.has_global::<Holder>() { return; }
    let form = cx.new(|cx| AiSettings::new(window, cx));
    cx.set_global(Holder(form));
}

pub(crate) fn section(cx: &App) -> Option<Entity<AiSettings>> {
    cx.try_global::<Holder>().map(|h| h.0.clone())
}

fn placeholders(provider: &str, protocol: Protocol) -> (&'static str, &'static str, &'static str) {
    let preset = presets::find(provider);
    let url_ph = preset.map(|p| p.base_url).filter(|u| !u.is_empty()).unwrap_or("https://api.example.com/v1");
    let key_req = preset.map_or(true, |p| p.key_required);
    let eff_proto = if provider == "custom" { protocol } else { preset.map_or(protocol, |p| p.protocol) };
    let key_ph = if !key_req {
        tr("本地服务可留空", "Optional for local services")
    } else if eff_proto == Protocol::Anthropic { "sk-ant-…" } else { "sk-…" };
    (url_ph, key_ph, tr("选择或输入模型 ID", "Select or enter model ID"))
}

impl AiSettings {
    fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let c = provider::stored();
        let (url_ph, key_ph, model_ph) = placeholders(&c.provider, c.protocol);
        let base_url = cx.new(|cx| InputState::new(window, cx).placeholder(url_ph).default_value(c.base_url.clone()));
        let api_key = cx.new(|cx| InputState::new(window, cx).placeholder(key_ph).masked(true).default_value(c.api_key.clone()));
        let model = cx.new(|cx| InputState::new(window, cx).placeholder(model_ph).default_value(c.model.clone()));
        let mut subs = Vec::new();
        for input in [&base_url, &api_key, &model] {
            subs.push(cx.subscribe(input, |this: &mut Self, _, e: &InputEvent, cx| {
                if matches!(e, InputEvent::Change) { this.save(cx); }
            }));
        }
        let should_fetch = c.is_complete();
        let mut this = AiSettings {
            enabled: c.enabled, provider: c.provider, protocol: c.protocol, providers: c.providers,
            menu_open: false, last_en: is_en(), base_url, api_key, model,
            models: Vec::new(), models_err: None, fetching: false, testing: false, test_note: None, _subs: subs,
        };
        if should_fetch { this.fetch_models(cx); }
        this
    }

    fn sync_placeholders(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let (url_ph, key_ph, model_ph) = placeholders(&self.provider, self.protocol);
        self.base_url.update(cx, |i, cx| i.set_placeholder(url_ph, window, cx));
        self.api_key.update(cx, |i, cx| i.set_placeholder(key_ph, window, cx));
        self.model.update(cx, |i, cx| i.set_placeholder(model_ph, window, cx));
    }

    fn current(&self, cx: &App) -> Config {
        let base_url = self.base_url.read(cx).value().trim().to_owned();
        let api_key = self.api_key.read(cx).value().trim().to_owned();
        let model = self.model.read(cx).value().trim().to_owned();
        let mut providers = self.providers.clone();
        if !self.provider.trim().is_empty() {
            providers.insert(self.provider.clone(), ProviderProfile {
                protocol: self.protocol, base_url: base_url.clone(), api_key: api_key.clone(), model: model.clone(),
            });
        }
        Config { enabled: self.enabled, provider: self.provider.clone(), protocol: self.protocol, base_url, api_key, model, providers }
    }

    fn save(&mut self, cx: &mut Context<Self>) {
        let cfg = self.current(cx);
        self.providers = cfg.providers.clone();
        if let Err(e) = provider::save(&cfg) {
            let msg = if is_en() { format!("Failed to save: {e}") } else { format!("保存失败：{e}") };
            self.test_note = Some(TestNote::Err(msg));
        }
        cx.notify();
    }

    fn select_provider(&mut self, id: &'static str, window: &mut Window, cx: &mut Context<Self>) {
        self.providers = self.current(cx).providers;
        self.provider = id.to_owned();
        self.menu_open = false;
        self.models.clear(); self.models_err = None; self.test_note = None;
        let preset_url = presets::find(id).map_or("", |p| p.base_url);
        let preset_proto = presets::find(id).map_or(Protocol::Openai, |p| p.protocol);
        let (proto, url, key, mdl) = match self.providers.get(id).cloned() {
            Some(p) => {
                let u = if p.base_url.trim().is_empty() { preset_url.to_owned() } else { p.base_url };
                (if id == "custom" { p.protocol } else { preset_proto }, u, p.api_key, p.model)
            }
            None => (preset_proto, preset_url.to_owned(), String::new(), String::new()),
        };
        self.protocol = proto;
        self.sync_placeholders(window, cx);
        self.base_url.update(cx, |i, cx| i.set_value(url, window, cx));
        self.api_key.update(cx, |i, cx| i.set_value(key, window, cx));
        self.model.update(cx, |i, cx| i.set_value(mdl, window, cx));
        self.save(cx);
        if self.current(cx).is_complete() { self.fetch_models(cx); }
    }

    fn fetch_models(&mut self, cx: &mut Context<Self>) {
        if self.fetching { return; }
        let config = provider::normalize(self.current(cx));
        self.fetching = true; self.models_err = None;
        let task = cx.background_spawn(async move { provider::list_models(&config) });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                this.fetching = false;
                match result {
                    Ok(ids) => { this.models = ids; this.models_err = None; }
                    Err(e) => { this.models.clear(); this.models_err = Some(e); }
                }
                cx.notify();
            });
        }).detach();
        cx.notify();
    }

    fn test(&mut self, cx: &mut Context<Self>) {
        if self.testing { return; }
        let config = provider::normalize(self.current(cx));
        self.testing = true; self.test_note = None;
        let task = cx.background_spawn(async move { provider::test_connection(&config) });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                this.testing = false;
                this.test_note = Some(match result { Ok(()) => TestNote::Ok, Err(e) => TestNote::Err(e) });
                cx.notify();
            });
        }).detach();
        cx.notify();
    }
}

fn row(title: &'static str, desc: &'static str, control: impl IntoElement) -> gpui::Div {
    let theme = Theme::ocean();
    div().flex().items_center().justify_between().gap(px(16.))
        .child(div().flex().flex_col().gap(px(2.)).min_w_0()
            .child(div().text_size(px(14.)).child(title))
            .when(!desc.is_empty(), |d| d.child(div().text_size(px(12.)).text_color(theme.text_secondary).child(desc))))
        .child(control)
}

fn field(input: &Entity<InputState>) -> gpui::Div {
    let theme = Theme::ocean();
    div().w(px(260.)).h(px(32.)).px(px(8.)).flex().items_center().rounded(px(6.)).border_1().border_color(theme.btn_border).bg(theme.paper)
        .text_size(px(13.)).child(div().flex_1().min_w_0().child(Input::new(input).appearance(false).bordered(false).focus_bordered(false).h(px(28.))))
}

fn button(id: &'static str, label: impl Into<SharedString>, enabled: bool) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id(id).h(px(30.)).px(px(12.)).flex().flex_none().items_center().rounded(px(6.)).border_1().border_color(theme.btn_border)
        .text_size(px(13.)).when(enabled, |b| b.cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover)))
        .when(!enabled, |b| b.opacity(0.5)).child(label.into())
}

impl Render for AiSettings {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if self.last_en != is_en() { self.last_en = is_en(); self.sync_placeholders(window, cx); }
        let theme = Theme::ocean();
        let on = self.enabled;
        let current_model = self.model.read(cx).value().to_string();
        let switch = div().id("ai-enabled-switch").w(px(44.)).h(px(24.)).p(px(2.)).flex().items_center().rounded(px(12.))
            .bg(if on { theme.accent } else { theme.shell }).border_1().border_color(if on { theme.accent } else { theme.btn_border })
            .cursor(CursorStyle::PointingHand)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.enabled = !this.enabled; this.save(cx); }))
            .child(div().size(px(18.)).rounded_full().bg(gpui::rgb(0xffffff)).shadow_sm().when(on, |k| k.ml_auto()));

        let preset = presets::find(&self.provider);
        let provider_label = preset.map(|p| p.label()).unwrap_or_else(|| tr("选择服务商…", "Select provider…"));
        let menu_open = self.menu_open;
        let menu = dropdown::shell("ai-provider-menu").w(px(260.)).children(
            PRESETS.iter().enumerate().map(|(ix, p)| {
                let pid = p.id;
                dropdown::row(("ai-preset", ix), Some(p.icon()), p.label(), self.provider == pid)
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                        cx.stop_propagation(); this.select_provider(pid, w, cx);
                    }))
            }),
        );
        let provider_select = div().relative().w(px(260.)).h(px(32.))
            .child(div().id("ai-provider-trigger").size_full().px(px(10.)).flex().items_center().gap(px(8.)).justify_between()
                .rounded(px(6.)).border_1().border_color(if menu_open { theme.accent } else { theme.btn_border }).bg(theme.paper)
                .text_size(px(13.)).text_color(if preset.is_some() { theme.text } else { theme.text_secondary })
                .cursor(CursorStyle::PointingHand).hover(move |s| s.border_color(theme.accent))
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                    cx.stop_propagation(); this.menu_open = !this.menu_open; cx.notify();
                }))
                .when_some(preset, |d, p| d.child(dropdown::duo_icon(p.icon(), 16.)))
                .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(provider_label))
                .child(dropdown::line_icon(Icon::ChevronDown, 14., theme.text_secondary)))
            .when(menu_open, |d| d.child(deferred(div().absolute().top(px(36.)).left_0().child(menu)).with_priority(3)));

        let cur_proto = self.protocol;
        let proto_seg = div().id("ai-proto-seg").w(px(260.)).p(px(2.)).flex().items_center().gap(px(2.)).rounded(px(6.))
            .bg(theme.shell).border_1().border_color(theme.hairline)
            .children([Protocol::Openai, Protocol::Anthropic].into_iter().enumerate().map(|(ix, p)| {
                let active = cur_proto == p;
                div().id(("ai-proto", ix)).flex_1().h(px(26.)).px(px(8.)).flex().items_center().justify_center().rounded(px(5.))
                    .text_size(px(12.)).font_weight(FontWeight(460.)).cursor(CursorStyle::PointingHand)
                    .when(active, |b| b.bg(theme.selected).text_color(theme.text).border_1().border_color(theme.accent))
                    .when(!active, |b| b.text_color(theme.text_secondary).hover(move |s| s.text_color(theme.text)))
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                        this.protocol = p; this.models.clear(); this.models_err = None; this.test_note = None;
                        this.sync_placeholders(w, cx); this.save(cx);
                    }))
                    .child(p.label())
            }));

        let eff_proto = if self.provider == "custom" { self.protocol } else { preset.map_or(self.protocol, |p| p.protocol) };
        let url_desc = match eff_proto {
            Protocol::Anthropic => tr("Anthropic Messages 兼容接口地址", "Anthropic Messages API endpoint"),
            Protocol::Openai => tr("OpenAI 兼容接口地址", "OpenAI-compatible API endpoint"),
        };
        let key_desc = if preset.is_some_and(|p| !p.key_required) { tr("本地服务可留空", "Optional for local services") } else { "" };
        let models_note = if self.fetching { tr("正在获取模型…", "Fetching models…").to_owned() }
            else if let Some(err) = &self.models_err { err.clone() }
            else if !self.models.is_empty() {
                if is_en() { format!("{} models available", self.models.len()) } else { format!("{} 个可用模型", self.models.len()) }
            } else { tr("选择服务商并填写 API Key 后获取模型", "Select a provider and enter an API Key to fetch models").to_owned() };
        let fetch_label = if self.fetching { tr("获取中…", "Fetching…") }
            else if self.models.is_empty() { tr("获取模型", "Fetch Models") }
            else { tr("重新获取模型", "Refresh Models") };
        let models = self.models.iter().take(40).cloned().fold(div().flex().flex_wrap().gap(px(6.)), |d, id| {
            let active = id == current_model;
            let pick = id.clone();
            d.child(div().id(SharedString::from(format!("ai-model-{id}"))).h(px(24.)).px(px(8.)).flex().items_center().rounded(px(12.))
                .text_size(px(12.)).border_1().border_color(if active { theme.accent } else { theme.hairline })
                .text_color(if active { theme.accent } else { theme.text_secondary })
                .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                    this.model.update(cx, |i, cx| i.set_value(pick.clone(), window, cx));
                    this.save(cx);
                }))
                .child(id))
        });

        div().id("ai-settings-section").bg(theme.paper).border_1().border_color(theme.hairline).rounded(px(10.))
            .py(px(18.)).px(px(20.)).flex().flex_col().gap(px(14.))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                if this.menu_open { this.menu_open = false; cx.notify(); }
            }))
            .child(div().text_size(px(15.)).line_height(px(22.)).font_weight(FontWeight(560.)).child(tr("AI 助手", "AI Assistant")))
            .child(row(tr("启用 AI 助手", "Enable AI Assistant"), tr("关闭会停止正在生成的回答。", "Disabling stops any response in progress."), switch))
            .child(row(tr("服务商", "Provider"), tr("选择内置预设或自定义接口", "Choose a built-in preset or custom endpoint"), provider_select))
            .when(self.provider == "custom", |d| d.child(row(tr("接口协议", "API Protocol"), tr("自定义接口的请求与流式协议", "Request and streaming protocol"), proto_seg)))
            .child(row("Base URL", url_desc, field(&self.base_url)))
            .child(row("API Key", key_desc, field(&self.api_key)))
            .child(row(tr("默认模型", "Default Model"), "", field(&self.model)))
            .child(div().flex().items_center().gap(px(8.))
                .child(div().flex_1().text_size(px(12.)).text_color(theme.text_secondary)
                    .child(SharedString::from(format!("{} · {models_note}", tr("AI 模型", "AI Models")))))
                .child(button("ai-fetch-models", fetch_label, !self.fetching)
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.fetch_models(cx)))))
            .when(!self.models.is_empty(), |d| d.child(div().id("ai-model-list").max_h(px(84.)).overflow_y_scroll().child(models)))
            .child(row(tr("连接测试", "Connection Test"), tr("测试不会读取笔记或本地上下文。", "Tests connectivity without sending notes or local context."),
                button("ai-test", if self.testing { tr("测试中…", "Testing…") } else { tr("测试连接", "Test Connection") }, !self.testing)
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.test(cx)))))
            .children(self.test_note.as_ref().map(|note| {
                let (ok, msg) = match note {
                    TestNote::Ok => (true, tr("连接成功。", "Connection succeeded.").to_owned()),
                    TestNote::Err(e) => (false, e.clone()),
                };
                div().text_size(px(12.)).text_color(if ok { gpui::rgb(0x087443) } else { gpui::rgb(0xb42318) }).child(SharedString::from(msg))
            }))
    }
}
