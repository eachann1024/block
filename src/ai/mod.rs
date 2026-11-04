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
pub(crate) mod settings;
mod chat_ui;
pub(crate) mod chat;
pub(super) mod agent;
pub(crate) mod md_view;

gpui::actions!(ai, [OpenAiSettings, ToggleAiPanel]);

pub fn bind_keys(cx: &mut gpui::App) {
    cx.bind_keys([
        gpui::KeyBinding::new("cmd-j", ToggleAiPanel, None),
    ]);
}
