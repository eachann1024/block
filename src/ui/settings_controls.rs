//! Shared controls for the full-window Settings page: cards, rows, buttons,
//! segmented switches and shortcut keycaps, all in the Ocean Paper palette.
use gpui::{
    App, Context, CursorStyle, Font, FontFeatures, FontStyle, FontWeight, Hsla, IntoElement,
    SharedString, Window, div, prelude::*, px,
};
use crate::theme::{Theme, hotkey_parts, is_dark};

/// Card surface: `#222222` in dark mode and `#ffffff` in light mode, so it lifts off `settings_bg`.
pub fn card_bg() -> Hsla {
    if is_dark() { Theme::ocean().menu } else { Theme::ocean().paper }
}

/// Active/selected surface: `#e5edfc` in light mode and `#25354f` in dark mode.
pub fn selected_bg() -> Hsla {
    if is_dark() { gpui::rgb(0x25354f).into() } else { Theme::ocean().selected }
}

pub fn mono_font() -> Font {
    Font {
        family: crate::theme::mono_font_family().into(),
        weight: FontWeight::MEDIUM,
        style: FontStyle::Normal,
        features: FontFeatures::default(),
        fallbacks: None,
    }
}

pub fn settings_card(gap_px: f32) -> gpui::Div {
    let theme = Theme::ocean();
    div().w_full().min_w_0().overflow_hidden().bg(card_bg()).border_1().border_color(theme.hairline).rounded(crate::ui::radius::SURFACE)
        .py(px(18.)).px(px(20.)).flex().flex_col().gap(px(gap_px))
}

pub fn card_title(title: &'static str) -> gpui::Div {
    let theme = Theme::ocean();
    div().text_size(px(15.)).line_height(px(22.)).font_weight(FontWeight(560.)).text_color(theme.text).child(title)
}

pub fn setting_row(title: &'static str, control: impl IntoElement) -> gpui::Div {
    let theme = Theme::ocean();
    div().w_full().min_w_0().min_h(px(36.)).flex().items_center().justify_between().gap(px(16.))
        .child(div().flex_none().whitespace_nowrap().text_size(px(14.)).text_color(theme.text).child(title))
        .child(control)
}

pub fn static_badge(label: impl Into<SharedString>, mono: bool) -> gpui::Div {
    let theme = Theme::ocean();
    div().h(px(32.)).px(px(12.)).rounded(crate::ui::radius::CONTROL).bg(theme.shell).border_1().border_color(theme.hairline)
        .flex().items_center().text_size(px(14.)).text_color(theme.text_secondary)
        .when(mono, |d| d.font(mono_font()))
        .child(label.into())
}

pub fn tag_chip(
    id: impl Into<gpui::ElementId>, label: impl Into<SharedString>, active: bool, mono: bool,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let sel_bg = selected_bg();
    let idle_bg = if is_dark() { gpui::rgb(0x2a2a2a).into() } else { theme.shell };
    div().id(id).h(px(30.)).px(px(10.)).flex().items_center().gap(px(6.)).rounded(crate::ui::radius::CONTROL).text_size(px(13.))
        .when(mono, |d| d.font(mono_font()))
        .bg(if active { sel_bg } else { idle_bg })
        .border_1().border_color(if active { theme.accent } else { theme.btn_border })
        .text_color(if active { theme.text } else { theme.text_secondary })
        .cursor(CursorStyle::PointingHand)
        .when(!active, |d| d.hover(move |s| s.bg(theme.hover).border_color(theme.accent).text_color(theme.text)))
        .when(active, |d| d.child(div().size(px(6.)).rounded(px(3.)).bg(theme.accent)))
        .child(label.into())
}

pub fn action_button(
    id: impl Into<gpui::ElementId>, label: impl Into<SharedString>, active: bool, enabled: bool,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let sel_bg = selected_bg();
    div().id(id).h(px(34.)).px(px(12.)).flex().flex_none().items_center().justify_center().rounded(crate::ui::radius::CONTROL)
        .bg(if active { sel_bg } else { theme.shell })
        .border_1().border_color(if active { theme.accent } else { theme.btn_border })
        .text_size(px(14.)).font_weight(FontWeight(460.)).text_color(theme.text)
        .when(enabled, |b| b.cursor(CursorStyle::PointingHand)
            .hover(move |s| s.bg(if active { sel_bg } else { theme.hover })))
        .when(!enabled, |b| b.opacity(0.5))
        .child(label.into())
}

