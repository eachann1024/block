//! Annotation model settings are seeded once from chat, then saved independently.
use gpui::App;
use serde::{Deserialize, Serialize};
use super::{backend::{BackendConfig, BackendKind}, provider};

#[derive(Clone, Serialize, Deserialize)]
pub(super) struct AnnotationConfig {
    pub backend: BackendKind,
    pub provider: String,
    pub model: String,
    pub thinking: String,
}

impl AnnotationConfig {
    fn path() -> std::path::PathBuf { crate::files::app_data_dir("Block").join("ai-annotations.json") }

    pub(super) fn load_or_seed(cx: &App) -> Result<Self, String> {
        match std::fs::read(Self::path()) {
            Ok(bytes) => return serde_json::from_slice(&bytes).map_err(|error| error.to_string()),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {},
            Err(error) => return Err(error.to_string()),
        }
        let mut backends = BackendConfig::load();
        let (backend, model) = super::chat::entity(cx).map(|chat| {
            let chat = chat.read(cx); (chat.selected_backend(), chat.selected_model())
        }).unwrap_or_else(|| {
            let kind = backends.ensure_valid_selection(&super::chat::recognized_backends(cx));
            (kind, backends.selected_model(kind))
        });
        let config = Self { backend, provider: provider::config().provider, model,
            thinking: backends.thinking_level(backend).into() };
        config.save()?;
        Ok(config)
    }

    pub(super) fn save(&self) -> Result<(), String> {
        let path = Self::path();
        if let Some(parent) = path.parent() { std::fs::create_dir_all(parent).map_err(|e| e.to_string())?; }
        std::fs::write(&path, serde_json::to_vec_pretty(self).map_err(|e| e.to_string())?)
            .map_err(|e| e.to_string())
    }

    pub(super) fn builtin_config(&self) -> Result<provider::Config, String> {
        let mut config = provider::config();
        if config.provider != self.provider {
            let profile = config.providers.get(&self.provider).cloned()
                .ok_or_else(|| crate::theme::tr("标注使用的服务商尚未配置", "The annotation provider is not configured").to_owned())?;
            config.provider = self.provider.clone(); config.protocol = profile.protocol;
            config.base_url = profile.base_url; config.api_key = profile.api_key;
        }
        config.model = self.model.clone();
        Ok(config)
    }
}
