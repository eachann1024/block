//! Inline AI editing, after thinkly's GooseAIMenu: a prompt card opened from the
//! selection toolbar or the slash menu that streams a rewrite, then shows it for
//! review (应用修改 / 放弃 / 重新生成). Included under `crate::editor` so the
//! session can reach the editor's selection and transactions.
pub(crate) mod presets;
#[cfg(not(target_family = "wasm"))]
pub(crate) mod backend;
mod anthropic;
pub(crate) mod provider;
pub(super) mod session;
#[cfg(not(target_family = "wasm"))]
pub(in crate::editor) mod inline_backend;
#[cfg(not(target_family = "wasm"))]
mod annotation_config;
pub(super) mod panel;
pub(super) mod preview;
pub(crate) mod settings;
mod settings_view;
#[cfg(not(target_family = "wasm"))]
mod chat_ui;
#[cfg(not(target_family = "wasm"))]
mod chat_process;
#[cfg(not(target_family = "wasm"))]
pub(crate) mod plan_link;
#[cfg(not(target_family = "wasm"))]
mod chat_deck;
#[cfg(not(target_family = "wasm"))]
mod chat_empty;
#[cfg(not(target_family = "wasm"))]
mod backend_ui;
#[cfg(not(target_family = "wasm"))]
mod chat_composer;
#[cfg(not(target_family = "wasm"))]
mod chat_reference_picker;
#[cfg(not(target_family = "wasm"))]
pub(crate) mod chat;
#[cfg(target_family = "wasm")]
pub(crate) mod chat {
    pub fn toggle(_: gpui::WeakEntity<crate::editor::Editor>, _: &mut gpui::Window, _: &mut gpui::App) {}
    pub fn hide(_: &mut gpui::App) {}
    pub fn panel(_: &gpui::App) -> Option<gpui::Empty> { None }
    pub fn resize_capture(_: &gpui::App) -> Option<gpui::AnyElement> { None }
    pub fn is_open(_: &gpui::App) -> bool { false }
    pub fn is_fullscreen(_: &gpui::App) -> bool { false }
    pub fn saved_panel_width(_: &gpui::App) -> f32 { 0. }
    pub fn recognized_backends(_: &gpui::App) -> Vec<super::backend::BackendKind> {
        vec![super::backend::BackendKind::Builtin]
    }
    pub fn backend_status(_: super::backend::BackendKind, _: &gpui::App) -> Option<super::backend::BackendStatus> {
        None
    }
    pub fn sync_settings(_: &mut gpui::App) {}
}
#[cfg(target_family = "wasm")]
pub(crate) mod backend {
    use serde::{Deserialize, Serialize};

    #[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
    #[serde(rename_all = "lowercase")]
    pub(crate) enum BackendKind {
        #[default]
        Builtin,
        Codex,
        Claude,
        Dsh,
        Pi,
        Agy,
    }

