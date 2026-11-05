//! AI 开关胶囊：外层命中盒静止，悬停上浮与开启爆发分开播放。
use std::f32::consts::{PI, TAU};
use std::time::Duration;
use gpui::{
    Animation, AnimationExt, AnyElement, App, BoxShadow, CursorStyle, FontWeight, IntoElement,
    MouseButton, MouseDownEvent, SharedString, Window, div, point, prelude::*, px,
};
use gpui_component::animation::cubic_bezier;
use crate::{
    icons::{Icon, icon},
    theme::Theme,
    ui::{motion::{ease_out_expo, ease_out_quart}, radius, settings_controls::selected_bg},
};

#[derive(Clone, Copy, Debug, Default)]
pub struct TogglePillState {
    pub hovered: bool,
    pub hover_seq: u64,
    pub toggle_seq: u64,
}

impl TogglePillState {
    /// 更新悬停。仅在进入悬停时递增 `hover_seq`，变化时返回 `true`。
    pub fn set_hovered(&mut self, active: bool) -> bool {
        let changed = self.hovered != active;
        self.hovered = active;
        if changed && active {
            self.hover_seq = self.hover_seq.saturating_add(1);
        }
        changed
    }

    pub fn record_toggle(&mut self) {
        self.toggle_seq = self.toggle_seq.saturating_add(1);
    }
}

pub fn toggle_pill(
    id: &'static str,
    glyph: Icon,
    on: bool,
    label: impl Into<SharedString>,
    state: TogglePillState,
    on_hover: impl Fn(&bool, &mut Window, &mut App) + 'static,
    on_toggle: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let group = SharedString::from(id);
    let enabling = state.toggle_seq > 0 && on;
    div()
        .id(id)
        .group(group)
        .relative()
        .children(crate::ui::smoke_overlay::probe(id))
        .h(px(32.))
        .cursor(CursorStyle::PointingHand)
        .on_hover(on_hover)
        .on_mouse_down(MouseButton::Left, on_toggle)
        .when(state.hovered, |d| d.child(hover_twinkles(id, state.hover_seq)))
        .when(enabling, |d| {
            d.child(enable_puff_burst(id, state.toggle_seq)).child(enable_halo_ring(id, state.toggle_seq))
        })
        .child(pill_surface(id, glyph, on, label.into(), state))
}

fn pill_surface(id: &'static str, glyph: Icon, on: bool, label: SharedString, state: TogglePillState) -> AnyElement {
    let theme = Theme::ocean();
    let face = div()
        .relative()
        .h(px(32.))
        .px(px(12.))
        .rounded(radius::CONTROL)
        .flex()
        .items_center()
        .gap(px(7.))
        .text_size(px(14.))
        .font_weight(FontWeight(460.))
        .text_color(theme.text)
        .bg(if on { selected_bg() } else if state.hovered { theme.hover } else { theme.shell })
        .border_1()
        .border_color(if on || state.hovered { theme.accent } else { theme.btn_border })
        .child(motion_icon(id, glyph, on, state))
        .child(label);
    let popped = if state.toggle_seq == 0 {
        face.into_any_element()
    } else {
        let enabling = on;
        let anim = Animation::new(Duration::from_millis(if enabling { 340 } else { 180 }));
        let anim = if enabling { anim } else { anim.with_easing(ease_out_quart()) };
        face.with_animation(
            SharedString::from(format!("{id}-pop-{}", state.toggle_seq)),
            anim,
            move |el, delta| el.top(px(if enabling { pop_y(delta) } else { 1.2 * (delta * PI).sin() })),
        )
        .into_any_element()
    };
    if !state.hovered {
        return popped;
    }
    let seq = state.hover_seq;
    div().relative().rounded(radius::CONTROL).child(popped).with_animation(
        SharedString::from(format!("{id}-lift-{seq}")),
        Animation::new(Duration::from_millis(240)).with_easing(cubic_bezier(0.22, 1.0, 0.36, 1.0)),
        move |el, delta| {
            el.top(px(-1.5 * delta)).shadow(vec![BoxShadow {
                color: theme.accent.opacity(0.16),
                offset: point(px(0.), px(5.)),
                blur_radius: px(14.),
                spread_radius: px(0.),
                inset: false,
            }])
        },
    )
    .into_any_element()
}

