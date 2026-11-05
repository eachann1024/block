use std::{sync::mpsc, time::Duration};
use gpui::Context;
use super::AiChat;
use super::super::backend::{self};

#[derive(Clone, Debug, PartialEq)]
pub(in crate::editor::ai) enum ModelStatus { Idle, Loading, Ready, Error(String) }

impl AiChat {
    pub(in crate::editor::ai) fn selected_model(&self) -> String {
        self.backend_config.selected_model(self.selected_backend())
    }
    pub(in crate::editor::ai) fn model_choices(&self) -> Vec<String> {
        let kind = self.selected_backend();
        let mut choices: Vec<String> = self.model_cache.iter().find(|(backend, _, _)| *backend == kind).map(|(_, _, choices)| choices.iter().map(|item| item.value.clone()).filter(|value| !value.is_empty()).collect()).unwrap_or_default();
        let selected = self.selected_model();
        if !selected.is_empty() && !choices.contains(&selected) { choices.insert(0, selected); }
        choices
    }
    pub(in crate::editor::ai) fn model_label(&self, value: &str) -> String {
        self.model_cache.iter().find(|(backend, _, _)| *backend == self.selected_backend())
            .and_then(|(_, _, choices)| choices.iter().find(|item| item.value == value))
            .map(|item| item.label.clone()).unwrap_or_else(|| if value.is_empty() {
                crate::theme::tr("默认模型", "Default model").to_owned()
            } else { value.to_owned() })
    }
    pub(in crate::editor::ai) fn model_status(&self) -> ModelStatus {
        self.model_cache.iter().find(|(backend, _, _)| *backend == self.selected_backend())
            .map(|(_, status, _)| status.clone()).unwrap_or(ModelStatus::Idle)
    }
    pub(in crate::editor::ai) fn select_model(&mut self, model: &str, cx: &mut Context<Self>) -> Result<(), String> {
        if self.streaming.is_some() { return Err(crate::theme::tr("请等待当前回复结束后再切换模型", "Wait for the current response before changing model").into()); }
        self.backend_config.set_model(self.selected_backend(), model).map_err(|error| format!("{}: {error}", crate::theme::tr("保存模型失败", "Could not save model")))?;
        cx.notify(); Ok(())
    }
    pub(in crate::editor::ai) fn refresh_models(&mut self, cx: &mut Context<Self>) {
        let kind = self.selected_backend();
        if self.model_cache.iter().any(|(backend, status, _)| *backend == kind && *status == ModelStatus::Loading) { return; }
        self.model_cache.retain(|(backend, _, _)| *backend != kind);
        self.model_cache.push((kind, ModelStatus::Loading, Vec::new()));
        let (sender, receiver) = mpsc::channel();
        std::thread::spawn(move || { let _ = sender.send(backend::list_models(kind)); });
        cx.spawn(async move |this, cx| loop {
            cx.background_executor().timer(Duration::from_millis(60)).await;
            match receiver.try_recv() {
                Ok(result) => {
                    let _ = this.update(cx, |this, cx| {
                        let (status, mut choices) = match result { Ok(choices) => (ModelStatus::Ready, choices), Err(error) => (ModelStatus::Error(format!("{}: {error}", crate::theme::tr("获取模型列表失败", "Could not load models"))), Vec::new()) };
                        choices.sort_by(|a, b| a.value.cmp(&b.value)); choices.dedup_by(|a, b| a.value == b.value);
                        this.model_cache.retain(|(backend, _, _)| *backend != kind);
                        this.model_cache.push((kind, status, choices)); cx.notify();
                    });
                    break;
                }
                Err(mpsc::TryRecvError::Empty) => {},
                Err(mpsc::TryRecvError::Disconnected) => {
                    let _ = this.update(cx, |this, cx| { this.model_cache.retain(|(backend, _, _)| *backend != kind); this.model_cache.push((kind, ModelStatus::Error(crate::theme::tr("获取模型列表中断", "Model discovery stopped").into()), Vec::new())); cx.notify(); });
                    break;
                }
            }
        }).detach();
        cx.notify();
    }
}
