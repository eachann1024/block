//! Rendering of the "生成图片" view (design A-Export): header, preview, option sidebar.
use std::rc::Rc;

use gpui::{
    AnyElement, ClickEvent, Context, FontWeight, Hsla, IntoElement, ParentElement, Render, Styled,
    Window, div, img, prelude::*, px,
};

use super::layout::SCALE;
use super::palette::ExportTheme;
use super::view::ExportView;
use super::view_widgets::{button, png_size, section, segmented, style_grid};
use crate::icons::{Icon, IconColors, icon};
use crate::theme::Theme;

// Design A-Export: 300px options panel, preview padded 48/32, shown at the PNG's logical
// size (pixels / SCALE, so 17px body text is 17px) and only shrunk to fit, never below MIN_SCALE.
const OPTIONS_WIDTH: f32 = 300.;
const PREVIEW_PAD_X: f32 = 32.;
const PREVIEW_PAD_Y: f32 = 48.;
const MIN_SCALE: f32 = 0.75;
const DANGER: u32 = 0xc0392b;

fn c(value: u32) -> Hsla {
    gpui::rgb(value).into()
}

impl Render for ExportView {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let room =
            f32::from(window.viewport_size().width) - OPTIONS_WIDTH - 1. - PREVIEW_PAD_X * 2.;
        div()
            .id("export-view")
            .track_focus(&self.focus)
            .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, window, cx| {
                if event.keystroke.key == "escape" {
                    this.back(window, cx);
                    cx.stop_propagation();
                }
            }))
            .size_full()
            .flex()
            .flex_col()
            .bg(theme.shell)
            .text_color(theme.text)
            .text_size(px(14.))
            .child(self.header(cx))
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .child(self.preview(room))
                    .child(self.sidebar(cx)),
            )
    }
}

