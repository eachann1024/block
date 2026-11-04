//! Rendering of the "生成图片" view (design A-Export): header, preview, option sidebar.
use std::rc::Rc;

use gpui::{
    ClickEvent, Context, FontWeight, Hsla, IntoElement, ParentElement, Render, Styled, Window, div,
    prelude::*, px,
};

use super::palette::ExportTheme;
use super::view::ExportView;
use super::view_stage::{MIN_SCALE, PREVIEW_PAD_X};
use super::view_widgets::{button, section, segmented, style_grid, switch_row};
use crate::icons::{Icon, IconColors, icon};
use crate::theme::{Theme, tr};

const OPTIONS_WIDTH: f32 = 300.;

fn c(value: u32) -> Hsla {
    gpui::rgb(value).into()
}

impl Render for ExportView {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let room =
            f32::from(window.viewport_size().width) - OPTIONS_WIDTH - 1. - PREVIEW_PAD_X * 2.;
        let fit_scale = (room / 816.).clamp(MIN_SCALE, 1.);
        div()
            .id("export-view")
            .track_focus(&self.focus)
            .on_key_down(cx.listener(move |this, event: &gpui::KeyDownEvent, window, cx| {
                let ks = &event.keystroke;
                if ks.key == "escape" {
                    this.back(window, cx);
                } else if ks.modifiers.platform && ks.key == "c" {
                    this.copy(cx);
                } else if ks.modifiers.platform && ks.key == "s" {
                    this.save(cx);
                } else if !ks.modifiers.platform && (ks.key == "=" || ks.key == "+") {
                    this.zoom_in(fit_scale, cx);
                } else if !ks.modifiers.platform && ks.key == "-" {
                    this.zoom_out(fit_scale, cx);
                } else if !ks.modifiers.platform && ks.key == "0" {
                    this.zoom_reset(cx);
                } else {
                    return;
                }
                cx.stop_propagation();
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
                    .child(self.preview(room, cx))
                    .child(self.sidebar(cx)),
            )
    }
}

impl ExportView {
    fn header(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let back_icon = icon(Icon::ChevronLeft, px(16.), theme.icon_colors(theme.text_secondary));
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
        let center = div()
            .flex_1()
            .min_w_0()
            .flex()
            .items_center()
            .justify_center()
            .gap(px(8.))
            .child(back)
            .child(div().text_size(px(13.)).font_weight(FontWeight(560.)).child(tr("生成图片", "Export Image")))
            .child(
                div()
                    .min_w_0()
                    .truncate()
                    .text_size(px(13.))
                    .text_color(theme.text_secondary)
                    .child(self.title.clone()),
            );
        div()
            .h(px(40.))
            .flex_none()
            .flex()
            .items_center()
            .px(px(16.))
            .gap(px(12.))
            .child(div().w(px(52.)).flex_none())
            .child(center)
            .child(div().w(px(52.)).flex_none())
    }

    fn sidebar(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let has_sel = self.selection.is_some();
        let is_dark = self.style.is_dark();
        let scope = segmented(
            "scope",
            [(tr("整篇", "Full Note"), !self.use_selection, true), (tr("选中生成", "Selection"), self.use_selection, has_sel)],
            true,
            Rc::new(cx.listener(|this, idx: &usize, _, cx| this.set_scope(*idx == 1, cx))),
        );
        let appearance = segmented(
            "theme",
            [(tr("浅色 · 4", "Light · 4"), !is_dark, true), (tr("深色 · 4", "Dark · 4"), is_dark, true)],
            false,
            Rc::new(cx.listener(|this, idx: &usize, _, cx| this.set_dark(*idx == 1, cx))),
        );
        let styles = style_grid(
            self.style,
            Rc::new(cx.listener(|this, style: &ExportTheme, _, cx| this.set_style(*style, cx))),
        );
        let theme_sec = div().flex().flex_col().gap(px(8.)).child(appearance).child(styles);
        let width_box = div()
            .h(px(36.))
            .flex()
            .items_center()
            .justify_between()
            .px(px(10.))
            .rounded(px(8.))
            .bg(theme.sidebar)
            .text_size(px(13.))
            .text_color(theme.text_secondary)
            .child(tr("标准 · 720", "Standard · 720"))
            .child(div().text_size(px(12.)).child(tr("816 px 纸宽", "816 px page")));
        let content_sec = div()
            .flex()
            .flex_col()
            .gap(px(4.))
            .child(switch_row(
                "export-title-toggle",
                tr("标题", "Title"),
                self.show_title,
                Rc::new(cx.listener(|this, _: &ClickEvent, _, cx| this.toggle_title(cx))),
            ))
            .child(switch_row(
                "export-watermark",
                tr("底部水印", "Watermark"),
                self.watermark,
                Rc::new(cx.listener(|this, _: &ClickEvent, _, cx| this.toggle_watermark(cx))),
            ));
        let ready = self.ready();
        let has_saved = self.saved_path.is_some();
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
            .child(section(tr("范围", "Scope"), scope))
            .child(section(tr("主题", "Theme"), theme_sec))
            .child(section(tr("宽度", "Width"), width_box))
            .child(section(tr("内容", "Content"), content_sec))
            .child(div().flex_1())
            .when_some(self.notice.clone(), |d, text| {
                d.child(
                    div()
                        .px(px(10.))
                        .py(px(8.))
                        .rounded(px(8.))
                        .bg(c(0xf4f7fd))
                        .text_size(px(12.))
                        .text_color(theme.text_secondary)
                        .flex()
                        .items_center()
                        .justify_between()
                        .gap(px(8.))
                        .child(div().flex_1().min_w_0().truncate().child(text))
                        .when(has_saved, |row| {
                            row.child(
                                div()
                                    .id("export-reveal")
                                    .flex_none()
                                    .cursor_pointer()
                                    .font_weight(FontWeight(500.))
                                    .text_color(theme.accent)
                                    .on_click(cx.listener(|this, _: &ClickEvent, _, _| this.reveal_saved()))
                                    .child(tr("在访达中显示", "Show in Finder")),
                            )
                        }),
                )
            })
            .child(self.save_button(ready, cx))
            .child(self.copy_button(ready, cx))
    }

    fn save_button(&self, ready: bool, cx: &mut Context<Self>) -> impl IntoElement {
        let mut fill = c(0xffffff);
        fill.a = 0.35;
        let colors = IconColors { line: c(0xffffff), fill, accent: c(0xffffff) };
        let glyph = icon(Icon::Download, px(14.), colors);
        button("export-save", tr("保存 PNG", "Save PNG"), glyph, true, ready)
            .when(ready, |b| b.on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.save(cx))))
    }

    fn copy_button(&self, ready: bool, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let glyph = icon(
            if self.copied { Icon::Check } else { Icon::Copy },
            px(14.),
            theme.icon_colors(theme.text),
        );
        let label = if self.copied { tr("已复制", "Copied") } else { tr("复制", "Copy") };
        button("export-copy", label, glyph, false, ready)
            .when(ready, |b| b.on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.copy(cx))))
    }
}
