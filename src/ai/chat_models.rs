use std::{sync::mpsc, time::Duration};
use gpui::Context;
use super::AiChat;
use super::super::backend;
pub(in crate::editor::ai) use backend::model_cache::ModelStatus;

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
        self.model_cache.choices(kind).iter().map(|item| item.value.clone()).filter(|value| !value.is_empty()).collect()
    }
    pub(in crate::editor::ai) fn model_label(&self, value: &str) -> String {
        self.model_cache.choices(self.selected_backend()).iter().find(|item| item.value == value)
            .map(|item| item.label.clone()).unwrap_or_else(|| value.to_owned())
    }
    pub(in crate::editor::ai) fn model_status(&self) -> ModelStatus {
        self.model_cache.status(self.selected_backend())
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
        let Some(generation) = self.model_cache.begin_refresh(kind) else { return; };
        let (sender, receiver) = mpsc::channel();
        std::thread::spawn(move || { let _ = sender.send(backend::list_models(kind)); });
        cx.spawn(async move |this, cx| loop {
            cx.background_executor().timer(Duration::from_millis(60)).await;
            match receiver.try_recv() {
                Ok(result) => {
                    let _ = this.update(cx, |this, cx| {
                        let result = result.map_err(|error| format!("{}: {error}", crate::theme::tr("获取模型列表失败", "Could not load models")));
                        if this.model_cache.finish(kind, generation, result) { cx.notify(); }
                    });
                    break;
                }
                Err(mpsc::TryRecvError::Empty) => {},
                Err(mpsc::TryRecvError::Disconnected) => {
                    let _ = this.update(cx, |this, cx| {
                        let error = crate::theme::tr("获取模型列表中断", "Model discovery stopped").into();
                        if this.model_cache.finish(kind, generation, Err(error)) { cx.notify(); }
                    });
                    break;
                }
            }
        }).detach();
        cx.notify();
    }
}