fn pop_y(delta: f32) -> f32 {
    if delta < 0.28 {
        1.4 * (delta / 0.28)
    } else {
        -2.2 * ((delta - 0.28) / 0.72 * PI).sin() * (1.0 - delta)
    }
}

fn motion_icon(id: &'static str, glyph: Icon, on: bool, state: TogglePillState) -> AnyElement {
    let theme = Theme::ocean();
    let colors = if on || state.hovered {
        theme.icon_colors(theme.accent)
    } else {
        theme.mono_icon_colors(theme.text_secondary)
    };
    let glyph = div().relative().child(icon(glyph, px(15.), colors));
    let leapt = if state.toggle_seq > 0 && on {
        glyph
            .with_animation(
                SharedString::from(format!("{id}-leap-{}", state.toggle_seq)),
                Animation::new(Duration::from_millis(340)),
                |el, delta| el.top(px(-2.6 * (delta * PI).sin())),
            )
            .into_any_element()
    } else {
        glyph.into_any_element()
    };
    if !state.hovered {
        return leapt;
    }
    let seq = state.hover_seq;
    div().relative().child(leapt).with_animation(
        SharedString::from(format!("{id}-float-{seq}")),
        Animation::new(Duration::from_millis(340)),
        |el, delta| el.top(px(-1.4 * (delta * PI).sin())).left(px(0.8 * (delta * TAU).sin())),
    )
    .into_any_element()
}

fn hover_twinkles(id: &'static str, seq: u64) -> impl IntoElement {
    let theme = Theme::ocean();
    const SPARKS: [(f32, f32, f32); 3] = [(11., 7., 3.6), (22., 15., 4.8), (14., 24., 4.2)];
    div().absolute().top_0().left_0().size(px(0.)).children(SPARKS.into_iter().enumerate().map(move |(ix, (x, y, sz))| {
        div().absolute().rounded(radius::PILL).with_animation(
            SharedString::from(format!("{id}-twinkle-{seq}-{ix}")),
            Animation::new(Duration::from_millis(460)),
            move |el, delta| {
                el.left(px(x)).top(px(y)).size(px(sz)).bg(theme.accent.opacity(0.9 * (1.0 - delta).powi(2)))
            },
        )
    }))
}

fn enable_halo_ring(id: &'static str, seq: u64) -> impl IntoElement {
    let theme = Theme::ocean();
    div().absolute().rounded(radius::CONTROL).border_1().with_animation(
        SharedString::from(format!("{id}-halo-{seq}")),
        Animation::new(Duration::from_millis(480)).with_easing(ease_out_expo()),
        move |el, delta| {
            let inset = px(-6. * delta);
            el.top(inset).left(inset).right(inset).bottom(inset).border_color(theme.accent.opacity(0.62 * (1.0 - delta)))
        },
    )
}

fn enable_puff_burst(id: &'static str, seq: u64) -> impl IntoElement {
    let theme = Theme::ocean();
    const PUFFS: [(f32, f32, f32, f32, u64); 9] = [
        (0.10, 12., -42., -30., 880),
        (0.22, 16., -24., -46., 1040),
        (0.34, 11., 2., -50., 920),
        (0.48, 18., 22., -44., 1120),
        (0.62, 13., 46., -28., 980),
        (0.78, 15., 58., -8., 1080),
        (0.18, 14., -52., 16., 1000),
        (0.55, 12., 28., 18., 900),
        (0.84, 10., 40., 10., 1100),
    ];
    div().absolute().top_0().left_0().w_full().h(px(0.)).children(PUFFS.into_iter().enumerate().map(
        move |(ix, (xr, full, dx, dy, dur))| {
            let fill = if ix % 2 == 0 { theme.icon_fill } else { theme.accent };
            let x0 = 96. * xr;
            div().absolute().rounded(radius::PILL).with_animation(
                SharedString::from(format!("{id}-puff-{seq}-{ix}")),
                Animation::new(Duration::from_millis(dur)).with_easing(cubic_bezier(0.215, 0.61, 0.355, 1.0)),
                move |el, delta| {
                    let sz = full * (0.2 + 0.8 * delta);
                    let fade = (1.0 - delta).powi(2);
                    el.left(px(x0 + dx * delta - sz * 0.5))
                        .top(px(16. + dy * delta - sz * 0.5))
                        .size(px(sz))
                        .bg(fill.opacity(fade))
                },
            )
        },
    ))
}
