use std::time::Duration;
use gpui::{
    Animation, AnimationExt, App, BoxShadow, CursorStyle, Div, ElementId, FontWeight, IntoElement,
    MouseButton, MouseDownEvent, Pixels, SharedString, Stateful, Styled, Window, div, point,
    prelude::*, px, rgba,
};
use gpui_component::animation::cubic_bezier;
use crate::{
    icons::{Icon, icon, stateful_icon},
    theme::{Theme, is_dark},
    ui::{dropdown::shadows, motion::modal_enter, radius},
};

fn ease_note_spring() -> impl Fn(f32) -> f32 + 'static {
    cubic_bezier(0.22, 1.0, 0.36, 1.0)
}

fn ease_power3_out() -> impl Fn(f32) -> f32 + 'static {
    cubic_bezier(0.215, 0.61, 0.355, 1.0)
}

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

/// Interactive capsule link button modeled after `note.cooool.top`:
/// crisp offset 3D shadow (`5px 7px -> 2px 3px`), spring press-glide (`translate(3px, 4px)`),
/// and a radial 7-particle `.puff` burst (`scale 0.2 -> 1.0`, `opacity 0.95 -> 0`) on hover/click.
pub fn bubble_link_pill(
    id: &'static str,
    glyph: Icon,
    label: &'static str,
    meta: &'static str,
    url: &'static str,
    hovered: bool,
    burst_seq: u64,
    on_hover: impl Fn(&bool, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let group = SharedString::from(format!("pill-{id}"));
    let probe = crate::ui::smoke_overlay::probe(id);
    let shadow_color = if is_dark() {
        rgba(0x182e60c8)
    } else {
        rgba(0x3559a861)
    };
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
        .px(px(18.))
        .rounded(radius::PILL)
        .flex()
        .items_center()
        .gap(px(10.))
        .cursor(CursorStyle::PointingHand)
        .bg(if hovered { theme.selected } else { theme.shell.opacity(0.65) })
        .border_1()
        .border_color(if hovered { theme.accent } else { theme.accent.opacity(0.38) })
        .shadow(vec![BoxShadow {
            color: shadow_color.into(),
            offset: if hovered { point(px(2.), px(3.)) } else { point(px(5.), px(7.)) },
            blur_radius: px(0.),
            spread_radius: px(0.),
            inset: false,
        }])
        .hover(|s| s.bg(theme.selected).border_color(theme.accent))
        .on_hover(on_hover)
        .on_mouse_down(MouseButton::Left, move |_, _, cx| {
            cx.stop_propagation();
            cx.open_url(url);
        })
        .when(burst_seq > 0, |d| d.child(puff_burst_cluster(id, burst_seq, 320., 23.)))
        .child(lead)
        .child(
            div()
                .flex_1()
                .font_weight(FontWeight(500.))
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
                .when(hovered, |d| d.left(px(3.)).top(px(-2.)))
                .child(icon(Icon::LinkOpen, px(14.), trail_colors)),
        );
    if hovered {
        pill.with_animation(
            SharedString::from(format!("{id}-glide-{burst_seq}")),
            Animation::new(Duration::from_millis(280)).with_easing(ease_note_spring()),
            |el, delta| el.left(px(3.0 * delta)).top(px(4.0 * delta)),
        )
        .into_any_element()
    } else {
        pill.into_any_element()
    }
}

/// Compact author/profile link pill with pointer cursor and radial `.puff` burst on hover.
pub fn author_puff_link(
    id: &'static str,
    label: &'static str,
    url: &'static str,
    hovered: bool,
    burst_seq: u64,
    on_hover: impl Fn(&bool, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let probe = crate::ui::smoke_overlay::probe(id);
    let el = div()
        .id(id)
        .relative()
        .children(probe)
        .px(px(12.))
        .py(px(3.))
        .rounded(radius::PILL)
        .flex()
        .items_center()
        .gap(px(6.))
        .cursor(CursorStyle::PointingHand)
        .bg(if hovered { theme.hover } else { gpui::transparent_black() })
        .text_size(px(14.))
        .line_height(px(20.))
        .text_color(if hovered { theme.accent } else { theme.text_secondary })
        .hover(|s| s.bg(theme.hover).text_color(theme.accent))
        .on_hover(on_hover)
        .on_mouse_down(MouseButton::Left, move |_, _, cx| {
            cx.stop_propagation();
            cx.open_url(url);
        })
        .when(burst_seq > 0, |d| d.child(puff_burst_cluster(id, burst_seq, 136., 13.)))
        .child(label);
    if hovered {
        el.with_animation(
            SharedString::from(format!("{id}-lift-{burst_seq}")),
            Animation::new(Duration::from_millis(260)).with_easing(ease_note_spring()),
            |d, delta| d.top(px(-1.5 * delta)),
        )
        .into_any_element()
    } else {
        el.into_any_element()
    }
}

/// Radial 7-puff particle burst ported from `https://note.cooool.top/`:
/// spawns across the button width at vertical center, expands from `scale 0.2 -> 1.0`,
/// travels along `(dx, dy)` (biased upward by `-20px`), and fades `opacity -> 0` over ~1s.
fn puff_burst_cluster(id: &'static str, seq: u64, width: f32, cy: f32) -> impl IntoElement {
    let theme = Theme::ocean();
    // (x_ratio, diameter_px, dx_px, dy_px, duration_ms, use_icon_fill)
    const PUFFS: [(f32, f32, f32, f32, u64, bool); 7] = [
        (0.18, 16., -54., -38., 950, false),
        (0.30, 26., -26., -52., 1080, true),
        (0.45, 14., 8., -56., 920, false),
        (0.60, 22., 44., -44., 1140, true),
        (0.76, 13., 62., -18., 1020, false),
        (0.24, 18., -58., 14., 1100, false),
        (0.70, 15., 38., 20., 980, true),
    ];
    div()
        .absolute()
        .top_0()
        .left_0()
        .w_full()
        .h(px(0.))
        .children(PUFFS.into_iter().enumerate().map(
            move |(ix, (xr, full_sz, dx, dy, dur_ms, alt))| {
                let anim_id = SharedString::from(format!("{id}-puff-{seq}-{ix}"));
                let base_x = width * xr;
                let fill = if alt { theme.icon_fill } else { theme.accent };
                let base_alpha = if alt { 0.68 } else { 0.34 };
                div()
                    .absolute()
                    .rounded(radius::PILL)
                    .border_1()
                    .with_animation(
                        anim_id,
                        Animation::new(Duration::from_millis(dur_ms)).with_easing(ease_power3_out()),
                        move |el, delta| {
                            let scale = 0.2 + 0.8 * delta;
                            let sz = full_sz * scale;
                            let x = base_x + dx * delta - sz * 0.5;
                            let y = cy + dy * delta - sz * 0.5;
                            let fade = (1.0 - delta) * (1.0 - delta);
                            el.left(px(x))
                                .top(px(y))
                                .size(px(sz))
                                .bg(fill.opacity(base_alpha * fade))
                                .border_color(theme.accent.opacity(0.52 * fade))
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
