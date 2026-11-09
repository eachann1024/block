//! Preferences state and actions (⌘,): theme, editor font, folders, and global hotkeys.
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
    Folders,
    Shortcuts,
}

impl SettingsTab {
    pub(crate) const ALL: [Self; 4] = [Self::Ai, Self::Appearance, Self::Folders, Self::Shortcuts];
    #[doc(hidden)]
    #[allow(dead_code, non_upper_case_globals)]
    pub(crate) const General: Self = Self::Folders;

    pub(crate) fn title(self) -> &'static str {
        match self {
            Self::Ai => tr("AI 助手", "AI Assistant"),
            Self::Appearance => tr("外观与字体", "Appearance & Fonts"),
            Self::Folders => tr("文件夹", "Folders"),
            Self::Shortcuts => tr("快捷键", "Shortcuts"),
        }
    }

    pub(crate) fn icon(self) -> Icon {
        match self {
            Self::Ai => Icon::Sparkles,
            Self::Appearance => Icon::Sliders,
            Self::Folders => Icon::Folder,
            Self::Shortcuts => Icon::Code,
        }
    }
}

pub(crate) struct FontPickerModal {
    pub role: crate::ui::font_picker::FontPickerRole,
    pub search_input: gpui::Entity<gpui_component::input::InputState>,
    pub scroll_handle: gpui::UniformListScrollHandle,
    pub catalog: std::rc::Rc<[crate::ui::font_picker_cache::CandidateItem]>,
    pub filtered: std::rc::Rc<[crate::ui::font_picker_cache::FlatFontItem]>,
    pub cursor: usize,
    pub last_query: String,
    pub _search_sub: gpui::Subscription,
}

