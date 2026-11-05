//! Preferences state and actions (⌘,): theme, editor font, and global hotkeys.
use gpui::{AppContext, Context, FocusHandle, Keystroke, Window};
use crate::files::state::{LanguageMode, ThemeMode, update_state};
use crate::icons::Icon;
use crate::theme::{
    DEFAULT_QUICK_SHORTCUT, DEFAULT_SEARCH_SHORTCUT, quick_shortcut,
    search_shortcut, set_language_mode, set_shortcuts, set_theme_mode, tr,
};
use super::Workspace;

#[derive(Clone, Copy, PartialEq, Eq)]
pub(crate) enum ShortcutKind {
    Search,
    Quick,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) enum SettingsTab {
    #[default]
    Ai,
    Appearance,
    General,
    Shortcuts,
}

impl SettingsTab {
    pub(crate) const ALL: [Self; 4] = [Self::Ai, Self::Appearance, Self::General, Self::Shortcuts];

    pub(crate) fn title(self) -> &'static str {
        match self {
            Self::Ai => tr("AI 助手", "AI Assistant"),
            Self::Appearance => tr("外观与字体", "Appearance & Fonts"),
            Self::General => tr("通用与编辑", "General & Editing"),
            Self::Shortcuts => tr("基础快捷键", "Shortcuts"),
        }
    }

    pub(crate) fn icon(self) -> Icon {
        match self {
            Self::Ai => Icon::Sparkles,
            Self::Appearance => Icon::Sliders,
            Self::General => Icon::NoteCheck,
            Self::Shortcuts => Icon::Code,
        }
    }
}

pub(crate) struct FontPickerModal {
    pub role: crate::ui::font_picker::FontPickerRole,
    pub search_input: gpui::Entity<gpui_component::input::InputState>,
    pub scroll_handle: gpui::UniformListScrollHandle,
    pub catalog: std::rc::Rc<Vec<crate::ui::font_picker_cache::CandidateGroup>>,
    pub filtered: std::rc::Rc<[crate::ui::font_picker_cache::FlatFontItem]>,
    pub last_query: String,
    pub _search_sub: gpui::Subscription,
}

pub(crate) struct SettingsModal {
    pub focus: FocusHandle,
    pub recording: Option<ShortcutKind>,
    pub tab: SettingsTab,
    pub assets_input: gpui::Entity<gpui_component::input::InputState>,
    pub _assets_sub: gpui::Subscription,
    pub hidden_folder_input: gpui::Entity<gpui_component::input::InputState>,
    pub _hidden_folder_sub: gpui::Subscription,
    pub font_picker: Option<FontPickerModal>,
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
    /// Toggles the Settings page (opens on the AI tab, closes when already open).
    pub(crate) fn open_settings(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.settings.is_some() {
            self.close_settings(window, cx);
        } else {
            self.open_settings_tab(SettingsTab::Ai, window, cx);
        }
    }

    pub(crate) fn open_settings_tab(&mut self, tab: SettingsTab, window: &mut Window, cx: &mut Context<Self>) {
        if let Some(m) = &mut self.settings {
            m.tab = tab;
            cx.notify();
            return;
        }
        if self.renaming.is_some() { self.cancel_rename(window, cx); }
        if self.search.is_some() { self.close_search(window, cx); }
        self.close_overlays(cx);
        let focus = cx.focus_handle();
        window.focus(&focus, cx);
        let assets_input = cx.new(|cx| {
            gpui_component::input::InputState::new(window, cx)
                .placeholder(crate::theme::DEFAULT_ASSETS_FOLDER)
                .default_value(crate::theme::assets_folder())
        });
        let _assets_sub = cx.subscribe_in(&assets_input, window, |this, input, event: &gpui_component::input::InputEvent, window, cx| {
            if !matches!(event, gpui_component::input::InputEvent::Change | gpui_component::input::InputEvent::Blur) { return; }
            let val = input.read(cx).value().to_string();
            let sanitized = crate::theme::sanitize_assets_folder(&val);
            if matches!(event, gpui_component::input::InputEvent::Blur) && val != sanitized {
                input.update(cx, |i, cx| i.set_value(sanitized.clone(), window, cx));
            }
            if sanitized != crate::theme::assets_folder() {
                crate::theme::set_assets_folder(sanitized.clone());
                cx.background_executor().spawn(async move {
                    let _ = update_state(|s| {
                        s.assets_folder = (sanitized != crate::theme::DEFAULT_ASSETS_FOLDER).then_some(sanitized);
                    });
                }).detach();
                this.rescan_current_folder(cx);
            }
            cx.notify();
        });
        let hidden_folder_input = cx.new(|cx| {
            gpui_component::input::InputState::new(window, cx)
                .placeholder(crate::theme::hidden_folder_placeholder())
                .default_value(crate::theme::hidden_folder_regex())
        });
        let _hidden_folder_sub = cx.subscribe_in(&hidden_folder_input, window, |this, input, event: &gpui_component::input::InputEvent, _, cx| {
            if matches!(event, gpui_component::input::InputEvent::Change) {
                let val = input.read(cx).value().to_string();
                if val != crate::theme::hidden_folder_regex() {
                    this.apply_hidden_folder_regex(val, cx);
                }
            }
        });
        self.settings = Some(SettingsModal { focus, recording: None, tab, assets_input, _assets_sub, hidden_folder_input, _hidden_folder_sub, font_picker: None });
        crate::editor::ai::settings::ensure(window, cx);
        cx.notify();
    }