pub fn status_action_button(
    id: &'static str,
    icon: Option<crate::icons::Icon>,
    label: impl Into<SharedString>,
    success: bool,
    enabled: bool,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let (bg_color, border_color, text_color, icon_color, hover_bg): (Hsla, Hsla, Hsla, Hsla, Hsla) = if success {
        let fg: Hsla = if dark { gpui::rgb(0x6dd58c).into() } else { gpui::rgb(0x1e7e3e).into() };
        (
            if dark { gpui::rgb(0x193524).into() } else { gpui::rgb(0xeaf6ed).into() },
            if dark { gpui::rgb(0x347d4e).into() } else { gpui::rgb(0x5bb974).into() },
            fg,
            fg,
            if dark { gpui::rgb(0x20422d).into() } else { gpui::rgb(0xdef0e3).into() },
        )
    } else {
        (theme.shell, theme.btn_border, theme.text, theme.text_secondary, theme.hover)
    };
    div().id(id).h(px(34.)).px(px(12.)).flex().flex_none().items_center().justify_center().gap(px(6.))
        .rounded(crate::ui::radius::CONTROL)
        .bg(bg_color).border_1().border_color(border_color)
        .text_size(px(14.)).font_weight(FontWeight(460.)).text_color(text_color)
        .when(enabled, |b| b.cursor(CursorStyle::PointingHand).hover(move |s| s.bg(hover_bg)))
        .when(!enabled, |b| b.opacity(0.5))
        .children(icon.map(|ic| crate::icons::icon(ic, px(14.), theme.mono_icon_colors(icon_color))))
        .child(label.into())
}

pub fn toggle_pill(id: &'static str, on: bool, label: impl Into<SharedString>) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let sel_bg = selected_bg();
    div().id(id).h(px(32.)).px(px(12.)).rounded(crate::ui::radius::CONTROL)
        .bg(if on { sel_bg } else { theme.shell })
        .border_1().border_color(if on { theme.accent } else { theme.btn_border })
        .flex().items_center().gap(px(8.))
        .text_size(px(14.)).font_weight(FontWeight(460.)).text_color(theme.text)
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(if on { sel_bg } else { theme.hover }))
        .child(div().size(px(8.)).rounded(px(4.)).bg(if on { theme.accent } else { theme.text_secondary }))
        .child(label.into())
}

pub fn segmented<V: 'static, T: Copy + 'static, const N: usize>(
    id: impl Into<gpui::ElementId>,
    options: [(&'static str, bool, T); N],
    make_handler: impl Fn(T, &mut Context<V>) -> Box<dyn Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static>,
) -> impl FnOnce(&mut Context<V>) -> gpui::Stateful<gpui::Div> {
    let id: gpui::ElementId = id.into();
    move |cx| {
        let theme = Theme::ocean();
        let sel_bg = selected_bg();
        div().id(id.clone()).p(px(3.)).flex().items_center().gap(px(2.)).rounded(crate::ui::radius::CONTROL).bg(theme.shell)
            .border_1().border_color(theme.hairline)
            .children(options.into_iter().enumerate().map(|(ix, (label, active, val))| {
                let handler = make_handler(val, cx);
                div().id((id.clone(), label)).h(px(32.)).px(px(12.)).flex().items_center().justify_center().rounded(crate::ui::radius::CONTROL)
                    .text_size(px(14.)).font_weight(FontWeight(460.)).cursor(CursorStyle::PointingHand)
                    .when(active, |b| b.bg(sel_bg).text_color(theme.text).border_1().border_color(theme.accent))
                    .when(!active, |b| b.text_color(theme.text_secondary).hover(move |s| s.text_color(theme.text)))
                    .on_mouse_down(gpui::MouseButton::Left, move |e, w, cx| handler(e, w, cx))
                    .child(label)
            }))
    }
}

pub fn keycap_chip(part: impl AsRef<str>, recording: bool) -> gpui::Div {
    crate::ui::shortcut_label::keycap_chip(part, recording)
}

pub fn shortcut_recorder(
    id: &'static str,
    spec: &str,
    recording: bool,
    on_down: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let tray_bg = if recording { theme.selected } else if is_dark() { theme.code_bg } else { theme.shell };
    let tray = div().id(id).h(px(32.)).px(px(5.)).flex().items_center().justify_center().gap(px(4.)).rounded(crate::ui::radius::CONTROL)
        .bg(tray_bg).border_1().border_color(if recording { theme.accent } else { theme.btn_border })
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.border_color(theme.accent))
        .on_mouse_down(gpui::MouseButton::Left, on_down);
    if recording {
        let prompt: SharedString = crate::theme::tr("按下快捷键…", "Press shortcut…").into();
        tray.child(div().px(px(6.)).text_size(px(14.)).font_weight(FontWeight(500.)).text_color(theme.accent).child(prompt))
    } else {
        tray.children(hotkey_parts(spec).into_iter().map(|p| keycap_chip(p, recording)))
    }
}

pub fn static_keycap(spec: &str) -> gpui::Div {
    let theme = Theme::ocean();
    let tray_bg = if is_dark() { theme.code_bg } else { theme.shell };
    div().h(px(32.)).px(px(5.)).flex().items_center().justify_center().gap(px(4.)).rounded(crate::ui::radius::CONTROL)
        .bg(tray_bg).border_1().border_color(theme.hairline)
        .cursor(CursorStyle::Arrow)
        .children(hotkey_parts(spec).into_iter().map(|p| keycap_chip(p, false)))
}
