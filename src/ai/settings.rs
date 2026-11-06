//! 设置 › AI 助手 state: three provider cards (DeepSeek / OpenAI 协议 / Claude 协议), Base URL,
//! API Key, 默认模型, model fetch and 连接测试. Every edit is saved straight to `ai.json`, which
//! inline AI and the chat panel read per request. Rendering lives in `settings_view.rs`.
use std::collections::BTreeMap;
use gpui::{App, AppContext, Context, Entity, Global, Subscription, Window};
use gpui_component::input::{InputEvent, InputState};

use super::presets::{self, Protocol, ProviderProfile};
use super::provider::{self, Config};
use crate::theme::{is_en, tr};

pub(super) enum TestNote { Ok, Err(String) }

pub(crate) struct AiSettings {
    pub(super) enabled: bool, pub(super) provider: String, protocol: Protocol,
    providers: BTreeMap<String, ProviderProfile>, pub(super) show_key: bool, pub(super) last_en: bool,
    pub(super) base_url: Entity<InputState>, pub(super) api_key: Entity<InputState>,
    pub(super) model_filter: Entity<InputState>, pub(super) model: String,
    pub(super) models: Vec<String>, pub(super) models_err: Option<String>,
    pub(super) fetching: bool, pub(super) testing: bool, pub(super) test_note: Option<TestNote>,
    pub(super) toggle_anim: crate::ui::toggle_pill::TogglePillState,
    _subs: Vec<Subscription>,
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

fn placeholders(provider: &str, protocol: Protocol) -> (&'static str, &'static str) {
    let preset = presets::find(provider);
    let url_ph = preset.map_or("https://api.example.com/v1", |p| p.base_url);
    let key_ph = if preset.map_or(protocol, |p| p.protocol) == Protocol::Anthropic { "sk-ant-…" } else { "sk-…" };
    (url_ph, key_ph)
}

impl AiSettings {
    fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let c = provider::stored();
        let (url_ph, key_ph) = placeholders(&c.provider, c.protocol);
        let base_url = cx.new(|cx| InputState::new(window, cx).placeholder(url_ph).default_value(c.base_url.clone()));
        let api_key = cx.new(|cx| InputState::new(window, cx).placeholder(key_ph).masked(true).default_value(c.api_key.clone()));
        let model_filter = cx.new(|cx| InputState::new(window, cx).placeholder(tr("筛选模型…", "Filter models…")));
        let model = c.model.trim().to_owned();
        let models = if model.is_empty() { Vec::new() } else { vec![model.clone()] };
        let mut subs = Vec::new();
        for input in [&base_url, &api_key] {
            subs.push(cx.subscribe(input, |this: &mut Self, _, e: &InputEvent, cx| {
                if matches!(e, InputEvent::Change) { this.test_note = None; this.save(cx); }
            }));
        }
        subs.push(cx.subscribe(&model_filter, |_: &mut Self, _, e: &InputEvent, cx| {
            if matches!(e, InputEvent::Change) { cx.notify(); }
        }));
        let should_fetch = c.is_complete();
        let mut this = AiSettings {
            enabled: c.enabled, provider: c.provider, protocol: c.protocol, providers: c.providers,
            show_key: false, last_en: is_en(), base_url, api_key, model_filter, model,
            models, models_err: None, fetching: false, testing: false, test_note: None,
            toggle_anim: Default::default(), _subs: subs,
        };
        if should_fetch { this.fetch_models(cx); }
        this
    }

    pub(super) fn sync_placeholders(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let (url_ph, key_ph) = placeholders(&self.provider, self.protocol);
        self.base_url.update(cx, |i, cx| i.set_placeholder(url_ph, window, cx));
        self.api_key.update(cx, |i, cx| i.set_placeholder(key_ph, window, cx));
        self.model_filter.update(cx, |i, cx| i.set_placeholder(tr("筛选模型…", "Filter models…"), window, cx));
    }

    fn current(&self, cx: &App) -> Config {
        let base_url = self.base_url.read(cx).value().trim().to_owned();
        let api_key = self.api_key.read(cx).value().trim().to_owned();
        let model = self.model.trim().to_owned();
        let mut providers = self.providers.clone();
        if !self.provider.trim().is_empty() {
            providers.insert(self.provider.clone(), ProviderProfile {
                protocol: self.protocol, base_url: base_url.clone(), api_key: api_key.clone(), model: model.clone(),
            });
        }
        Config { enabled: self.enabled, provider: self.provider.clone(), protocol: self.protocol, base_url, api_key, model, providers }
    }

    pub(super) fn save(&mut self, cx: &mut Context<Self>) {
        let cfg = self.current(cx);
        self.providers = cfg.providers.clone();
        if let Err(e) = provider::save(&cfg) {
            let msg = if is_en() { format!("Failed to save: {e}") } else { format!("保存失败：{e}") };
            self.test_note = Some(TestNote::Err(msg));
        }
        super::chat::sync_settings(cx);
        cx.notify();
    }

    pub(super) fn select_provider(&mut self, id: &'static str, window: &mut Window, cx: &mut Context<Self>) {
        self.providers = self.current(cx).providers;
        self.provider = id.to_owned();
        self.models_err = None; self.test_note = None;
        let preset_url = presets::find(id).map_or("", |p| p.base_url);
        let preset_proto = presets::find(id).map_or(Protocol::Openai, |p| p.protocol);
        let (proto, url, key, mdl) = match self.providers.get(id).cloned() {
            Some(p) => {
                let u = if p.base_url.trim().is_empty() { preset_url.to_owned() } else { p.base_url };
                (preset_proto, u, p.api_key, p.model)
            }
            None => (preset_proto, preset_url.to_owned(), String::new(), String::new()),
        };
        self.protocol = proto;
        self.sync_placeholders(window, cx);
        self.base_url.update(cx, |i, cx| i.set_value(url, window, cx));
        self.api_key.update(cx, |i, cx| i.set_value(key, window, cx));
        self.model_filter.update(cx, |i, cx| i.set_value(String::new(), window, cx));
        self.model = mdl.trim().to_owned();
        self.models = if self.model.is_empty() { Vec::new() } else { vec![self.model.clone()] };
        self.save(cx);
        if self.current(cx).is_complete() { self.fetch_models(cx); }
    }

    pub(super) fn select_model(&mut self, id: String, cx: &mut Context<Self>) {
        self.model = id;
        self.test_note = None;
        self.save(cx);
    }

    pub(super) fn fetch_models(&mut self, cx: &mut Context<Self>) {
        if self.fetching { return; }
        let config = provider::normalize(self.current(cx));
        self.fetching = true; self.models_err = None;
        let task = cx.background_spawn(async move { provider::list_models(&config) });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                this.fetching = false;
                match result {
                    Ok(mut ids) => {
                        if !this.model.is_empty() && !ids.contains(&this.model) {
                            ids.insert(0, this.model.clone());
                        }
                        this.models = ids;
                        this.models_err = None;
                    }
                    Err(e) => {
                        this.models = if this.model.is_empty() { Vec::new() } else { vec![this.model.clone()] };
                        this.models_err = Some(e);
                    }
                }
                cx.notify();
            });
        }).detach();
        cx.notify();
    }

    pub(super) fn test(&mut self, cx: &mut Context<Self>) {
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
