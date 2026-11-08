//! Vector-key shortcut label: splits a label string into continuous text slices
//! and known key glyphs (`⌘`, `⌥`, `↩`/`↵`/`⏎`, `⇧`, `⌃`, `⌫`), rendering each
//! key as a centered fixed-square vector icon and each text run with matching
//! line-height so CJK/Latin text and key symbols share one vertical center.

use gpui::{
    Div, Font, FontFeatures, FontStyle, FontWeight, Hsla, Pixels, SharedString, TextRun, Window,
    div, prelude::*, px, transparent_black,
};
use crate::icons::{Icon, IconColors, icon};

const KEY_INK_RATIO: f32 = 15.6 / 24.0;
const KEY_INK_EM: f32 = 0.9;
const KEY_GAP: f32 = 2.;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Segment<'a> {
    Text(&'a str),
    Key(Icon),
}

#[derive(Clone)]
pub struct ShortcutStyle {
    pub font: Font,
    pub font_size: Pixels,
    pub line_height: Pixels,
    pub icon_size: Pixels,
    pub color: Hsla,
}

impl ShortcutStyle {
    pub fn ui(font_size: Pixels, line_height: Pixels, weight: FontWeight, color: Hsla) -> Self {
        Self {
            font: ui_font(weight),
            font_size,
            line_height,
            icon_size: font_size * (KEY_INK_EM / KEY_INK_RATIO),
            color,
        }
    }
}

pub fn ui_font(weight: FontWeight) -> Font {
    Font {
        // Same face as `theme::ui_font_family`: SF Pro on macOS.
        family: ".SystemUIFont".into(),
        weight,
        style: FontStyle::Normal,
        features: FontFeatures::default(),
        fallbacks: None,
    }
}

pub fn key_char_icon(ch: char) -> Option<Icon> {
    match ch {
        '⌘' => Some(Icon::KeyCommand),
        '⌥' => Some(Icon::KeyOption),
        '↩' | '↵' | '⏎' => Some(Icon::KeyReturn),
        '⇧' => Some(Icon::KeyShift),
        '⌃' => Some(Icon::KeyControl),
        '⌫' => Some(Icon::KeyBackspace),
        '↑' => Some(Icon::ArrowUp),
        '↓' => Some(Icon::ArrowDown),
        _ => None,
    }
}

pub fn key_icon(part: &str) -> Option<Icon> {
    let mut chars = part.chars();
    let ch = chars.next()?;
    chars.next().is_none().then(|| key_char_icon(ch)).flatten()
}

pub fn parse_segments(text: &str) -> Vec<Segment<'_>> {
    let mut out = Vec::new();
    let mut text_start = None;
    for (idx, ch) in text.char_indices() {
        if let Some(glyph) = key_char_icon(ch) {
            if let Some(start) = text_start.take() {
                out.push(Segment::Text(&text[start..idx]));
            }
            out.push(Segment::Key(glyph));
        } else if text_start.is_none() {
            text_start = Some(idx);
        }
    }
    if let Some(start) = text_start {
        out.push(Segment::Text(&text[start..]));
    }
    out
}

pub fn key_glyph_box(glyph: Icon, size: Pixels, color: Hsla) -> Div {
    let colors = IconColors { line: color, fill: transparent_black(), accent: color };
    div()
        .relative()
        .top(px(0.5))
        .size(size)
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .child(icon(glyph, size, colors))
}

pub fn shortcut_label(text: impl AsRef<str>, style: ShortcutStyle) -> Div {
    let segments = parse_segments(text.as_ref());
    let mut row = div()
        .h(style.line_height)
        .flex()
        .items_center()
        .whitespace_nowrap()
        .font(style.font)
        .text_size(style.font_size)
        .line_height(style.line_height)
        .text_color(style.color);
    let mut prev_key = false;
    for seg in segments {
        match seg {
            Segment::Key(glyph) => {
                if prev_key {
                    row = row.child(div().w(px(KEY_GAP)).flex_none());
                }
                prev_key = true;
                row = row.child(key_glyph_box(glyph, style.icon_size, style.color));
            }
            Segment::Text(slice) => {
                prev_key = false;
                let s: SharedString = slice.to_owned().into();
                row = row.child(
                    div()
                        .h(style.line_height)
                        .flex_none()
                        .flex()
                        .items_center()
                        .line_height(style.line_height)
                        .whitespace_nowrap()
                        .child(s),
                );
            }
        }
    }
    row
}

