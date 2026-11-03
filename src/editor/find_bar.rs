//! Floating find bar (top-right of the editor) and the highlight layer for matches.
use gpui::{BorderStyle, Bounds, Context, CursorStyle, MouseButton, Pixels, Window, div, prelude::*, px, quad, rgb, transparent_black};
use gpui_component::input::{Escape, Input};
use crate::{icons::{Icon, IconColors, icon}, theme::Theme};
use super::view::{Editor, ExitBlock, layout::TextLayout};

const MATCH: u32 = 0xfbe7a6;
const CURRENT: u32 = 0xf4c84f;
const RING: u32 = 0xc9971c;

fn tint(color: gpui::Hsla) -> IconColors { IconColors { line: color, fill: transparent_black(), accent: transparent_black() } }

fn button(glyph: Icon, id: &'static str, on: impl Fn(&mut Editor, &mut Window, &mut Context<Editor>) + 'static, cx: &mut Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean();
    div().id(id).size(px(24.)).flex_none().flex().items_center().justify_center().rounded(px(6.))
        .cursor(CursorStyle::PointingHand).hover(|s| s.bg(theme.hover))
        .child(icon(glyph, px(14.), tint(theme.text_secondary)))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| { on(this, window, cx); cx.stop_propagation(); }))
}

/// The bar itself; absolutely positioned, so the caller adds it to the editor root.
pub(super) fn find_bar(editor: &Editor, cx: &mut Context<Editor>) -> Option<impl IntoElement> {
    let find = editor.find.as_ref()?;
    let theme = Theme::ocean();
    let count = find.matches.len();
    let query_empty = find.input.read(cx).value().is_empty();
    let label = if query_empty { String::new() } else if count == 0 { "无结果".to_owned() } else { format!("{} / {}", find.current + 1, count) };
    Some(div().id("find-bar").absolute().top(px(10.)).right(px(18.)).w(px(340.)).h(px(36.)).px(px(8.))
        .flex().items_center().gap(px(4.)).rounded(px(8.)).bg(rgb(0xffffff)).border_1().border_color(rgb(0xe4e4e4))
        .shadow_md().text_size(px(12.)).text_color(theme.text).occlude()
        .on_action(cx.listener(|this, _: &Escape, window, cx| { this.close_find(window, cx); cx.stop_propagation(); }))
        .on_action(cx.listener(|this, _: &ExitBlock, window, cx| { this.find_step(false, window, cx); cx.stop_propagation(); }))
        .child(icon(Icon::FindSearch, px(14.), tint(theme.text_secondary)))
        .child(div().flex_1().min_w_0().child(Input::new(&find.input).appearance(false).bordered(false).focus_bordered(false)))
        .child(div().flex_none().min_w(px(44.)).flex().justify_end().text_color(theme.text_secondary).child(label))
        .child(div().w(px(1.)).h(px(16.)).flex_none().bg(rgb(0xeaeaea)))
        .child(button(Icon::ArrowUp, "find-prev", |this, w, cx| this.find_step(false, w, cx), cx))
        .child(button(Icon::ArrowDown, "find-next", |this, w, cx| this.find_step(true, w, cx), cx))
        .child(button(Icon::Close, "find-close", |this, w, cx| this.close_find(w, cx), cx)))
}

/// Paint all matches of `block` under the text; the current match is stronger.
pub(super) fn paint_matches(editor: &Editor, block: u64, layout: &TextLayout, window: &mut Window) {
    let Some((all, current)) = editor.find_ranges(block) else { return };
    for range in all {
        let is_cur = current.as_ref() == Some(&range);
        let color = if is_cur { CURRENT } else { MATCH };
        for rect in layout.rects(range) {
            let rect: Bounds<Pixels> = rect;
            if is_cur {
                window.paint_quad(quad(rect, px(2.), rgb(color), px(1.), rgb(RING), BorderStyle::Solid));
            } else {
                window.paint_quad(quad(rect, px(2.), rgb(color), px(0.), transparent_black(), BorderStyle::default()));
            }
        }
    }
}
