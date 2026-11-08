use gpui::{Context, Window};
use super::Workspace;

impl Workspace {
    pub(super) fn install_shortcut_router(window: &mut Window, cx: &mut Context<Self>) {
        let workspace = cx.entity().downgrade();
        let owner = window.window_handle();
        cx.intercept_keystrokes(move |event, window, cx| {
            if window.window_handle() != owner { return; }
            let Some(workspace) = workspace.upgrade() else { return; };
            if workspace.read(cx).settings.as_ref().is_some_and(|settings| settings.recording.is_some()) {
                // System and in-app bindings for this chord would open a panel before the recorder saw it.
                let consumed = workspace.update(cx, |this, cx| this.capture_recorded_shortcut(&event.keystroke, cx));
                if consumed { cx.stop_propagation(); }
                return;
            }
            crate::ui::app_shortcuts::capture(event, window, cx);
        }).detach();
    }
}
