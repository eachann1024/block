//! Vector-key shortcut label: splits a label string into continuous text slices
//! and known key glyphs (`⌘`, `⌥`, `↩`/`↵`/`⏎`, `⇧`, `⌃`, `⌫`), rendering each
//! key as a centered fixed-square vector icon and each text run with matching
//! line-height so CJK/Latin text and key symbols share one vertical center.

use gpui::{
    Div, Font, FontFeatures, FontStyle, FontWeight, Hsla, Pixels, SharedString, TextRun, Window,
    div, prelude::*, transparent_black,
};
use crate::icons::{Icon, IconColors, icon};

const KEY_INK_RATIO: f32 = 15.6 / 24.0;
const KEY_INK_EM: f32 = 0.9;

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
    for seg in segments {
        match seg {
            Segment::Key(glyph) => {
                row = row.child(key_glyph_box(glyph, style.icon_size, style.color));
            }
            Segment::Text(slice) => {
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
    for seg in parse_segments(text) {
        match seg {
            Segment::Key(_) => width += style.icon_size,
            Segment::Text(slice) => {
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
