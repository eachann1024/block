//! Geometry-only image placement. No window or pixels types, so a parent can
//! check zoom and pan numerically without a GPUI runtime.

/// Custom-zoom floor. An 8192px edge in a 1px stage is about 1e-4 after the 8% inset,
/// so this floor does not reject that fit. Fit itself is not raised to this value.
pub const MIN_SCALE: f32 = 1.0 / 65536.0;
pub const MAX_SCALE: f32 = 16.0;
const FIT_INSET: f32 = 32.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ZoomMode {
    Fit,
    Actual,
    Custom,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct ViewTransform {
    pub mode: ZoomMode,
    pub scale: f32,
    pub pan_x: f32,
    pub pan_y: f32,
}

impl ViewTransform {
    pub fn fit() -> Self {
        Self { mode: ZoomMode::Fit, scale: 1.0, pan_x: 0.0, pan_y: 0.0 }
    }

    pub fn actual() -> Self {
        Self { mode: ZoomMode::Actual, scale: 1.0, pan_x: 0.0, pan_y: 0.0 }
    }
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Placed {
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
    pub scale: f32,
}

pub fn clamp_scale(scale: f32) -> f32 {
    if !scale.is_finite() || scale <= 0.0 { return 1.0; }
    scale.clamp(MIN_SCALE, MAX_SCALE)
}

/// Keep a real positive scale. A fit scale below [`MIN_SCALE`] must not snap upward.
fn preserve_scale(scale: f32) -> f32 {
    if !scale.is_finite() || scale <= 0.0 { 1.0 } else { scale.min(MAX_SCALE) }
}

fn next_scale(current: f32, raw: f32) -> f32 {
    if !raw.is_finite() || raw <= 0.0 { return clamp_scale(current); }
    let floor = if current.is_finite() && current > 0.0 && current < MIN_SCALE { current } else { MIN_SCALE };
    raw.clamp(floor, MAX_SCALE)
}

fn finite(value: f32) -> f32 {
    if value.is_finite() { value } else { 0.0 }
}

/// Largest scale that fits inside the stage, never above 100%.
/// Inset is 32px, or 8% of that axis when smaller. Not raised to [`MIN_SCALE`].
pub fn fit_scale(nat_w: f32, nat_h: f32, stage_w: f32, stage_h: f32) -> f32 {
    if !(nat_w.is_finite() && nat_h.is_finite() && stage_w.is_finite() && stage_h.is_finite()) {
        return 1.0;
    }
    if !(nat_w > 0.0 && nat_h > 0.0 && stage_w > 0.0 && stage_h > 0.0) {
        return 1.0;
    }
    let inset_x = FIT_INSET.min(stage_w * 0.08);
    let inset_y = FIT_INSET.min(stage_h * 0.08);
    let avail_w = stage_w - inset_x * 2.0;
    let avail_h = stage_h - inset_y * 2.0;
    if !(avail_w > 0.0 && avail_h > 0.0) {
        return 1.0;
    }
    let scale = (avail_w / nat_w).min(avail_h / nat_h);
    if !scale.is_finite() || scale <= 0.0 { 1.0 } else { scale.min(1.0) }
}

pub fn effective_scale(view: ViewTransform, nat_w: f32, nat_h: f32, stage_w: f32, stage_h: f32) -> f32 {
    match view.mode {
        ZoomMode::Fit => fit_scale(nat_w, nat_h, stage_w, stage_h),
        ZoomMode::Actual => 1.0,
        ZoomMode::Custom => preserve_scale(view.scale),
    }
}

/// Pointer-anchored zoom. `ox` and `oy` are the pointer relative to the stage center.
/// `view.scale` is the scale on screen. It is not replaced by [`MIN_SCALE`] before the ratio.
pub fn zoom_at(view: ViewTransform, ox: f32, oy: f32, raw_scale: f32) -> ViewTransform {
    let current_ok = view.scale.is_finite() && view.scale > 0.0;
    let next = next_scale(if current_ok { view.scale } else { f32::NAN }, raw_scale);
    if !current_ok || !ox.is_finite() || !oy.is_finite() || !next.is_finite() {
        return ViewTransform { mode: ZoomMode::Custom, scale: if next.is_finite() { next } else { 1.0 }, pan_x: finite(view.pan_x), pan_y: finite(view.pan_y) };
    }
    let ratio = next / view.scale;
    if !ratio.is_finite() {
        return ViewTransform { mode: ZoomMode::Custom, scale: next, pan_x: finite(view.pan_x), pan_y: finite(view.pan_y) };
    }
    ViewTransform {
        mode: ZoomMode::Custom,
        scale: next,
        pan_x: finite(ox - (ox - finite(view.pan_x)) * ratio),
        pan_y: finite(oy - (oy - finite(view.pan_y)) * ratio),
    }
}

pub fn pan_by(mut view: ViewTransform, dx: f32, dy: f32) -> ViewTransform {
    if !dx.is_finite() || !dy.is_finite() { return view; }
    view.mode = ZoomMode::Custom;
    view.scale = preserve_scale(view.scale);
    view.pan_x = finite(view.pan_x + dx);
    view.pan_y = finite(view.pan_y + dy);
    view
}

pub fn place(view: ViewTransform, nat_w: f32, nat_h: f32, stage_w: f32, stage_h: f32) -> Placed {
    let scale = effective_scale(view, nat_w, nat_h, stage_w, stage_h);
    let width = if nat_w.is_finite() && (nat_w * scale).is_finite() { (nat_w * scale).max(0.0) } else { 0.0 };
    let height = if nat_h.is_finite() && (nat_h * scale).is_finite() { (nat_h * scale).max(0.0) } else { 0.0 };
    let pan_x = if view.mode == ZoomMode::Fit { 0.0 } else { finite(view.pan_x) };
    let pan_y = if view.mode == ZoomMode::Fit { 0.0 } else { finite(view.pan_y) };
    Placed {
        x: (stage_w - width) * 0.5 + pan_x,
        y: (stage_h - height) * 0.5 + pan_y,
        width,
        height,
        scale,
    }
}

/// Positive `delta_y` zooms in. Precise (pixel) deltas use a smaller gain than wheel notches.
pub fn wheel_factor(delta_y: f32, precise: bool) -> f32 {
    if !delta_y.is_finite() || delta_y == 0.0 { return 1.0; }
    (delta_y * if precise { 0.008 } else { 0.12 }).exp().clamp(0.5, 2.0)
}

pub fn percent_label(scale: f32) -> String {
    if !scale.is_finite() || scale < 0.0 { return "—".into(); }
    let percent = scale * 100.0;
    if percent >= 1.0 || percent == 0.0 {
        format!("{}%", percent.round() as i32)
    } else if percent >= 0.01 {
        format!("{percent:.2}%")
    } else {
        format!("{percent:.4}%")
    }
}
