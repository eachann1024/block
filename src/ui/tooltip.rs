use gpui::{
    AnyView, App, Context, FontWeight, IntoElement, Render, SharedString, Window, div, prelude::*,
    px, rgb, rgba,
};
use crate::theme::is_dark;

pub struct GlobalTooltip {
    text: SharedString,
}

impl Render for GlobalTooltip {
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
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
            .child(self.text.clone());
        crate::ui::motion::tooltip_enter(pill, "global-tooltip")
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
