//! In-page settings view ("画布内页与实时字体预览") replacing the editor canvas when open.
use gpui::{
    AnyElement, App, Context, CursorStyle, Font, FontFallbacks, FontFeatures, FontStyle,
    FontWeight, KeyDownEvent, MouseButton, SharedString, Window, div, prelude::*, px, relative,
};
use crate::files::state::{FontChoice, ThemeMode};
use crate::theme::{
    DEFAULT_QUICK_SHORTCUT, DEFAULT_SEARCH_SHORTCUT, Theme, editor_font, font_choice,
    font_smoothing, format_hotkey, quick_shortcut, search_shortcut, theme_mode,
};
use super::Workspace;
use super::settings::{ShortcutKind, keystroke_to_spec};

impl Workspace {
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
            ("默认无衬线", cur_font == FontChoice::Sans, FontChoice::Sans),
            ("霞鹜文楷", cur_font == FontChoice::Serif, FontChoice::Serif),
            ("等宽", cur_font == FontChoice::Mono, FontChoice::Mono),
        ], |font, cx| Box::new(cx.listener(move |this, _, _, cx| this.select_font(font, cx))));

        Some(div().id("settings-page").track_focus(&modal.focus).flex_1().min_w_0().h_full()
            .overflow_y_scroll().bg(theme.settings_bg).text_color(theme.text)
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
                        .text_color(theme.text).child("设置"))
                    .child(div().id("settings-back").h(px(32.)).px(px(10.)).rounded(px(8.))
                        .bg(theme.paper).border_1().border_color(theme.btn_border)
                        .flex().items_center().justify_center().text_size(px(14.)).font_weight(FontWeight(460.))
                        .text_color(theme.text).cursor(CursorStyle::PointingHand)
                        .hover(move |s| s.bg(theme.btn_hover))
                        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.close_settings(window, cx)))
                        .child("返回编辑")))
                .child(div().bg(theme.paper).border_1().border_color(theme.hairline).rounded(px(10.))
                    .py(px(18.)).px(px(20.)).flex().flex_col().gap(px(16.))
                    .child(div().text_size(px(15.)).line_height(px(22.)).font_weight(FontWeight(560.))
                        .text_color(theme.text).child("外观与字体"))
                    .child(setting_row("外观主题", theme_seg(cx)))
                    .child(setting_row("正文字体", font_seg(cx)))
                    .child(setting_row("字体平滑", smoothing_toggle(smoothing,
                        cx.listener(|this, _, _, cx| this.toggle_smoothing(cx)))))
                    .child(div().py(px(14.)).px(px(16.)).rounded(px(8.)).bg(theme.shell)
                        .border_1().border_color(theme.hairline).font(preview_font())
                        .text_size(px(17.)).line_height(relative(1.5)).text_color(theme.text)
                        .child("把周会改成异步：一段文字能说清的事，不再约三十分钟同步会。0123456789")))
                .child(div().bg(theme.paper).border_1().border_color(theme.hairline).rounded(px(10.))
                    .py(px(18.)).px(px(20.)).flex().flex_col().gap(px(12.))
                    .child(div().flex().items_center().justify_between()
                        .child(div().text_size(px(15.)).line_height(px(22.)).font_weight(FontWeight(560.))
                            .text_color(theme.text).child("基础快捷键"))
                        .when(customized, |row| row.child(div().id("reset-shortcuts")
                            .px(px(8.)).py(px(4.)).rounded(px(6.)).text_size(px(14.)).text_color(theme.accent)
                            .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
                            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                                cx.stop_propagation();
                                this.reset_shortcuts(cx);
                            }))
                            .child("恢复默认"))))
                    .child(setting_row("搜索与全局唤起",
                        shortcut_recorder("rec-search", &search_spec, recording == Some(ShortcutKind::Search),
                            cx.listener(|this, _, window, cx| {
                                cx.stop_propagation();
                                if let Some(m) = &mut this.settings {
                                    m.recording = Some(ShortcutKind::Search);
                                    window.focus(&m.focus);
                                }
                                cx.notify();
                            }))))
                    .child(setting_row("速记小窗",
                        shortcut_recorder("rec-quick", &quick_spec, recording == Some(ShortcutKind::Quick),
                            cx.listener(|this, _, window, cx| {
                                cx.stop_propagation();
                                if let Some(m) = &mut this.settings {
                                    m.recording = Some(ShortcutKind::Quick);
                                    window.focus(&m.focus);
                                }
                                cx.notify();
                            }))))
                    .child(setting_row("当前笔记查找", static_keycap("⌘F")))
                    .child(setting_row("新建笔记", static_keycap("⌘N"))))
                .children(crate::editor::ai::settings::section(cx)))
            .into_any_element())
    }
}

fn preview_font() -> Font {
    let (family, fallbacks) = editor_font();
    Font {
        family: family.into(),
        weight: FontWeight::NORMAL,
        style: FontStyle::Normal,
        features: FontFeatures::default(),
        fallbacks: (!fallbacks.is_empty()).then(|| FontFallbacks::from_fonts(fallbacks.iter().map(|s| (*s).to_string()).collect())),
    }
}

fn mono_font() -> Font {
    Font {
        family: crate::theme::mono_font_family().into(),
        weight: FontWeight::NORMAL,
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

fn smoothing_toggle(
    on: bool,
    on_down: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id("smoothing-switch").h(px(34.)).px(px(14.)).rounded(px(8.)).border_1()
        .border_color(if on { theme.accent } else { theme.btn_border })
        .bg(if on { theme.selected } else { theme.shell })
        .flex().items_center().gap(px(8.)).text_size(px(14.)).font_weight(FontWeight(460.))
        .text_color(theme.text).cursor(CursorStyle::PointingHand)
        .on_mouse_down(MouseButton::Left, on_down)
        .child(div().size(px(8.)).rounded(px(4.)).bg(if on { theme.accent } else { theme.text_secondary }))
        .child(if on { "灰度抗锯齿（已开启）" } else { "系统默认" })
}

fn shortcut_recorder(
    id: &'static str,
    spec: &str,
    recording: bool,
    on_down: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let label: SharedString = if recording { "按下快捷键…".into() } else { format_hotkey(spec).into() };
    div().id(id).h(px(30.)).px(px(10.)).flex().items_center().justify_center().rounded(px(6.))
        .bg(if recording { theme.selected } else { theme.shell })
        .border_1().border_color(if recording { theme.accent } else { theme.btn_border })
        .font(mono_font()).text_size(px(14.)).text_color(theme.text)
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.border_color(theme.accent))
        .on_mouse_down(MouseButton::Left, on_down)
        .child(label)
}

fn static_keycap(label: &'static str) -> gpui::Div {
    let theme = Theme::ocean();
    div().h(px(30.)).px(px(10.)).flex().items_center().justify_center().rounded(px(6.))
        .bg(theme.shell).border_1().border_color(theme.btn_border)
        .font(mono_font()).text_size(px(14.)).text_color(theme.text)
        .cursor(CursorStyle::Arrow)
        .child(label)
}