pub(crate) struct SettingsModal {
    pub focus: FocusHandle,
    pub recording: Option<ShortcutKind>,
    pub tab: SettingsTab,
    pub assets_input: gpui::Entity<gpui_component::input::InputState>,
    pub _assets_sub: gpui::Subscription,
    pub folder_draft_input: gpui::Entity<gpui_component::input::InputState>,
    pub _folder_draft_sub: gpui::Subscription,
    pub regex_draft_input: gpui::Entity<gpui_component::input::InputState>,
    pub _regex_draft_sub: gpui::Subscription,
    pub advanced_open: bool,
    pub folder_error: Option<String>,
    pub regex_error: Option<String>,
    pub font_picker: Option<FontPickerModal>,
    pub font_picker_leaving: bool,
    pub sidebar_intro_pending: bool,
    pub sidebar_intro_leaving: bool,
    pub sidebar_intro_task: Option<gpui::Task<()>>,
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
        if self.history_restore_busy { return; }
        crate::editor::ai::chat::hide(cx);
        if self.is_history_open() || self.history_opening { self.close_page_history_focused(window, cx); }
        if let Some(m) = &mut self.settings {
            m.tab = tab;
            cx.notify();
            return;
        }
        self.present_sidebar();
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
                this.set_assets_folder(sanitized, cx);
            }
            cx.notify();
        });

        let folder_draft_input = cx.new(|cx| {
            gpui_component::input::InputState::new(window, cx)
                .placeholder(super::settings_folders::folder_draft_placeholder())
                .default_value("")
        });
        let _folder_draft_sub = cx.subscribe_in(&folder_draft_input, window, |this, _, event: &gpui_component::input::InputEvent, window, cx| {
            match event {
                gpui_component::input::InputEvent::PressEnter { .. } => {
                    this.add_exact_folder_rule(window, cx);
                }
                gpui_component::input::InputEvent::Change => {
                    if let Some(m) = &mut this.settings {
                        if m.folder_error.take().is_some() { cx.notify(); }
                    }
                }
                _ => {}
            }
        });

        let regex_draft_input = cx.new(|cx| {
            gpui_component::input::InputState::new(window, cx)
                .placeholder(super::settings_folders::regex_draft_placeholder())
                .default_value("")
        });
        let _regex_draft_sub = cx.subscribe_in(&regex_draft_input, window, |this, input, event: &gpui_component::input::InputEvent, window, cx| {
            match event {
                gpui_component::input::InputEvent::PressEnter { .. } => {
                    this.add_regex_folder_rule(window, cx);
                }
                gpui_component::input::InputEvent::Change => {
                    this.handle_regex_draft_change(input, window, cx);
                }
                _ => {}
            }
        });

        self.settings = Some(SettingsModal {
            focus, recording: None, tab, assets_input, _assets_sub,
            folder_draft_input, _folder_draft_sub, regex_draft_input, _regex_draft_sub,
            sidebar_intro_pending: true, sidebar_intro_leaving: false, sidebar_intro_task: None,
            advanced_open: false, folder_error: None, regex_error: None, font_picker: None, font_picker_leaving: false,
        });
        crate::editor::ai::settings::ensure(window, cx);
        cx.notify();
    }

    pub(super) fn select_settings_tab(&mut self, tab: SettingsTab, cx: &mut Context<Self>) {
        self.cancel_settings_sidebar_intro();
        let ended = self.end_shortcut_recording(cx);
        if let Some(m) = &mut self.settings {
            if m.tab == tab && !ended && m.font_picker.is_none() { return; }
            m.tab = tab;
            m.font_picker = None;
            m.font_picker_leaving = false;
            crate::ui::motion::popup_reset("font-picker-body");
            crate::ui::motion::popup_reset("font-picker-code");
            cx.notify();
        }
    }

    pub(super) fn close_settings(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.settings.is_none() { return; }
        let _ = self.end_shortcut_recording(cx);
        if self.settings.take().is_none() { return; }
        crate::ui::motion::popup_reset("font-picker-body");
        crate::ui::motion::popup_reset("font-picker-code");
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
            modal.folder_draft_input.update(cx, |i, cx| i.set_placeholder(super::settings_folders::folder_draft_placeholder(), window, cx));
            modal.regex_draft_input.update(cx, |i, cx| i.set_placeholder(super::settings_folders::regex_draft_placeholder(), window, cx));
            if let Some(picker) = &modal.font_picker {
                picker.search_input.update(cx, |i, cx| i.set_placeholder(tr("搜索字体", "Search fonts"), window, cx));
            }
        }
        if let Some(search) = &self.search {
            search.state.input.update(cx, |i, cx| i.set_placeholder(tr("搜索标题与正文", "Search titles and content"), window, cx));
        }
        cx.background_executor().spawn(async move { let _ = update_state(|s| s.language = mode); }).detach();
        crate::apply_app_menus(cx);
        crate::search::window::refresh_style(cx);
        crate::feedback::sync_language(window, cx);
        crate::quick::refresh_style(cx);
        if self.notice == "选择文件夹以浏览 Markdown 笔记" || self.notice == "Select a folder to browse Markdown notes" {
            self.notice = tr("选择文件夹以浏览 Markdown 笔记", "Select a folder to browse Markdown notes").into();
        }
        self.editor.update(cx, |editor, cx| { editor.sync_language(window, cx); editor.reset_layouts(); cx.notify(); });
        cx.notify();
    }

    pub(super) fn begin_shortcut_recording(&mut self, kind: ShortcutKind, window: &mut Window, cx: &mut Context<Self>) {
        let Some(focus) = self.settings.as_mut().map(|modal| {
            modal.recording = Some(kind);
            modal.focus.clone()
        }) else { return; };
        window.focus(&focus, cx);
        crate::quick::suspend_global_hotkeys(cx);
        cx.notify();
    }

    /// Restores system hotkeys after a cancelled recording. False when nothing was recording.
    pub(super) fn end_shortcut_recording(&mut self, cx: &mut Context<Self>) -> bool {
        if self.settings.as_mut().and_then(|modal| modal.recording.take()).is_none() { return false; }
        crate::quick::rebind_hotkeys(cx);
        cx.notify();
        true
    }

    /// Saves a recorded chord. Escape stays unhandled so the page can cancel recording.
    pub(super) fn capture_recorded_shortcut(&mut self, keystroke: &Keystroke, cx: &mut Context<Self>) -> bool {
        let Some(kind) = self.settings.as_ref().and_then(|m| m.recording) else { return false; };
        if keystroke.key == "escape" && !keystroke.modifiers.modified() { return false; }
        if let Some(spec) = keystroke_to_spec(keystroke) {
            self.apply_shortcut(kind, spec, cx);
        }
        true
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

    pub(super) fn rescan_current_folder(&mut self, cx: &mut Context<Self>) {
        if self.busy() { return; }
        let Some(root) = self.folder.clone() else { return };
        self.scan_generation = self.scan_generation.wrapping_add(1);
        let generation = self.scan_generation;
        let scan_root = root.clone();
        cx.spawn(async move |this, cx| {
            let result = cx.background_executor().spawn(async move {
                let scan_res = crate::files::scan::scan(&scan_root)?;
                let index = crate::files::index::TreeIndex::new(scan_res.nodes);
                Ok::<_, std::io::Error>(index)
            }).await;
            let _ = this.update(cx, |this, cx| {
                if this.scan_generation == generation && this.folder.as_ref() == Some(&root) {
                    if let Ok(index) = result {
                        this.tree.update(cx, |tree, cx| tree.replace_index(index, None, None, cx));
                        this.refresh(cx);
                    }
                }
            });
        }).detach();
    }
}
