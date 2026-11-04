use gpui::{
    AnyElement, AnyView, App, Bounds, Context, ElementId, FontWeight, Hsla, IntoElement, Pixels,
    Render, SharedString, Window, anchored, deferred, div, point, prelude::*, px, rgb, rgba,
    transparent_black,
};
use crate::theme::is_dark;
use crate::ui::shortcut_label::{ShortcutStyle, measure_width, shortcut_label};

const TOOLTIP_HEIGHT: f32 = 30.;
const TOOLTIP_PAD_X: f32 = 12.;
const TOOLTIP_BORDER: f32 = 1.;
const TOOLTIP_CHROME_W: f32 = TOOLTIP_PAD_X * 2. + TOOLTIP_BORDER * 2.;
const TOOLTIP_FONT_SIZE: f32 = 13.;
const TOOLTIP_LINE_HEIGHT: f32 = 18.;
const TOOLTIP_OFFSET_ABOVE: f32 = 36.;
const TOOLTIP_OFFSET_BELOW: f32 = 6.;
const TOOLTIP_WINDOW_MARGIN: f32 = 8.;

fn tooltip_style(color: Hsla) -> ShortcutStyle {
    ShortcutStyle::ui(
        px(TOOLTIP_FONT_SIZE),
        px(TOOLTIP_LINE_HEIGHT),
        FontWeight::MEDIUM,
        color,
    )
}

pub fn tooltip_pill(text: impl Into<SharedString>, id: impl Into<ElementId>) -> impl IntoElement {
    let text: SharedString = text.into();
    let dark = is_dark();
    let (bg, fg, border) = if dark {
        (rgb(0x202020), rgb(0xececec), rgba(0xffffff12))
    } else {
        (rgb(0x0f0f0f), rgb(0xffffff), rgba(0x0f0f0f00))
    };
    let style = tooltip_style(fg.into());
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
        .whitespace_nowrap()
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
    let text: SharedString = text.into();
    let style = tooltip_style(transparent_black());
    let content_w = measure_width(text.as_ref(), &style, window);
    let pill_w = content_w + px(TOOLTIP_CHROME_W);
    let x = target.center().x - pill_w / 2.;
    let above = target.top() - px(TOOLTIP_OFFSET_ABOVE);
    let y = if above >= viewport_top { above } else { target.bottom() + px(TOOLTIP_OFFSET_BELOW) };
    deferred(
        anchored()
            .position(point(x, y))
            .snap_to_window_with_margin(px(TOOLTIP_WINDOW_MARGIN))
            .child(tooltip_pill(text, id)),
    )
    .into_any_element()
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