impl ExportView {
    fn header(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let back_icon = icon(
            Icon::ChevronLeft,
            px(16.),
            theme.icon_colors(theme.text_secondary),
        );
        let back = div()
            .id("export-back")
            .size(px(28.))
            .flex_none()
            .flex()
            .items_center()
            .justify_center()
            .rounded(px(6.))
            .cursor_pointer()
            .hover(|s| s.bg(theme.hover))
            .on_click(cx.listener(|this, _: &ClickEvent, window, cx| this.back(window, cx)))
            .child(back_icon);
        let label = div()
            .text_size(px(13.))
            .font_weight(FontWeight(560.))
            .child("生成图片");
        let title = div()
            .min_w_0()
            .truncate()
            .text_size(px(13.))
            .text_color(theme.text_secondary)
            .child(self.title.clone());
        div()
            .h(px(40.))
            .flex_none()
            .flex()
            .items_center()
            .px(px(16.))
            .gap(px(12.))
            .child(div().w(px(52.)).flex_none())
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .items_center()
                    .justify_center()
                    .gap(px(8.))
                    .child(back)
                    .child(label)
                    .child(title),
            )
            .child(div().w(px(52.)).flex_none())
    }

    fn preview(&self, room: f32) -> impl IntoElement {
        let theme = Theme::ocean();
        let body: AnyElement = match (&self.png, &self.error) {
            (Some(png), _) => {
                let picture = match png_size(&png.bytes) {
                    Some((w, h)) => {
                        let (w, h) = (w / SCALE, h / SCALE);
                        let scale = (room / w).clamp(MIN_SCALE, 1.);
                        img(png.clone()).w(px(w * scale)).h(px(h * scale))
                    }
                    None => img(png.clone()).w_full(),
                };
                div()
                    .flex_none()
                    .when(self.rendering, |d| d.opacity(0.55))
                    .child(picture)
                    .into_any_element()
            }
            (None, Some(message)) => div()
                .w_full()
                .max_w(px(480.))
                .text_size(px(13.))
                .text_color(c(DANGER))
                .child(message.clone())
                .into_any_element(),
            (None, None) => div()
                .text_size(px(13.))
                .text_color(theme.text_secondary)
                .child("正在生成…")
                .into_any_element(),
        };
        div()
            .id("export-preview")
            .flex_1()
            .min_w_0()
            .h_full()
            .bg(theme.preview_bg)
            .overflow_scroll()
            .child(
                div()
                    .min_h_full()
                    .min_w_full()
                    .flex()
                    .justify_center()
                    .items_start()
                    .px(px(PREVIEW_PAD_X))
                    .py(px(PREVIEW_PAD_Y))
                    .child(body),
            )
    }

    fn sidebar(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let has_sel = self.selection.is_some();
        let is_dark = self.style.is_dark();
        let scope = segmented(
            "scope",
            [
                ("整篇", !self.use_selection, true),
                ("选中生成", self.use_selection, has_sel),
            ],
            true,
            Rc::new(cx.listener(|this, index: &usize, _, cx| this.set_scope(*index == 1, cx))),
        );
        let appearance = segmented(
            "theme",
            [("浅色", !is_dark, true), ("深色", is_dark, true)],
            false,
            Rc::new(cx.listener(|this, index: &usize, _, cx| this.set_dark(*index == 1, cx))),
        );
        let styles = style_grid(
            self.style,
            Rc::new(cx.listener(|this, style: &ExportTheme, _, cx| this.set_style(*style, cx))),
        );
        let theme_sec = div()
            .flex()
            .flex_col()
            .gap(px(8.))
            .child(appearance)
            .child(styles);
        let width_box = div()
            .h(px(36.))
            .flex()
            .items_center()
            .px(px(10.))
            .rounded(px(8.))
            .bg(theme.sidebar)
            .text_size(px(13.))
            .text_color(theme.text_secondary)
            .child("标准 · 720");
        let ready = self.ready();
        div()
            .id("export-options")
            .w(px(OPTIONS_WIDTH))
            .flex_none()
            .h_full()
            .overflow_y_scroll()
            .bg(theme.paper)
            .border_l_1()
            .border_color(theme.sidebar_border)
            .p(px(16.))
            .flex()
            .flex_col()
            .gap(px(18.))
            .child(section("范围", scope))
            .child(section("主题", theme_sec))
            .child(section("宽度", width_box))
            .child(self.watermark_row(cx))
            .child(div().flex_1())
            .when_some(self.notice.clone(), |d, text| {
                d.child(
                    div()
                        .text_size(px(12.))
                        .text_color(theme.text_secondary)
                        .child(text),
                )
            })
            .child(self.save_button(ready, cx))
            .child(self.copy_button(ready, cx))
    }

    fn watermark_row(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let on = self.watermark;
        let knob = div()
            .absolute()
            .top(px(2.))
            .left(px(if on { 14. } else { 2. }))
            .size(px(14.))
            .rounded(px(7.))
            .bg(c(0xffffff));
        div()
            .h(px(32.))
            .flex()
            .items_center()
            .justify_between()
            .child(div().text_size(px(13.)).child("水印"))
            .child(
                div()
                    .id("export-watermark")
                    .relative()
                    .w(px(30.))
                    .h(px(18.))
                    .rounded(px(9.))
                    .cursor_pointer()
                    .bg(if on { theme.accent } else { c(0xd5d8de) })
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.toggle_watermark(cx)))
                    .child(knob),
            )
    }

    fn save_button(&self, ready: bool, cx: &mut Context<Self>) -> impl IntoElement {
        let mut fill = c(0xffffff);
        fill.a = 0.35;
        let colors = IconColors {
            line: c(0xffffff),
            fill,
            accent: c(0xffffff),
        };
        let glyph = icon(Icon::Download, px(14.), colors);
        button("export-save", "保存 PNG", glyph, true, ready).when(ready, |b| {
            b.on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.save(cx)))
        })
    }

    fn copy_button(&self, ready: bool, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let colors = theme.icon_colors(theme.text);
        let glyph = icon(
            if self.copied { Icon::Check } else { Icon::Copy },
            px(14.),
            colors,
        );
        let label = if self.copied { "已复制" } else { "复制" };
        button("export-copy", label, glyph, false, ready).when(ready, |b| {
            b.on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.copy(cx)))
        })
    }
}
