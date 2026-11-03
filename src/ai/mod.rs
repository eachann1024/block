//! Inline AI editing, after thinkly's GooseAIMenu: a prompt card opened from the
//! selection toolbar or the slash menu that streams a rewrite, then shows it for
//! review (应用修改 / 放弃 / 重新生成). Included under `crate::editor` so the
//! session can reach the editor's selection and transactions.
pub(super) mod provider;
pub(super) mod session;
pub(super) mod panel;
