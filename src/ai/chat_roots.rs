use std::path::{Path, PathBuf};
use gpui::App;
use super::{agent, AiChat};

pub(in crate::editor::ai) fn registered_roots() -> Vec<PathBuf> {
    let Ok(state) = crate::files::load_state() else { return Vec::new() };
    state.notebooks.into_iter().filter_map(|root| root.canonicalize().ok()).filter(|root| root.is_dir()).collect()
}
impl AiChat {
    /// Cross-notebook roots must resolve exactly to a registered notebook.
    pub(super) fn context_for_root(&self, root: &Path, cx: &App) -> Result<agent::Ctx, String> {
        if !root.is_absolute() || root.components().any(|part| matches!(part, std::path::Component::ParentDir | std::path::Component::CurDir)) {
            return Err("Invalid notebook root".into());
        }
        let canonical = root.canonicalize().map_err(|error| error.to_string())?;
        let current = self.agent_ctx(cx);
        if current.root.canonicalize().ok().as_ref() == Some(&canonical) { return Ok(current); }
        if !registered_roots().contains(&canonical) { return Err("Notebook root is not registered".into()); }
        let editor = self.editor.as_ref().and_then(|editor| editor.upgrade());
        let open_path = editor.as_ref().and_then(|editor| editor.read(cx).path.clone());
        let page = open_path.as_ref().and_then(|path| path.strip_prefix(&canonical).ok())
            .and_then(|path| path.with_extension("").to_str().map(str::to_owned));
        let markdown = editor.map(|editor| crate::editor::markdown::serialize(&editor.read(cx).doc)).unwrap_or_default();
        Ok(agent::Ctx { root: canonical, current: page, current_md: markdown })
    }
}
