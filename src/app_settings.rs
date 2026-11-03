//! Centered preferences modal (⌘,): theme, editor font, font smoothing, and global hotkeys.
use gpui::{
    AnyElement, App, Context, CursorStyle, FocusHandle, FontWeight, KeyDownEvent, Keystroke,
    MouseButton, SharedString, Window, div, prelude::*, px,
};
use crate::files::state::{FontChoice, ThemeMode, update_state};
use crate::icons::{Icon, icon};
use crate::theme::{
    DEFAULT_QUICK_SHORTCUT, DEFAULT_SEARCH_SHORTCUT, Theme, font_choice, font_smoothing,
    format_hotkey, quick_shortcut, search_shortcut, set_font_choice, set_font_smoothing,
    set_shortcuts, set_theme_mode, theme_mode,
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

fn keystroke_to_spec(ks: &Keystroke) -> Option<String> {
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
        cx.notify();
    }

    pub(super) fn close_settings(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.settings.take().is_none() { return; }
        self.restore_focus(window, cx);
        cx.notify();
    }

    fn select_theme(&mut self, mode: ThemeMode, cx: &mut Context<Self>) {
        set_theme_mode(mode);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| s.theme = mode);
        }).detach();
        crate::quick::refresh_style(cx);
        cx.notify();
    }

    fn select_font(&mut self, font: FontChoice, cx: &mut Context<Self>) {
        set_font_choice(font);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| s.font_family = font);
        }).detach();
        self.editor.update(cx, |editor, cx| { editor.reset_layouts(); cx.notify(); });
        crate::quick::refresh_style(cx);
        cx.notify();
    }

    fn toggle_smoothing(&mut self, cx: &mut Context<Self>) {
        let next = !font_smoothing();
        set_font_smoothing(next);
        cx.background_executor().spawn(async move {
            let _ = update_state(|s| s.font_smoothing = Some(next));
        }).detach();
        cx.notify();
    }

    fn apply_shortcut(&mut self, kind: ShortcutKind, spec: String, cx: &mut Context<Self>) {
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

    fn reset_shortcuts(&mut self, cx: &mut Context<Self>) {
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

    pub(super) fn settings_view(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let modal = self.settings.as_ref()?;
        let theme = Theme::ocean();
        let cur_theme = theme_mode();
        let cur_font = font_choice();
        let smoothing = font_smoothing();
        let recording = modal.recording;
        let search_spec = search_shortcut();
        let quick_spec = quick_shortcut();
        let customized = search_spec != DEFAULT_SEARCH_SHORTCUT || quick_spec != DEFAULT_QUICK_SHORTCUT;

        let theme_seg = segmented("theme-seg", [
            ("浅色", cur_theme == ThemeMode::Light, ThemeMode::Light),
            ("深色", cur_theme == ThemeMode::Dark, ThemeMode::Dark),
            ("跟随系统", cur_theme == ThemeMode::System, ThemeMode::System),
        ], |mode, cx| Box::new(cx.listener(move |this, _, _, cx| this.select_theme(mode, cx))));
        let font_seg = segmented("font-seg", [
            ("无衬线", cur_font == FontChoice::Sans, FontChoice::Sans),
            ("衬线", cur_font == FontChoice::Serif, FontChoice::Serif),
            ("等宽", cur_font == FontChoice::Mono, FontChoice::Mono),
        ], |font, cx| Box::new(cx.listener(move |this, _, _, cx| this.select_font(font, cx))));

        Some(div().id("settings-overlay").absolute().top_0().left_0().size_full().occlude()
            .bg(gpui::rgba(0x20242c44)).flex().flex_col().items_center().pt(px(88.)).px(px(24.))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.close_settings(window, cx)))
            .child(div().id("settings-dialog").track_focus(&modal.focus).w(px(520.)).max_w_full()
                .flex().flex_col().overflow_hidden().rounded(px(12.)).bg(theme.menu)
                .border_1().border_color(theme.menu_border).shadow_lg().text_color(theme.text)
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                    cx.stop_propagation();
                    if let Some(m) = &mut this.settings {
                        if m.recording.take().is_some() { cx.notify(); }
                    }
                }))
                .on_key_down(cx.listener(|this, event: &KeyDownEvent, window, cx| {
                    let Some(kind) = this.settings.as_ref().and_then(|m| m.recording) else {
                        if event.keystroke.key == "escape" {
                            cx.stop_propagation();
                            this.close_settings(window, cx);
                        }
                        return;
                    };
                    cx.stop_propagation();
                    if event.keystroke.key == "escape" && !event.keystroke.modifiers.modified() {
                        if let Some(m) = &mut this.settings { m.recording = None; }
                        cx.notify();
                        return;
                    }
                    if let Some(spec) = keystroke_to_spec(&event.keystroke) {
                        this.apply_shortcut(kind, spec, cx);
                    }
                }))
                .child(div().h(px(52.)).flex_none().px(px(20.)).flex().items_center().justify_between()
                    .border_b_1().border_color(theme.hairline)
                    .child(div().text_size(px(16.)).font_weight(FontWeight(600.)).child("偏好设置"))
                    .child(div().id("settings-close").size(px(28.)).flex().items_center().justify_center().rounded(px(6.))
                        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
                        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.close_settings(window, cx)))
                        .child(icon(Icon::Close, px(14.), theme.icon_colors(theme.text_secondary)))))
                .child(div().p(px(20.)).flex().flex_col().gap(px(18.))
                    .child(setting_row("外观模式", "切换浅色、深色或跟随 macOS 外观", theme_seg(cx)))
                    .child(setting_row("编辑字体", "正文排版所使用的字体族", font_seg(cx)))
                    .child(setting_row("字体平滑", "启用 macOS 字体平滑渲染",
                        switch_button(smoothing, cx.listener(|this, _, _, cx| this.toggle_smoothing(cx)))))
                    .child(div().h(px(1.)).bg(theme.hairline))
                    .child(div().flex().items_center().justify_between()
                        .child(div().text_size(px(14.)).font_weight(FontWeight(600.)).child("全局快捷键"))
                        .when(customized, |row| row.child(div().id("reset-shortcuts")
                            .px(px(8.)).py(px(4.)).rounded(px(6.)).text_size(px(14.)).text_color(theme.accent)
                            .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
                            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                                cx.stop_propagation();
                                this.reset_shortcuts(cx);
                            }))
                            .child("恢复默认"))))
                    .child(setting_row("全局搜索", "在任意应用中唤起并搜索笔记",
                        shortcut_recorder("rec-search", &search_spec, recording == Some(ShortcutKind::Search),
                            cx.listener(|this, _, window, cx| {
                                cx.stop_propagation();
                                if let Some(m) = &mut this.settings {
                                    m.recording = Some(ShortcutKind::Search);
                                    window.focus(&m.focus);
                                }
                                cx.notify();
                            }))))
                    .child(setting_row("速记小窗", "在任意应用中呼出或隐藏速记窗口",
                        shortcut_recorder("rec-quick", &quick_spec, recording == Some(ShortcutKind::Quick),
                            cx.listener(|this, _, window, cx| {
                                cx.stop_propagation();
                                if let Some(m) = &mut this.settings {
                                    m.recording = Some(ShortcutKind::Quick);
                                    window.focus(&m.focus);
                                }
                                cx.notify();
                            })))))
            ).into_any_element())
    }
}

