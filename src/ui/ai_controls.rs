use crate::ui::tooltip::TooltipExt;
use gpui::{
    BoxShadow, CursorStyle, Div, ElementId, FontWeight, Hsla, MouseButton, SharedString,
    Stateful, div, hsla, prelude::*, px, rgb,
};
use crate::{
    dropdown,
    icons::{Icon, icon, stateful_icon},
    theme::{Theme, tooltip, tr},
};

pub const PANEL_WIDTH: f32 = 360.;

pub fn dictation_button(recording: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> Stateful<Div> {
    use gpui::{Animation, AnimationExt};
    let theme = super::ai_theme::theme();
    let glyph = if recording {
        let mut bars = div().flex().items_center().gap(px(2.)).h(px(16.));
        for index in 0..3 {
            bars = bars.child(div().w(px(2.)).rounded_full().bg(theme.text)
                .with_animation(format!("ai-dictation-wave-{index}"), Animation::new(std::time::Duration::from_millis(900)).repeat(), move |bar, delta| {
                    let phase = delta * std::f32::consts::TAU - index as f32 * std::f32::consts::PI / 3.;
                    bar.h(px(4. + (phase.sin() + 1.) * 4.))
                }));
        }
        bars
    } else {
        div().relative().size(px(16.)).flex().justify_center()
            .child(div().absolute().top(px(1.)).w(px(6.)).h(px(9.)).rounded_full().border_1().border_color(theme.text_secondary))
            .child(div().absolute().top(px(5.)).w(px(10.)).h(px(7.)).rounded_b_full().border_b_1().border_l_1().border_r_1().border_color(theme.text_secondary))
            .child(div().absolute().bottom(px(1.)).w(px(6.)).h(px(1.)).bg(theme.text_secondary))
            .child(div().absolute().bottom(px(2.)).w(px(1.)).h(px(3.)).bg(theme.text_secondary))
    };
    div().id("ai-dictation").relative().children(super::smoke_overlay::probe("ai-dictation"))
        .size(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(6.))
        .cursor(CursorStyle::PointingHand).hover(move |style| style.bg(theme.hover))
        .tooltip_above(tooltip(if recording { tr("停止听写", "Stop dictation") } else { tr("语音输入", "Dictate") }))
        .child(glyph).on_mouse_down(MouseButton::Left, on_click)
}

pub fn enable_action_button(id: impl Into<SharedString>, label: impl Into<SharedString>,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> Stateful<Div> {
    let theme = Theme::ocean();
    let eid = ElementId::from(id.into());
    div().id(eid.clone()).relative().children(crate::ui::smoke_overlay::probe_for(&eid))
        .h(px(38.)).px(px(18.)).flex_none().flex().items_center().justify_center().gap(px(6.)).rounded(crate::ui::radius::CONTROL)
        .bg(theme.selected).border_1().border_color(theme.accent).text_size(px(14.)).line_height(px(20.)).font_weight(FontWeight(560.)).text_color(theme.text)
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
        .child(label.into()).child(icon(Icon::ArrowRight, px(14.), theme.mono_icon_colors(theme.accent)))
        .on_mouse_down(MouseButton::Left, on_click)
}

pub fn titlebar_ai_button(open: bool, on_click: impl Fn(&mut gpui::Window, &mut gpui::App) + 'static) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id("titlebar-ai").group("titlebar-ai").relative().children(crate::ui::smoke_overlay::probe("titlebar-ai"))
        .size(px(26.)).flex_none().flex().items_center().justify_center().rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::PointingHand)
        .when(open, |button| button.bg(theme.selected).border_1().border_color(theme.accent))
        .when(!open, |button| button.hover(|style| style.bg(theme.hover)))
        .tooltip_above(tooltip(if cfg!(target_os = "macos") { tr("AI 助手 ⌘J", "AI Assistant ⌘J") } else { tr("AI 助手 Ctrl+J", "AI Assistant Ctrl+J") }))
        .child(stateful_icon("titlebar-ai", Icon::Sparkles, px(18.), open, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(if open { theme.accent } else { theme.text })))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| { cx.stop_propagation(); on_click(window, cx); })
}

pub const FULLSCREEN_DECK_WIDTH: f32 = 688.;

pub fn backend_rail_item(
    id: impl Into<SharedString>, name: &'static str, logo: &'static str, selected: bool, ready: bool, wide: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: SharedString = id.into();
    let eid = ElementId::from(id);
    let btn_sz = if wide { 38. } else { 34. };
    let ico_sz = if wide { 20. } else { 18. };
    div().id(eid.clone()).relative().children(crate::ui::smoke_overlay::probe_for(&eid))
        .size(px(btn_sz)).min_w(px(btn_sz)).max_w(px(btn_sz)).min_h(px(btn_sz)).max_h(px(btn_sz))
        .flex_none().flex().items_center().justify_center().rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::PointingHand)
        .when(selected, |d| d.bg(theme.selected))
        .when(!selected, |d| d.hover(move |s| s.bg(theme.hover)))
        .tooltip_above(tooltip(name))
        .child(super::settings_controls::brand_glyph(logo, ico_sz, if selected { theme.text } else { theme.text_secondary }))
        .when(selected, |d| d.child(div().absolute().right(px(-6.)).top(px(8.)).w(px(2.5)).h(px(btn_sz - 16.)).rounded_full().bg(theme.accent)))
        .when(!ready, |d| d.child(div().absolute().right(px(4.)).bottom(px(4.)).size(px(5.)).rounded_full().bg(theme.text_secondary)))
        .on_mouse_down(MouseButton::Left, on_click)
}

