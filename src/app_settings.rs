//! Preferences state and actions (⌘,): theme, editor font, font smoothing, and global hotkeys.
use gpui::{Context, FocusHandle, Keystroke, Window};
use crate::files::state::{FontChoice, LanguageMode, ThemeMode, update_state};
use crate::theme::{
    DEFAULT_QUICK_SHORTCUT, DEFAULT_SEARCH_SHORTCUT, font_smoothing, quick_shortcut,
    search_shortcut, set_font_choice, set_font_smoothing, set_language_mode, set_shortcuts, set_theme_mode,
};
use super::Workspace;

#[derive(Clone, Copy, PartialEq, Eq)]
pub(crate) enum ShortcutKind {
    Search,
    Quick,
}

pub(crate) struct SettingsModal {
    pub focus: FocusHandle,
    pub recording: Option<ShortcutKind>,
}

pub(super) fn keystroke_to_spec(ks: &Keystroke) -> Option<String> {
    let m = &ks.modifiers;
    if !m.control && !m.alt && !m.shift && !m.platform {
        return None;
    }
    let key = ks.key.to_ascii_lowercase();
    if matches!(key.as_str(), "control" | "alt" | "shift" | "platform" | "Command" | "meta" | "capslock" | "fn") {
        return None;
    }
    let mut parts: Vec<&str> = Vec::new();
    if m.control { parts.push("ctrl"); }
    if m.alt { parts.push("alt"); }
    if m.shift { parts.push("shift"); }
    if m.platform { parts.push("cmd"); }
    parts.push(&key);
    let spec = parts.join("-");
    crate::quick::parse_hotkey(&spec).map(|_| spec)
}

impl Workspace {
    pub(crate) fn open_settings(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.settings.is_some() {
            self.close_settings(window, cx);
            return;
        }
        if self.renaming.is_some() { self.cancel_rename(window, cx); }
        if self.search.is_some() { self.close_search(window, cx); }
        self.close_overlays(cx);
        let focus = cx.focus_handle();
        window.focus(&focus);
        self.settings = Some(SettingsModal { focus, recording: None });
        crate::editor::ai::settings::ensure(window, cx);
        cx.notify();
    }

    pub(super) fn close_settings(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.settings.take().is_none() { return; }
        self.restore_focus(window, cx);
        cx.notify();
    }

    pub(super) fn select_theme(&mut self, mode: ThemeMode, cx: &mut Context<Self>) {
        set_theme_mode(mode);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| s.theme = mode);
        }).detach();
        crate::quick::refresh_style(cx);
        cx.notify();
    }

    pub(crate) fn select_language(&mut self, mode: LanguageMode, cx: &mut Context<Self>) {
        set_language_mode(mode);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| s.language = mode);
        }).detach();
        crate::apply_app_menus(cx);
        self.editor.update(cx, |editor, cx| { editor.reset_layouts(); cx.notify(); });
        crate::quick::refresh_style(cx);
        cx.notify();
    }

    pub(super) fn select_font(&mut self, font: FontChoice, cx: &mut Context<Self>) {
        set_font_choice(font);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| s.font_family = font);
        }).detach();
        self.editor.update(cx, |editor, cx| { editor.reset_layouts(); cx.notify(); });
        crate::quick::refresh_style(cx);
        cx.notify();
    }

    pub(super) fn toggle_smoothing(&mut self, cx: &mut Context<Self>) {
        let next = !font_smoothing();
        set_font_smoothing(next);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| s.font_smoothing = Some(next));
        }).detach();
        cx.notify();
    }

    pub(super) fn apply_shortcut(&mut self, kind: ShortcutKind, spec: String, cx: &mut Context<Self>) {
        let (mut search, mut quick) = (search_shortcut(), quick_shortcut());
        match kind {
            ShortcutKind::Search => search = spec,
            ShortcutKind::Quick => quick = spec,
        }
        set_shortcuts(search.clone(), quick.clone());
        crate::quick::rebind_hotkeys(cx);
        crate::quick::refresh_style(cx);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                s.shortcut_search = (search != DEFAULT_SEARCH_SHORTCUT).then_some(search);
                s.shortcut_quick = (quick != DEFAULT_QUICK_SHORTCUT).then_some(quick);
            });
        }).detach();
        if let Some(modal) = &mut self.settings { modal.recording = None; }
        cx.notify();
    }

    pub(super) fn reset_shortcuts(&mut self, cx: &mut Context<Self>) {
        set_shortcuts(DEFAULT_SEARCH_SHORTCUT.into(), DEFAULT_QUICK_SHORTCUT.into());
        crate::quick::rebind_hotkeys(cx);
        crate::quick::refresh_style(cx);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                s.shortcut_search = None;
                s.shortcut_quick = None;
            });
        }).detach();
        if let Some(modal) = &mut self.settings { modal.recording = None; }
        cx.notify();
    }
}
