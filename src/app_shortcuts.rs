use gpui::{App, Context, InputPreference, Keystroke, WeakEntity, Window};
use super::Workspace;

impl Workspace {
    pub(super) fn install_shortcut_router(window: &mut Window, cx: &mut Context<Self>) {
        let workspace = cx.entity().downgrade();
        let owner = window.window_handle();
        #[cfg(target_os = "macos")]
        {
            let workspace = workspace.clone();
            let mut async_cx = cx.to_async();
            let registered = gpui_macos::on_pre_ime_key(window, move |keystroke| {
                owner.update(&mut async_cx, |_, window, cx| {
                    // GPUI exposes no public dirty-window query. Refresh contexts only
                    // for registered application chords, leaving local editing keys alone.
                    if crate::ui::app_shortcuts::has_binding(keystroke, cx) {
                        window.draw(cx).clear(cx);
                    }
                    Self::route_application_shortcut(&workspace, keystroke, InputPreference::KeyBindings, window, cx)
                }).unwrap_or(false)
            });
            if let Err(error) = registered {
                crate::log::error(&format!("Could not register pre-IME application shortcuts: {error}"));
            }
        }
        cx.intercept_keystrokes(move |event, window, cx| {
            if window.window_handle() != owner { return; }
            if Self::route_application_shortcut(&workspace, &event.keystroke, event.input_preference, window, cx) {
                cx.stop_propagation();
            }
        }).detach();
    }

    fn route_application_shortcut(workspace: &WeakEntity<Self>, keystroke: &Keystroke, input_preference: InputPreference, window: &mut Window, cx: &mut App) -> bool {
        let Some(workspace) = workspace.upgrade() else { return false; };
        if workspace.read(cx).settings.as_ref().is_some_and(|settings| settings.recording.is_some()) {
            // The recorder sees the chord before any system or application binding opens a panel.
            return workspace.update(cx, |this, cx| this.capture_recorded_shortcut(keystroke, cx));
        }
        crate::ui::app_shortcuts::capture(keystroke, input_preference, window, cx)
    }
}
