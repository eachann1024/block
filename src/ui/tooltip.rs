use gpui::{
    AnyElement, AnyView, App, Bounds, Context, ElementId, FontWeight, Hsla, IntoElement, Pixels,
    Render, SharedString, Window, deferred, div, point, prelude::*, px, rgb, rgba,
    transparent_black,
};
use crate::icons::{Icon, IconColors, icon};
use crate::theme::is_dark;
use crate::ui::shortcut_label::{ShortcutStyle, shortcut_label};

const TOOLTIP_HEIGHT: f32 = 30.;
const TOOLTIP_PAD_X: f32 = 12.;
const TOOLTIP_FONT_SIZE: f32 = 13.;
const TOOLTIP_LINE_HEIGHT: f32 = 18.;
const TOOLTIP_ICON_SIZE: f32 = 14.;
const TOOLTIP_ICON_GAP: f32 = 5.;
const TOOLTIP_OFFSET_BELOW: f32 = 6.;

fn tooltip_style(color: Hsla) -> ShortcutStyle {
    ShortcutStyle::ui(
        px(TOOLTIP_FONT_SIZE),
        px(TOOLTIP_LINE_HEIGHT),
        FontWeight::MEDIUM,
        color,
    )
}

pub fn tooltip_pill(text: impl Into<SharedString>, id: impl Into<ElementId>) -> impl IntoElement {
    tooltip_pill_with_icon(None, text, id)
}

pub fn tooltip_pill_with_icon(
    lead_icon: Option<Icon>,
    text: impl Into<SharedString>,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    let text: SharedString = text.into();
    let dark = is_dark();
    let (bg, fg, border) = if dark {
        (rgb(0x202020), rgb(0xececec), rgba(0xffffff12))
    } else {
        (rgb(0x0f0f0f), rgb(0xffffff), rgba(0x0f0f0f00))
    };
    let fg_hsla: Hsla = fg.into();
    let style = tooltip_style(fg_hsla);
    let icon_colors = IconColors {
        line: fg_hsla,
        fill: transparent_black(),
        accent: fg_hsla,
    };
    let pill = div()
        .h(px(TOOLTIP_HEIGHT))
        .px(px(TOOLTIP_PAD_X))
        .rounded(crate::ui::radius::CONTROL)
        .bg(bg)
        .text_color(fg)
        .border_1()
        .border_color(border)
        .shadow_md()
        .flex()
        .items_center()
        .when(lead_icon.is_some(), |d| d.gap(px(TOOLTIP_ICON_GAP)))
        .whitespace_nowrap()
        .when_some(lead_icon, |d, glyph| {
            d.child(
                div()
                    .size(px(TOOLTIP_ICON_SIZE))
                    .flex_none()
                    .flex()
                    .items_center()
                    .justify_center()
                    .child(icon(glyph, px(TOOLTIP_ICON_SIZE), icon_colors)),
            )
        })
        .child(shortcut_label(text.as_ref(), style));
    crate::ui::motion::tooltip_enter(pill, id)
}

pub fn anchored_tooltip_above(
    target: Bounds<Pixels>,
    viewport_top: Pixels,
    text: impl Into<SharedString>,
    id: impl Into<ElementId>,
    window: &Window,
) -> AnyElement {
    anchored_tooltip_above_with_icon(target, viewport_top, None, text, id, window)
}

pub fn anchored_tooltip_above_with_icon(
    target: Bounds<Pixels>,
    viewport_top: Pixels,
    lead_icon: Option<Icon>,
    text: impl Into<SharedString>,
    id: impl Into<ElementId>,
    window: &Window,
) -> AnyElement {
    use crate::ui::placement::{AlignX, PlacementSpec, smart_anchored};
    let viewport = Bounds::new(
        point(px(0.), viewport_top.max(px(0.))),
        gpui::size(window.viewport_size().width,
            (window.viewport_size().height - viewport_top.max(px(0.))).max(px(0.))),
    );
    deferred(
        smart_anchored(PlacementSpec::above(target)
            .viewport(viewport)
            .align_x(AlignX::Center)
            .gap(px(TOOLTIP_OFFSET_BELOW)))
            .passive()
            .child(tooltip_pill_with_icon(lead_icon, text, id)),
    ).into_any_element()
}

pub struct GlobalTooltip {
    text: SharedString,
}

impl Render for GlobalTooltip {
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        tooltip_pill(self.text.clone(), "global-tooltip")
    }
}

pub fn build_tooltip(text: impl Into<SharedString>, _window: &mut Window, cx: &mut App) -> AnyView {
    let text = text.into();
    cx.new(|_| GlobalTooltip { text }).into()
}

pub fn tooltip(text: impl Into<SharedString>) -> impl Fn(&mut Window, &mut App) -> AnyView + 'static {
    let text = text.into();
    move |window, cx| build_tooltip(text.clone(), window, cx)
}

/// Keeps GPUI's hover delay/dismissal while anchoring to the entire control.
pub trait TooltipExt: StatefulInteractiveElement + ParentElement + Styled + Sized {
    fn tooltip_above(self, build: impl Fn(&mut Window, &mut App) -> AnyView + 'static) -> Self {
        let bounds = std::rc::Rc::new(std::cell::Cell::new(None));
        let tracked = bounds.clone();
        self.child(crate::ui::placement::track_bounds(tracked, false))
            .tooltip(move |window, cx| {
                let content = build(window, cx);
                let bounds = bounds.clone();
                cx.new(|_| PositionedTooltip { content, bounds }).into()
            })
    }
}

impl<T: StatefulInteractiveElement + ParentElement + Styled> TooltipExt for T {}

struct PositionedTooltip {
    content: AnyView,
    bounds: std::rc::Rc<std::cell::Cell<Option<Bounds<Pixels>>>>,
}

impl Render for PositionedTooltip {
    fn render(&mut self, window: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        use crate::ui::placement::{AlignX, PlacementSpec, smart_anchored};
        let target = self.bounds.get().unwrap_or_else(|| {
            Bounds::new(window.mouse_position(), gpui::size(px(0.), px(0.)))
        });
        smart_anchored(PlacementSpec::above(target)
            .align_x(AlignX::Center)
            .gap(px(TOOLTIP_OFFSET_BELOW)))
            .passive()
            .child(self.content.clone())
    }
}
