use crate::ui::tooltip::TooltipExt;
use gpui::{
    AnimationExt, BoxShadow, CursorStyle, Div, ElementId, IntoElement, Pixels, SharedString,
    Stateful, Styled, div, point, prelude::*, px, rgb, rgba, transparent_black,
};
use crate::{
    icons::{Icon, IconColors, icon, stateful_icon},
    theme::{Theme, is_dark, tooltip},
};

pub const BG: u32 = 0xffffff;
pub const BORDER: u32 = 0xececea;
pub const INK: u32 = 0x37352f;
pub const MUTED: u32 = 0x5e5d59;
pub const HOVER: u32 = 0xf1f1ef;
pub const ON_BG: u32 = 0xe7effd;
pub const ON_INK: u32 = 0x2f6fe4;
pub const OPEN_BG: u32 = 0xefefed;
pub const SEP: u32 = 0xe6e6e3;
pub const ACCENT: u32 = 0x2f6fe4;

#[derive(Clone, Copy)]
pub struct ToolbarColors {
    pub bg: u32,
    pub border: u32,
    pub ink: u32,
    pub muted: u32,
    pub hover: u32,
    pub on_bg: u32,
    pub on_ink: u32,
    pub open_bg: u32,
    pub sep: u32,
    pub accent: u32,
    pub input_bg: u32,
}

impl ToolbarColors {
    pub fn current() -> Self {
        if is_dark() {
            Self {
                bg: 0x26282d, border: 0x3a3e46, ink: 0xe8eaee, muted: 0x9aa0aa,
                hover: 0x3a3e46, on_bg: 0x33415c, on_ink: 0x9dbcf5, open_bg: 0x3a3e46,
                sep: 0x454a53, accent: 0x547dd0, input_bg: 0x33363c,
            }
        } else {
            Self {
                bg: BG, border: BORDER, ink: INK, muted: MUTED,
                hover: HOVER, on_bg: ON_BG, on_ink: ON_INK, open_bg: OPEN_BG,
                sep: SEP, accent: ACCENT, input_bg: 0xf1f1ef,
            }
        }
    }
}

pub fn shadows() -> Vec<BoxShadow> {
    vec![
        BoxShadow { color: rgba(0x1f1e1d1a).into(), offset: point(px(0.), px(8.)), blur_radius: px(24.), spread_radius: px(0.), inset: false },
        BoxShadow { color: rgba(0x1f1e1d0f).into(), offset: point(px(0.), px(1.)), blur_radius: px(3.), spread_radius: px(0.), inset: false },
    ]
}

pub fn shell() -> Div {
    let tc = ToolbarColors::current();
    div()
        .flex()
        .items_center()
        .gap(px(2.))
        .py(px(4.))
        .px(px(6.))
        .rounded(crate::ui::radius::SURFACE)
        .border_1()
        .border_color(rgb(tc.border))
        .bg(rgb(tc.bg))
        .shadow(shadows())
        .occlude()
}

pub fn pill(id: impl Into<ElementId>, width: Option<Pixels>) -> Stateful<Div> {
    shell().id(id).when_some(width, |d, w| d.w(w))
}

pub fn animate<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    crate::ui::motion::bar_enter(el, id)
}

pub fn animate_delayed<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    crate::ui::motion::bar_enter_delayed(el, id)
}

pub fn sep() -> impl IntoElement {
    let tc = ToolbarColors::current();
    gpui_component::separator::Separator::vertical()
        .color(rgb(tc.sep))
        .relative()
        .w(px(1.))
        .h(px(20.))
        .mx(px(4.))
        .flex_none()
}

pub fn glyph(glyph: Icon, size: f32, color: u32) -> Div {
    let resolved = if is_dark() && color == INK { 0xe8eaee } else { color };
    let line: gpui::Hsla = rgb(resolved).into();
    icon(glyph, px(size), IconColors { line, fill: transparent_black(), accent: line })
}

pub fn duo_icon_colors(tc: ToolbarColors, on: bool) -> IconColors {
    let theme = Theme::ocean();
    if on {
        IconColors { line: rgb(tc.ink).into(), fill: theme.icon_fill, accent: rgb(tc.on_ink).into() }
    } else {
        theme.icon_colors(rgb(tc.ink).into())
    }
}

pub fn icon_toggle_button(
    id: impl Into<ElementId>,
    g: Icon,
    tip: &'static str,
    on: bool,
    open: bool,
) -> gpui_base::Button {
    let tc = ToolbarColors::current();
    let ink = if on { tc.on_ink } else { tc.ink };
    let id: ElementId = id.into();
    let group = SharedString::from(format!("fb-btn-{id:?}"));
    let lead = stateful_icon(
        group.clone(),
        g,
        px(16.),
        on || open,
        Theme::ocean().mono_icon_colors(rgb(ink).into()),
        duo_icon_colors(tc, on),
    );
    gpui_base::Button::new(id)
        .focusable(false)
        .role(gpui::Role::Button)
        .selected(on)
        .aria_toggled(if on { gpui::Toggled::True } else { gpui::Toggled::False })
        .accessibility_label(tip)
        .group(group)
        .relative()
        .h(px(32.))
        .min_w(px(32.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .gap(px(1.))
        .px(px(6.))
        .rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(rgb(tc.hover)))
        .when(open && !on, |b| b.bg(rgb(tc.open_bg)))
        .when(on, |b| b.bg(rgb(tc.on_bg)))
        .children(crate::ui::smoke_overlay::probe(match g { Icon::TbBold => "toolbar-bold", Icon::TbItalic => "toolbar-italic", _ => tip }))
        .child(lead)
        .when(!open, |b| b.tooltip_above(tooltip(tip)))
}

pub fn disabled_icon_button(g: Icon) -> gpui_base::Button {
    let dim = if is_dark() { 0x5a5a5a } else { 0x9a9994 };
    gpui_base::Button::new("toolbar-disabled")
        .focusable(false)
        .disabled(true)
        .h(px(28.))
        .min_w(px(28.))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .px(px(6.))
        .rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::Arrow)
        .child(glyph(g, 16., dim))
}