fn setting_row(title: &'static str, desc: &'static str, control: impl IntoElement) -> gpui::Div {
    let theme = Theme::ocean();
    div().flex().items_center().justify_between().gap(px(16.))
        .child(div().flex().flex_col().gap(px(2.))
            .child(div().text_size(px(14.)).font_weight(FontWeight(500.)).text_color(theme.text).child(title))
            .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(desc)))
        .child(control)
}

fn segmented<T: Copy + 'static, const N: usize>(
    id: &'static str,
    options: [(&'static str, bool, T); N],
    make_handler: impl Fn(T, &mut Context<Workspace>) -> Box<dyn Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static>,
) -> impl FnOnce(&mut Context<Workspace>) -> gpui::Stateful<gpui::Div> {
    move |cx| {
        let theme = Theme::ocean();
        div().id(id).p(px(3.)).flex().items_center().gap(px(2.)).rounded(px(8.)).bg(theme.shell)
            .border_1().border_color(theme.btn_border)
            .children(options.into_iter().enumerate().map(|(ix, (label, active, val))| {
                let handler = make_handler(val, cx);
                div().id((id, ix)).h(px(28.)).px(px(12.)).flex().items_center().justify_center().rounded(px(6.))
                    .text_size(px(14.)).font_weight(FontWeight(if active { 500. } else { 400. }))
                    .text_color(if active { theme.text } else { theme.text_secondary })
                    .cursor(CursorStyle::PointingHand)
                    .when(active, |b| b.bg(theme.paper).shadow_sm())
                    .when(!active, |b| b.hover(move |s| s.text_color(theme.text)))
                    .on_mouse_down(MouseButton::Left, move |e, w, cx| handler(e, w, cx))
                    .child(label)
            }))
    }
}

fn switch_button(on: bool, on_down: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id("smoothing-switch").w(px(44.)).h(px(24.)).p(px(2.)).flex().items_center().rounded(px(12.))
        .bg(if on { theme.accent } else { theme.shell }).border_1()
        .border_color(if on { theme.accent } else { theme.btn_border })
        .cursor(CursorStyle::PointingHand)
        .on_mouse_down(MouseButton::Left, on_down)
        .child(div().size(px(18.)).rounded_full().bg(gpui::rgb(0xffffff)).shadow_sm()
            .when(on, |knob| knob.ml_auto()))
}

fn shortcut_recorder(
    id: &'static str,
    spec: &str,
    recording: bool,
    on_down: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let label: SharedString = if recording { "按下快捷键…".into() } else { format_hotkey(spec).into() };
    div().id(id).h(px(32.)).min_w(px(108.)).px(px(12.)).flex().items_center().justify_center().rounded(px(8.))
        .bg(if recording { theme.selected } else { theme.shell })
        .border_1().border_color(if recording { theme.accent } else { theme.btn_border })
        .text_size(px(14.)).font_weight(FontWeight(500.))
        .text_color(if recording { theme.accent } else { theme.text })
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.border_color(theme.accent))
        .on_mouse_down(MouseButton::Left, on_down)
        .child(label)
}
