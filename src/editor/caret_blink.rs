//! Caret blink: solid while the caret moves, then a soft fade loop.
//! Each editor keeps one pending wake-up; repaints only happen at phase edges and during fades.
use std::{cell::RefCell, collections::HashMap, time::{Duration, Instant}};
use gpui::{App, Entity, EntityId};
use super::view::Editor;

const HOLD_MS: f32 = 480.;
const FADE_MS: f32 = 120.;
const PERIOD_MS: f32 = 2. * (HOLD_MS + FADE_MS);
const FRAME_MS: f32 = 33.;

struct Blink { key: (u64, usize), epoch: Instant, wake: Option<Instant> }
thread_local! { static BLINKS: RefCell<HashMap<EntityId, Blink>> = RefCell::new(HashMap::new()); }

/// Caret opacity for this paint; restarts the cycle whenever `key` (caret position) changes.
pub(super) fn alpha(editor: &Entity<Editor>, key: (u64, usize), cx: &mut App) -> f32 {
    let now = Instant::now();
    let (alpha, wait) = BLINKS.with(|blinks| {
        let mut blinks = blinks.borrow_mut();
        let blink = blinks.entry(editor.entity_id()).or_insert(Blink { key, epoch: now, wake: None });
        // A wake-up long past means the caret was hidden (blur, inactive window): start solid again.
        let stale = blink.wake.is_some_and(|wake| now.saturating_duration_since(wake) > Duration::from_millis(100));
        if blink.key != key || stale { blink.key = key; blink.epoch = now; }
        let (alpha, wait_ms) = phase(now.duration_since(blink.epoch).as_secs_f32() * 1000. % PERIOD_MS);
        let due = now + Duration::from_millis(wait_ms);
        let schedule = blink.wake.is_none_or(|wake| wake <= now || wake > due);
        if schedule { blink.wake = Some(due); }
        (alpha, schedule.then_some(wait_ms))
    });
    if let Some(ms) = wait {
        let editor = editor.downgrade();
        cx.spawn(async move |cx| {
            cx.background_executor().timer(Duration::from_millis(ms)).await;
            let gone = editor.update(cx, |_, cx| cx.notify()).is_err();
            if gone { BLINKS.with(|b| b.borrow_mut().remove(&editor.entity_id())); }
        }).detach();
    }
    alpha
}

/// (opacity, milliseconds until the next visible change) at time `t` within one period.
fn phase(t: f32) -> (f32, u64) {
    let ease = |x: f32| x * x * (3. - 2. * x);
    let (alpha, wait) = if t < HOLD_MS { (1., HOLD_MS - t) }
        else if t < HOLD_MS + FADE_MS { (1. - ease((t - HOLD_MS) / FADE_MS), FRAME_MS) }
        else if t < 2. * HOLD_MS + FADE_MS { (0., 2. * HOLD_MS + FADE_MS - t) }
        else { (ease((t - 2. * HOLD_MS - FADE_MS) / FADE_MS), FRAME_MS) };
    (alpha, wait.ceil().max(1.) as u64)
}
