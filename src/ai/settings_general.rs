//! General AI features use the same model controls as the chat composer.
use std::{cell::Cell, rc::Rc};
use gpui::{AppContext, Bounds, Context, Entity, Pixels, Subscription, Window, div, prelude::*, px};
use gpui_component::input::{Input, InputEvent, InputState};
use super::{AiSettings, TestNote};
use super::super::{annotation_config::AnnotationConfig, backend::{self, BackendKind, ModelOption, model_cache::{ModelCache, ModelStatus}}, backend_ui};
use crate::{theme::{Theme, tr}, ui::{ai_controls, settings_controls::{card_title, setting_row, settings_card}}};

pub(super) struct GeneralSettings {
    config: Option<AnnotationConfig>,
    open: bool,
    backend: BackendKind,
    models: ModelCache,
    error: Option<String>,
    search: Entity<InputState>,
    bounds: Rc<Cell<Option<Bounds<Pixels>>>>,
    _subscription: Subscription,
}

impl GeneralSettings {
    pub(super) fn new(window: &mut Window, cx: &mut Context<AiSettings>) -> Self {
        let result = AnnotationConfig::load_or_seed(cx);
        let backend = result.as_ref().map_or(BackendKind::Builtin, |c| c.backend);
        let error = result.as_ref().err().cloned();
        let search = cx.new(|cx| InputState::new(window, cx).placeholder(tr("搜索模型…", "Search models…")));
        let subscription = cx.subscribe(&search, |_, _, event: &InputEvent, cx| {
            if matches!(event, InputEvent::Change) { cx.notify(); }
        });
        Self { config: result.ok(), open: false, backend, models: Default::default(), error,
            search, bounds: Default::default(), _subscription: subscription }
    }

    pub(super) fn invalidate_builtin_models(&mut self) {
        self.models.invalidate(BackendKind::Builtin);
    }
}

impl AiSettings {
    pub(super) fn initialize_general(&mut self, cx: &mut Context<Self>) {
        if let Some(config) = &self.general.config && config.model.is_empty() {
            self.load_annotation_models(config.backend, cx);
        }
    }