    impl BackendKind {
        pub(crate) const ALL: [Self; 6] = [Self::Builtin, Self::Codex, Self::Claude, Self::Dsh, Self::Pi, Self::Agy];
        pub(crate) fn key(self) -> &'static str {
            match self {
                Self::Builtin => "builtin",
                Self::Codex => "codex",
                Self::Claude => "claude",
                Self::Dsh => "dsh",
                Self::Pi => "pi",
                Self::Agy => "agy",
            }
        }
    }

    #[derive(Clone, Debug, PartialEq, Eq)]
    pub(crate) enum BackendStatus {
        Ready { version: String },
        Installed { version: String },
        NotAuthenticated { message: String },
        Unsupported { message: String },
        Error { message: String },
    }

    #[derive(Clone, Debug, PartialEq, Eq)]
    pub(crate) struct ModelOption {
        pub value: String,
        pub label: String,
    }

    #[derive(Clone, Default)]
    pub(crate) struct BackendConfig {
        pub selected: BackendKind,
    }

    impl BackendConfig {
        pub(crate) fn load() -> Self { Self::default() }
        pub(crate) fn is_enabled(&self, kind: BackendKind) -> bool { kind == BackendKind::Builtin }
        pub(crate) fn set_enabled(&mut self, _: BackendKind, _: bool, _: &[BackendKind]) -> Result<(), String> { Ok(()) }
        pub(crate) fn promote_priority(&mut self, _: BackendKind, _: &[BackendKind]) -> Result<(), String> { Ok(()) }
        pub(crate) fn ordered_enabled(&self, recognized: &[BackendKind]) -> Vec<BackendKind> { recognized.to_vec() }
        pub(crate) fn full_permissions(&self, _: BackendKind) -> bool { false }
        pub(crate) fn set_full_permissions(&mut self, _: BackendKind, _: bool) -> Result<(), String> { Ok(()) }
        pub(crate) fn thinking_level(&self, _: BackendKind) -> &'static str { "high" }
        pub(crate) fn set_thinking_level(&mut self, _: BackendKind, _: &str) -> Result<(), String> { Ok(()) }
    }

    pub(crate) mod cli_env {
        pub(crate) fn resolve(_: &str) -> Option<std::path::PathBuf> { None }
    }

    pub(crate) mod model_cache {
        use super::{BackendKind, ModelOption};

        #[derive(Clone, Debug, Default, PartialEq, Eq)]
        pub(crate) enum ModelStatus { #[default] Idle, Loading, Ready, Error(String) }

        #[derive(Default)]
        pub(crate) struct ModelCache {
            choices: Vec<ModelOption>,
            status: ModelStatus,
        }

        impl ModelCache {
            pub(crate) fn choices(&self, _: BackendKind) -> &[ModelOption] { &self.choices }
            pub(crate) fn status(&self, _: BackendKind) -> ModelStatus { self.status.clone() }
            pub(crate) fn begin_refresh(&mut self, _: BackendKind) -> Option<u64> { None }
            pub(crate) fn finish(&mut self, _: BackendKind, _: u64, _: Result<Vec<ModelOption>, String>) -> bool { false }
            pub(crate) fn invalidate(&mut self, _: BackendKind) {}
        }
    }

    pub(crate) fn list_models(_: BackendKind) -> Result<Vec<ModelOption>, String> { Ok(Vec::new()) }
}
#[cfg(target_family = "wasm")]
mod backend_ui {
    use super::backend::BackendKind;
    pub(super) fn backend_name(backend: BackendKind) -> &'static str {
        match backend {
            BackendKind::Builtin => crate::theme::tr("内置 AI", "Built-in AI"),
            BackendKind::Codex => "Codex",
            BackendKind::Claude => "Claude",
            BackendKind::Dsh => "dsh",
            BackendKind::Pi => "Pi",
            BackendKind::Agy => "agy",
        }
    }
    pub(super) fn backend_logo(backend: BackendKind) -> &'static str {
        match backend {
            BackendKind::Builtin => "agents/builtin.svg",
            BackendKind::Codex => "agents/codex.svg",
            BackendKind::Claude => "agents/claude.svg",
            BackendKind::Dsh => "agents/dsh.svg",
            BackendKind::Pi => "agents/pi.svg",
            BackendKind::Agy => "agents/agy.svg",
        }
    }
    pub(super) fn thinking_label(level: &str) -> &'static str {
        match level {
            "low" => "Low",
            "medium" => "Medium",
            _ => "High",
        }
    }
}
#[cfg(target_family = "wasm")]
mod annotation_config {
    use super::{backend::BackendKind, provider};

    #[derive(Clone)]
    pub(super) struct AnnotationConfig {
        pub backend: BackendKind,
        pub provider: String,
        pub model: String,
        pub thinking: String,
    }

    impl AnnotationConfig {
        pub(super) fn load_or_seed(_: &gpui::App) -> Result<Self, String> {
            let c = provider::config();
            Ok(Self {
                backend: BackendKind::Builtin,
                provider: c.provider,
                model: c.model,
                thinking: "high".into(),
            })
        }
        pub(super) fn save(&self) -> Result<(), String> { Ok(()) }
        pub(super) fn builtin_config(&self) -> Result<provider::Config, String> {
            Ok(provider::config())
        }
    }
}
#[cfg(not(target_family = "wasm"))]
pub(super) mod agent;
#[cfg(not(target_family = "wasm"))]
pub(crate) mod md_view;

gpui::actions!(ai, [OpenAiSettings, ToggleAiPanel, CloseAiPanel]);

pub fn bind_keys(cx: &mut gpui::App) {
    crate::ui::app_shortcuts::bind(cx, [
        gpui::KeyBinding::new("cmd-j", ToggleAiPanel, None),
        #[cfg(not(target_os = "macos"))]
        gpui::KeyBinding::new("ctrl-j", ToggleAiPanel, None),
        gpui::KeyBinding::new("escape", CloseAiPanel, Some("AiChat")),
    ]);
}