pub fn model_row(
    id: impl Into<SharedString>, logo: &'static str, label: impl Into<SharedString>,
    shortcut: Option<SharedString>, active: bool, wide: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let eid = ElementId::from(id.into());
    div().id(eid.clone()).relative().children(crate::ui::smoke_overlay::probe_for(&eid))
        .h(px(if wide { 38. } else { 34. })).w_full().flex_none().px(px(if wide { 12. } else { 10. })).gap(px(if wide { 10. } else { 8. }))
        .rounded(crate::ui::radius::CONTROL).flex().items_center()
        .text_size(px(if wide { 14.5 } else { 14. })).line_height(px(20.)).text_color(theme.text)
        .cursor(CursorStyle::PointingHand)
        .when(active, |row| row.bg(theme.selected).font_weight(FontWeight(500.)))
        .hover(move |style| style.bg(theme.hover))
        .child(super::settings_controls::brand_glyph(logo, if wide { 16. } else { 15. }, theme.text))
        .child(div().flex_1().min_w_0().flex().items_center().gap(px(6.))
            .child(div().overflow_hidden().whitespace_nowrap().text_ellipsis().child(label.into()))
            .when(active, |d| d.child(dropdown::line_icon(Icon::Check, 14., theme.accent))))
        .when_some(shortcut, |row, key| row.child(
            div().px(px(6.)).py(px(1.5)).flex_none().rounded(px(5.)).bg(theme.shell).border_1().border_color(theme.hairline)
                .font_family("JetBrains Mono").text_size(px(12.)).line_height(px(16.)).text_color(theme.text_secondary).child(key),
        ))
        .on_mouse_down(MouseButton::Left, on_click)
}

pub fn thinking_level_item(
    id: impl Into<SharedString>, label: &'static str, active: bool, compact: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let eid = ElementId::from(id.into());
    let base = div().id(eid.clone()).relative().children(crate::ui::smoke_overlay::probe_for(&eid))
        .cursor(CursorStyle::PointingHand).text_size(px(if compact { 13. } else { 14. })).line_height(px(20.));
    if compact {
        base.flex_1().h(px(26.)).px(px(6.)).rounded(px(6.)).flex().items_center().justify_center().gap(px(4.))
            .when(active, |d| d.bg(theme.selected).border_1().border_color(theme.accent).text_color(theme.text).font_weight(FontWeight(500.)))
            .when(!active, |d| d.text_color(theme.text_secondary).hover(move |s| s.bg(theme.hover)))
            .child(label)
            .on_mouse_down(MouseButton::Left, on_click)
    } else {
        base.w_full().h(px(36.)).px(px(12.)).rounded(crate::ui::radius::CONTROL).flex().items_center().justify_between()
            .when(active, |d| d.bg(theme.selected).text_color(theme.text).font_weight(FontWeight(500.)))
            .when(!active, |d| d.text_color(theme.text).hover(move |s| s.bg(theme.hover)))
            .child(label)
            .child(div().size(px(16.)).flex_none().flex().items_center().justify_center()
                .when(active, |s| s.child(dropdown::line_icon(Icon::Check, 14., theme.accent))))
            .on_mouse_down(MouseButton::Left, on_click)
    }
}

pub fn deck_trigger_capsule(
    id: impl Into<SharedString>, logo: &'static str, label: impl Into<SharedString>,
    thinking: Option<&'static str>, fullscreen: bool, open: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let eid = ElementId::from(id.into());
    let max_w = if fullscreen { 360. } else { 212. };
    div().id(eid.clone()).relative().children(crate::ui::smoke_overlay::probe_for(&eid))
        .h(px(if fullscreen { 32. } else { 30. })).px(px(if fullscreen { 12. } else { 10. })).max_w(px(max_w)).flex_none().flex().items_center().gap(px(7.))
        .rounded(crate::ui::radius::CONTROL).bg(if open { theme.selected } else { theme.shell }).border_1()
        .border_color(if open { theme.accent } else { theme.btn_border })
        .text_size(px(if fullscreen { 14. } else { 13.5 })).line_height(px(20.)).text_color(theme.text).font_weight(FontWeight(500.))
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover).border_color(theme.accent))
        .child(super::settings_controls::brand_glyph(logo, if fullscreen { 16. } else { 15. }, theme.text))
        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(label.into()))
        .when_some(thinking, |d, level| d.child(div().flex_none().text_color(theme.text_secondary).font_weight(FontWeight(460.)).child(level)))
        .child(dropdown::line_icon(if open { Icon::ChevronUp } else { Icon::ChevronDown }, 12., theme.text_secondary))
        .on_mouse_down(MouseButton::Left, on_click)
}

