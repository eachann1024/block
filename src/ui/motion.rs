use std::time::Duration;
use gpui::{Animation, AnimationExt, ElementId, IntoElement, Styled, px};
use gpui_component::animation::cubic_bezier;

pub fn ease_out_expo() -> impl Fn(f32) -> f32 + 'static {
    cubic_bezier(0.16, 1.0, 0.3, 1.0)
}

pub fn ease_out_quart() -> impl Fn(f32) -> f32 + 'static {
    cubic_bezier(0.22, 1.0, 0.36, 1.0)
}

pub fn popup_enter<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    el.with_animation(
        id,
        Animation::new(Duration::from_millis(160)).with_easing(ease_out_expo()),
        |el, delta| el.opacity(delta).relative().top(px(-4.0 * (1.0 - delta))),
    )
}

pub fn bar_enter<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    el.with_animation(
        id,
        Animation::new(Duration::from_millis(180)).with_easing(ease_out_expo()),
        |el, delta| el.opacity(delta).relative().top(px(6.0 * (1.0 - delta))),
    )
}

pub fn reveal_fade<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    el.with_animation(
        id,
        Animation::new(Duration::from_millis(120)).with_easing(ease_out_quart()),
        |el, delta| el.opacity(delta),
    )
}

pub fn tooltip_enter<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    el.with_animation(
        id,
        Animation::new(Duration::from_millis(100)),
        |el, delta| el.opacity(delta),
    )
}

pub fn modal_enter<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    el.with_animation(
        id,
        Animation::new(Duration::from_millis(180)).with_easing(ease_out_expo()),
        |el, delta| el.opacity(delta).mt(px(-8.0 * (1.0 - delta))),
    )
}

pub fn slide_in_right<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    el.with_animation(
        id,
        Animation::new(Duration::from_millis(240)).with_easing(cubic_bezier(0.2, 0.8, 0.2, 1.0)),
        |el, delta| el.opacity(delta).relative().left(px(16.0 * (1.0 - delta))),
    )
}

pub fn fade_up<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    el.with_animation(
        id,
        Animation::new(Duration::from_millis(300)).with_easing(ease_out_quart()),
        |el, delta| el.opacity(delta).relative().top(px(4.0 * (1.0 - delta))),
    )
}

