//! Floating find bar (top-right of the editor) and the highlight layer for matches.
use gpui::{BorderStyle, Bounds, Context, CursorStyle, MouseButton, Pixels, Window, div, prelude::*, px, quad, rgb, transparent_black};
use gpui_component::input::{Escape, Input};
use crate::{icons::{Icon, icon, stateful_icon}, theme::Theme};
use super::view::{Editor, ExitBlock, layout::TextLayout};

// Highlights sit beneath existing text, so dark mode needs dark amber fills.
fn match_colors() -> (u32, u32, u32) {
    if crate::theme::is_dark() {
        (0x40361e, 0x59451c, 0xc9971c)
    } else {
        (0xfbe7a6, 0xf4c84f, 0xc9971c)
    }
}

fn button(glyph: Icon, id: &'static str, on: impl Fn(&mut Editor, &mut Window, &mut Context<Editor>) + 'static, cx: &mut Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean();
    div().id(id).group(id).relative().children(crate::ui::smoke_overlay::probe(id)).size(px(32.)).flex_none().flex().items_center().justify_center().rounded(crate::ui::radius::CONTROL)
        .cursor(CursorStyle::PointingHand).hover(|s| s.bg(theme.hover))
        .child(stateful_icon(id, glyph, px(14.), false, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text)))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| { on(this, window, cx); cx.stop_propagation(); }))
}

/// The bar itself; absolutely positioned, so the caller adds it to the editor root.
pub(super) fn find_bar(editor: &Editor, cx: &mut Context<Editor>) -> Option<impl IntoElement> {
    let find = editor.find.as_ref()?;
    let theme = Theme::ocean();
    let count = find.matches.len();
    let query_empty = find.input.read(cx).value().is_empty();
    let label = if query_empty { String::new() } else if count == 0 { crate::theme::tr("无结果", "No results").to_owned() } else { format!("{} / {}", find.current + 1, count) };
    let card = crate::ui::popover::find_card("find-bar")
        .key_context("FindBar")
        .on_action(cx.listener(|this, _: &Escape, window, cx| { this.close_find(window, cx); cx.stop_propagation(); }))
        .on_action(cx.listener(|this, _: &ExitBlock, window, cx| { this.find_step(false, window, cx); cx.stop_propagation(); }))
        .child(icon(Icon::FindSearch, px(14.), theme.mono_icon_colors(theme.text_secondary)))
        .child(div().w(px(180.)).h(px(28.)).flex_none().flex().items_center().text_size(px(14.))
            .child(Input::new(&find.input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).px(px(0.))))
        .child(div().flex_none().w(px(72.)).px(px(6.)).flex().justify_end().text_color(theme.text_secondary).child(label))
        .child(div().w(px(1.)).h(px(18.)).mx(px(2.)).flex_none().bg(theme.sidebar_border))
        .child(button(Icon::ChevronUp, "find-prev", |this, w, cx| this.find_step(false, w, cx), cx))
        .child(button(Icon::ChevronDown, "find-next", |this, w, cx| this.find_step(true, w, cx), cx))
        .child(button(Icon::Close, "find-close", |this, w, cx| this.close_find(w, cx), cx));
    Some(div().absolute().top(px(12.)).right(px(20.)).child(crate::ui::popover::animate(card, "find-bar-enter")))
}

/// Paint all matches of `block` under the text; the current match is stronger.
pub(super) fn paint_matches(editor: &Editor, block: u64, layout: &TextLayout, window: &mut Window) {
    let Some((all, current)) = editor.find_ranges(block) else { return };
    let (matched, active, ring) = match_colors();
    for range in all {
        let is_cur = current.as_ref() == Some(&range);
        let color = if is_cur { active } else { matched };
        for rect in layout.rects(range) {
            let rect: Bounds<Pixels> = rect;
            if is_cur {
                window.paint_quad(quad(rect, px(2.), rgb(color), px(1.), rgb(ring), BorderStyle::Solid));
            } else {
                window.paint_quad(quad(rect, px(2.), rgb(color), px(0.), transparent_black(), BorderStyle::default()));
            }
        }
    }
}
