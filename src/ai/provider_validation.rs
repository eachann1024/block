//! Shared request validation; connection tests intentionally do not require AI to be enabled.
use super::{Config, missing_key_msg, missing_model_msg, missing_url_msg};
use crate::theme::tr;

pub(crate) fn validate_request(config: &Config) -> Result<String, (String, bool)> {
    if !config.enabled {
        return Err((tr("AI 助手已关闭，请前往「设置 › AI 助手」开启。", "AI Assistant is disabled. Enable it in Settings › AI Assistant.").into(), true));
    }
    resolve_model(config)
}

/// Validates that provider, API key (when required), and model are explicitly configured.
pub(super) fn resolve_model(config: &Config) -> Result<String, (String, bool)> {
    if config.base_url.trim().is_empty() { return Err((missing_url_msg().into(), true)); }
    if config.key_required() && config.api_key.trim().is_empty() { return Err((missing_key_msg().into(), true)); }
    if config.model.trim().is_empty() { return Err((missing_model_msg().into(), true)); }
    Ok(config.model.trim().to_owned())
}
