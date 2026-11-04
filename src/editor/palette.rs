//! Selection color picker (F-Toolbar): 10 text colors + 10 background colors,
//! last-used color persistence, mutual exclusivity, and hover-triggered panel.
use std::{ops::Range, sync::Mutex, time::Duration};
use gpui::{AnyElement, Context, CursorStyle, Entity, Font, FontFeatures, FontStyle, FontWeight,
    MouseButton, Window, div, point, prelude::*, px, rgb, rgba};
use crate::{
    editor::{format_cmds::FormatPop, model::InlineTag, text_runs::{css, named_color_hex}, view::Editor},
    files::state::{LastFormatColors, load_state, update_state},
    theme::is_dark,
};

pub(in crate::editor) struct SwatchSpec {
    pub name: &'static str,
    pub text_label: &'static str,
    pub bg_label: &'static str,
    pub text_light: u32,
    pub text_dark: u32,
    pub bg_light: Option<u32>,
    pub bg_dark: Option<u32>,
}

pub(in crate::editor) const SWATCHES: [SwatchSpec; 10] = [
    SwatchSpec { name: "default", text_label: "默认", bg_label: "无背景", text_light: 0x1f1e1d, text_dark: 0xf0efec, bg_light: None, bg_dark: None },
    SwatchSpec { name: "gray", text_label: "灰色", bg_label: "灰色背景", text_light: 0x5e5d59, text_dark: 0xc3c2b7, bg_light: Some(0xebeced), bg_dark: Some(0x3d3d3a) },
    SwatchSpec { name: "brown", text_label: "褐色", bg_label: "褐色背景", text_light: 0x64473a, text_dark: 0xe7c1ad, bg_light: Some(0xe9e5e3), bg_dark: Some(0x49352c) },
    SwatchSpec { name: "red", text_label: "红色", bg_label: "红色背景", text_light: 0xb42318, text_dark: 0xffb4ab, bg_light: Some(0xfbe4e4), bg_dark: Some(0x512b31) },
    SwatchSpec { name: "orange", text_label: "橙色", bg_label: "橙色背景", text_light: 0x974e08, text_dark: 0xffc38f, bg_light: Some(0xf6e9d9), bg_dark: Some(0x50351f) },
    SwatchSpec { name: "yellow", text_label: "黄色", bg_label: "黄色背景", text_light: 0x855600, text_dark: 0xf3d17c, bg_light: Some(0xfbf3db), bg_dark: Some(0x453a1e) },
    SwatchSpec { name: "green", text_label: "绿色", bg_label: "绿色背景", text_light: 0x086b42, text_dark: 0x8ce0b1, bg_light: Some(0xddedea), bg_dark: Some(0x253f34) },
    SwatchSpec { name: "blue", text_label: "蓝色", bg_label: "蓝色背景", text_light: 0x245bb2, text_dark: 0xa8c8ff, bg_light: Some(0xddebf1), bg_dark: Some(0x223f52) },
    SwatchSpec { name: "purple", text_label: "紫色", bg_label: "紫色背景", text_light: 0x6940a5, text_dark: 0xd0baf8, bg_light: Some(0xeae4f2), bg_dark: Some(0x3b3055) },
    SwatchSpec { name: "pink", text_label: "粉色", bg_label: "粉色背景", text_light: 0xad1a72, text_dark: 0xf1b6d7, bg_light: Some(0xf4dfeb), bg_dark: Some(0x4b2c42) },
];

#[derive(Clone, Copy, PartialEq, Eq)]
pub(in crate::editor) enum SelectionColor {
    Single(&'static str),
    Mixed,
}

static LAST_COLORS: Mutex<Option<(&'static str, &'static str)>> = Mutex::new(None);
static PALETTE_HOVER_GEN: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);

fn sanitize_color_name(raw: &str) -> &'static str {
    let trimmed = raw.trim();
    SWATCHES.iter().find(|s| s.name.eq_ignore_ascii_case(trimmed)).map_or("default", |s| s.name)
}

fn resolve_tag_color(tag: &InlineTag, background: bool) -> Option<&'static str> {
    let suffix = if background { "background-color" } else { "text-color" };
    for (k, v) in &tag.attrs {
        if k == &format!("data-thinkly-{suffix}") || k == &format!("data-goose-{suffix}") {
            let name = sanitize_color_name(v);
            if name != "default" { return Some(name); }
        }
    }
    let prop = if background { "background-color" } else { "color" };
    let val = css(&tag.attrs, prop).or_else(|| background.then(|| css(&tag.attrs, "background")).flatten())?;
    let name = sanitize_color_name(val);
    if name != "default" { return Some(name); }
    let hex = named_color_hex(val, background, false)?;
    SWATCHES.iter().find(|s| {
        if background { s.bg_light.is_some_and(|b| format!("#{b:06x}").eq_ignore_ascii_case(hex)) }
        else { format!("#{:06x}", s.text_light).eq_ignore_ascii_case(hex) }
    }).map(|s| s.name)
}

