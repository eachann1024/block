use std::any::Any;
use std::cell::RefCell;
use std::collections::HashMap;
use std::time::Duration;
use gpui::{Animation, AnimationExt, AnyElement, App, Context, ElementId, Entity, IntoElement, Styled, px};
use gpui_component::animation::cubic_bezier;

/// Shared by every menu, popover and scrim so open and close take the same time.
pub const POPUP_MS: u64 = 160;

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum PopupMotion {
    Enter,
    Exit,
}

#[derive(Default)]
struct Slot {
    visible: bool,
    leaving: bool,
    armed: bool,
    pop_gen: u64,
}

thread_local! {
    static SLOTS: RefCell<HashMap<String, Slot>> = RefCell::new(HashMap::new());
    static HELD: RefCell<HashMap<String, Box<dyn Any>>> = RefCell::new(HashMap::new());
}

fn mark_open(slot: &mut Slot) -> (PopupMotion, u64) {
    let restart = !slot.visible || slot.leaving;
    slot.visible = true;
    slot.leaving = false;
    slot.armed = false;
    if restart { slot.pop_gen = slot.pop_gen.wrapping_add(1); }
    (PopupMotion::Enter, slot.pop_gen)
}

fn mark_exit(slot: &mut Slot) -> Option<(PopupMotion, u64)> {
    if !slot.visible { return None; }
    if !slot.leaving {
        slot.leaving = true;
        slot.armed = false;
        slot.pop_gen = slot.pop_gen.wrapping_add(1);
    }
    Some((PopupMotion::Exit, slot.pop_gen))
}

/// Keeps a popup mounted for one exit after `open` becomes false.
/// The generation changes only when the direction changes, so the animation plays once.
pub fn popup_motion(key: &str, open: bool) -> Option<(PopupMotion, u64)> {
    SLOTS.with(|slots| {
        let mut slots = slots.borrow_mut();
        if open {
            if let Some(slot) = slots.get_mut(key) { return Some(mark_open(slot)); }
            let mut slot = Slot::default();
            let motion = mark_open(&mut slot);
            slots.insert(key.to_owned(), slot);
            Some(motion)
        } else {
            slots.get_mut(key).and_then(mark_exit)
        }
    })
}

/// Remembers `current` so the popup can keep drawing for one exit after it becomes `None`.
pub fn popup_hold<T: Clone + 'static>(key: &str, current: Option<T>) -> Option<(PopupMotion, u64, T)> {
    let (phase, pop_gen) = popup_motion(key, current.is_some())?;
    if let Some(value) = current {
        HELD.with(|held| { held.borrow_mut().insert(key.to_owned(), Box::new(value)); });
    }
    let value = HELD.with(|held| held.borrow().get(key).and_then(|item| item.downcast_ref::<T>().cloned()))?;
    Some((phase, pop_gen, value))
}

fn drop_held(key: &str) {
    HELD.with(|held| { held.borrow_mut().remove(key); });
}

/// Drops a popup immediately. Use when its host unmounts and an exit would play on the next visit.
pub fn popup_reset(key: &str) {
    SLOTS.with(|slots| {
        if let Some(slot) = slots.borrow_mut().get_mut(key) {
            *slot = Slot::default();
        }
    });
    drop_held(key);
}

fn take_popup_arm(key: &str, pop_gen: u64) -> bool {
    SLOTS.with(|slots| {
        let mut slots = slots.borrow_mut();
        let Some(slot) = slots.get_mut(key) else { return false };
        if slot.pop_gen != pop_gen || !slot.leaving || slot.armed { return false; }
        slot.armed = true;
        true
    })
}

pub fn popup_finish(key: &str, pop_gen: u64) -> bool {
    SLOTS.with(|slots| {
        let mut slots = slots.borrow_mut();
        let Some(slot) = slots.get_mut(key) else { return false };
        if slot.pop_gen != pop_gen || !slot.leaving { return false; }
        *slot = Slot { pop_gen: slot.pop_gen, ..Slot::default() };
        drop_held(key);
        true
    })
}

pub fn schedule_popup_finish<T: 'static>(key: &str, pop_gen: u64, cx: &mut Context<T>) {
    if !take_popup_arm(key, pop_gen) { return; }
    let key = key.to_owned();
    cx.spawn(async move |this, cx| {
        cx.background_executor().timer(Duration::from_millis(POPUP_MS)).await;
        let _ = this.update(cx, |_, cx| {
            if popup_finish(&key, pop_gen) { cx.notify(); }
        });
    }).detach();
}

pub fn schedule_popup_finish_for<T: 'static>(key: &str, pop_gen: u64, entity: Entity<T>, cx: &mut App) {
    if !take_popup_arm(key, pop_gen) { return; }
    let key = key.to_owned();
    let entity = entity.downgrade();
    cx.spawn(async move |cx| {
        cx.background_executor().timer(Duration::from_millis(POPUP_MS)).await;
        cx.update(|cx| {
            entity.update(cx, |_, cx| {
                if popup_finish(&key, pop_gen) { cx.notify(); }
            }).ok();
        });
    }).detach();
}

pub fn animate_popup<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    phase: PopupMotion,
    id: impl Into<ElementId>,
) -> AnyElement {
    let id = id.into();
    match phase {
        PopupMotion::Enter => popup_enter(el, id).into_any_element(),
        PopupMotion::Exit => popup_exit(el, id).into_any_element(),
    }
}

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
        Animation::new(Duration::from_millis(POPUP_MS)).with_easing(ease_out_expo()),
        |el, delta| el.opacity(delta).relative().top(px(-4.0 * (1.0 - delta))),
    )
}

/// Mirror of [`popup_enter`]: fade out and slide back toward the trigger.
pub fn popup_exit<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
) -> impl IntoElement {
    el.with_animation(
        id,
        Animation::new(Duration::from_millis(POPUP_MS)).with_easing(ease_out_expo()),
        |el, delta| el.opacity(1.0 - delta).relative().top(px(-4.0 * delta)),
    )
}

/// Fade a full-size layer in or out without moving it. Same duration as the popup.
pub fn layer_fade<E: IntoElement + Styled + AnimationExt + 'static>(
    el: E,
    id: impl Into<ElementId>,
    entering: bool,
) -> impl IntoElement {
    el.with_animation(
        id,
        Animation::new(Duration::from_millis(POPUP_MS)).with_easing(ease_out_expo()),
        move |el, delta| el.opacity(if entering { delta } else { 1.0 - delta }),
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

