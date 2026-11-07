mod bridge;
pub(crate) mod cli_env;
mod gate;
pub(crate) mod skill_resources;
mod claude;
mod codex;
mod config;
mod dsh;
mod pi;
mod runtime;
pub(crate) mod priority;
mod types;

pub(crate) use bridge::{ToolBridge, ToolRequest};
pub(crate) use config::BackendConfig;
pub(crate) use runtime::BackendRuntime;
pub(crate) use types::*;

pub(crate) fn adapter(kind: BackendKind) -> Result<Box<dyn BackendAdapter>, BackendError> {
    match kind {
        BackendKind::Builtin => Err(BackendError::new("builtin_adapter", "Built-in AI uses its existing provider")),
        BackendKind::Codex => Ok(Box::new(codex::CodexAdapter::default())),
        BackendKind::Claude => Ok(Box::new(claude::ClaudeAdapter::default())),
        BackendKind::Dsh => Ok(Box::new(dsh::DshAdapter::default())),
        BackendKind::Pi => Ok(Box::new(pi::PiAdapter::default())),
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
        BackendKind::Pi => pi::list_models(),
    }
}

pub(crate) fn is_recognized(kind: BackendKind, status: Option<&BackendStatus>) -> bool {
    if kind == BackendKind::Builtin {
        return true;
    }
    if matches!(status, Some(BackendStatus::Missing)) {
        return false;
    }
    if status.is_some() {
        return true;
    }
    cli_env::is_installed(kind)
}