pub(in crate::editor) fn last_colors() -> (&'static str, &'static str) {
    let mut guard = LAST_COLORS.lock().unwrap_or_else(|e| e.into_inner());
    if let Some(pair) = *guard { return pair; }
    let loaded = load_state().ok().and_then(|s| s.last_format_colors).unwrap_or_default();
    let pair = (sanitize_color_name(&loaded.text_color), sanitize_color_name(&loaded.background_color));
    *guard = Some(pair);
    pair
}

fn save_last_colors(text: &'static str, bg: &'static str) {
    *LAST_COLORS.lock().unwrap_or_else(|e| e.into_inner()) = Some((text, bg));
    let _ = update_state(|state| {
        state.last_format_colors = Some(LastFormatColors { text_color: text.to_owned(), background_color: bg.to_owned() });
    });
}

/// Returns `(text_icon_tint, bottom_bar_color)` for the toolbar palette button.
pub(in crate::editor) fn button_preview_colors() -> (Option<u32>, Option<u32>) {
    let (last_text, last_bg) = last_colors();
    let dark = is_dark();
    let text_hex = SWATCHES.iter().find(|s| s.name == last_text && s.name != "default")
        .map(|s| if dark { s.text_dark } else { s.text_light });
    let bg_hex = SWATCHES.iter().find(|s| s.name == last_bg && s.name != "default")
        .and_then(|s| if dark { s.bg_dark } else { s.bg_light });
    (text_hex, bg_hex.or(text_hex))
}

impl Editor {
    pub(in crate::editor) fn selection_colors(&self) -> (SelectionColor, SelectionColor) {
        let ranges = self.selection_ranges();
        if ranges.is_empty() {
            return (SelectionColor::Single("default"), SelectionColor::Single("default"));
        }
        let mut text_acc: Option<SelectionColor> = None;
        let mut bg_acc: Option<SelectionColor> = None;
        let merge = |acc: &mut Option<SelectionColor>, next: &'static str| {
            *acc = Some(match *acc {
                None => SelectionColor::Single(next),
                Some(SelectionColor::Single(prev)) if prev == next => SelectionColor::Single(prev),
                _ => SelectionColor::Mixed,
            });
        };
        for (id, range) in ranges {
            let Some(block) = self.doc.get(id) else { continue };
            let mut cuts = vec![range.start, range.end];
            for tag in &block.meta.inline {
                if tag.start < range.end && tag.end > range.start {
                    cuts.push(tag.start.max(range.start));
                    cuts.push(tag.end.min(range.end));
                }
            }
            cuts.sort_unstable();
            cuts.dedup();
            for pair in cuts.windows(2) {
                let (s, e) = (pair[0], pair[1]);
                if s >= e { continue; }
                let mut t_col = "default";
                let mut b_col = "default";
                for tag in block.meta.inline.iter().filter(|t| t.name == "span" && t.start <= s && e <= t.end) {
                    if let Some(c) = resolve_tag_color(tag, false) { t_col = c; }
                    if let Some(c) = resolve_tag_color(tag, true) { b_col = c; }
                }
                merge(&mut text_acc, t_col);
                merge(&mut bg_acc, b_col);
            }
        }
        (
            text_acc.unwrap_or(SelectionColor::Single("default")),
            bg_acc.unwrap_or(SelectionColor::Single("default")),
        )
    }

    pub(in crate::editor) fn has_selection_color(&self) -> bool {
        let (t, b) = self.selection_colors();
        t != SelectionColor::Single("default") || b != SelectionColor::Single("default")
    }

    pub(in crate::editor) fn apply_text_color(&mut self, color: &'static str, window: &mut Window, cx: &mut Context<Self>) {
        let val = (color != "default").then_some(color);
        self.apply_exclusive_colors(val, None, cx);
        save_last_colors(color, "default");
        self.close_format(window, cx);
    }

    pub(in crate::editor) fn apply_bg_color(&mut self, color: &'static str, window: &mut Window, cx: &mut Context<Self>) {
        let val = (color != "default").then_some(color);
        self.apply_exclusive_colors(None, val, cx);
        save_last_colors("default", color);
        self.close_format(window, cx);
    }

    pub(in crate::editor) fn click_palette_button(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let (last_text, last_bg) = last_colors();
        let (sel_text, sel_bg) = self.selection_colors();
        let has_last = last_text != "default" || last_bg != "default";
        let using_last = has_last
            && sel_text == SelectionColor::Single(last_text)
            && sel_bg == SelectionColor::Single(last_bg);
        if using_last {
            self.apply_exclusive_colors(None, None, cx);
        } else {
            self.apply_exclusive_colors(
                (last_text != "default").then_some(last_text),
                (last_bg != "default").then_some(last_bg),
                cx,
            );
        }
        self.close_format(window, cx);
    }

    fn apply_exclusive_colors(&mut self, text: Option<&str>, bg: Option<&str>, cx: &mut Context<Self>) {
        let ranges: Vec<(u64, Range<usize>)> = self.selection_ranges();
        if ranges.is_empty() { return; }
        self.commit_composition(cx);
        let before = self.snapshot();
        for (id, range) in ranges {
            self.doc.set_style(id, range.clone(), "color", text);
            self.doc.set_style(id, range, "background-color", bg);
        }
        self.record(before, cx);
    }

    pub(in crate::editor) fn hover_palette_zone(&mut self, hovered: bool, anchor: Option<gpui::Point<gpui::Pixels>>, cx: &mut Context<Self>) {
        let next = PALETTE_HOVER_GEN.fetch_add(1, std::sync::atomic::Ordering::SeqCst) + 1;
        if hovered {
            if !matches!(self.format, Some(FormatPop::Palette { .. })) {
                if self.selection_ranges().is_empty() { return; }
                let at = anchor.unwrap_or_else(|| self.toolbar_anchor.unwrap_or_default() + point(px(0.), px(40.)));
                self.format = Some(FormatPop::Palette { anchor: at });
                cx.notify();
            }
            return;
        }
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(Duration::from_millis(150)).await;
            if PALETTE_HOVER_GEN.load(std::sync::atomic::Ordering::SeqCst) != next { return; }
            this.update(cx, |this, cx| {
                if matches!(this.format, Some(FormatPop::Palette { .. })) {
                    this.format = None;
                    cx.notify();
                }
            }).ok();
        }).detach();
    }
}

