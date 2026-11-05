mod bridge;
mod gate;
pub(crate) mod skill_resources;
mod claude;
mod codex;
mod config;
mod dsh;
mod runtime;
pub(crate) mod priority;
mod types;

pub(crate) use bridge::{ToolBridge, ToolRequest, ToolResponder};
pub(crate) use config::{BackendConfig, DshOptions};
pub(crate) use runtime::BackendRuntime;
pub(crate) use types::*;

pub(crate) fn adapter(kind: BackendKind) -> Result<Box<dyn BackendAdapter>, BackendError> {
    match kind {
        BackendKind::Builtin => Err(BackendError::new("builtin_adapter", "Built-in AI uses its existing provider")),
        BackendKind::Codex => Ok(Box::new(codex::CodexAdapter::default())),
        BackendKind::Claude => Ok(Box::new(claude::ClaudeAdapter::default())),
        BackendKind::Dsh => Ok(Box::new(dsh::DshAdapter::default())),
    }
}

pub(crate) fn list_models(kind: BackendKind) -> Result<Vec<ModelOption>, String> {
    match kind {
        BackendKind::Builtin => super::provider::list_models(&super::provider::config()).map(|models| models.into_iter().map(|value| ModelOption { label: value.clone(), value }).collect()),
        BackendKind::Codex => codex::models::list().map(|models| models.into_iter().map(|value| ModelOption { label: value.clone(), value }).collect()),
        BackendKind::Claude => claude::models::list(),
        // Official dsh provider's installed 0.2.1-rc.5 advisory defaults; availability depends on the account.
        BackendKind::Dsh => Ok(vec![ModelOption { value: "deepseek-flash".into(), label: "DeepSeek-V4.1-Flash".into() },
            ModelOption { value: "deepseek-v4-pro".into(), label: "DeepSeek-V4-Pro".into() }]),
    }
}