    fn load_annotation_models(&mut self, kind: BackendKind, cx: &mut Context<Self>) {
        self.general.backend = kind;
        self.general.error = None;
        let Some(generation) = self.general.models.begin_refresh(kind) else { cx.notify(); return; };
        let builtin = self.general.config.as_ref().filter(|config| config.backend == kind && kind == BackendKind::Builtin)
            .map(AnnotationConfig::builtin_config);
        let task = cx.background_spawn(async move {
            if let Some(config) = builtin {
                super::super::provider::list_models(&config?).map(|models| models.into_iter().map(|value| ModelOption { label: value.clone(), value }).collect())
            } else { backend::list_models(kind) }
        });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                if !this.general.models.finish(kind, generation, result) { return; }
                // Resolve an initially unknown model once, never follow later chat changes.
                if this.general.models.status(kind) == ModelStatus::Ready
                    && let Some(config) = &this.general.config
                    && config.backend == kind && config.model.is_empty()
                    && let Some(first) = this.general.models.choices(kind).first() {
                    let mut config = config.clone(); config.model = first.value.clone();
                    this.save_annotation_config(config, cx);
                }
                cx.notify();
            });
        }).detach();
        cx.notify();
    }

    fn save_annotation_config(&mut self, config: AnnotationConfig, cx: &mut Context<Self>) {
        match config.save() {
            Ok(()) => { self.general.config = Some(config); self.general.error = None; },
            Err(error) => { self.general.error = Some(error.clone()); self.test_note = Some(TestNote::Err(error)); },
        }
        cx.notify();
    }

    pub(in crate::editor::ai) fn general_settings(&self, cx: &mut Context<Self>) -> gpui::Div {
        let config = self.general.config.as_ref();
        let kind = config.map_or(self.general.backend, |c| c.backend);
        let label = config.filter(|c| !c.model.is_empty()).map_or_else(
            || backend_ui::backend_name(kind).to_owned(), |c| c.model.clone());
        let thinking = config.map(|c| backend_ui::thinking_label(&c.thinking));
        let model_error = match self.general.models.status(self.general.backend) {
            ModelStatus::Error(error) => Some(error), _ => None,
        };
        let trigger = ai_controls::deck_trigger_capsule("annotation-model-trigger", backend_ui::backend_logo(kind),
            label, thinking, true, self.general.open, cx.listener(|this, _, window, cx| {
                cx.stop_propagation();
                this.general.open = !this.general.open;
                if this.general.open {
                    this.general.search.update(cx, |input, cx| { input.set_value("", window, cx); input.focus(window, cx); });
                    let kind = this.general.config.as_ref().map_or(this.general.backend, |c| c.backend);
                    this.load_annotation_models(kind, cx);
                }
                cx.notify();
            })).child(crate::ui::placement::track_bounds(self.general.bounds.clone(), self.general.open));
        div().w_full().min_w_0().flex().flex_col().gap(px(12.))
            .child(card_title(tr("普通功能", "General Features")))
            .child(settings_card(12.).child(setting_row(tr("标注功能", "Annotations"), trigger)))
            .children(self.general.error.as_ref().or(model_error.as_ref()).map(|error| div().text_size(px(14.)).text_color(Theme::ocean().text_secondary).child(error.clone())))
    }

    pub(in crate::editor::ai) fn annotation_menu(&self, window: &Window, cx: &mut Context<Self>) -> Option<gpui::Deferred> {
        if !self.enabled || !self.general.open { return None; }
        let trigger = self.general.bounds.get()?;
        let theme = Theme::ocean();
        let kind = self.general.backend;
        let config = self.general.config.as_ref();
        let query = self.general.search.read(cx).value().to_lowercase();
        let mut rail = div().w(px(46.)).flex_none().p(px(6.)).border_r_1().border_color(theme.hairline).flex().flex_col().gap(px(6.));
        for backend in backend::BackendConfig::load().ordered_enabled(&super::super::chat::recognized_backends(cx)) {
            rail = rail.child(ai_controls::backend_rail_item(format!("annotation-backend-{backend:?}"),
                backend_ui::backend_name(backend), backend_ui::backend_logo(backend), kind == backend, true, false,
                cx.listener(move |this, _, window, cx| {
                    cx.stop_propagation();
                    this.general.search.update(cx, |input, cx| input.set_value("", window, cx));
                    this.load_annotation_models(backend, cx);
                })));
        }
        let mut models = div().id("annotation-model-list").h(px(208.)).overflow_y_scroll().p(px(6.)).flex().flex_col().gap(px(2.));
        let choices = self.general.models.choices(kind);
        for (index, model) in choices.iter().enumerate().filter(|(_, m)| m.value.to_lowercase().contains(&query) || m.label.to_lowercase().contains(&query)) {
            let value = model.value.clone();
            let active = config.is_some_and(|c| c.backend == kind && c.model == value);
            models = models.child(ai_controls::model_row(format!("annotation-model-{index}"), backend_ui::backend_logo(kind),
                model.label.clone(), None, active, false, cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    let config = AnnotationConfig { backend: kind, provider: this.general.config.as_ref().filter(|c| c.backend == kind).map_or_else(|| super::super::provider::config().provider, |c| c.provider.clone()),
                        model: value.clone(), thinking: this.general.config.as_ref().map_or("high".into(), |c| c.thinking.clone()) };
                    this.save_annotation_config(config, cx);
                })));
        }
        if choices.is_empty() {
            models = models.child(div().p(px(8.)).text_size(px(14.)).child(if self.general.models.status(kind) == ModelStatus::Loading {
                tr("正在获取模型…", "Loading models…")
            } else { tr("暂无可选模型", "No models available") }));
        }
        let mut levels = div().p(px(8.)).flex().flex_col().gap(px(4.));
        for (level, label) in [("low", "Low"), ("medium", "Medium"), ("high", "High")] {
            levels = levels.child(ai_controls::thinking_level_item(format!("annotation-thinking-{level}"), label,
                config.is_some_and(|c| c.thinking == level), false, cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    if let Some(mut config) = this.general.config.clone() { config.thinking = level.into(); this.save_annotation_config(config, cx); }
                })));
        }
        let search = div().h(px(40.)).px(px(10.)).border_b_1().border_color(theme.hairline)
            .child(Input::new(&self.general.search).appearance(false).bordered(false).focus_bordered(false));
        let width = px(520.).min(window.viewport_size().width - px(16.));
        let panel = ai_controls::panel_shell("annotation-model-menu").w(width).max_h(window.viewport_size().height - px(16.))
            .on_scroll(|_, _, cx| cx.stop_propagation())
            .on_mouse_down_out(cx.listener(|this, event: &gpui::MouseDownEvent, _, cx| {
                if !this.general.bounds.get().is_some_and(|bounds| bounds.contains(&event.position)) { this.general.open = false; cx.notify(); }
            }))
            .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, _, cx| {
                if event.keystroke.key == "escape" { this.general.open = false; cx.stop_propagation(); cx.notify(); }
            }))
            .child(div().flex().child(rail)
                .child(div().flex_1().min_w_0().flex().flex_col().child(search).child(models))
                .child(div().w(px(140.)).flex_none().border_l_1().border_color(theme.hairline).flex().flex_col()
                    .child(div().h(px(40.)).px(px(10.)).flex().items_center().text_size(px(14.)).child(tr("思考强度", "Thinking Level")))
                    .child(levels)));
        let bounds = self.general.bounds.clone();
        Some(crate::ui::placement::smart_popover_with(move |_, _| {
            crate::ui::placement::PlacementSpec::above(bounds.get().unwrap_or(trigger))
                .align_x(crate::ui::placement::AlignX::Right)
        }, panel, 20))
    }
}
