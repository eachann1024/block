//! Preview stage and floating bottom HUD (dimensions pill + zoom controls) for `ExportView`.
use gpui::{
    AnyElement, ClickEvent, Context, Hsla, IntoElement, ParentElement, Styled, div, img,
    prelude::*, px,
};

use super::layout::SCALE;
use super::view::ExportView;
use super::view_widgets::{fmt_int, png_size};
use crate::theme::Theme;

pub(super) const PREVIEW_PAD_X: f32 = 32.;
const PREVIEW_PAD_TOP: f32 = 36.;
const PREVIEW_PAD_BOTTOM: f32 = 68.;
pub(super) const MIN_SCALE: f32 = 0.75;
const DANGER: u32 = 0xc0392b;

fn c(value: u32) -> Hsla {
    gpui::rgb(value).into()
}

impl ExportView {
    pub(super) fn preview(&self, room: f32, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let dims = self.png.as_ref().and_then(|png| png_size(&png.bytes));
        let fit_scale = dims
            .map(|(w, _)| (room / (w / SCALE)).clamp(MIN_SCALE, 1.))
            .unwrap_or_else(|| (room / 816.).clamp(MIN_SCALE, 1.));
        let scale = self.zoom.unwrap_or(fit_scale);
        let body: AnyElement = match (&self.png, &self.error) {
            (Some(png), _) => {
                let picture = match dims {
                    Some((w, h)) => {
                        img(png.clone()).w(px(w / SCALE * scale)).h(px(h / SCALE * scale))
                    }
                    None => img(png.clone()).w_full(),
                };
                div()
                    .flex_none()
                    .shadow_lg()
                    .when(self.rendering, |d| d.opacity(0.55))
                    .child(picture)
                    .into_any_element()
            }
            (None, Some(msg)) => div()
                .w_full()
                .max_w(px(480.))
                .text_size(px(13.))
                .text_color(c(DANGER))
                .child(msg.clone())
                .into_any_element(),
            (None, None) => div()
                .text_size(px(13.))
                .text_color(theme.text_secondary)
                .child("正在生成…")
                .into_any_element(),
        };
        let dim_label = match dims {
            Some((pw, ph)) => {
                format!("{} × {} px · 2× 高清", fmt_int(pw as u32), fmt_int(ph as u32))
            }
            None => "816 px 纸宽 · 2× 高清".to_string(),
        };
        let zoom_label = self
            .zoom
            .map_or_else(|| "适应".into(), |z| format!("{}%", (z * 100.).round() as i32));
        let scroller = div()
            .id("export-preview")
            .size_full()
            .overflow_scroll()
            .child(
                div()
                    .min_h_full()
                    .min_w_full()
                    .flex()
                    .justify_center()
                    .items_start()
                    .px(px(PREVIEW_PAD_X))
                    .pt(px(PREVIEW_PAD_TOP))
                    .pb(px(PREVIEW_PAD_BOTTOM))
                    .child(body),
            );
        div()
            .relative()
            .flex_1()
            .min_w_0()
            .h_full()
            .bg(theme.preview_bg)
            .child(scroller)
            .child(self.stage_hud(dim_label, zoom_label, fit_scale, cx))
    }

    fn stage_hud(
        &self,
        dim_label: String,
        zoom_label: String,
        fit_scale: f32,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let theme = Theme::ocean();
        let pill = div()
            .bg(theme.paper)
            .border_1()
            .border_color(c(0xe2e4e8))
            .rounded(px(6.))
            .px(px(10.))
            .py(px(4.))
            .text_size(px(12.))
            .text_color(theme.text_secondary)
            .shadow_sm()
            .child(dim_label);
        let zbtn = |id: &'static str, s: &'static str| {
            div()
                .id(id)
                .size(px(24.))
                .flex()
                .items_center()
                .justify_center()
                .rounded(px(6.))
                .cursor_pointer()
                .text_size(px(14.))
                .text_color(theme.text_secondary)
                .hover(|h| h.bg(theme.hover).text_color(theme.text))
                .child(s)
        };
        let minus = zbtn("export-zoom-out", "−")
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| this.zoom_out(fit_scale, cx)));
        let plus = zbtn("export-zoom-in", "+")
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| this.zoom_in(fit_scale, cx)));
        let reset = div()
            .id("export-zoom-reset")
            .h(px(24.))
            .px(px(8.))
            .flex()
            .items_center()
            .justify_center()
            .rounded(px(6.))
            .cursor_pointer()
            .text_size(px(12.))
            .text_color(theme.text)
            .hover(|h| h.bg(theme.hover))
            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.zoom_reset(cx)))
            .child(zoom_label);
        let zoom_bar = div()
            .bg(theme.paper)
            .border_1()
            .border_color(c(0xe2e4e8))
            .rounded(px(8.))
            .p(px(2.))
            .flex()
            .items_center()
            .gap(px(2.))
            .shadow_sm()
            .child(minus)
            .child(reset)
            .child(plus);
        div()
            .absolute()
            .left(px(16.))
            .right(px(16.))
            .bottom(px(12.))
            .flex()
            .items_center()
            .justify_between()
            .child(pill)
            .child(zoom_bar)
    }
}
