//! Shared feedback titlebar trigger and feedback dialog control primitives.
use std::time::Duration;
use gpui::{
    Animation, AnimationExt as _, App, AvailableSpace, CursorStyle, ElementId, FontWeight,
    InteractiveElement, IntoElement, MouseButton, ParentElement, SharedString,
    StatefulInteractiveElement, Styled, Window, div, prelude::FluentBuilder as _, px,
};
use gpui_component::animation::cubic_bezier;
use crate::icons::{Icon, icon, stateful_icon};
use crate::theme::{Theme, tooltip, tr};
use super::{modal::puff_burst_cluster, radius};

fn prepaint_element(bounds: gpui::Bounds<gpui::Pixels>, window: &mut Window, cx: &mut App, mut el: gpui::AnyElement) -> gpui::AnyElement {
    let space = gpui::size(AvailableSpace::Definite(bounds.size.width), AvailableSpace::Definite(bounds.size.height));
    el.prepaint_as_root(bounds.origin, space, window, cx);
    el
}

/// Titlebar feedback icon button with Duo two-color hover state, radial `.puff` particle burst,
/// damped wave rotation + spring bounce on hover/click, and an unsent-draft indicator dot.
pub fn titlebar_feedback_button(
    open: bool,
    has_draft: bool,
    on_activate: impl Fn(&mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let on_activate = std::rc::Rc::new(on_activate);
    div().id("titlebar-feedback-wrap").child(gpui::canvas(move |bounds, window, cx| {
        let state = window.use_keyed_state(
            ElementId::Name("titlebar-feedback-motion".into()),
            cx,
            |_, _| (false, 0u64),
        );
        let (hovered, seq) = *state.read(cx);
        let idle = if open {
            theme.icon_colors(theme.accent)
        } else {
            theme.mono_icon_colors(theme.text_secondary)
        };
        let duo_hover = if open {
            theme.icon_colors(theme.accent)
        } else {
            theme.icon_colors(theme.text)
        };
        let state_hover = state.clone();
        let state_click = state.clone();
        let on_activate = on_activate.clone();

        let icon_node = stateful_icon(
            "titlebar-feedback",
            Icon::MessageSquare,
            px(16.),
            open || hovered,
            idle,
            duo_hover,
        );
        let animated_icon = if seq > 0 {
            div()
                .flex()
                .items_center()
                .justify_center()
                .child(icon_node)
                .with_animation(
                    ElementId::NamedInteger("titlebar-feedback-wave".into(), seq),
                    Animation::new(Duration::from_millis(560))
                        .with_easing(cubic_bezier(0.22, 1.0, 0.36, 1.0)),
                    |el, t| {
                        let lift = -3.0 * (t * std::f32::consts::PI).sin() * (1.0 - t * 0.45);
                        el.mt(px(lift))
                    },
                )
                .into_any_element()
        } else {
            div().flex().items_center().justify_center().child(icon_node).into_any_element()
        };

        let halo = (seq > 0).then(|| {
            div()
                .absolute()
                .top(px(0.))
                .left(px(0.))
                .size(px(26.))
                .rounded(radius::CONTROL)
                .border_1()
                .with_animation(
                    ElementId::NamedInteger("titlebar-feedback-halo".into(), seq),
                    Animation::new(Duration::from_millis(520))
                        .with_easing(cubic_bezier(0.16, 1.0, 0.3, 1.0)),
                    move |el, t| {
                        let expand = t * 5.0;
                        let mut ring = theme.accent;
                        ring.a = 0.55 * (1.0 - t).powi(2);
                        el.top(px(-expand))
                            .left(px(-expand))
                            .size(px(26.0 + expand * 2.0))
                            .border_color(ring)
                    },
                )
        });

        let mut btn = div()
            .id("titlebar-feedback")
            .group("titlebar-feedback")
            .relative()
            .size(px(26.))
            .rounded(radius::CONTROL)
            .flex()
            .items_center()
            .justify_center()
            .cursor(CursorStyle::PointingHand)
            .when(open, |d| d.bg(theme.selected))
            .when(!open, |d| d.hover(move |s| s.bg(theme.hover)))
            .on_hover(move |&enter, _, cx| {
                let (was_hovered, cur_seq) = *state_hover.read(cx);
                if was_hovered != enter {
                    state_hover.write(cx, (enter, cur_seq));
                }
            })
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                cx.stop_propagation();
                let (was_hovered, cur_seq) = *state_click.read(cx);
                state_click.write(cx, (was_hovered, cur_seq.wrapping_add(1)));
                on_activate(window, cx);
            })
            .children(halo)
            .when(seq > 0, |d| d.child(puff_burst_cluster("titlebar-feedback-puff", seq, 26., 26.)))
            .child(animated_icon)
            .when(has_draft && !open, |d| {
                d.child(
                    div()
                        .id("titlebar-feedback-draft-dot")
                        .absolute()
                        .top(px(3.))
                        .right(px(3.))
                        .size(px(5.5))
                        .rounded_full()
                        .bg(theme.accent),
                )
            });
        if !open {
            btn = btn.tooltip(tooltip(tr("反馈", "Feedback")));
        }
        if super::smoke_overlay::is_smoke_active() {
            super::smoke_overlay::record_bounds("titlebar-feedback", bounds);
        }
        prepaint_element(bounds, window, cx, btn.into_any_element())
    }, |_, mut el, window, cx| el.paint(window, cx)).size(px(26.)))
}

