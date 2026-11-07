use gpui::{Context, Window};
use super::Workspace;

impl Workspace {
    pub(super) fn install_shortcut_router(window: &mut Window, cx: &mut Context<Self>) {
        let workspace = cx.entity().downgrade();
        let owner = window.window_handle();
        cx.intercept_keystrokes(move |event, window, cx| {
            if window.window_handle() != owner { return; }
            let Some(workspace) = workspace.upgrade() else { return; };
            if workspace.read(cx).settings.as_ref().is_some_and(|settings| settings.recording.is_some()) { return; }
            crate::ui::app_shortcuts::capture(event, window, cx);
        }).detach();
    }
}