pub fn measure_width(text: &str, style: &ShortcutStyle, window: &Window) -> Pixels {
    let mut width = Pixels::ZERO;
    let mut prev_key = false;
    for seg in parse_segments(text) {
        match seg {
            Segment::Key(_) => {
                if prev_key {
                    width += px(KEY_GAP);
                }
                prev_key = true;
                width += style.icon_size;
            }
            Segment::Text(slice) => {
                prev_key = false;
                if slice.is_empty() {
                    continue;
                }
                let run = TextRun {
                    len: slice.len(),
                    font: style.font.clone(),
                    color: transparent_black(),
                    background_color: None,
                    underline: None,
                    strikethrough: None,
                };
                let s: SharedString = slice.to_owned().into();
                let line = window.text_system().shape_line(s, style.font_size, &[run], None);
                width += line.width;
            }
        }
    }
    width
}

pub fn shortcut_hint(text: impl AsRef<str>, color: Hsla) -> Div {
    hint_shell().child(hint_label(text.as_ref(), color))
}

/// Hint that stays `idle` and switches to `hover` with the menu row's group.
/// `active` (keyboard highlight, already on the danger fill) paints `hover` immediately.
pub fn shortcut_hint_group(text: impl AsRef<str>, group: SharedString, idle: Hsla, hover: Hsla, active: bool) -> Div {
    let text = text.as_ref();
    if active {
        return shortcut_hint(text, hover);
    }
    hint_shell().child(
        div()
            .relative()
            .child(div().group_hover(group.clone(), |s| s.invisible()).child(hint_label(text, idle)))
            .child(
                div()
                    .absolute()
                    .top_0()
                    .left_0()
                    .invisible()
                    .group_hover(group, |s| s.visible())
                    .child(hint_label(text, hover)),
            ),
    )
}

fn hint_label(text: &str, color: Hsla) -> Div {
    shortcut_label(text, ShortcutStyle::ui(px(12.), px(16.), FontWeight::NORMAL, color))
}

fn hint_shell() -> Div {
    div().ml_auto().pl(px(8.)).flex_none().flex().items_center()
}

pub fn kbd(text: impl AsRef<str>) -> Div {
    let theme = crate::theme::Theme::ocean();
    let style = ShortcutStyle::ui(px(11.), px(16.), FontWeight::NORMAL, theme.text_secondary);
    div()
        .h(px(18.))
        .px(px(5.))
        .rounded(crate::ui::radius::MICRO_SM)
        .bg(theme.shell)
        .border_1()
        .border_color(theme.btn_border)
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .child(shortcut_label(text, style))
}

pub fn keycap_chip(part: impl AsRef<str>, recording: bool) -> Div {
    let theme = crate::theme::Theme::ocean();
    let dark = crate::theme::is_dark();
    let bg = if dark { 0x252a31 } else { 0xf4f4f2 };
    let bd = if recording { theme.accent } else { theme.btn_border };
    let fg = if recording { theme.accent } else { theme.text };
    let style = ShortcutStyle::ui(px(12.), px(16.), FontWeight::MEDIUM, fg);
    div()
        .min_w(px(22.))
        .h(px(22.))
        .px(px(6.))
        .rounded(crate::ui::radius::MICRO_MD)
        .bg(gpui::rgb(bg))
        .border_1()
        .border_color(bd)
        .flex()
        .items_center()
        .justify_center()
        .child(shortcut_label(part, style))
}
