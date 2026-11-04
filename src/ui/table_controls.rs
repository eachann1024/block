//! Table chrome controls: the row/column grip (tgrip) and the table options button (thandle).
//! The add bars live in `reveal::table_add_bar`, which already carries the shared values.
use std::time::Duration;
use gpui::{
    Animation, AnimationExt, CursorStyle, Div, Hsla, Stateful, div, prelude::*, px, rgb, rgba,
};
use crate::{
    icons::{Icon, icon},
    theme::{Theme, build_tooltip, is_dark, tr},
    ui::{motion::ease_out_expo, reveal::hover_icon},
};

/// Long and short side of the grip hit area; a column grip is 24x14, a row grip 14x24.
pub const GRIP_LONG: f32 = 24.;
pub const GRIP_SHORT: f32 = 14.;

/// 3x2 dots (2x3 when vertical), 4px apart center-to-center, 1.5px diameter, inside a 12x8 (or 8x12) box.
pub fn dots(vertical: bool, color: Hsla) -> Div {
    let (long, short) = (12., 8.);
    let (w, h) = if vertical { (short, long) } else { (long, short) };
    let mut grid = div().relative().flex_none().w(px(w)).h(px(h));
    for along in 0..3 {
        for across in 0..2 {
            let (a, b) = (2. + 4. * along as f32 - 0.75, 2. + 4. * across as f32 - 0.75);
            let (x, y) = if vertical { (b, a) } else { (a, b) };
            grid = grid.child(div().absolute().left(px(x)).top(px(y)).size(px(1.5)).rounded_full().bg(color));
        }
    }
    grid
}

/// At rest a 24x8 / 8x24 pill shows; hovered, pressed or with its menu open the grip grows into the
/// 24x14 / 14x24 capsule with a 6-dot grid. The hit area is the same size in both states.
pub fn grip(id: (&'static str, usize), vertical: bool, expanded: bool, dragging: bool, tip: &'static str) -> Stateful<Div> {
    let dark = is_dark();
    let (w, h) = if vertical { (GRIP_SHORT, GRIP_LONG) } else { (GRIP_LONG, GRIP_SHORT) };
    let bg: Hsla = if dark {
        if expanded { rgb(0x262626).into() } else { rgb(0x1e1e1e).into() }
    } else {
        rgb(0xffffff).into()
    };
    let border: Hsla = if dark {
        if expanded { rgb(0x6b6b6b).into() } else { rgb(0x555555).into() }
    } else {
        rgba(0x1f1e1d52).into()
    };
    let dot: Hsla = if dark { Theme::ocean().text.opacity(0.55) } else { rgba(0x1f1e1d8c).into() };
    let shell = div().id(id).w(px(w)).h(px(h)).flex_none().flex().items_center().justify_center()
        .cursor(if dragging { CursorStyle::ClosedHand } else { CursorStyle::OpenHand });
    let shell = if expanded {
        let key = id.1 * 2 + usize::from(vertical);
        shell.rounded(px(7.)).bg(bg).border_1().border_color(border).child(
            dots(vertical, dot).with_animation(("tgrip-dots", key),
                Animation::new(Duration::from_millis(150)).with_easing(ease_out_expo()),
                |el, delta| el.opacity(delta)))
    } else {
        let (rw, rh) = if vertical { (8., GRIP_LONG) } else { (GRIP_LONG, 8.) };
        shell.child(div().w(px(rw)).h(px(rh)).rounded_full().bg(bg).border_1().border_color(border))
    };
    shell.when(!dragging, |el| el.tooltip(move |window, cx| build_tooltip(tip, window, cx)))
}

/// 26x26 options button: transparent, #f5f4ed on hover or while its menu is open.
pub fn thandle(open: bool) -> Stateful<Div> {
    let theme = Theme::ocean();
    let dark = is_dark();
    let hover_bg: Hsla = if dark { rgb(0x2c2c2a).into() } else { rgb(0xf5f4ed).into() };
    let base: Hsla = if dark { theme.text_secondary } else { rgb(0x73726c).into() };
    let hover: Hsla = if dark { theme.text } else { rgb(0x1f1e1d).into() };
    div().id("table-more").group("hover-icon").size(px(26.)).flex_none().rounded(px(8.))
        .flex().items_center().justify_center()
        .when(open, move |s| s.bg(hover_bg))
        .hover(move |s| s.bg(hover_bg))
        .cursor(CursorStyle::PointingHand)
        .child(if open {
            icon(Icon::More, px(16.), theme.icon_colors(hover))
        } else {
            hover_icon(Icon::More, 16., base, hover)
        })
        .tooltip(|window, cx| build_tooltip(tr("表格选项", "Table options"), window, cx))
}
