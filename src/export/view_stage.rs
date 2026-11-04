//! Preview stage and floating bottom HUD (dimensions pill + zoom controls) for `ExportView`.
use gpui::{
    AnyElement, ClickEvent, Context, FontWeight, Hsla, IntoElement, ParentElement, Styled, div,
    img, prelude::*, px,
};

use super::format::{ExportFormat, render_markdown};
use super::layout::SCALE;
use super::view::ExportView;
use super::view_widgets::{fmt_int, png_size};
use crate::theme::{Theme, tr};

pub(super) const PREVIEW_PAD_X: f32 = 32.;
const PREVIEW_PAD_TOP: f32 = 36.;
const PREVIEW_PAD_BOTTOM: f32 = 76.;
pub(super) const MIN_SCALE: f32 = 0.75;
const DANGER: u32 = 0xc0392b;

fn c(value: u32) -> Hsla {
    gpui::rgb(value).into()
}

fn state_card(text: impl IntoElement, is_error: bool) -> AnyElement {
    let theme = Theme::ocean();
    div()
        .max_w(px(520.))
        .px(px(20.))
        .py(px(16.))
        .rounded(px(10.))
        .bg(theme.paper)
        .border_1()
        .border_color(if is_error { c(0xe8b4ae) } else { theme.sidebar_border })
        .shadow_sm()
        .text_size(px(14.))
        .text_color(if is_error { c(DANGER) } else { theme.text_secondary })
        .child(text)
        .into_any_element()
}

impl ExportView {
    pub(super) fn preview(&self, room: f32, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let dims = self.png.as_ref().and_then(|png| png_size(&png.bytes));
        let fit_scale = dims
            .map(|(w, _)| (room / (w / SCALE)).clamp(MIN_SCALE, 1.))
            .unwrap_or_else(|| (room / 816.).clamp(MIN_SCALE, 1.));
        let scale = self.zoom.unwrap_or(fit_scale);
        let mut centered_state = false;
        let body: AnyElement = if self.format == ExportFormat::Markdown {
            match self.active_blocks() {
                Ok(blocks) => {
                    let md = render_markdown(&blocks, &self.render_options());
                    let pal = self.style.palette();
                    let bg = c(pal.inner_canvas.0 & 0xff_ff_ff);
                    let fg = c(pal.text.0 & 0xff_ff_ff);
                    let border = c(pal.code_border.0 & 0xff_ff_ff);
                    div()
                        .w(px((816. * scale).round()))
                        .p(px((48. * scale).round()))
                        .rounded(px(8.))
                        .border_1()
                        .border_color(border)
                        .bg(bg)
                        .text_color(fg)
                        .shadow_lg()
                        .font_family("Menlo")
                        .text_size(px((14. * scale).clamp(12., 18.)))
                        .line_height(px((22. * scale).clamp(18., 30.)))
                        .child(md)
                        .into_any_element()
                }
                Err(msg) => {
                    centered_state = true;
                    state_card(msg, true)
                }
            }
        } else {
            match (&self.png, &self.error) {
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
                (None, Some(msg)) => {
                    centered_state = true;
                    state_card(msg.clone(), true)
                }
                (None, None) => {
                    centered_state = true;
                    state_card(tr("正在生成预览…", "Rendering preview…"), false)
                }
            }
        };
        let dim_label = match self.format {
            ExportFormat::Png => match dims {
                Some((pw, ph)) => format!(
                    "{} × {} px · {}",
                    fmt_int(pw as u32),
                    fmt_int(ph as u32),
                    tr("2×", "2×")
                ),
                None => tr("816 px 纸宽 · 2×", "816 px page · 2×").to_string(),
            },
            ExportFormat::Pdf => match dims {
                Some((pw, ph)) => format!(
                    "PDF · {} × {} pt",
                    fmt_int((pw / SCALE) as u32),
                    fmt_int((ph / SCALE) as u32),
                ),
                None => tr("PDF · 高清纸面", "PDF · HD Page").to_string(),
            },
            ExportFormat::Word => format!("Word (.docx) · {}", self.style.label()),
            ExportFormat::Markdown => tr("Markdown (.md) · 纯文本", "Markdown (.md)").to_string(),
            ExportFormat::Html => format!("HTML (.html) · {}", self.style.label()),
        };
        let zoom_label = self
            .zoom
            .map_or_else(|| tr("适应", "Fit").into(), |z| format!("{}%", (z * 100.).round() as i32));
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
                    .when(centered_state, |d| d.items_center())
                    .when(!centered_state, |d| d.items_start())
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
        let zbtn = |id: &'static str, s: &'static str| {
            div()
                .id(id)
                .size(px(28.))
                .flex()
                .items_center()
                .justify_center()
                .rounded(px(6.))
                .cursor_pointer()
                .text_size(px(15.))
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
            .h(px(28.))
            .px(px(8.))
            .flex()
            .items_center()
            .justify_center()
            .rounded(px(6.))
            .cursor_pointer()
            .text_size(px(14.))
            .font_weight(FontWeight(500.))
            .text_color(theme.text)
            .hover(|h| h.bg(theme.hover))
            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.zoom_reset(cx)))
            .child(zoom_label);
        let bar = div()
            .occlude()
            .bg(theme.paper)
            .border_1()
            .border_color(theme.sidebar_border)
            .rounded(px(10.))
            .pl(px(12.))
            .pr(px(6.))
            .py(px(4.))
            .flex()
            .items_center()
            .gap(px(10.))
            .shadow_md()
            .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(dim_label))
            .child(div().w(px(1.)).h(px(16.)).bg(theme.hairline))
            .child(div().flex().items_center().gap(px(2.)).child(minus).child(reset).child(plus));
        div()
            .absolute()
            .left(px(16.))
            .right(px(16.))
            .bottom(px(16.))
            .flex()
            .justify_center()
            .child(bar)
    }
}
