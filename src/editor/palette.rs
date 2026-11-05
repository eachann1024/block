//! Selection color and highlight menu (F-Toolbar): 6 text colors + 2 highlight options.
use std::ops::Range;
use gpui::{AnyElement, Context, CursorStyle, Entity, MouseButton, Window, div, prelude::*, px, rgb, rgba};
use crate::{
    editor::{
        format_cmds::FormatPop,
        model::InlineTag,
        text_runs::{css, named_color_hex},
        view::Editor,
    },
    icons::Icon,
    theme::{is_dark, tr},
};

pub(in crate::editor) struct TextSwatchSpec {
    pub name: &'static str,
    pub zh: &'static str,
    pub en: &'static str,
    pub light: u32,
    pub dark: u32,
}

pub(in crate::editor) const TEXT_SWATCHES: [TextSwatchSpec; 6] = [
    TextSwatchSpec { name: "default", zh: "默认", en: "Default", light: 0x1a1a1a, dark: 0xf0efec },
    TextSwatchSpec { name: "red", zh: "红色", en: "Red", light: 0x93342c, dark: 0xffb4ab },
    TextSwatchSpec { name: "yellow", zh: "黄色", en: "Yellow", light: 0x714f17, dark: 0xf3d17c },
    TextSwatchSpec { name: "green", zh: "绿色", en: "Green", light: 0x326a23, dark: 0x8ce0b1 },
    TextSwatchSpec { name: "teal", zh: "青色", en: "Teal", light: 0x2f6259, dark: 0x7fd8c8 },
    TextSwatchSpec { name: "purple", zh: "紫色", en: "Purple", light: 0x5036b1, dark: 0xd0baf8 },
];

pub(in crate::editor) struct HighlightSpec {
    pub name: &'static str,
    pub zh: &'static str,
    pub en: &'static str,
}

pub(in crate::editor) const HIGHLIGHT_SWATCHES: [HighlightSpec; 2] = [
    HighlightSpec { name: "default", zh: "无高亮", en: "No highlight" },
    HighlightSpec { name: "yellow", zh: "黄色", en: "Yellow" },
];

#[derive(Clone, Copy, PartialEq, Eq)]
pub(in crate::editor) enum SelectionColor {
    Single(&'static str),
    Mixed,
}

fn canonical_color_name(raw: &str, background: bool) -> Option<&'static str> {
    let lower = raw.trim().to_ascii_lowercase();
    if lower.is_empty() || matches!(lower.as_str(), "default" | "transparent" | "none") {
        return None;
    }
    if background {
        match lower.as_str() {
            "yellow" | "gray" | "brown" | "red" | "orange" | "green" | "blue" | "purple" | "pink" => Some("yellow"),
            _ => named_color_hex(&lower, true, false).map(|_| "yellow"),
        }
    } else {
        match lower.as_str() {
            "red" => Some("red"),
            "yellow" => Some("yellow"),
            "green" => Some("green"),
            "teal" => Some("teal"),
            "purple" => Some("purple"),
            "gray" => Some("gray"),
            "brown" => Some("brown"),
            "orange" => Some("orange"),
            "blue" => Some("blue"),
            "pink" => Some("pink"),
            _ => match named_color_hex(&lower, false, false)? {
                "#93342c" => Some("red"),
                "#714f17" => Some("yellow"),
                "#326a23" => Some("green"),
                "#2f6259" => Some("teal"),
                "#5036b1" => Some("purple"),
                "#5e5d59" => Some("gray"),
                "#64473a" => Some("brown"),
                "#974e08" => Some("orange"),
                "#245bb2" => Some("blue"),
                "#ad1a72" => Some("pink"),
                _ => None,
            },
        }
    }
}

