use std::time::Duration;
use gpui::{
    Animation, AnimationExt, App, BoxShadow, CursorStyle, Div, ElementId, FontWeight, IntoElement,
    MouseButton, MouseDownEvent, Pixels, SharedString, Stateful, Styled, Window, div, point,
    prelude::*, px, rgba,
};
use crate::{
    icons::{Icon, icon, stateful_icon},
    theme::Theme,
    ui::{
        dropdown::shadows,
        motion::{ease_out_expo, modal_enter},
        radius,
    },
};

pub fn backdrop(
    id: impl Into<ElementId>,
    child: impl IntoElement + Styled + AnimationExt + 'static,
) -> Stateful<Div> {
    div()
        .id(id)
        .absolute()
        .top_0()
        .left_0()
        .size_full()
        .occlude()
        .bg(rgba(0x20242c2e))
        .flex()
        .flex_col()
        .items_center()
        .pt(px(96.))
        .px(px(24.))
        .child(modal_enter(child, "modal-card-enter"))
}

pub fn card(id: impl Into<ElementId>, width: Pixels) -> Stateful<Div> {
    let theme = Theme::ocean();
    div()
        .id(id)
        .w(width)
        .max_w_full()
        .flex()
        .flex_col()
        .overflow_hidden()
        .rounded(radius::SURFACE)
        .bg(theme.menu)
        .border_1()
        .border_color(theme.menu_border)
        .shadow(shadows())
        .text_color(theme.text)
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
}

/// Interactive capsule link button with tactile 3D rim lift and floating Ocean bubbles on hover.
pub fn bubble_link_pill(
    id: &'static str,
    glyph: Icon,
    label: &'static str,
    meta: &'static str,
    url: &'static str,
    hovered: bool,
    on_hover: impl Fn(&bool, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let group = SharedString::from(format!("pill-{id}"));
    let probe = crate::ui::smoke_overlay::probe(id);
    let lead = stateful_icon(
        group.clone(),
        glyph,
        px(16.),
        hovered,
        theme.icon_colors(theme.accent),
        theme.icon_colors(theme.accent),
    );
    let trail_colors = if hovered {
        theme.icon_colors(theme.accent)
    } else {
        theme.mono_icon_colors(theme.text_secondary)
    };
    let pill = div()
        .id(id)
        .group(group)
        .relative()
        .children(probe)
        .w_full()
        .h(px(46.))
        .px(px(16.))
        .rounded(radius::PILL)
        .flex()
        .items_center()
        .gap(px(10.))
        .cursor(CursorStyle::PointingHand)
        .bg(if hovered { theme.selected } else { theme.shell.opacity(0.55) })
        .border_1()
        .border_color(if hovered { theme.accent } else { theme.btn_border })
        .shadow(if hovered {
            vec![
                BoxShadow {
                    color: theme.accent.opacity(0.55),
                    offset: point(px(0.), px(3.5)),
                    blur_radius: px(0.),
                    spread_radius: px(0.),
                    inset: false,
                },
                BoxShadow {
                    color: theme.accent.opacity(0.20),
                    offset: point(px(0.), px(8.)),
                    blur_radius: px(18.),
                    spread_radius: px(0.),
                    inset: false,
                },
            ]
        } else {
            vec![BoxShadow {
                color: theme.accent.opacity(0.10),
                offset: point(px(0.), px(2.)),
                blur_radius: px(0.),
                spread_radius: px(0.),
                inset: false,
            }]
        })
        .hover(|s| s.bg(theme.selected).border_color(theme.accent))
        .on_hover(on_hover)
        .on_mouse_down(MouseButton::Left, move |_, _, cx| {
            cx.stop_propagation();
            cx.open_url(url);
        })
        .when(hovered, |d| d.child(bubble_cluster(id)))
        .child(lead)
        .child(
            div()
                .flex_1()
                .font_weight(FontWeight(460.))
                .text_color(theme.text)
                .child(label),
        )
        .child(
            div()
                .font_family("JetBrains Mono")
                .text_size(px(13.))
                .text_color(if hovered { theme.accent } else { theme.text_secondary })
                .child(meta),
        )
        .child(
            div()
                .relative()
                .when(hovered, |d| d.left(px(2.)).top(px(-2.)))
                .child(icon(Icon::LinkOpen, px(14.), trail_colors)),
        );
    if hovered {
        pill.with_animation(
            SharedString::from(format!("{id}-lift")),
            Animation::new(Duration::from_millis(240)).with_easing(ease_out_expo()),
            |el, delta| el.top(px(-2.5 * delta)),
        )
        .into_any_element()
    } else {
        pill.into_any_element()
    }
}

fn bubble_cluster(id: &'static str) -> impl IntoElement {
    let theme = Theme::ocean();
    // Spec: (size, left_px, target_top_px, fill_alpha, border_alpha, use_icon_fill)
    const BUBBLES: [(f32, f32, f32, f32, f32, bool); 5] = [
        (10., 32., -13., 0.22, 0.45, false),
        (19., 40., -20., 0.20, 0.50, false),
        (12., 53., -10., 0.55, 0.52, true),
        (13., 74., -17., 0.22, 0.48, false),
        (9., 268., -13., 0.20, 0.42, false),
    ];
    div()
        .absolute()
        .top_0()
        .left_0()
        .w_full()
        .h(px(0.))
        .children(BUBBLES.into_iter().enumerate().map(
            move |(ix, (sz, left, target_top, fill_a, bd_a, alt))| {
                let anim_id = SharedString::from(format!("{id}-bubble-{ix}"));
                let base_fill = if alt { theme.icon_fill } else { theme.accent };
                div()
                    .absolute()
                    .left(px(left))
                    .size(px(sz))
                    .rounded(radius::PILL)
                    .border_1()
                    .with_animation(
                        anim_id,
                        Animation::new(Duration::from_millis(380 + (ix as u64) * 35))
                            .with_easing(ease_out_expo()),
                        move |el, delta| {
                            el.top(px(2.0 + (target_top - 2.0) * delta))
                                .bg(base_fill.opacity(fill_a * delta))
                                .border_color(theme.accent.opacity(bd_a * delta))
                        },
                    )
            },
        ))
}

pub fn action_button(
    id: &'static str,
    label: &'static str,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let probe = crate::ui::smoke_overlay::probe(id);
    div()
        .id(id)
        .relative()
        .children(probe)
        .w_full()
        .h(px(38.))
        .px(px(16.))
        .rounded(radius::CONTROL)
        .bg(theme.paper)
        .border_1()
        .border_color(theme.btn_border)
        .flex()
        .items_center()
        .justify_center()
        .font_weight(FontWeight(460.))
        .text_color(theme.text)
        .cursor(CursorStyle::PointingHand)
        .hover(|s| s.bg(theme.hover).border_color(theme.accent))
        .on_mouse_down(MouseButton::Left, on_down)
        .child(label)
}
