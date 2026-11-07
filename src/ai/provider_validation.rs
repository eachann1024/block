//! Shared request validation; connection tests intentionally do not require AI to be enabled.
use super::{Config, missing_key_msg, missing_model_msg, missing_url_msg};
use crate::theme::tr;

pub(crate) fn validate_request(config: &Config) -> Result<String, (String, bool)> {
    if !config.enabled {
        return Err((tr("AI 助手已关闭，请前往「设置 › AI 助手」开启。", "AI Assistant is disabled. Enable it in Settings › AI Assistant.").into(), true));
    }
    if config.base_url.trim().is_empty() { return Err((missing_url_msg().into(), true)); }
    if config.key_required() && config.api_key.trim().is_empty() { return Err((missing_key_msg().into(), true)); }
    Ok(config.model.trim().to_owned())
}

/// Validates that provider and API key (when required) are configured, resolving a model from the provider list when none is stored.
pub(super) fn resolve_model(config: &Config) -> Result<String, (String, bool)> {
    if config.base_url.trim().is_empty() { return Err((missing_url_msg().into(), true)); }
    if config.key_required() && config.api_key.trim().is_empty() { return Err((missing_key_msg().into(), true)); }
    if !config.model.trim().is_empty() { return Ok(config.model.trim().to_owned()); }
    let first = super::list_models(config).map_err(|e| (e, false))?.into_iter().find(|m| !m.trim().is_empty())
        .ok_or_else(|| (missing_model_msg().into(), false))?;
    let mut stored = super::stored();
    if stored.model.trim().is_empty() {
        stored.model = first.clone();
        if let Some(p) = stored.providers.get_mut(&stored.provider) { p.model = first.clone(); }
        let _ = super::save(&stored);
    }
    Ok(first)
}
