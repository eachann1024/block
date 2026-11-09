//! 设置 › AI 助手 state: three provider cards (DeepSeek / OpenAI 协议 / Claude 协议), Base URL,
//! API Key and 连接测试. Every edit is saved straight to `ai.json`, which inline AI and the
//! chat panel read per request. Rendering lives in `settings_view.rs`.
use std::collections::BTreeMap;
use gpui::{App, AppContext, Context, Entity, Global, Subscription, Window};
use gpui_component::input::{InputEvent, InputState};

use super::presets::{self, Protocol, ProviderProfile};
use super::provider::{self, Config};
use crate::theme::is_en;
#[path = "settings_agent.rs"]
mod settings_agent;
#[path = "settings_general.rs"]
mod settings_general;

pub(super) enum TestNote { Ok, Err(String) }

pub(crate) struct AiSettings {
    pub(super) enabled: bool, pub(super) provider: String, protocol: Protocol,
    providers: BTreeMap<String, ProviderProfile>, pub(super) show_key: bool, pub(super) last_en: bool,
    pub(super) base_url: Entity<InputState>, pub(super) api_key: Entity<InputState>,
    pub(super) model: String,
    pub(super) testing: bool, pub(super) test_note: Option<TestNote>,
    pub(super) toggle_anim: crate::ui::toggle_pill::TogglePillState,
    pub(super) focused_backend: super::backend::BackendKind,
    pub(super) general: settings_general::GeneralSettings,
    _subs: Vec<Subscription>,
}

struct Holder(Entity<AiSettings>);
impl Global for Holder {}

/// Creates the form the first time 偏好设置 opens (inputs need a window).
pub(crate) fn ensure(window: &mut Window, cx: &mut App) {
    let chat_entity = super::chat::chat(window, cx);
    chat_entity.update(cx, |chat, cx| {
        for kind in super::backend::BackendKind::ALL {
            if kind != super::backend::BackendKind::Builtin && chat.backend_status(kind).is_none() {
                chat.probe_backend(kind, cx);
            }
        }
    });
    if cx.has_global::<Holder>() { return; }
    let form = cx.new(|cx| AiSettings::new(window, cx));
    cx.set_global(Holder(form));
}

pub(crate) fn section(cx: &App) -> Option<Entity<AiSettings>> {
    cx.try_global::<Holder>().map(|h| h.0.clone())
}

pub(crate) fn reload(window: &mut Window, cx: &mut App) {
    let Some(entity) = section(cx) else { return };
    entity.update(cx, |this, cx| {
        let c = provider::stored();
        this.enabled = c.enabled;
        this.provider = c.provider;
        this.protocol = c.protocol;
        this.providers = c.providers;
        this.model = c.model.trim().to_owned();
        this.sync_placeholders(window, cx);
        let (url, key) = (c.base_url, c.api_key);
        this.base_url.update(cx, |i, cx| i.set_value(url, window, cx));
        this.api_key.update(cx, |i, cx| i.set_value(key, window, cx));
        this.select_focused_backend(super::backend::BackendKind::Builtin, window, cx);
        cx.notify();
    });
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
        let model = c.model.trim().to_owned();
        let mut subs = Vec::new();
        for input in [&base_url, &api_key] {
            subs.push(cx.subscribe(input, |this: &mut Self, _, e: &InputEvent, cx| {
                if matches!(e, InputEvent::Change) { this.test_note = None; this.save(cx); }
            }));
        }
        let general = settings_general::GeneralSettings::new(window, cx);
        let mut settings = AiSettings {
            enabled: c.enabled, provider: c.provider, protocol: c.protocol, providers: c.providers,
            show_key: false, last_en: is_en(), base_url, api_key, model,
            testing: false, test_note: None,
            toggle_anim: Default::default(), focused_backend: super::backend::BackendKind::Builtin,
            general, _subs: subs,
        };
        settings.initialize_general(cx);
        settings
    }

    pub(super) fn sync_placeholders(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let (url_ph, key_ph) = placeholders(&self.provider, self.protocol);
        self.base_url.update(cx, |i, cx| i.set_placeholder(url_ph, window, cx));
        self.api_key.update(cx, |i, cx| i.set_placeholder(key_ph, window, cx));
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
        self.test_note = None;
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
        self.model = mdl.trim().to_owned();
        self.save(cx);
    }

    pub(super) fn toggle_backend(&mut self, kind: super::backend::BackendKind, cx: &mut Context<Self>) {
        let recognized = super::chat::recognized_backends(cx);
        let mut config = super::backend::BackendConfig::load();
        let next = !config.is_enabled(kind);
        if let Err(error) = config.set_enabled(kind, next, &recognized) {
            self.test_note = Some(TestNote::Err(error));
            cx.notify();
            return;
        }
        super::chat::sync_settings(cx);
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
                this.model = provider::stored().model.trim().to_owned();
                super::chat::sync_settings(cx);
                cx.notify();
            });
        }).detach();
        cx.notify();
    }
}
