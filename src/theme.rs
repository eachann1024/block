//! Ocean paper theme (design A). Every color, radius and size the UI paints
//! comes from here so a later skin only has to swap this struct.

use gpui::{Hsla, Pixels, hsla, px};

use crate::icons::IconColors;

fn hex(value: u32) -> Hsla {
    let r = ((value >> 16) & 0xff) as f32 / 255.0;
    let g = ((value >> 8) & 0xff) as f32 / 255.0;
    let b = (value & 0xff) as f32 / 255.0;
    let max = r.max(g).max(b);
    let min = r.min(g).min(b);
    let l = (max + min) / 2.0;
    let d = max - min;
    let s = if d == 0.0 {
        0.0
    } else if l > 0.5 {
        d / (2.0 - max - min)
    } else {
        d / (max + min)
    };
    let h = if d == 0.0 {
        0.0
    } else if max == r {
        ((g - b) / d + if g < b { 6.0 } else { 0.0 }) / 6.0
    } else if max == g {
        ((b - r) / d + 2.0) / 6.0
    } else {
        ((r - g) / d + 4.0) / 6.0
    };
    hsla(h, s, l, 1.0)
}

pub struct Theme {
    pub shell: Hsla,
    pub sidebar: Hsla,
    pub paper: Hsla,
    pub menu: Hsla,
    pub text: Hsla,
    pub text_secondary: Hsla,
    pub accent: Hsla,
    pub icon_fill: Hsla,
    pub hover: Hsla,
    pub selected: Hsla,
    pub hairline: Hsla,
    pub code_bg: Hsla,
    pub quote_bar: Hsla,
    pub menu_item_height: Pixels,
    pub sidebar_width: Pixels,
    pub content_width: Pixels,
    pub editor_font: Pixels,
    pub ui_font: Pixels,
}

impl Theme {
    /// Icon tints: `line` is the stroke color the caller wants.
    pub fn icon_colors(&self, line: Hsla) -> IconColors {
        IconColors { line, fill: self.icon_fill, accent: self.accent }
    }

    pub fn ocean() -> Self {
        Self {
            shell: hex(0xf1f1f1),
            sidebar: hex(0xfafafa),
            paper: hex(0xffffff),
            menu: hex(0xffffff),
            text: hex(0x20242c),
            text_secondary: hex(0x526071),
            accent: hex(0x547dd0),
            icon_fill: hex(0xc9d8f3),
            hover: hex(0xeef3fc),
            selected: hex(0xe5edfc),
            hairline: hex(0xe4e6ea),
            code_bg: hex(0xf2f4f8),
            quote_bar: hex(0xc5d0e6),
            menu_item_height: px(32.),
            sidebar_width: px(248.),
            content_width: px(720.),
            editor_font: px(17.),
            ui_font: px(14.),
        }
    }
}
