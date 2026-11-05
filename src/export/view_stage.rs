//! Preview stage for `ExportView`. Zoom controls live in the window title bar.
use gpui::{
    AnyElement, Context, Hsla, IntoElement, ParentElement, Styled, div, img, prelude::*, px,
};

use super::format::ExportFormat;
use super::layout::SCALE;
use super::view::ExportView;
use super::view_widgets::{fmt_int, png_size};
use crate::theme::{Theme, tr};

pub(super) const PREVIEW_PAD_X: f32 = 32.;
const PREVIEW_PAD_TOP: f32 = 36.;
const PREVIEW_PAD_BOTTOM: f32 = 36.;
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
        .rounded(crate::ui::radius::SURFACE)
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
    pub(super) fn preview(&self, room: f32, _cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let (dims, fit_scale) = self.fit_scale(room);
        let scale = self.zoom.unwrap_or(fit_scale);
        let mut centered_state = false;
        let body: AnyElement = match (&self.png, &self.error) {
            (Some(png), _) => {
                let src: gpui::ImageSource = self
                    .preview_img
                    .clone()
                    .map_or_else(|| png.clone().into(), Into::into);
                let picture = match dims {
                    Some((w, h)) => img(src).w(px(w / SCALE * scale)).h(px(h / SCALE * scale)),
                    None => img(src).w_full(),
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
        };
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
    }

    fn fit_scale(&self, room: f32) -> (Option<(f32, f32)>, f32) {
        let dims = self.png.as_ref().and_then(|png| png_size(&png.bytes));
        let scale = dims
            .map(|(w, _)| (room / (w / SCALE)).clamp(MIN_SCALE, 1.))
            .unwrap_or_else(|| (room / 816.).clamp(MIN_SCALE, 1.));
        (dims, scale)
    }

    pub(super) fn stage_labels(&self, room: f32) -> (String, String, f32) {
        let (dims, fit_scale) = self.fit_scale(room);
        let px_label = |n: f32| fmt_int(n.round().max(0.) as u32);
        let dim_label = match self.format {
            ExportFormat::Png => match dims {
                Some((pw, ph)) => format!("{} × {} px · {}", px_label(pw), px_label(ph), tr("2×", "2×")),
                None => tr("816 px 纸宽 · 2×", "816 px page · 2×").to_string(),
            },
            ExportFormat::Pdf => match dims {
                Some((pw, ph)) => format!("PDF · {} × {} pt", px_label(pw / SCALE), px_label(ph / SCALE)),
                None => tr("PDF · 高清纸面", "PDF · HD Page").to_string(),
            },
            ExportFormat::Word => format!("Word (.docx) · {}", self.style.label()),
            ExportFormat::Markdown => tr("Markdown (.md) · 纯文本", "Markdown (.md)").to_string(),
            ExportFormat::Html => format!("HTML (.html) · {}", self.style.label()),
        };
        let zoom_label = self.zoom.map_or_else(|| tr("适应", "Fit").into(), |z| format!("{}%", (z * 100.).round() as i32));
        (dim_label, zoom_label, fit_scale)
    }
}
