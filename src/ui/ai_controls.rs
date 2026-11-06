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

pub fn titlebar_ai_button(open: bool, on_click: impl Fn(&mut gpui::Window, &mut gpui::App) + 'static) -> Stateful<Div> {
    let theme = Theme::ocean();
    div().id("titlebar-ai").group("titlebar-ai").relative().children(crate::ui::smoke_overlay::probe("titlebar-ai"))
        .size(px(26.)).flex_none().flex().items_center().justify_center().rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::PointingHand)
        .when(open, |button| button.bg(theme.selected).border_1().border_color(theme.accent))
        .when(!open, |button| button.hover(|style| style.bg(theme.hover)))
        .tooltip(tooltip(if cfg!(target_os = "macos") { tr("AI 助手 ⌘J", "AI Assistant ⌘J") } else { tr("AI 助手 Ctrl+J", "AI Assistant Ctrl+J") }))
        .child(stateful_icon("titlebar-ai", Icon::Sparkles, px(16.), open, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(if open { theme.accent } else { theme.text })))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| { cx.stop_propagation(); on_click(window, cx); })
}

pub fn backend_card(id: impl Into<SharedString>, name: &'static str, status: String, selected: bool, ready: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: SharedString = id.into();
    div().id(id.clone()).relative().children(crate::ui::smoke_overlay::probe_for(&ElementId::from(id)))
        .flex_1().min_w_0().p(px(10.)).rounded(px(10.)).border_1().cursor(CursorStyle::PointingHand)
        .bg(if selected { theme.selected } else { theme.shell }).border_color(if selected { theme.accent } else { theme.hairline })
        .hover(|style| style.bg(theme.hover))
        .child(div().flex().items_center().justify_between().font_weight(FontWeight(560.)).child(name)
            .child(div().size(px(7.)).rounded_full().bg(if ready { theme.accent } else { theme.text_secondary })))
        .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(status))
        .on_mouse_down(MouseButton::Left, on_click)
}

pub fn deck_trigger_capsule(
    id: impl Into<SharedString>,
    label: impl Into<SharedString>,
    open: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: SharedString = id.into();
    let eid = ElementId::from(id);
    let probe = crate::ui::smoke_overlay::probe_for(&eid);
    div().id(eid).relative().children(probe)
        .h(px(30.)).px(px(10.)).max_w(px(220.)).flex_none().flex().items_center().gap(px(6.))
        .rounded(crate::ui::radius::CONTROL).bg(if open { theme.selected } else { theme.shell }).border_1()
        .border_color(if open { theme.accent } else { theme.btn_border })
        .text_size(px(14.)).line_height(px(20.)).text_color(theme.text).font_weight(FontWeight(500.))
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover).border_color(theme.accent))
        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(label.into()))
        .child(dropdown::line_icon(Icon::ChevronDown, 12., theme.text_secondary))
        .on_mouse_down(MouseButton::Left, on_click)
}

pub fn panel_shell(id: impl Into<SharedString>) -> Stateful<Div> {
    let theme = Theme::ocean();
    let id: SharedString = id.into();
    let eid = ElementId::from(id);
    let probe = crate::ui::smoke_overlay::probe_for(&eid);
    let shadows: Vec<BoxShadow> = dropdown::shadows();
    div().id(eid).relative().children(probe).w(px(PANEL_WIDTH)).p(px(10.)).flex().flex_col().gap(px(8.))
        .rounded(px(10.)).border_1().border_color(theme.menu_border).bg(theme.menu)
        .text_color(theme.text).text_size(px(14.)).line_height(px(20.)).shadow(shadows).occlude()
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
    let theme = Theme::ocean();
    let id: SharedString = id.into();
    let eid = ElementId::from(id);
    let probe = crate::ui::smoke_overlay::probe_for(&eid);
    div().id(eid).relative().children(probe).size(px(24.)).flex_none().flex().items_center().justify_center()
        .rounded(px(6.)).bg(theme.primary)
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(theme.accent))
        .tooltip(tooltip(tr("发送（回车）", "Send (Enter)")))
        .child(icon(Icon::ArrowUp, px(13.), theme.mono_icon_colors(rgb(0xffffff).into())))
        .on_mouse_down(MouseButton::Left, on_click)
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
        .tooltip(tooltip(tip))
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
