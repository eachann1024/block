use std::{sync::mpsc, time::Duration};
use gpui::Context;
use super::AiChat;
use super::super::backend::{self};

#[derive(Clone, Debug, PartialEq)]
pub(in crate::editor::ai) enum ModelStatus { Idle, Loading, Ready, Error(String) }

impl AiChat {
    pub(in crate::editor::ai) fn selected_model(&self) -> String {
        let stored = self.backend_config.selected_model(self.selected_backend());
        let choices = self.model_choices();
        if !stored.is_empty() && (choices.is_empty() || choices.contains(&stored)) {
            return stored;
        }
        choices.into_iter().next().unwrap_or(stored)
    }
    pub(in crate::editor::ai) fn model_choices(&self) -> Vec<String> {
        let kind = self.selected_backend();
        self.model_cache.iter().find(|(backend, _, _)| *backend == kind)
            .map(|(_, _, choices)| choices.iter().map(|item| item.value.clone()).filter(|value| !value.is_empty()).collect())
            .unwrap_or_default()
    }
    pub(in crate::editor::ai) fn model_label(&self, value: &str) -> String {
        self.model_cache.iter().find(|(backend, _, _)| *backend == self.selected_backend())
            .and_then(|(_, _, choices)| choices.iter().find(|item| item.value == value))
            .map(|item| item.label.clone()).unwrap_or_else(|| value.to_owned())
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
        self.refresh_models_for(self.selected_backend(), cx);
    }
    pub(in crate::editor::ai) fn refresh_models_for(&mut self, kind: backend::BackendKind, cx: &mut Context<Self>) {
        if self.model_cache.iter().any(|(backend, status, _)| *backend == kind && *status == ModelStatus::Loading) { return; }
        let had_entry = if let Some((_, status, choices)) = self.model_cache.iter_mut().find(|(backend, _, _)| *backend == kind) {
            if choices.is_empty() { *status = ModelStatus::Loading; }
            true
        } else {
            self.model_cache.push((kind, ModelStatus::Loading, Vec::new()));
            false
        };
        let (sender, receiver) = mpsc::channel();
        std::thread::spawn(move || { let _ = sender.send(backend::list_models(kind)); });
        cx.spawn(async move |this, cx| loop {
            cx.background_executor().timer(Duration::from_millis(60)).await;
            match receiver.try_recv() {
                Ok(result) => {
                    let _ = this.update(cx, |this, cx| {
                        let (status, choices) = match result { Ok(choices) => (ModelStatus::Ready, choices), Err(error) => (ModelStatus::Error(format!("{}: {error}", crate::theme::tr("获取模型列表失败", "Could not load models"))), Vec::new()) };
                        let unchanged = this.model_cache.iter().any(|(backend, old_status, old_choices)| {
                            *backend == kind && *old_status == status && old_choices.len() == choices.len()
                                && old_choices.iter().zip(&choices).all(|(a, b)| a.value == b.value && a.label == b.label)
                        });
                        if unchanged { return; }
                        let fallback = if choices.is_empty() {
                            this.model_cache.iter().find(|(backend, _, _)| *backend == kind).map(|(_, _, old)| old.clone()).unwrap_or_default()
                        } else { choices };
                        this.model_cache.retain(|(backend, _, _)| *backend != kind);
                        this.model_cache.push((kind, status, fallback)); cx.notify();
                    });
                    break;
                }
                Err(mpsc::TryRecvError::Empty) => {},
                Err(mpsc::TryRecvError::Disconnected) => {
                    let _ = this.update(cx, |this, cx| {
                        let fallback = this.model_cache.iter().find(|(backend, _, _)| *backend == kind).map(|(_, _, old)| old.clone()).unwrap_or_default();
                        this.model_cache.retain(|(backend, _, _)| *backend != kind);
                        this.model_cache.push((kind, ModelStatus::Error(crate::theme::tr("获取模型列表中断", "Model discovery stopped").into()), fallback));
                        cx.notify();
                    });
                    break;
                }
            }
        }).detach();
        if !had_entry { cx.notify(); }
    }
}
