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
mod dictation;
#[cfg(not(target_family = "wasm"))]
mod chat_reference_picker;
#[cfg(not(target_family = "wasm"))]
pub(crate) mod chat;
#[cfg(target_family = "wasm")]
pub(crate) mod chat {
    pub fn toggle(_: gpui::WeakEntity<crate::editor::Editor>, _: &mut gpui::Window, _: &mut gpui::App) {}
    pub fn panel(_: &gpui::App) -> Option<gpui::Empty> { None }
    pub fn is_open(_: &gpui::App) -> bool { false }
    pub fn is_fullscreen(_: &gpui::App) -> bool { false }
}
#[cfg(not(target_family = "wasm"))]
pub(super) mod agent;
#[cfg(not(target_family = "wasm"))]
pub(crate) mod md_view;

gpui::actions!(ai, [OpenAiSettings, ToggleAiPanel]);

pub fn bind_keys(cx: &mut gpui::App) {
    crate::ui::app_shortcuts::bind(cx, [
        gpui::KeyBinding::new("cmd-j", ToggleAiPanel, None),
        #[cfg(not(target_os = "macos"))]
        gpui::KeyBinding::new("ctrl-j", ToggleAiPanel, None),
    ]);
}