pub fn panel_shell(id: impl Into<SharedString>) -> Stateful<Div> {
    let theme = Theme::ocean();
    let eid = ElementId::from(id.into());
    let shadows: Vec<BoxShadow> = dropdown::shadows();
    div().id(eid.clone()).relative().children(crate::ui::smoke_overlay::probe_for(&eid))
        .w(px(PANEL_WIDTH)).flex().flex_col()
        .rounded(crate::ui::radius::SURFACE).border_1().border_color(theme.menu_border).bg(theme.menu)
        .text_color(theme.text).text_size(px(14.)).line_height(px(20.)).shadow(shadows).occlude().overflow_hidden()
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
}

pub fn input_capsule(busy: bool) -> Div {
    let theme = Theme::ocean();
    div().min_h(px(36.)).pl(px(10.)).pr(px(6.)).flex().items_center().gap(px(8.))
        .rounded(px(8.)).border_1()
        .border_color(if busy { theme.hairline } else { theme.btn_border })
        .bg(theme.code_bg)
}

pub fn send_button(
    id: impl Into<SharedString>,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    send_button_enabled(id, true, on_click)
}

pub fn send_button_enabled(
    id: impl Into<SharedString>, enabled: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let theme = super::ai_theme::theme();
    let id: SharedString = id.into();
    let eid = ElementId::from(id);
    let probe = crate::ui::smoke_overlay::probe_for(&eid);
    let (bg, fg) = if enabled {
        (theme.text, theme.paper)
    } else {
        (theme.btn_border, theme.text)
    };
    div().id(eid).relative().children(probe).size(px(24.)).flex_none().flex().items_center().justify_center()
        .rounded(px(6.)).bg(bg)
        .when(enabled, |button| button.cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.text_secondary)))
        .tooltip_above(tooltip(tr("发送（回车）", "Send (Enter)")))
        .child(icon(Icon::ArrowUp, px(16.), theme.mono_icon_colors(fg)))
        .on_mouse_down(MouseButton::Left, move |event, window, cx| { if enabled { on_click(event, window, cx); } })
}

pub fn action_chip(
    id: impl Into<SharedString>,
    lead: Option<Icon>,
    label: impl Into<SharedString>,
    primary: bool,
    open: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: SharedString = id.into();
    let eid = ElementId::from(id.clone());
    let probe = crate::ui::smoke_overlay::probe_for(&eid);
    div().id(eid).group(id.clone()).relative().children(probe)
        .h(px(28.)).px(px(8.)).flex_none().flex().items_center().gap(px(4.))
        .rounded(px(6.)).text_size(px(14.)).line_height(px(20.)).cursor(CursorStyle::PointingHand)
        .when(primary, |d| {
            d.px(px(10.)).bg(theme.selected).border_1().border_color(theme.accent)
                .text_color(theme.text).font_weight(FontWeight(500.))
                .hover(move |s| s.bg(theme.hover))
        })
        .when(!primary && open, |d| {
            d.bg(theme.selected).border_1().border_color(theme.accent).text_color(theme.text)
        })
        .when(!primary && !open, |d| {
            d.text_color(theme.text).hover(move |s| s.bg(theme.hover))
        })
        .when_some(lead, |d, ic| {
            d.child(stateful_icon(id, ic, px(14.), open || primary, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.accent)))
        })
        .child(label.into())
        .on_mouse_down(MouseButton::Left, on_click)
}

pub fn icon_chip(
    id: impl Into<SharedString>,
    ic: Icon,
    tip: &'static str,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: SharedString = id.into();
    let eid = ElementId::from(id.clone());
    let probe = crate::ui::smoke_overlay::probe_for(&eid);
    div().id(eid).group(id.clone()).relative().children(probe)
        .size(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(6.))
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
        .tooltip_above(tooltip(tip))
        .child(stateful_icon(id, ic, px(14.), false, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text)))
        .on_mouse_down(MouseButton::Left, on_click)
}

pub fn diff_legend() -> Div {
    let (del_fg, del_bg): (Hsla, Hsla) = (rgb(0xb42318).into(), hsla(0., 0.75, 0.5, 0.12));
    let (ins_fg, ins_bg): (Hsla, Hsla) = (rgb(0x087443).into(), hsla(150. / 360., 0.6, 0.4, 0.14));
    div().flex().items_center().gap(px(4.)).text_size(px(14.)).line_height(px(20.))
        .child(div().px(px(5.)).rounded(crate::ui::radius::MICRO_XS).bg(del_bg).text_color(del_fg).line_through().child(tr("删除", "Removed")))
        .child(div().px(px(5.)).rounded(crate::ui::radius::MICRO_XS).bg(ins_bg).text_color(ins_fg).child(tr("新增", "Added")))
}