/// Selectable category or OS chip for the feedback modal, with spring lift on selection.
pub fn feedback_chip(
    id: impl Into<SharedString>,
    label: impl Into<SharedString>,
    active: bool,
    seq: u64,
    on_pick: impl Fn(&mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let id = id.into();
    let label = label.into();
    let anim_id = SharedString::from(format!("{id}-spring"));
    let pill = div()
        .id(id.clone())
        .relative()
        .children(super::smoke_overlay::probe(id))
        .h(px(32.))
        .px(px(13.))
        .rounded(radius::CONTROL)
        .border_1()
        .flex()
        .items_center()
        .justify_center()
        .text_size(px(14.))
        .cursor(CursorStyle::PointingHand)
        .when(active, |d| {
            d.border_color(theme.accent)
                .bg(theme.selected)
                .text_color(theme.primary)
                .font_weight(FontWeight(600.))
        })
        .when(!active, |d| {
            d.border_color(theme.btn_border)
                .bg(theme.sidebar)
                .text_color(theme.text_secondary)
                .font_weight(FontWeight(500.))
                .hover(move |s| {
                    s.border_color(theme.accent)
                        .bg(theme.hover)
                        .text_color(theme.text)
                })
        })
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            cx.stop_propagation();
            on_pick(window, cx);
        })
        .child(label);
    if active && seq > 0 {
        pill.with_animation(
            ElementId::NamedInteger(anim_id, seq),
            Animation::new(Duration::from_millis(240))
                .with_easing(cubic_bezier(0.22, 1.0, 0.36, 1.0)),
            |el, t| el.mt(px(-2.0 * (t * std::f32::consts::PI).sin())),
        )
        .into_any_element()
    } else {
        pill.into_any_element()
    }
}

/// Primary send button with `.puff` particle burst on hover/click.
pub fn feedback_send_button(
    id: impl Into<SharedString>,
    label: impl Into<SharedString>,
    busy: bool,
    on_send: impl Fn(&mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let id = id.into();
    let label = label.into();
    let state_key = SharedString::from(format!("{id}-puff-seq"));
    let on_send = std::rc::Rc::new(on_send);
    div().child(gpui::canvas(move |bounds, window, cx| {
        let seq_state = window.use_keyed_state(ElementId::Name(state_key.clone()), cx, |_, _| 0u64);
        let seq = *seq_state.read(cx);
        let click_state = seq_state.clone();
        let on_send = on_send.clone();
        if super::smoke_overlay::is_smoke_active() {
            super::smoke_overlay::record_bounds(id.clone(), bounds);
        }
        let btn = div()
            .id(id.clone())
            .relative()
            .h(px(36.))
            .px(px(20.))
            .rounded(px(8.))
            .bg(theme.accent)
            .text_color(gpui::white())
            .text_size(px(14.))
            .font_weight(FontWeight(600.))
            .flex()
            .items_center()
            .justify_center()
            .when(busy, |d| d.opacity(0.6).cursor(CursorStyle::Arrow))
            .when(!busy, |d| d.cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.primary)))
            .when(seq > 0, |d| d.child(puff_burst_cluster("feedback-send-puff", seq, 84., 36.)))
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                cx.stop_propagation();
                if !busy {
                    let next = click_state.read(cx).wrapping_add(1);
                    click_state.write(cx, next);
                    on_send(window, cx);
                }
            })
            .child(label.clone());
        prepaint_element(bounds, window, cx, btn.into_any_element())
    }, |_, mut el, window, cx| el.paint(window, cx)).h(px(36.)).w(px(84.)))
}

/// Animated check badge for the feedback completion view.
pub fn feedback_done_badge(seq: u64) -> impl IntoElement {
    let theme = Theme::ocean();
    div()
        .relative()
        .size(px(46.))
        .rounded_full()
        .bg(theme.selected)
        .border_1()
        .border_color(theme.accent)
        .flex()
        .items_center()
        .justify_center()
        .child(puff_burst_cluster("feedback-done-puff", seq.max(1), 46., 46.))
        .child(icon(Icon::Check, px(22.), theme.icon_colors(theme.accent)))
        .with_animation(
            ElementId::NamedInteger("feedback-done-pop".into(), seq.max(1)),
            Animation::new(Duration::from_millis(360))
                .with_easing(cubic_bezier(0.22, 1.0, 0.36, 1.0)),
            |el, t| {
                let lift = -4.0 * (t * std::f32::consts::PI).sin();
                el.mt(px(lift)).opacity(0.35 + 0.65 * t)
            },
        )
}
