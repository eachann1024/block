//! Inline AI editing, after thinkly's GooseAIMenu: a prompt card opened from the
//! selection toolbar or the slash menu that streams a rewrite, then shows it for
//! review (应用修改 / 放弃 / 重新生成). Included under `crate::editor` so the
//! session can reach the editor's selection and transactions.
pub(crate) mod presets;
mod anthropic;
pub(crate) mod provider;
pub(super) mod session;
pub(super) mod panel;
pub(super) mod preview;
#[cfg(not(target_family = "wasm"))]
pub(crate) mod settings;
#[cfg(not(target_family = "wasm"))]
mod chat_ui;
#[cfg(not(target_family = "wasm"))]
pub(crate) mod chat;
#[cfg(not(target_family = "wasm"))]
pub(super) mod agent;
#[cfg(not(target_family = "wasm"))]
pub(crate) mod md_view;

gpui::actions!(ai, [OpenAiSettings, ToggleAiPanel]);

pub fn bind_keys(cx: &mut gpui::App) {
    cx.bind_keys([
        gpui::KeyBinding::new("cmd-j", ToggleAiPanel, None),
        #[cfg(not(target_os = "macos"))]
        gpui::KeyBinding::new("ctrl-j", ToggleAiPanel, None),
    ]);
}
