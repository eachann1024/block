//! Empty-block hint on the caret line: a soft line band, the hint text past the caret,
//! and right-aligned menu keycaps for plain paragraphs. Never part of the text or hit testing.
use std::cell::RefCell;
use gpui::{App, Bounds, FontWeight, Hsla, Pixels, SharedString, ShapedLine, TextAlign, Window, fill, point, px, quad, rgb, size, transparent_black};
use crate::theme::{Theme, is_dark, tr};
use super::{model::{Block, Kind}, text_runs::plain, view::layout::TextLayout};

/// Hint text starts after the caret (2px) plus a 6px gap.
const TEXT_INSET: f32 = 8.;
const BAND_BLEED: f32 = 8.;
const KEY_H: f32 = 20.;
const KEY_GAP: f32 = 6.;
const MENU_KEYS: [&str; 2] = ["/", "、"];

#[derive(Clone, Copy, PartialEq)]
pub(super) struct Hint { pub text: &'static str, pub menu_keys: bool, pub armed: bool }

pub(crate) fn paragraph_text() -> &'static str { tr("开始输入…", "Start typing…") }

pub(super) fn hint_for(block: &Block, delete_armed: bool) -> Option<Hint> {
    if !block.text.is_empty() || block.meta.table.is_some() || block.is_atomic()
        || block.meta.code_language.is_some() { return None; }
    let text = match (block.kind, block.meta.heading_level) {
        (Kind::Paragraph, _) if delete_armed => crate::ui::reveal::delete_arm_placeholder_text(),
        (Kind::Paragraph, _) => paragraph_text(),
        (Kind::H1, _) => tr("标题 1", "Heading 1"),
        (Kind::H2, _) => tr("标题 2", "Heading 2"),
        (Kind::H3, Some(4)) => tr("标题 4", "Heading 4"),
        (Kind::H3, Some(5)) => tr("标题 5", "Heading 5"),
        (Kind::H3, Some(6)) => tr("标题 6", "Heading 6"),
        (Kind::H3, _) => tr("标题 3", "Heading 3"),
        _ => return None,
    };
    let menu_keys = block.kind == Kind::Paragraph && !delete_armed;
    Some(Hint { text, menu_keys, armed: delete_armed })
}

fn hint_color() -> Hsla { if is_dark() { rgb(0x7a7975).into() } else { rgb(0x9b9a95).into() } }

/// Soft band behind the caret line; painted before the caret so it stays underneath.
pub(super) fn paint_band(layout: &TextLayout, hint: Hint, window: &mut Window) {
    if hint.armed { return; }
    let color: Hsla = if is_dark() { rgb(0x1e1e1e).into() } else { rgb(0xf8f8f6).into() };
    let b = layout.bounds;
    let h = layout.line_height + px(4.);
    let origin = point(b.left() - px(BAND_BLEED), b.top() + (layout.line_height - h) * 0.5);
    window.paint_quad(fill(Bounds::new(origin, size(b.size.width + px(2. * BAND_BLEED), h)), color).corner_radii(px(6.)));
}

struct Shaped { key: u64, text: &'static str, hint: ShapedLine, keys: Vec<ShapedLine>, label: ShapedLine }
thread_local! { static SHAPED: RefCell<Option<Shaped>> = const { RefCell::new(None) }; }

fn shape(text: &str, font_size: Pixels, color: Hsla, mono: bool, window: &mut Window) -> ShapedLine {
    let mut run = plain(text.len(), color);
    run.font.weight = FontWeight::NORMAL;
    if mono { run.font.family = crate::theme::mono_font_family(); }
    window.text_system().shape_line(SharedString::from(text.to_owned()), font_size, &[run], None)
}

pub(super) fn paint(layout: &TextLayout, hint: Hint, window: &mut Window, cx: &mut App) {
    let dark = is_dark();
    let font_size = layout.lines.first().map_or(Theme::ocean().editor_font, |(_, l)| l.font_size());
    let key = crate::theme::font_settings_hash() ^ ((crate::theme::is_en() as u64) << 32)
        ^ ((dark as u64) << 33) ^ (((f32::from(font_size) as u64) & 0xff) << 34) ^ ((hint.armed as u64) << 42);
    let (line, keys, label) = SHAPED.with(|slot| {
        let mut slot = slot.borrow_mut();
        if slot.as_ref().is_none_or(|s| s.key != key || s.text != hint.text) {
            let color = if hint.armed { crate::ui::reveal::delete_arm_placeholder_color() } else { hint_color() };
            let keys_color: Hsla = if dark { rgb(0xa5a4a0).into() } else { rgb(0x6f6e69).into() };
            *slot = Some(Shaped { key, text: hint.text,
                hint: shape(hint.text, font_size, color, false, window),
                keys: MENU_KEYS.iter().map(|k| shape(k, px(12.), keys_color, true, window)).collect(),
                label: shape(tr("菜单", "Menu"), px(13.), hint_color(), false, window) });
        }
        let s = slot.as_ref().unwrap();
        (s.hint.clone(), s.keys.clone(), s.label.clone())
    });
    let b = layout.bounds;
    let _ = line.paint(point(b.left() + px(TEXT_INSET), b.top()), layout.line_height, TextAlign::Left, None, window, cx);
    if !hint.menu_keys { return; }
    let widths: Vec<Pixels> = keys.iter().map(|k| (k.width + px(10.)).max(px(KEY_H))).collect();
    let total = label.width + widths.iter().fold(px(0.), |a, w| a + *w + px(KEY_GAP));
    // Keep the keycaps clear of the hint text; drop them in narrow columns.
    if b.size.width < px(TEXT_INSET) + line.width + total + px(24.) { return; }
    let center_y = b.top() + layout.line_height * 0.5;
    let mut x = b.right() - total;
    let border: Hsla = if dark { rgb(0x3a3a3a).into() } else { rgb(0xdedcd6).into() };
    for (shaped, w) in keys.iter().zip(&widths) {
        let cap = Bounds::new(point(x, center_y - px(KEY_H / 2.)), size(*w, px(KEY_H)));
        window.paint_quad(quad(cap, px(5.), transparent_black(), px(1.), border, Default::default()));
        let _ = shaped.paint(point(x + (*w - shaped.width) * 0.5, cap.top()), px(KEY_H), TextAlign::Left, None, window, cx);
        x += *w + px(KEY_GAP);
    }
    let _ = label.paint(point(x, center_y - px(KEY_H / 2.)), px(KEY_H), TextAlign::Left, None, window, cx);
}
