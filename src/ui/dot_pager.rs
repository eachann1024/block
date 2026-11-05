//! Minimal dot/pill pager indicator for switching between a small fixed set of slots or pages.
use std::rc::Rc;
use gpui::{
    App, CursorStyle, Div, ElementId, Hsla, MouseButton, SharedString, Window, div, prelude::*, px,
    rgb,
};
use crate::theme::{Theme, is_dark};

pub fn dot_pager(
    id_prefix: &'static str,
    count: usize,
    active: usize,
    tip: impl Into<SharedString>,
    on_pick: impl Fn(usize, &mut Window, &mut App) + 'static,
) -> Div {
    let theme = Theme::ocean();
    let dark = is_dark();
    let idle_col: Hsla = if dark { rgb(0x384152).into() } else { rgb(0xc6cfd9).into() };
    let hover_col: Hsla = theme.text_secondary;
    let on_pick = Rc::new(on_pick);
    let tip: SharedString = tip.into();

    div()
        .flex()
        .items_center()
        .gap(px(2.))
        .occlude()
        .children((0..count).map(move |ix| {
            let is_on = ix == active;
            let pick = on_pick.clone();
            let tip = tip.clone();
            let group_name: SharedString = format!("{id_prefix}-dot-grp-{ix}").into();
            div()
                .id(ElementId::NamedInteger(id_prefix.into(), ix as u64))
                .group(group_name.clone())
                .w(px(22.))
                .h(px(24.))
                .rounded(crate::ui::radius::CONTROL)
                .flex()
                .items_center()
                .justify_center()
                .cursor(CursorStyle::PointingHand)
                .tooltip(crate::ui::tooltip::tooltip(tip))
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    pick(ix, window, cx);
                })
                .child(
                    div()
                        .w(px(if is_on { 18. } else { 6. }))
                        .h(px(6.))
                        .rounded(crate::ui::radius::PILL)
                        .bg(if is_on { theme.accent } else { idle_col })
                        .when(!is_on, |dot| {
                            dot.group_hover(group_name, move |s| s.bg(hover_col))
                        }),
                )
        }))
}
