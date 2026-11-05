//! Restore confirmation modal dialog for Page History.
use std::rc::Rc;
use gpui::{
    App, CursorStyle, FontWeight, IntoElement, MouseButton, MouseDownEvent,
    ParentElement, Stateful, Styled, Window, div, prelude::*, px,
};
use crate::{
    icons::{icon, Icon},
    theme::{tr, Theme},
    ui::{modal, radius},
};

pub fn render_restore_modal(
    version_time: &str,
    on_cancel: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_confirm: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let on_cancel: Rc<dyn Fn(&MouseDownEvent, &mut Window, &mut App)> = Rc::new(on_cancel);
    let on_cancel_close = on_cancel.clone();
    let on_cancel_btn = on_cancel.clone();
    let on_cancel_bg = on_cancel;
    let title = if crate::theme::is_en() {
        format!("Restore to \"{version_time}\"?")
    } else {
        format!("恢复到「{version_time}」？")
    };
    let body = tr("当前内容会保留在历史中。", "Current content will be retained in history.");

    let card = modal::card("restore-confirm-dialog", px(400.))
        .p(px(20.))
        .gap(px(14.))
        .child(
            div()
                .flex()
                .items_center()
                .justify_between()
                .child(
                    div()
                        .text_size(px(16.))
                        .font_weight(FontWeight(560.))
                        .text_color(theme.text)
                        .child(title),
                )
                .child(
                    div()
                        .id("restore-modal-close")
                        .size(px(28.))
                        .rounded(radius::CONTROL)
                        .flex()
                        .items_center()
                        .justify_center()
                        .cursor(CursorStyle::PointingHand)
                        .hover(|s| s.bg(theme.hover))
                        .on_mouse_down(MouseButton::Left, move |ev, w, cx| on_cancel_close(ev, w, cx))
                        .child(icon(Icon::Close, px(14.), theme.icon_colors(theme.text_secondary))),
                ),
        )
        .child(
            div()
                .text_size(px(14.))
                .line_height(px(20.))
                .text_color(theme.text_secondary)
                .child(body),
        )
        .child(
            div()
                .flex()
                .justify_end()
                .gap(px(8.))
                .pt(px(6.))
                .child(modal_btn("restore-cancel-btn", tr("取消", "Cancel"), false, move |ev, w, cx| on_cancel_btn(ev, w, cx)))
                .child(modal_btn("restore-confirm-btn", tr("恢复", "Restore"), true, on_confirm)),
        );

    modal::backdrop("restore-confirm-overlay", card)
        .on_mouse_down(MouseButton::Left, move |ev, w, cx| on_cancel_bg(ev, w, cx))
}

fn modal_btn(
    id: &'static str,
    label: &'static str,
    primary: bool,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .h(px(32.))
        .px(px(14.))
        .rounded(radius::CONTROL)
        .flex()
        .items_center()
        .justify_center()
        .text_size(px(14.))
        .font_weight(FontWeight(460.))
        .cursor(CursorStyle::PointingHand)
        .when(primary, |b| {
            b.bg(theme.accent)
                .text_color(gpui::white())
                .hover(|s| s.opacity(0.9))
        })
        .when(!primary, |b| {
            b.bg(theme.paper)
                .text_color(theme.text)
                .border_1()
                .border_color(theme.btn_border)
                .hover(|s| s.bg(theme.hover))
        })
        .on_mouse_down(MouseButton::Left, on_down)
        .child(label)
}