fn resolve_tag_color(tag: &InlineTag, background: bool) -> Option<&'static str> {
    let suffix = if background { "background-color" } else { "text-color" };
    for (k, v) in &tag.attrs {
        if k == &format!("data-thinkly-{suffix}") || k == &format!("data-goose-{suffix}") {
            if let Some(name) = canonical_color_name(v, background) {
                return Some(name);
            }
        }
    }
    let prop = if background { "background-color" } else { "color" };
    let val = css(&tag.attrs, prop).or_else(|| background.then(|| css(&tag.attrs, "background")).flatten())?;
    canonical_color_name(val, background)
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
        let mut inspect = |inline: &[InlineTag], range: Range<usize>| {
            if range.start >= range.end { return; }
            let mut cuts = vec![range.start, range.end];
            for tag in inline {
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
                let (mut t_col, mut b_col) = ("default", "default");
                for tag in inline.iter().filter(|t| t.name == "span" && t.start <= s && e <= t.end) {
                    if let Some(c) = resolve_tag_color(tag, false) { t_col = c; }
                    if let Some(c) = resolve_tag_color(tag, true) { b_col = c; }
                }
                merge(&mut text_acc, t_col);
                merge(&mut bg_acc, b_col);
            }
        };
        for (id, range) in ranges {
            let Some(block) = self.doc.get(id) else { continue };
            if let Some(table) = &block.meta.table {
                for (row, cell, body) in table.cell_ranges() {
                    let start = range.start.max(body.start).min(body.end) - body.start;
                    let end = range.end.max(body.start).min(body.end) - body.start;
                    if start < end { inspect(&table.rows[row].cells[cell].content.meta.inline, start..end); }
                }
            } else {
                inspect(&block.meta.inline, range);
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
        self.set_selection_style("color", (color != "default").then_some(color), cx);
        self.close_format(window, cx);
    }

    pub(in crate::editor) fn apply_bg_color(&mut self, color: &'static str, window: &mut Window, cx: &mut Context<Self>) {
        self.set_selection_style("background-color", (color != "default").then_some(color), cx);
        self.close_format(window, cx);
    }

    fn set_selection_style(&mut self, prop: &str, value: Option<&str>, cx: &mut Context<Self>) {
        let ranges: Vec<(u64, Range<usize>)> = self.selection_ranges();
        if ranges.is_empty() { return; }
        self.commit_composition(cx);
        let before = self.snapshot();
        for (id, range) in ranges {
            self.doc.set_style(id, range, prop, value);
        }
        self.record(before, cx);
    }

    pub(in crate::editor) fn toggle_palette(&mut self, spec: crate::dropdown::PlacementSpec, cx: &mut Context<Self>) {
        let was = matches!(self.format, Some(FormatPop::Palette { .. }));
        self.format = None;
        if !was {
            self.format = Some(FormatPop::Palette { spec });
        }
        cx.notify();
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
    let p = crate::dropdown::palette();
    let swatch_border = if dark { 0xffffff24 } else { 0x1f1e1d1f };

    let mut col = crate::dropdown::shell("palette-panel")
        .w(px(196.))
        .child(crate::dropdown::group_label(tr("文本", "Text")));

    for (idx, spec) in TEXT_SWATCHES.iter().enumerate() {
        let name = spec.name;
        let selected = sel_text == SelectionColor::Single(name);
        let swatch_hex = if dark { spec.dark } else { spec.light };
        let editor = editor.clone();
        col = col.child(
            div().id(("fg-sw", idx))
                .h(px(28.)).px(px(10.)).flex().items_center().gap(px(8.)).rounded(crate::ui::radius::CONTROL)
                .cursor(CursorStyle::PointingHand)
                .when(selected, move |s| s.bg(p.checked_bg))
                .hover(move |s| s.bg(p.hover_bg))
                .text_size(px(14.)).line_height(px(20.)).text_color(p.text)
                .child(div().size(px(16.)).rounded(crate::ui::radius::MICRO_SM).flex_none().bg(rgb(swatch_hex)).border_1().border_color(rgba(swatch_border)))
                .child(div().flex_1().child(tr(spec.zh, spec.en)))
                .when(selected, move |r| r.child(div().ml_auto().pl(px(12.)).flex_none().flex().items_center().child(crate::dropdown::line_icon(Icon::Check, 16., p.text))))
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    editor.update(cx, |this, cx| this.apply_text_color(name, window, cx));
                    cx.stop_propagation();
                }),
        );
    }

    col = col
        .child(crate::dropdown::separator())
        .child(crate::dropdown::group_label(tr("高亮", "Highlight")));

    for (idx, spec) in HIGHLIGHT_SWATCHES.iter().enumerate() {
        let name = spec.name;
        let selected = sel_bg == SelectionColor::Single(name);
        let editor = editor.clone();
        let swatch = if name == "default" {
            div().size(px(16.)).rounded(crate::ui::radius::MICRO_SM).flex_none()
                .border_1().border_color(rgb(if dark { 0x6b707b } else { 0xb5b4b0 }))
        } else {
            div().size(px(16.)).rounded(crate::ui::radius::MICRO_SM).flex_none()
                .bg(rgb(if dark { 0x453a1e } else { 0xfde6b3 }))
                .border_1().border_color(rgb(if dark { 0x6a582c } else { 0xfcd88c }))
        };
        col = col.child(
            div().id(("bg-sw", idx))
                .h(px(28.)).px(px(10.)).flex().items_center().gap(px(8.)).rounded(crate::ui::radius::CONTROL)
                .cursor(CursorStyle::PointingHand)
                .when(selected, move |s| s.bg(p.checked_bg))
                .hover(move |s| s.bg(p.hover_bg))
                .text_size(px(14.)).line_height(px(20.)).text_color(p.text)
                .child(swatch)
                .child(div().flex_1().child(tr(spec.zh, spec.en)))
                .when(selected, move |r| r.child(div().ml_auto().pl(px(12.)).flex_none().flex().items_center().child(crate::dropdown::line_icon(Icon::Check, 16., p.text))))
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    editor.update(cx, |this, cx| this.apply_bg_color(name, window, cx));
                    cx.stop_propagation();
                }),
        );
    }

    col.into_any_element()
}
