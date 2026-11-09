use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use serde_json::Value;

use super::BackendKind;

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub(crate) struct BackendConfig {
    #[serde(default)]
    pub selected: BackendKind,
    #[serde(default)]
    pub priority: Vec<BackendKind>,
    #[serde(default)]
    pub adapters: BTreeMap<String, Value>,
}

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
        let key = backend.key();
        let mut options = self.adapters.get(key).cloned().unwrap_or_else(|| serde_json::json!({}));
        if !options.is_object() { options = serde_json::json!({}); }
        options["fullPermissions"] = Value::Bool(self.full_permissions(backend));
        options["thinkingLevel"] = Value::String(self.thinking_level(backend).into());
        options
    }

    pub(crate) fn thinking_level(&self, kind: BackendKind) -> &'static str {
        match self.adapters.get(kind.key()).and_then(|v| v.get("thinkingLevel")).and_then(Value::as_str) {
            Some("low") => "low",
            Some("medium") => "medium",
            _ => "high",
        }
    }

    pub(crate) fn set_thinking_level(&mut self, kind: BackendKind, level: &str) -> Result<(), String> {
        let normalized = match level {
            "low" => "low",
            "medium" => "medium",
            _ => "high",
        };
        let previous = self.adapters.clone();
        let entry = self.adapters.entry(kind.key().into()).or_insert_with(|| serde_json::json!({}));
        if !entry.is_object() { *entry = serde_json::json!({}); }
        entry["thinkingLevel"] = Value::String(normalized.into());
        if let Err(error) = self.save() { self.adapters = previous; return Err(error); }
        Ok(())
    }

    pub(crate) fn full_permissions(&self, kind: BackendKind) -> bool {
        self.adapters.get(kind.key()).and_then(|v| v.get("fullPermissions")).and_then(Value::as_bool).unwrap_or(false)
    }

    pub(crate) fn set_full_permissions(&mut self, kind: BackendKind, enabled: bool) -> Result<(), String> {
        let previous = self.adapters.clone();
        let entry = self.adapters.entry(kind.key().into()).or_insert_with(|| serde_json::json!({}));
        if !entry.is_object() { *entry = serde_json::json!({}); }
        entry["fullPermissions"] = Value::Bool(enabled);
        if let Err(error) = self.save() { self.adapters = previous; return Err(error); }
        Ok(())
    }

    pub(crate) fn priority_order(&self, recognized: &[BackendKind]) -> Vec<BackendKind> {
        let mut order = Vec::new();
        for &kind in self.priority.iter().chain(recognized) {
            if recognized.contains(&kind) && !order.contains(&kind) { order.push(kind); }
        }
        order
    }

    pub(crate) fn ordered_enabled(&self, recognized: &[BackendKind]) -> Vec<BackendKind> {
        self.priority_order(recognized).into_iter().filter(|&kind| self.is_enabled(kind)).collect()
    }

    #[allow(dead_code)]
    pub(crate) fn promote_priority(&mut self, kind: BackendKind, recognized: &[BackendKind]) -> Result<(), String> {
        let previous = self.clone();
        self.priority = self.priority_order(recognized);
        self.priority.retain(|&item| item != kind);
        self.priority.insert(0, kind);
        self.selected = kind;
        if let Err(error) = self.save() { *self = previous; return Err(error); }
        Ok(())
    }

    pub(crate) fn selected_model(&self, kind: BackendKind) -> String {
        match kind {
            BackendKind::Builtin => super::super::provider::config().model,
            BackendKind::Codex | BackendKind::Claude | BackendKind::Dsh | BackendKind::Pi | BackendKind::Agy => self
                .adapters
                .get(kind.key())
                .and_then(|v| v.get("model"))
                .and_then(Value::as_str)
                .unwrap_or("")
                .to_owned(),
        }
    }

    pub(crate) fn set_model(&mut self, kind: BackendKind, model: &str) -> Result<(), String> {
        let model = model.trim();
        if kind == BackendKind::Builtin {
            let mut config = super::super::provider::stored();
            config.model = model.into();
            if let Some(profile) = config.providers.get_mut(&config.provider) { profile.model = model.into(); }
            return super::super::provider::save(&config);
        }
        let key = kind.key();
        let previous = self.adapters.clone();
        let entry = self.adapters.entry(key.into()).or_insert_with(|| serde_json::json!({}));
        if !entry.is_object() { *entry = serde_json::json!({}); }
        entry["model"] = model.into();
        if let Err(error) = self.save() { self.adapters = previous; return Err(error); }
        Ok(())
    }

    pub(crate) fn is_enabled(&self, kind: BackendKind) -> bool {
        if let Some(enabled) = self
            .adapters
            .get(kind.key())
            .and_then(|v| v.get("enabled"))
            .and_then(Value::as_bool)
        {
            return enabled;
        }
        kind == BackendKind::Builtin
    }

    pub(crate) fn can_disable(&self, kind: BackendKind, recognized: &[BackendKind]) -> bool {
        if !self.is_enabled(kind) {
            return false;
        }
        let enabled_count = recognized.iter().filter(|&&b| self.is_enabled(b)).count();
        enabled_count > 1
    }

    pub(crate) fn ensure_valid_selection(&mut self, recognized: &[BackendKind]) -> BackendKind {
        if recognized.contains(&self.selected) && self.is_enabled(self.selected) {
            return self.selected;
        }
        if let Some(&first) = self.ordered_enabled(recognized).first().or_else(|| recognized.first()) {
            self.selected = first;
        }
        self.selected
    }

    pub(crate) fn set_enabled(
        &mut self,
        kind: BackendKind,
        enabled: bool,
        recognized: &[BackendKind],
    ) -> Result<(), String> {
        if !enabled && !self.can_disable(kind, recognized) {
            return Err(crate::theme::tr("至少需要保留一个启用的 AI 后端", "At least one AI backend must remain enabled").into());
        }
        let previous = self.adapters.clone();
        let prev_selected = self.selected;
        let entry = self.adapters.entry(kind.key().into()).or_insert_with(|| serde_json::json!({}));
        if !entry.is_object() {
            *entry = serde_json::json!({});
        }
        entry["enabled"] = Value::Bool(enabled);
        self.ensure_valid_selection(recognized);
        if let Err(error) = self.save() {
            self.adapters = previous;
            self.selected = prev_selected;
            return Err(error);
        }
        Ok(())
    }
}
