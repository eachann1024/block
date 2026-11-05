use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use serde_json::Value;

use super::BackendKind;

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub(crate) struct BackendConfig {
    #[serde(default)]
    pub selected: BackendKind,
    #[serde(default)]
    pub adapters: BTreeMap<String, Value>,
}

#[derive(Clone, Debug)]
pub(crate) struct DshOptions { pub provider: String, pub model: String, pub auth_source: String }

impl BackendConfig {
    fn path() -> std::path::PathBuf { crate::files::app_data_dir("Block").join("ai-backends.json") }

    pub(crate) fn load() -> Self {
        std::fs::read(Self::path()).ok().and_then(|bytes| serde_json::from_slice(&bytes).ok()).unwrap_or_default()
    }

    pub(crate) fn save(&self) -> Result<(), String> {
        let path = Self::path();
        if let Some(parent) = path.parent() { std::fs::create_dir_all(parent).map_err(|error| error.to_string())?; }
        let data = serde_json::to_vec_pretty(self).map_err(|error| error.to_string())?;
        std::fs::write(path, data).map_err(|error| error.to_string())
    }

    pub(crate) fn options(&self, backend: BackendKind) -> Value {
        let key = match backend {
            BackendKind::Builtin => "builtin", BackendKind::Codex => "codex",
            BackendKind::Claude => "claude", BackendKind::Dsh => "dsh",
        };
        let mut options = self.adapters.get(key).cloned().unwrap_or_else(|| serde_json::json!({}));
        if backend == BackendKind::Dsh {
            let resolved = self.dsh_options();
            if !options.is_object() { options = serde_json::json!({}); }
            options["provider"] = resolved.provider.into();
            options["model"] = resolved.model.into();
            options["authSource"] = resolved.auth_source.into();
        }
        options
    }

    pub(crate) fn selected_model(&self, kind: BackendKind) -> String {
        match kind {
            BackendKind::Builtin => super::super::provider::config().model,
            BackendKind::Dsh => self.dsh_options().model,
            BackendKind::Codex | BackendKind::Claude => {
                let key = if kind == BackendKind::Codex { "codex" } else { "claude" };
                self.adapters.get(key).and_then(|v| v.get("model")).and_then(Value::as_str).unwrap_or("").to_owned()
            }
        }
    }

    pub(crate) fn set_model(&mut self, kind: BackendKind, model: &str) -> Result<(), String> {
        let model = model.trim();
        if kind == BackendKind::Builtin {
            if model.is_empty() { return Err("Select a model in AI settings".into()); }
            let mut config = super::super::provider::stored();
            config.model = model.into();
            if let Some(profile) = config.providers.get_mut(&config.provider) { profile.model = model.into(); }
            return super::super::provider::save(&config);
        }
        if kind == BackendKind::Dsh {
            let mut options = self.dsh_options(); options.model = model.into(); return self.set_dsh_options(options);
        }
        let key = if kind == BackendKind::Codex { "codex" } else { "claude" };
        let previous = self.adapters.clone();
        let entry = self.adapters.entry(key.into()).or_insert_with(|| serde_json::json!({}));
        if !entry.is_object() { *entry = serde_json::json!({}); }
        entry["model"] = model.into();
        if let Err(error) = self.save() { self.adapters = previous; return Err(error); }
        Ok(())
    }

    pub(crate) fn dsh_options(&self) -> DshOptions {
        let saved = self.adapters.get("dsh");
        let text = |key| saved.and_then(|value| value.get(key)).and_then(Value::as_str).unwrap_or("").trim().to_owned();
        let model = text("model");
        let fallback = if model.is_empty() {
            let config = super::super::provider::config();
            if config.provider == "deepseek" { config.model } else { String::new() }
        } else { model };
        // Default from the installed dsh version's official cordis.patch.yml; access depends on the account.
        let fallback = if fallback.trim().is_empty() { "deepseek-flash".into() } else { fallback };
        DshOptions { provider: "deepseek-official".into(), model: fallback,
            auth_source: if text("authSource") == "block" { "block".into() } else { "native".into() } }
    }

    pub(crate) fn set_dsh_options(&mut self, options: DshOptions) -> Result<(), String> {
        if options.provider != "deepseek-official" || !matches!(options.auth_source.as_str(), "native" | "block") {
            return Err("Unsupported dsh provider or authentication source".into());
        }
        if options.model.trim().is_empty() { return Err("Select a dsh model".into()); }
        let previous = self.adapters.insert("dsh".into(), serde_json::json!({
            "provider": options.provider, "model": options.model.trim(), "authSource": options.auth_source,
        }));
        if let Err(error) = self.save() {
            if let Some(previous) = previous { self.adapters.insert("dsh".into(), previous); }
            else { self.adapters.remove("dsh"); }
            return Err(error);
        }
        Ok(())
    }
}