    pub(super) fn select_settings_tab(&mut self, tab: SettingsTab, cx: &mut Context<Self>) {
        if let Some(m) = &mut self.settings {
            m.tab = tab;
            m.recording = None;
            m.font_picker = None;
            cx.notify();
        }
    }

    pub(super) fn close_settings(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.settings.take().is_none() { return; }
        self.restore_focus(window, cx);
        cx.notify();
    }

    pub(super) fn select_theme(&mut self, mode: ThemeMode, cx: &mut Context<Self>) {
        set_theme_mode(mode);
        crate::theme::sync_gpui_theme(cx);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| s.theme = mode);
        }).detach();
        self.editor.update(cx, |editor, cx| { editor.reset_layouts(); cx.notify(); });
        self.tree.update(cx, |_, cx| cx.notify());
        crate::quick::refresh_style(cx);
        cx.notify();
    }

    pub(crate) fn select_language(&mut self, mode: LanguageMode, window: &mut Window, cx: &mut Context<Self>) {
        set_language_mode(mode);
        if let Some(modal) = &self.settings {
            modal.hidden_folder_input.update(cx, |i, cx| {
                i.set_placeholder(crate::theme::hidden_folder_placeholder(), window, cx);
            });
        }
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| s.language = mode);
        }).detach();
        crate::apply_app_menus(cx);
        self.editor.update(cx, |editor, cx| { editor.reset_layouts(); cx.notify(); });
        crate::quick::refresh_style(cx);
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

    pub(super) fn set_hide_assets(&mut self, hide: bool, cx: &mut Context<Self>) {
        crate::theme::set_hide_assets_folder(hide);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                s.hide_assets_folder = (!hide).then_some(false);
            });
        }).detach();
        self.rescan_current_folder(cx);
        cx.notify();
    }

    pub(super) fn reset_assets_settings(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        crate::theme::set_assets_folder(crate::theme::DEFAULT_ASSETS_FOLDER.into());
        crate::theme::set_hide_assets_folder(true);
        if let Some(modal) = &self.settings {
            modal.assets_input.update(cx, |input, cx| {
                input.set_value(crate::theme::DEFAULT_ASSETS_FOLDER, window, cx);
            });
        }
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                s.assets_folder = None;
                s.hide_assets_folder = None;
            });
        }).detach();
        self.rescan_current_folder(cx);
        cx.notify();
    }

    pub(super) fn apply_hidden_folder_regex(&mut self, val: String, cx: &mut Context<Self>) {
        crate::theme::set_hidden_folder_regex(val);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                let cur = crate::theme::hidden_folder_regex();
                s.hidden_folder_regex = (!cur.trim().is_empty()).then_some(cur);
            });
        }).detach();
        self.rescan_current_folder(cx);
        cx.notify();
    }

    pub(super) fn reset_hidden_folder_regex(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        crate::theme::set_hidden_folder_regex(String::new());
        if let Some(modal) = &self.settings {
            modal.hidden_folder_input.update(cx, |input, cx| input.set_value(String::new(), window, cx));
        }
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| {
                let cur = crate::theme::hidden_folder_regex();
                s.hidden_folder_regex = (!cur.trim().is_empty()).then_some(cur);
            });
        }).detach();
        self.rescan_current_folder(cx);
        cx.notify();
    }

    pub(super) fn rescan_current_folder(&mut self, cx: &mut Context<Self>) {
        if self.busy() { return; }
        let Some(root) = self.folder.clone() else { return };
        cx.spawn(async move |this, cx| {
            let result = cx.background_executor().spawn(async move {
                let scan_res = crate::files::scan::scan(&root)?;
                let index = crate::files::index::TreeIndex::new(scan_res.nodes);
                Ok::<_, std::io::Error>(index)
            }).await;
            let _ = this.update(cx, |this, cx| {
                if let Ok(index) = result {
                    this.tree.update(cx, |tree, cx| tree.replace_index(index, None, None, cx));
                    this.refresh(cx);
                }
            });
        }).detach();
    }
}
