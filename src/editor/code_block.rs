//! Fenced code block card: padding via layout margin, painted background, border and language label.
use gpui::{App, BorderStyle, Bounds, Edges, FontWeight, Length, Pixels, SharedString, Window, point, px, quad, rgb, size};
use super::text_runs::plain;

const PAD: f32 = 12.;
const LABEL: f32 = 24.;

/// Space around the text that the card occupies; the label row only exists when there is a label.
pub(super) fn margin(language: &str) -> Edges<Length> {
    let top = PAD + if language.is_empty() { 0. } else { LABEL };
    Edges { top: px(top).into(), right: px(PAD).into(), bottom: px(PAD).into(), left: px(PAD).into() }
}
/// Paint the card behind the text `bounds` (which exclude the margin).
pub(super) fn paint_card(bounds: Bounds<Pixels>, language: &str, window: &mut Window, cx: &mut App) {
    let top = PAD + if language.is_empty() { 0. } else { LABEL };
    let card = Bounds::new(bounds.origin - point(px(PAD), px(top)),
        size(bounds.size.width + px(PAD * 2.), bounds.size.height + px(top + PAD)));
    window.paint_quad(quad(card, px(8.), rgb(0xf5f6f8), px(1.), rgb(0xeceef1), BorderStyle::Solid));
    if language.is_empty() { return; }
    window.paint_quad(gpui::fill(Bounds::new(card.origin + point(px(1.), px(28.)), size(card.size.width - px(2.), px(1.))), rgb(0xeceef1)));
    let text: SharedString = language.to_owned().into();
    let mut run = plain(text.len(), rgb(0x526071).into());
    run.font.weight = FontWeight::MEDIUM;
    let line = window.text_system().shape_line(text, px(11.), &[run], None);
    let _ = line.paint(card.origin + point(px(PAD), px(7.)), px(16.), window, cx);
}
