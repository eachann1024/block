//! Low-level geometric primitives for tiny-skia export rendering.
use cosmic_text::Color;
use tiny_skia::{FillRule, Paint, Path, PathBuilder, Pixmap, Rect, Stroke, Transform};

use super::layout::SCALE;

pub(super) fn fill_rect(image: &mut Pixmap, x: f32, y: f32, w: f32, h: f32, color: Color) {
    let mut paint = Paint::default();
    paint.set_color_rgba8(color.r(), color.g(), color.b(), color.a());
    if let Some(rect) = Rect::from_xywh(x * SCALE, y * SCALE, w * SCALE, h * SCALE) {
        image.fill_rect(rect, &paint, Transform::identity(), None);
    }
}

pub(super) fn fill_rounded_rect(
    image: &mut Pixmap,
    x: f32,
    y: f32,
    w: f32,
    h: f32,
    r: f32,
    color: Color,
) {
    if let Some(path) = rounded_path(x * SCALE, y * SCALE, w * SCALE, h * SCALE, r * SCALE) {
        let mut paint = Paint::default();
        paint.anti_alias = true;
        paint.set_color_rgba8(color.r(), color.g(), color.b(), color.a());
        image.fill_path(
            &path,
            &paint,
            FillRule::Winding,
            Transform::identity(),
            None,
        );
    }
}

pub(super) fn stroke_rounded_rect(
    image: &mut Pixmap,
    x: f32,
    y: f32,
    w: f32,
    h: f32,
    r: f32,
    stroke_w: f32,
    color: Color,
) {
    if let Some(path) = rounded_path(x * SCALE, y * SCALE, w * SCALE, h * SCALE, r * SCALE) {
        let mut paint = Paint::default();
        paint.anti_alias = true;
        paint.set_color_rgba8(color.r(), color.g(), color.b(), color.a());
        let stroke = Stroke {
            width: stroke_w * SCALE,
            ..Stroke::default()
        };
        image.stroke_path(&path, &paint, &stroke, Transform::identity(), None);
    }
}

pub(super) fn fill_circle(image: &mut Pixmap, cx: f32, cy: f32, r: f32, color: Color) {
    if let Some(path) = PathBuilder::from_circle(cx * SCALE, cy * SCALE, r * SCALE) {
        let mut paint = Paint::default();
        paint.anti_alias = true;
        paint.set_color_rgba8(color.r(), color.g(), color.b(), color.a());
        image.fill_path(
            &path,
            &paint,
            FillRule::Winding,
            Transform::identity(),
            None,
        );
    }
}

/// A stroked check mark centered on `(cx, cy)`, sized for a 15px box.
pub(super) fn paint_check(image: &mut Pixmap, cx: f32, cy: f32, color: Color) {
    let mut pb = PathBuilder::new();
    pb.move_to((cx - 4.0) * SCALE, (cy + 0.2) * SCALE);
    pb.line_to((cx - 1.2) * SCALE, (cy + 3.0) * SCALE);
    pb.line_to((cx + 4.2) * SCALE, (cy - 3.2) * SCALE);
    let Some(path) = pb.finish() else { return };
    let mut paint = Paint::default();
    paint.anti_alias = true;
    paint.set_color_rgba8(color.r(), color.g(), color.b(), color.a());
    let stroke = Stroke {
        width: 2.0 * SCALE,
        line_cap: tiny_skia::LineCap::Round,
        line_join: tiny_skia::LineJoin::Round,
        ..Stroke::default()
    };
    image.stroke_path(&path, &paint, &stroke, Transform::identity(), None);
}

fn rounded_path(x: f32, y: f32, w: f32, h: f32, r: f32) -> Option<Path> {
    let r = r.min(w * 0.5).min(h * 0.5);
    const K: f32 = 0.552_284_75;
    let (right, bottom, k) = (x + w, y + h, r * K);
    let mut p = PathBuilder::new();
    p.move_to(x + r, y);
    p.line_to(right - r, y);
    p.cubic_to(right - r + k, y, right, y + r - k, right, y + r);
    p.line_to(right, bottom - r);
    p.cubic_to(
        right,
        bottom - r + k,
        right - r + k,
        bottom,
        right - r,
        bottom,
    );
    p.line_to(x + r, bottom);
    p.cubic_to(x + r - k, bottom, x, bottom - r + k, x, bottom - r);
    p.line_to(x, y + r);
    p.cubic_to(x, y + r - k, x + r - k, y, x + r, y);
    p.close();
    p.finish()
}
