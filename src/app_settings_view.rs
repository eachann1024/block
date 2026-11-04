//! In-page settings view replacing the editor canvas when open.
use gpui::{
    AnyElement, App, Context, CursorStyle, Font, FontFeatures, FontStyle,
    FontWeight, KeyDownEvent, MouseButton, SharedString, Window, div, prelude::*, px,
};
use crate::files::state::{FontChoice, LanguageMode, ThemeMode};
use crate::theme::{
    DEFAULT_QUICK_SHORTCUT, DEFAULT_SEARCH_SHORTCUT, Theme, font_choice,
    hotkey_parts, is_dark, language_mode, quick_shortcut, search_shortcut, theme_mode, tr,
};
use super::Workspace;
use super::settings::{ShortcutKind, keystroke_to_spec};

impl Workspace {
    pub(super) fn settings_view(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let modal = self.settings.as_ref()?;
        let theme = Theme::ocean();
        let cur_lang = language_mode();
        let cur_theme = theme_mode();
        let cur_font = font_choice();
        let recording = modal.recording;
        let search_spec = search_shortcut();
        let quick_spec = quick_shortcut();
        let customized = search_spec != DEFAULT_SEARCH_SHORTCUT || quick_spec != DEFAULT_QUICK_SHORTCUT;

        let lang_seg = segmented("lang-seg", [
            (tr("跟随系统", "System"), cur_lang == LanguageMode::System, LanguageMode::System),
            ("简体中文", cur_lang == LanguageMode::Zh, LanguageMode::Zh),
            ("English", cur_lang == LanguageMode::En, LanguageMode::En),
        ], |mode, cx| Box::new(cx.listener(move |this, _, _, cx| this.select_language(mode, cx))));
        let theme_seg = segmented("theme-seg", [
            (tr("跟随系统", "System"), cur_theme == ThemeMode::System, ThemeMode::System),
            (tr("浅色", "Light"), cur_theme == ThemeMode::Light, ThemeMode::Light),
            (tr("深色", "Dark"), cur_theme == ThemeMode::Dark, ThemeMode::Dark),
        ], |mode, cx| Box::new(cx.listener(move |this, _, _, cx| this.select_theme(mode, cx))));
        let font_seg = segmented("font-seg", [
            (tr("无衬线", "Sans-serif"), cur_font == FontChoice::Sans, FontChoice::Sans),
            (tr("衬线", "Serif"), cur_font == FontChoice::Serif, FontChoice::Serif),
        ], |font, cx| Box::new(cx.listener(move |this, _, _, cx| this.select_font(font, cx))));

        Some(div().id("settings-page").track_focus(&modal.focus).flex_1().min_w_0().h_full()
            .overflow_y_scroll().bg(theme.settings_bg).text_color(theme.text)
            .on_scroll(|_, _, cx| crate::dropdown::dismiss_all(cx))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
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
            .child(div().w_full().max_w(px(720.)).mx_auto().pt(px(40.)).px(px(28.)).pb(px(64.))
                .flex().flex_col().gap(px(28.))
                .child(div().flex().items_center().justify_between()
                    .child(div().text_size(px(24.)).line_height(px(32.)).font_weight(FontWeight(560.))
                        .text_color(theme.text).child(tr("设置", "Settings")))
                    .child(div().id("settings-back").relative().children(crate::ui::smoke_overlay::probe("settings-back")).h(px(32.)).px(px(10.)).rounded(px(8.))
                        .bg(theme.paper).border_1().border_color(theme.btn_border)
                        .flex().items_center().justify_center().text_size(px(14.)).font_weight(FontWeight(460.))
                        .text_color(theme.text).cursor(CursorStyle::PointingHand)
                        .hover(move |s| s.bg(theme.btn_hover))
                        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.close_settings(window, cx)))
                        .child(tr("返回编辑", "Back to Editor"))))
                .child(div().bg(theme.paper).border_1().border_color(theme.hairline).rounded(px(10.))
                    .py(px(18.)).px(px(20.)).flex().flex_col().gap(px(16.))
                    .child(div().text_size(px(15.)).line_height(px(22.)).font_weight(FontWeight(560.))
                        .text_color(theme.text).child(tr("外观与字体", "Appearance & Typography")))
                    .child(setting_row(tr("界面语言", "Language"), lang_seg(cx)))
                    .child(setting_row(tr("外观主题", "Theme"), theme_seg(cx)))
                    .child(setting_row(tr("正文字体", "Editor Font"), font_seg(cx))))
                .child(div().bg(theme.paper).border_1().border_color(theme.hairline).rounded(px(10.))
                    .py(px(18.)).px(px(20.)).flex().flex_col().gap(px(12.))
                    .child(div().flex().items_center().justify_between()
                        .child(div().text_size(px(15.)).line_height(px(22.)).font_weight(FontWeight(560.))
                            .text_color(theme.text).child(tr("基础快捷键", "Shortcuts")))
                        .when(customized, |row| row.child(div().id("reset-shortcuts")
                            .px(px(8.)).py(px(4.)).rounded(px(6.)).text_size(px(14.)).text_color(theme.accent)
                            .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
                            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                                cx.stop_propagation();
                                this.reset_shortcuts(cx);
                            }))
                            .child(tr("恢复默认", "Reset")))))
                    .child(setting_row(tr("搜索与全局唤起", "Search & Switcher"),
                        shortcut_recorder("rec-search", &search_spec, recording == Some(ShortcutKind::Search),
                            cx.listener(|this, _, window, cx| {
                                cx.stop_propagation();
                                if let Some(m) = &mut this.settings {
                                    m.recording = Some(ShortcutKind::Search);
                                    window.focus(&m.focus, cx);
                                }
                                cx.notify();
                            }))))
                    .child(setting_row(tr("速记小窗", "Quick Note"),
                        shortcut_recorder("rec-quick", &quick_spec, recording == Some(ShortcutKind::Quick),
                            cx.listener(|this, _, window, cx| {
                                cx.stop_propagation();
                                if let Some(m) = &mut this.settings {
                                    m.recording = Some(ShortcutKind::Quick);
                                    window.focus(&m.focus, cx);
                                }
                                cx.notify();
                            }))))
                    .child(setting_row(tr("当前笔记查找", "Find in Note"), static_keycap("secondary-f")))
                    .child(setting_row(tr("新建笔记", "New Note"), static_keycap("secondary-n"))))
                .children(crate::editor::ai::settings::section(cx)))
            .into_any_element())
    }
}

