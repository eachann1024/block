//! Visible smoke-test chrome: element bounds probe, click ripple and step HUD.
//! Everything is inert unless `--smoke` enabled it, so normal runs pay nothing.
use std::{cell::RefCell, collections::HashMap, sync::atomic::{AtomicBool, Ordering}, time::Duration};
use gpui::{
    Animation, AnimationExt, AnyElement, Bounds, FontWeight, Pixels, Point, SharedString, Styled,
    div, prelude::*, px,
};
use crate::theme::Theme;

static SMOKE_ACTIVE: AtomicBool = AtomicBool::new(false);

pub fn enable_smoke_mode() { SMOKE_ACTIVE.store(true, Ordering::Relaxed); }
pub fn is_smoke_active() -> bool { SMOKE_ACTIVE.load(Ordering::Relaxed) }

#[derive(Default)]
struct PulseState {
    seq: u64,
    point: Option<Point<Pixels>>,
    step_text: SharedString,
    passed: Option<bool>,
}

thread_local! {
    static TARGET_BOUNDS: RefCell<HashMap<SharedString, Bounds<Pixels>>> = RefCell::new(HashMap::new());
    static PULSE: RefCell<PulseState> = RefCell::new(PulseState::default());
}

pub fn record_bounds(id: impl Into<SharedString>, bounds: Bounds<Pixels>) {
    TARGET_BOUNDS.with(|map| { map.borrow_mut().insert(id.into(), bounds); });
}

pub fn target_bounds(id: &str) -> Option<Bounds<Pixels>> {
    TARGET_BOUNDS.with(|map| map.borrow().get(id).copied())
}

pub fn target_center(id: &str) -> Option<Point<Pixels>> {
    target_bounds(id).map(|bounds| bounds.center())
}

/// Invisible child that records its parent's window bounds under `id` while smoke mode is on.
pub fn probe(id: impl Into<SharedString>) -> Option<gpui::Canvas<()>> {
    if !is_smoke_active() { return None; }
    let id: SharedString = id.into();
    Some(gpui::canvas(move |bounds, _, _| record_bounds(id.clone(), bounds), |_, _, _, _| {})
        .absolute().top_0().left_0().size_full())
}

/// Probe keyed by an element id; skips the id formatting entirely outside smoke runs.
pub fn probe_for(id: &gpui::ElementId) -> Option<gpui::Canvas<()>> {
    if !is_smoke_active() { return None; }
    probe(id.to_string())
}

pub fn reset() {
    TARGET_BOUNDS.with(|map| map.borrow_mut().clear());
    PULSE.with(|state| *state.borrow_mut() = PulseState::default());
}

pub fn set_step(point: Option<Point<Pixels>>, step_text: impl Into<SharedString>) {
    PULSE.with(|state| {
        let mut state = state.borrow_mut();
        state.seq += 1;
        state.point = point;
        state.step_text = step_text.into();
    });
}

pub fn set_finished(passed: bool, summary: impl Into<SharedString>) {
    PULSE.with(|state| {
        let mut state = state.borrow_mut();
        state.passed = Some(passed);
        state.point = None;
        state.step_text = summary.into();
    });
}

/// Full-window, non-interactive layer: ripple at the last click plus a bottom HUD pill.
pub fn render() -> Option<AnyElement> {
    if !is_smoke_active() { return None; }
    let theme = Theme::ocean();
    let (seq, point, text) = PULSE.with(|state| {
        let state = state.borrow();
        (state.seq, state.point, state.step_text.clone())
    });
    let ripple = point.map(|pt| {
        let ring = div().absolute().border_2().rounded(px(999.))
            .with_animation(
                ("smoke-pulse", seq as usize),
                Animation::new(Duration::from_millis(320)).with_easing(crate::ui::motion::ease_out_expo()),
                move |el, delta| {
                    let radius = px(14. + 34. * delta);
                    el.left(pt.x - radius).top(pt.y - radius).size(radius * 2.)
                        .border_color(theme.accent.opacity(1.0 - delta * 0.85))
                        .bg(theme.accent.opacity(0.18 * (1.0 - delta)))
                },
            );
        let dot = div().absolute().left(pt.x - px(4.)).top(pt.y - px(4.)).size(px(8.))
            .rounded(px(999.)).bg(theme.accent);
        div().absolute().top_0().left_0().size_full().child(ring).child(dot)
    });
    let hud = (!text.is_empty()).then(|| {
        div().absolute().bottom(px(20.)).left_0().w_full().flex().justify_center().child(
            div().h(px(36.)).px(px(14.)).rounded(px(8.)).bg(theme.paper).border_1().border_color(theme.accent)
                .flex().items_center().gap(px(8.)).text_size(px(14.)).font_weight(FontWeight(500.))
                .text_color(theme.text)
                .child(div().size(px(8.)).flex_none().rounded(px(999.)).bg(theme.accent))
                .child(text),
        )
    });
    Some(div().absolute().top_0().left_0().size_full().children(ripple).children(hud).into_any_element())
}