#[derive(IntoElement)]
pub(in crate::editor) struct PalettePopover {
    pub editor: Entity<Editor>,
}

impl RenderOnce for PalettePopover {
    fn render(self, _: &mut Window, cx: &mut gpui::App) -> impl IntoElement {
        render_palette_panel(self.editor, cx)
    }
}

pub(in crate::editor) fn render_palette_panel(editor: Entity<Editor>, cx: &gpui::App) -> AnyElement {
    let (sel_text, sel_bg) = editor.read(cx).selection_colors();
    let dark = is_dark();
    let label_color = if dark { 0x9aa0aa } else { 0x5e5d59 };
    let hover_bg = if dark { 0x3a3e46 } else { 0xf1f1ef };
    let on_bg = if dark { 0x33415c } else { 0xe7effd };
    let on_border = if dark { 0x547dd0 } else { 0xb9cff7 };
    let chip_border = if dark { 0xffffff24 } else { 0x1f1e1d1f };

    let build_grid = |background: bool| {
        let mut col = div().flex().flex_col().gap(px(2.)).px(px(8.));
        for row_idx in 0..2 {
            let mut row = div().flex().gap(px(2.));
            for col_idx in 0..5 {
                let idx = row_idx * 5 + col_idx;
                let spec = &SWATCHES[idx];
                let name = spec.name;
                let tip = if background { spec.bg_label } else { spec.text_label };
                let selected = if background { sel_bg == SelectionColor::Single(name) } else { sel_text == SelectionColor::Single(name) };
                let editor = editor.clone();
                let inner = if background {
                    let bg_hex = if dark { spec.bg_dark } else { spec.bg_light };
                    div().size(px(24.)).rounded(px(6.)).border_1().border_color(rgba(chip_border))
                        .when_some(bg_hex, |d, h| d.bg(rgb(h))).into_any_element()
                } else {
                    let fg = if dark { spec.text_dark } else { spec.text_light };
                    div().font(Font { family: "Georgia".into(), weight: FontWeight::NORMAL, style: FontStyle::Normal, features: FontFeatures::default(), fallbacks: None })
                        .text_size(px(20.)).line_height(px(20.)).text_color(rgb(fg)).child("A").into_any_element()
                };
                row = row.child(
                    div().id((if background { "bg-sw" } else { "fg-sw" }, idx))
                        .size(px(32.)).rounded(px(6.)).border_1()
                        .border_color(if selected { rgb(on_border).into() } else { gpui::transparent_black() })
                        .when(selected, |d| d.bg(rgb(on_bg)))
                        .when(!selected, |d| d.hover(move |s| s.bg(rgb(hover_bg))))
                        .flex().items_center().justify_center().cursor(CursorStyle::PointingHand)
                        .child(inner)
                        .tooltip(move |window, cx| gpui_component::tooltip::Tooltip::new(tip).build(window, cx))
                        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                            editor.update(cx, |this, cx| {
                                if background { this.apply_bg_color(name, window, cx); }
                                else { this.apply_text_color(name, window, cx); }
                            });
                            cx.stop_propagation();
                        }),
                );
            }
            col = col.child(row);
        }
        col
    };

    let hover_editor = editor.clone();
    div().id("palette-panel").pt(px(6.)).pb(px(8.))
        .on_hover(move |hovered, _, cx| {
            hover_editor.update(cx, |this, cx| this.hover_palette_zone(*hovered, None, cx));
        })
        .child(div().pt(px(6.)).pb(px(4.)).pl(px(12.)).pr(px(8.)).text_size(px(12.)).line_height(px(16.)).text_color(rgb(label_color)).child("文本颜色"))
        .child(build_grid(false))
        .child(div().pt(px(8.)).pb(px(4.)).pl(px(12.)).pr(px(8.)).text_size(px(12.)).line_height(px(16.)).text_color(rgb(label_color)).child("背景颜色"))
        .child(build_grid(true))
        .into_any_element()
}
