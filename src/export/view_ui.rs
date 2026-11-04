//! Rendering of the "生成图片" view (design A-Export): header, preview, option sidebar.
use std::rc::Rc;

use gpui::{
    ClickEvent, Context, FontWeight, Hsla, IntoElement, ParentElement, Render, Styled, Window, div,
    prelude::*, px,
};

use super::format::ExportFormat;
use super::palette::ExportTheme;
use super::view::ExportView;
use super::view_stage::{MIN_SCALE, PREVIEW_PAD_X};
use super::view_widgets::{button, format_bar, section, segmented, style_grid, switch_row};
use crate::icons::{Icon, IconColors, icon, stateful_icon};
use crate::theme::{Theme, tr};
use crate::ui::icon_button::back_button;

const OPTIONS_WIDTH: f32 = 320.;

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
        let back = back_button(
            "export-back",
            tr("返回", "Back"),
            cx.listener(|this, _: &ClickEvent, window, cx| this.back(window, cx)),
        );
        let left = div()
            .flex_none()
            .flex()
            .items_center()
            .gap(px(10.))
            .min_w(px(if cfg!(target_os = "macos") { 200. } else { 140. }))
            .pl(px(if cfg!(target_os = "macos") { 78. } else { 12. }))
            .when(cfg!(target_os = "macos"), |d| {
                d.child(div().w(px(1.)).h(px(16.)).bg(theme.sidebar_border))
            })
            .child(back);
        let center = div()
            .id("export-header-drag")
            .flex_1()
            .min_w_0()
            .h_full()
            .flex()
            .items_center()
            .justify_center()
            .gap(px(8.))
            .on_click(|event, window, _| {
                if event.click_count() == 2 {
                    window.titlebar_double_click();
                }
            })
            .child(icon(Icon::Image, px(15.), theme.mono_icon_colors(theme.text_secondary)))
            .child(
                div()
                    .min_w_0()
                    .truncate()
                    .text_size(px(14.))
                    .font_weight(FontWeight(560.))
                    .text_color(theme.text)
                    .child(self.title.clone()),
            )
            .child(
                div()
                    .flex_none()
                    .text_size(px(14.))
                    .text_color(theme.text_secondary)
                    .child(tr("· 导出预览", "· Export Preview")),
            );
        let esc_badge = div().h(px(22.)).px(px(7.)).flex_none().flex().items_center()
            .rounded(px(5.)).bg(theme.paper).border_1().border_color(theme.btn_border)
            .text_size(px(12.)).text_color(theme.text_secondary).child("Esc");
        let right = div().flex_none().flex().items_center().justify_end().gap(px(8.))
            .min_w(px(if cfg!(target_os = "macos") { 200. } else { 140. }))
            .pr(px(if cfg!(target_os = "macos") { 16. } else { 0. }))
            .child(esc_badge)
            .child(crate::ui::window_controls::window_controls("export-win", true, true));
        div().h(px(40.)).flex_none().flex().items_center().bg(theme.shell)
            .border_b_1().border_color(theme.sidebar_border)
            .window_control_area(gpui::WindowControlArea::Drag)
            .child(left).child(center).child(right)
    }

    fn sidebar(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let has_sel = self.selection.is_some();
        let is_dark = self.style.is_dark();
        let formats = format_bar(
            self.format,
            Rc::new(cx.listener(|this, fmt: &ExportFormat, _, cx| this.set_format(*fmt, cx))),
        );
        let scope = segmented(
            "scope",
            [
                (tr("整篇笔记", "Full Note"), !self.use_selection, true),
                (tr("选中导出", "Selection"), self.use_selection, has_sel),
            ],
            Rc::new(cx.listener(|this, idx: &usize, _, cx| this.set_scope(*idx == 1, cx))),
        );
        let appearance = segmented(
            "theme",
            [
                (tr("浅色 · 4", "Light · 4"), !is_dark, true),
                (tr("深色 · 4", "Dark · 4"), is_dark, true),
            ],
            Rc::new(cx.listener(|this, idx: &usize, _, cx| this.set_dark(*idx == 1, cx))),
        );
        let styles = style_grid(
            self.style,
            Rc::new(cx.listener(|this, style: &ExportTheme, _, cx| this.set_style(*style, cx))),
        );
        let theme_sec = div().flex().flex_col().gap(px(8.)).child(appearance).child(styles);
        let content_sec = div()
            .flex()
            .flex_col()
            .px(px(12.))
            .py(px(2.))
            .rounded(px(10.))
            .bg(theme.shell)
            .border_1()
            .border_color(theme.hairline)
            .child(switch_row(
                "export-title-toggle",
                tr("显示标题", "Show Title"),
                self.show_title,
                Rc::new(cx.listener(|this, _: &ClickEvent, _, cx| this.toggle_title(cx))),
            ))
            .child(div().h(px(1.)).w_full().bg(theme.hairline))
            .child(switch_row(
                "export-watermark",
                tr("底部水印", "Watermark"),
                self.watermark,
                Rc::new(cx.listener(|this, _: &ClickEvent, _, cx| this.toggle_watermark(cx))),
            ));
        let ready = self.ready();
        let has_saved = self.saved_path.is_some();
        let scroll_body = div()
            .id("export-options")
            .flex_1()
            .min_h_0()
            .overflow_y_scroll()
            .p(px(16.))
            .flex()
            .flex_col()
            .gap(px(18.))
            .child(section(tr("导出格式", "Format"), formats))
            .child(section(tr("导出范围", "Scope"), scope))
            .child(section(tr("纸面主题", "Paper Theme"), theme_sec))
            .child(section(tr("版面元素", "Page Elements"), content_sec));
        let footer = div()
            .flex_none()
            .px(px(16.))
            .py(px(14.))
            .border_t_1()
            .border_color(theme.hairline)
            .bg(theme.paper)
            .flex()
            .flex_col()
            .gap(px(10.))
            .when_some(self.notice.clone(), |d, text| {
                d.child(
                    div()
                        .px(px(12.))
                        .py(px(8.))
                        .rounded(px(8.))
                        .bg(theme.selected)
                        .border_1()
                        .border_color(theme.accent)
                        .text_size(px(14.))
                        .text_color(theme.text)
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
                                    .on_click(
                                        cx.listener(|this, _: &ClickEvent, _, _| this.reveal_saved()),
                                    )
                                    .child(crate::files::ops::reveal_label()),
                            )
                        }),
                )
            })
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.))
                    .child(self.save_button(ready, cx))
                    .child(self.copy_button(ready, cx)),
            );
        div()
            .w(px(OPTIONS_WIDTH))
            .flex_none()
            .h_full()
            .bg(theme.paper)
            .border_l_1()
            .border_color(theme.sidebar_border)
            .flex()
            .flex_col()
            .child(scroll_body)
            .child(footer)
    }

    fn save_button(&self, ready: bool, cx: &mut Context<Self>) -> impl IntoElement {
        let mut fill = c(0xffffff);
        fill.a = 0.35;
        let colors = IconColors { line: c(0xffffff), fill, accent: c(0xffffff) };
        let glyph = icon(Icon::Download, px(15.), colors);
        button("export-save", self.format.save_label(), glyph, true, ready)
            .when(ready, |b| b.on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.save(cx))))
    }

    fn copy_button(&self, ready: bool, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let glyph = stateful_icon(
            "export-copy",
            if self.copied { Icon::Check } else { Icon::Copy },
            px(15.),
            self.copied,
            theme.mono_icon_colors(theme.text),
            theme.icon_colors(if self.copied { theme.accent } else { theme.text }),
        );
        let label = if self.copied { tr("已复制", "Copied") } else { tr("复制", "Copy") };
        button("export-copy", label, glyph, false, ready)
            .when(ready, |b| b.on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.copy(cx))))
    }
}
