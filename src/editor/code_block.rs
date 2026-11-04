//! Fenced code block card: padding via layout margin, painted background, border, language label and copy button.
use std::{cell::RefCell, collections::HashSet, time::Duration};
use gpui::{
    App, BorderStyle, Bounds, ClipboardItem, Context, CursorStyle, Edges, FontWeight, IntoElement,
    Length, MouseButton, Pixels, SharedString, Window, div, point, prelude::*, px, quad, rgb, size,
    transparent_black,
};
use crate::{icons::{Icon, IconColors, icon}, theme::is_dark};
use super::{text_runs::plain, view::Editor};

const HEADER_H: f32 = 30.;
const PAD_TOP: f32 = 12.;
const PAD_X: f32 = 14.;
const PAD_BOTTOM: f32 = 14.;

thread_local! {
    static COPIED_BLOCKS: RefCell<HashSet<u64>> = Default::default();
}

/// Space around the text that the card occupies (30px header + 12px/14px/14px code area padding).
pub(super) fn margin(_language: &str) -> Edges<Length> {
    let top = HEADER_H + PAD_TOP;
    Edges { top: px(top).into(), right: px(PAD_X).into(), bottom: px(PAD_BOTTOM).into(), left: px(PAD_X).into() }
}

/// Paint the card behind the text `bounds` (which exclude the margin).
pub(super) fn paint_card(bounds: Bounds<Pixels>, language: &str, window: &mut Window, cx: &mut App) {
    let dark = is_dark();
    let bg = if dark { rgb(0x16191e) } else { rgb(0xf5f6f8) };
    let border = if dark { rgb(0x282c34) } else { rgb(0xeceef1) };
    let label_fg = if dark { rgb(0x9aa3b2) } else { rgb(0x526071) };
    let top = HEADER_H + PAD_TOP;
    let card = Bounds::new(
        bounds.origin - point(px(PAD_X), px(top)),
        size(bounds.size.width + px(PAD_X * 2.), bounds.size.height + px(top + PAD_BOTTOM)),
    );
    window.paint_quad(quad(card, px(8.), bg, px(1.), border, BorderStyle::Solid));
    window.paint_quad(gpui::fill(
        Bounds::new(card.origin + point(px(1.), px(HEADER_H)), size(card.size.width - px(2.), px(1.))),
        border,
    ));
    if language.is_empty() { return; }
    let text: SharedString = language.to_owned().into();
    let mut run = plain(text.len(), label_fg.into());
    run.font.weight = FontWeight::NORMAL;
    let line = window.text_system().shape_line(text, px(12.), &[run], None);
    let _ = line.paint(card.origin + point(px(PAD_X), px(7.)), px(16.), window, cx);
}

/// Header "复制代码" 24×24 icon button overlaid at the top-right of a fenced code block.
pub(super) fn copy_button(editor: gpui::Entity<Editor>, block_id: u64, cx: &mut Context<Editor>) -> impl IntoElement {
    let dark = is_dark();
    let base_fg: gpui::Hsla = if dark { rgb(0x9aa3b2).into() } else { rgb(0x526071).into() };
    let hover_fg: gpui::Hsla = if dark { rgb(0xe6e9ef).into() } else { rgb(0x16181c).into() };
    let hover_bg = if dark { rgb(0x252a34) } else { rgb(0xe8eaee) };
    let copied = COPIED_BLOCKS.with(|set| set.borrow().contains(&block_id));
    let glyph = if copied { Icon::Check } else { Icon::Copy };
    let tint = |line| IconColors { line, fill: transparent_black(), accent: transparent_black() };
    div()
        .id(("code-copy", block_id as usize))
        .group("code-copy")
        .absolute()
        .top(px(3.))
        .right(px(8.))
        .size(px(24.))
        .rounded(px(4.))
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .child(
            div().relative().flex_none().size(px(13.))
                .child(div().group_hover("code-copy", |s| s.invisible()).child(icon(glyph, px(13.), tint(base_fg))))
                .child(div().absolute().top_0().left_0().invisible().group_hover("code-copy", |s| s.visible())
                    .child(icon(glyph, px(13.), tint(hover_fg)))),
        )
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
            cx.stop_propagation();
            let text = this.doc.get(block_id).map(|b| b.text.clone()).unwrap_or_default();
            cx.write_to_clipboard(ClipboardItem::new_string(text));
            COPIED_BLOCKS.with(|set| { set.borrow_mut().insert(block_id); });
            cx.notify();
            let entity = editor.downgrade();
            cx.spawn(async move |_, cx| {
                cx.background_executor().timer(Duration::from_millis(1500)).await;
                let _ = entity.update(cx, |_, cx| {
                    COPIED_BLOCKS.with(|set| { set.borrow_mut().remove(&block_id); });
                    cx.notify();
                });
            }).detach();
        }))
}