fn mono_font() -> Font {
    Font {
        family: crate::theme::mono_font_family().into(),
        weight: FontWeight::MEDIUM,
        style: FontStyle::Normal,
        features: FontFeatures::default(),
        fallbacks: None,
    }
}

fn setting_row(title: &'static str, control: impl IntoElement) -> gpui::Div {
    let theme = Theme::ocean();
    div().min_h(px(36.)).flex().items_center().justify_between().gap(px(16.))
        .child(div().text_size(px(14.)).text_color(theme.text).child(title))
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
            .border_1().border_color(theme.hairline)
            .children(options.into_iter().enumerate().map(|(ix, (label, active, val))| {
                let handler = make_handler(val, cx);
                div().id((id, ix)).h(px(34.)).px(px(14.)).flex().items_center().justify_center().rounded(px(6.))
                    .text_size(px(14.)).font_weight(FontWeight(460.)).cursor(CursorStyle::PointingHand)
                    .when(active, |b| b.bg(theme.selected).text_color(theme.text).border_1().border_color(theme.accent))
                    .when(!active, |b| b.text_color(theme.text_secondary).hover(move |s| s.text_color(theme.text)))
                    .on_mouse_down(MouseButton::Left, move |e, w, cx| handler(e, w, cx))
                    .child(label)
            }))
    }
}

fn keycap_chip(part: SharedString) -> gpui::Div {
    let theme = Theme::ocean();
    let is_symbol = matches!(part.as_ref(), "⌘" | "⇧" | "⌥" | "⌃");
    let cap_bg = if is_dark() { theme.shell } else { theme.paper };
    div().h(px(22.)).min_w(px(22.)).px(if is_symbol { px(5.) } else { px(6.) }).rounded(px(5.))
        .bg(cap_bg).border_1().border_color(theme.btn_border)
        .flex().items_center().justify_center()
        .when(!is_symbol, |d| d.font(mono_font()))
        .text_size(if is_symbol { px(13.) } else { px(12.) })
        .line_height(px(14.)).font_weight(FontWeight(500.)).text_color(theme.text)
        .child(part)
}

fn shortcut_recorder(
    id: &'static str,
    spec: &str,
    recording: bool,
    on_down: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let tray_bg = if recording { theme.selected } else if is_dark() { theme.code_bg } else { theme.shell };
    let tray = div().id(id).h(px(32.)).px(px(5.)).flex().items_center().justify_center().gap(px(4.)).rounded(px(8.))
        .bg(tray_bg).border_1().border_color(if recording { theme.accent } else { theme.btn_border })
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.border_color(theme.accent))
        .on_mouse_down(MouseButton::Left, on_down);
    if recording {
        let prompt: SharedString = tr("按下快捷键…", "Press shortcut…").into();
        tray.child(div().px(px(6.)).text_size(px(13.)).font_weight(FontWeight(500.)).text_color(theme.accent).child(prompt))
    } else {
        tray.children(hotkey_parts(spec).into_iter().map(|p| keycap_chip(p.into())))
    }
}

fn static_keycap(spec: &str) -> gpui::Div {
    let theme = Theme::ocean();
    let tray_bg = if is_dark() { theme.code_bg } else { theme.shell };
    div().h(px(32.)).px(px(5.)).flex().items_center().justify_center().gap(px(4.)).rounded(px(8.))
        .bg(tray_bg).border_1().border_color(theme.hairline)
        .cursor(CursorStyle::Arrow)
        .children(hotkey_parts(spec).into_iter().map(|p| keycap_chip(p.into())))
}
