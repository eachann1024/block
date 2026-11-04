//! Time-based value fade for painted (non-element) visuals, e.g. a highlight that eases between
//! two colors when its state flips. Matches CSS `transition: 200ms cubic-bezier(.165,.84,.44,1)`.
use std::{cell::RefCell, collections::HashMap, time::Duration};
use gpui::{Hsla, Window, rgb};
use gpui_component::animation::cubic_bezier;

const DURATION: Duration = Duration::from_millis(200);

struct Fade { from: f32, to: f32, start: f64 }

thread_local! { static FADES: RefCell<HashMap<u64, Fade>> = Default::default(); }

/// Seconds on a monotonic clock. `std::time::Instant` panics on wasm32, so the browser's
/// `performance.now()` stands in there.
#[cfg(not(target_family = "wasm"))]
fn now() -> f64 {
    thread_local! { static EPOCH: std::time::Instant = std::time::Instant::now(); }
    EPOCH.with(|epoch| epoch.elapsed().as_secs_f64())
}
#[cfg(target_family = "wasm")]
fn now() -> f64 {
    web_sys::window().and_then(|w| w.performance()).map_or(0., |p| p.now() / 1000.)
}

fn ease(t: f32) -> f32 {
    cubic_bezier(0.165, 0.84, 0.44, 1.0)(t)
}

/// Current level in `0.0..=1.0` for `key`, easing toward `target` (`0.0` or `1.0`).
/// A first sighting starts settled at `target`; requests another frame while still moving.
pub fn level(key: u64, target: f32, window: &mut Window) -> f32 {
    FADES.with(|map| {
        let mut map = map.borrow_mut();
        let now = now();
        let fade = map.entry(key).or_insert(Fade { from: target, to: target, start: now - DURATION.as_secs_f64() });
        let t = (((now - fade.start) as f32) / DURATION.as_secs_f32()).clamp(0., 1.);
        let current = fade.from + (fade.to - fade.from) * ease(t);
        if fade.to != target {
            *fade = Fade { from: current, to: target, start: now };
            window.request_animation_frame();
            return current;
        }
        if t < 1. { window.request_animation_frame(); }
        current
    })
}

/// Linear blend of two `0xRRGGBB` colors.
pub fn mix(a: u32, b: u32, t: f32) -> Hsla {
    let ch = |shift: u32| {
        let (x, y) = (((a >> shift) & 0xff) as f32, ((b >> shift) & 0xff) as f32);
        ((x + (y - x) * t).round() as u32) << shift
    };
    rgb(ch(16) | ch(8) | ch(0)).into()
}
