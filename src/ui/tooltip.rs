use gpui::{
    AnyElement, AnyView, App, Bounds, Context, ElementId, Font, FontFeatures, FontStyle,
    FontWeight, IntoElement, Pixels, Render, SharedString, TextRun, Window, anchored, deferred,
    div, point, prelude::*, px, rgb, rgba, transparent_black,
};
use crate::theme::is_dark;

pub fn tooltip_pill(text: impl Into<SharedString>, id: impl Into<ElementId>) -> impl IntoElement {
    let dark = is_dark();
    let (bg, fg, border) = if dark {
        (rgb(0x202020), rgb(0xececec), rgba(0xffffff12))
    } else {
        (rgb(0x0f0f0f), rgb(0xffffff), rgba(0x0f0f0f00))
    };
    let pill = div()
        .h(px(30.))
        .px(px(12.))
        .rounded(px(8.))
        .bg(bg)
        .text_color(fg)
        .border_1()
        .border_color(border)
        .shadow_md()
        .flex()
        .items_center()
        .text_size(px(13.))
        .line_height(px(18.))
        .font_weight(FontWeight::MEDIUM)
        .whitespace_nowrap()
        .child(text.into());
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
    let run = TextRun {
        len: text.len(),
        font: Font {
            family: ".SystemUIFont".into(),
            weight: FontWeight::MEDIUM,
            style: FontStyle::Normal,
            features: FontFeatures::default(),
            fallbacks: None,
        },
        color: transparent_black(),
        background_color: None,
        underline: None,
        strikethrough: None,
    };
    let line = window.text_system().shape_line(text.clone(), px(13.), &[run], None);
    let pill_w = line.width + px(26.);
    let x = target.center().x - pill_w / 2.;
    let above = target.top() - px(36.);
    let y = if above >= viewport_top { above } else { target.bottom() + px(6.) };
    deferred(
        anchored()
            .position(point(x, y))
            .snap_to_window_with_margin(px(8.))
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
