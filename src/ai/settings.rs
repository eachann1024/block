//! 偏好设置 › AI 助手, after thinkly's AI settings tab: 启用 AI 助手, AI 服务
//! (Base URL / API Key), AI 模型 (fetch + 默认模型) and 连接测试. Every edit is saved
//! straight to `ai.json`, which the inline AI and the chat panel read per request.
use gpui::{App, AppContext, Context, CursorStyle, Entity, FontWeight, Global, IntoElement, MouseButton, ParentElement,
    Render, SharedString, Styled, Subscription, Window, div, prelude::*, px};
use gpui_component::input::{Input, InputEvent, InputState};

use super::provider::{self, Config};
use crate::theme::Theme;

pub(crate) struct AiSettings {
    enabled: bool,
    base_url: Entity<InputState>,
    api_key: Entity<InputState>,
    model: Entity<InputState>,
    models: Vec<String>,
    models_note: String,
    fetching: bool,
    testing: bool,
    test_note: Option<(bool, String)>,
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

impl AiSettings {
    fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let c = provider::stored();
        let base_url = cx.new(|cx| InputState::new(window, cx).placeholder(provider::DEFAULT_BASE_URL).default_value(c.base_url.clone()));
        let api_key = cx.new(|cx| InputState::new(window, cx).placeholder("sk-…（本地网关可留空）").masked(true).default_value(c.api_key.clone()));
        let model = cx.new(|cx| InputState::new(window, cx).placeholder("请选择模型（留空自动选择）").default_value(c.model.clone()));
        let mut subs = Vec::new();
        for input in [&base_url, &api_key, &model] {
            subs.push(cx.subscribe(input, |this: &mut Self, _, e: &InputEvent, cx| {
                if matches!(e, InputEvent::Change) { this.save(cx); }
            }));
        }
        let mut this = AiSettings { enabled: c.enabled, base_url, api_key, model, models: Vec::new(), models_note: String::new(),
            fetching: false, testing: false, test_note: None, _subs: subs };
        this.fetch_models(cx);
        this
    }
    fn current(&self, cx: &App) -> Config {
        Config { enabled: self.enabled, base_url: self.base_url.read(cx).value().trim().to_owned(),
            api_key: self.api_key.read(cx).value().trim().to_owned(), model: self.model.read(cx).value().trim().to_owned() }
    }
    fn save(&mut self, cx: &mut Context<Self>) {
        if let Err(e) = provider::save(&self.current(cx)) { self.test_note = Some((false, format!("保存失败：{e}"))); }
        cx.notify();
    }
    fn fetch_models(&mut self, cx: &mut Context<Self>) {
        if self.fetching { return; }
        self.fetching = true;
        self.models_note = "正在获取模型…".into();
        let config = provider::normalize(self.current(cx));
        let task = cx.background_spawn(async move { provider::list_models(&config) });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                this.fetching = false;
                match result {
                    Ok(ids) => { this.models_note = format!("{} 个可用模型", ids.len()); this.models = ids; }
                    Err(e) => { this.models_note = e; this.models.clear(); }
                }
                cx.notify();
            });
        }).detach();
        cx.notify();
    }
    fn test(&mut self, cx: &mut Context<Self>) {
        if self.testing { return; }
        let config = provider::normalize(self.current(cx));
        self.testing = true;
        self.test_note = None;
        let task = cx.background_spawn(async move { provider::test_connection(&config) });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                this.testing = false;
                this.test_note = Some(match result { Ok(()) => (true, "连接成功。".into()), Err(e) => (false, e) });
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
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let on = self.enabled;
        let current_model = self.model.read(cx).value().to_string();
        let switch = div().id("ai-enabled-switch").w(px(44.)).h(px(24.)).p(px(2.)).flex().items_center().rounded(px(12.))
            .bg(if on { theme.accent } else { theme.shell }).border_1().border_color(if on { theme.accent } else { theme.btn_border })
            .cursor(CursorStyle::PointingHand)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.enabled = !this.enabled; this.save(cx); }))
            .child(div().size(px(18.)).rounded_full().bg(gpui::rgb(0xffffff)).shadow_sm().when(on, |k| k.ml_auto()));
        let models = self.models.iter().take(40).cloned().fold(div().flex().flex_wrap().gap(px(6.)), |d, id| {
            let active = id == current_model;
            let pick = id.clone();
            d.child(div().id(SharedString::from(format!("ai-model-{id}"))).h(px(24.)).px(px(8.)).flex().items_center().rounded(px(12.))
                .text_size(px(12.)).border_1().border_color(if active { theme.accent } else { theme.hairline })
                .text_color(if active { theme.accent } else { theme.text_secondary })
                .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                    let value = pick.clone();
                    this.model.update(cx, |i, cx| i.set_value(value, window, cx));
                    this.save(cx);
                }))
                .child(id))
        });
        div().id("ai-settings-section").bg(theme.paper).border_1().border_color(theme.hairline).rounded(px(10.))
            .py(px(18.)).px(px(20.)).flex().flex_col().gap(px(14.))
            .child(div().text_size(px(15.)).line_height(px(22.)).font_weight(FontWeight(560.)).child("AI 助手"))
            .child(row("启用 AI 助手", "关闭会停止正在生成的回答。", switch))
            .child(row("Base URL", "OpenAI 兼容接口地址", field(&self.base_url)))
            .child(row("API Key", "", field(&self.api_key)))
            .child(row("默认模型", "", field(&self.model)))
            .child(div().flex().items_center().gap(px(8.))
                .child(div().flex_1().text_size(px(12.)).text_color(theme.text_secondary).child(SharedString::from(format!("AI 模型 · {}", self.models_note))))
                .child(button("ai-fetch-models", "重新获取模型", !self.fetching)
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.fetch_models(cx)))))
            .when(!self.models.is_empty(), |d| d.child(div().id("ai-model-list").max_h(px(84.)).overflow_y_scroll().child(models)))
            .child(row("连接测试", "测试不会读取笔记或本地上下文。",
                button("ai-test", if self.testing { "测试中…" } else { "测试连接" }, !self.testing)
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.test(cx)))))
            .children(self.test_note.as_ref().map(|(ok, msg)| div().text_size(px(12.))
                .text_color(if *ok { gpui::rgb(0x087443) } else { gpui::rgb(0xb42318) }).child(SharedString::from(msg.clone()))))
    }
}
