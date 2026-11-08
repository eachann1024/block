use std::rc::Rc;

use gpui::{
    AnyElement, Context, FontWeight, HoverListenerMode, InteractiveElement, IntoElement,
    MouseButton, ParentElement, Pixels, Rgba, StatefulInteractiveElement, Styled, Window, div, px,
    rgb,
};

use super::{motion, popover, radius, smoke_overlay};

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OutlineEntry {
    pub block_id: u64,
    pub level: u8,
    pub title: String,
    pub active: bool,
    pub bar_lit: bool,
}

pub fn bar_width(level: u8) -> Pixels {
    match level {
        0 | 1 => px(20.),
        2 => px(14.),
        _ => px(10.),
    }
}

pub fn bar_color(bar_lit: bool) -> Rgba {
    match (crate::theme::is_dark(), bar_lit) {
        (true, true) => rgb(0xc6c6c6),
        (true, false) => rgb(0x464646),
        (false, true) => crate::theme::Theme::ocean().text.into(),
        (false, false) => rgb(0xcfd5de),
    }
}

pub fn outline_rail<V: 'static>(
    entries: Vec<OutlineEntry>,
    hovered: bool,
    on_hover_change: impl Fn(&mut V, bool, &mut Window, &mut Context<V>) + 'static,
    on_select: impl Fn(&mut V, u64, &mut Window, &mut Context<V>) + 'static,
    cx: &mut Context<V>,
) -> AnyElement {
    let on_hover = Rc::new(on_hover_change);
    let on_select = Rc::new(on_select);
    let wrap = div().id("outline-rail-wrap").absolute().left(px(18.)).top(px(36.))
        .occlude()
        .hover_listener_mode(HoverListenerMode::InputModalityIndependent)
        .on_hover(cx.listener(move |this, is_hovered: &bool, window, cx| {
            on_hover(this, *is_hovered, window, cx)
        }));

    if let Some((phase, pop_gen)) = motion::popup_motion("outline-popover", hovered) {
        motion::schedule_popup_finish("outline-popover", pop_gen, cx);
        let theme = crate::theme::Theme::ocean();
        let mut card = div().id("outline-popover").relative().w(px(236.)).max_h(px(360.))
            .overflow_y_scroll().p(px(6.)).rounded(radius::SURFACE).bg(theme.menu)
            .border_1().border_color(theme.hairline).shadow(popover::shadows())
            .flex().flex_col().gap(px(2.))
            .children(smoke_overlay::probe("outline-rail"))
            .children(smoke_overlay::probe("outline-popover"));
        for (idx, entry) in entries.into_iter().enumerate() {
            let block_id = entry.block_id;
            let on_select = on_select.clone();
            let indent = match entry.level { 0 | 1 => px(8.), 2 => px(18.), _ => px(28.) };
            let row = div().id(("outline-item", block_id as usize)).relative().w_full().h(px(30.))
                .pl(indent).pr(px(8.)).rounded(px(6.)).flex().items_center().gap(px(8.))
                .cursor_pointer();
            let row = if entry.active {
                row.bg(theme.selected).border_1().border_color(theme.accent)
                    .text_color(theme.text).font_weight(FontWeight::MEDIUM)
            } else {
                let fg = if entry.bar_lit { theme.text } else { theme.text_secondary };
                let hover_bg = theme.hover;
                let hover_fg = theme.text;
                row.text_color(fg).hover(move |style| style.bg(hover_bg).text_color(hover_fg))
            };
            card = card.child(
                row.on_mouse_down(MouseButton::Left, |_, window, cx| {
                    window.prevent_default();
                    cx.stop_propagation();
                })
                .on_click(cx.listener(move |this, _, window, cx| {
                    on_select(this, block_id, window, cx)
                }))
                .children(smoke_overlay::probe(format!("outline-item-{idx}")))
                .children(smoke_overlay::probe(format!("outline-bar-{idx}")))
                .child(div().w(bar_width(entry.level)).h(px(2.)).rounded(px(1.))
                    .bg(bar_color(entry.bar_lit)).flex_none())
                .child(div().flex_1().min_w_0().truncate().text_size(px(13.))
                    .line_height(px(18.)).child(entry.title)),
            );
        }
        let animated = match phase {
            motion::PopupMotion::Enter => motion::popup_enter(card, ("outline-popover-motion", pop_gen)).into_any_element(),
            motion::PopupMotion::Exit => motion::popup_exit(card, ("outline-popover-motion", pop_gen)).into_any_element(),
        };
        return wrap.child(animated).into_any_element();
    }

    let mut rail = div().id("outline-rail").relative().flex().flex_col().items_start()
        .gap(px(7.)).py(px(4.)).pr(px(8.))
        .children(smoke_overlay::probe("outline-rail"));
    for (idx, entry) in entries.into_iter().enumerate() {
        let block_id = entry.block_id;
        let on_select = on_select.clone();
        rail = rail.child(
            div().id(("outline-bar", block_id as usize)).relative().h(px(6.)).pr(px(6.))
                .flex().items_center().justify_start().cursor_pointer()
                .on_mouse_down(MouseButton::Left, |_, window, cx| {
                    window.prevent_default();
                    cx.stop_propagation();
                })
                .on_click(cx.listener(move |this, _, window, cx| {
                    on_select(this, block_id, window, cx)
                }))
                .children(smoke_overlay::probe(format!("outline-bar-{idx}")))
                .child(div().w(bar_width(entry.level)).h(px(2.)).rounded(px(1.))
                    .bg(bar_color(entry.bar_lit))),
        );
    }
    wrap.child(rail).into_any_element()
}
