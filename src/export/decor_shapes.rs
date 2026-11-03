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

pub(super) fn stroke_rect(
    image: &mut Pixmap,
    x: f32,
    y: f32,
    w: f32,
    h: f32,
    stroke_w: f32,
    color: Color,
) {
    fill_rect(image, x, y, w, stroke_w, color);
    fill_rect(image, x, y + h - stroke_w, w, stroke_w, color);
    fill_rect(image, x, y, stroke_w, h, color);
    fill_rect(image, x + w - stroke_w, y, stroke_w, h, color);
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

pub(super) fn stroke_circle(
    image: &mut Pixmap,
    cx: f32,
    cy: f32,
    r: f32,
    stroke_w: f32,
    color: Color,
) {
    if let Some(path) = PathBuilder::from_circle(cx * SCALE, cy * SCALE, r * SCALE) {
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

pub(super) fn paint_dashed_rect(
    image: &mut Pixmap,
    x: f32,
    y: f32,
    w: f32,
    h: f32,
    sw: f32,
    color: Color,
) {
    let mut t = 4.0;
    while t + 6.0 < w {
        fill_rect(image, x + t, y, 6.0, sw, color);
        fill_rect(image, x + t, y + h - sw, 6.0, sw, color);
        t += 11.0;
    }
    let mut t = 4.0;
    while t + 6.0 < h {
        fill_rect(image, x, y + t, sw, 6.0, color);
        fill_rect(image, x + w - sw, y + t, sw, 6.0, color);
        t += 11.0;
    }
}

pub(super) fn paint_diamond(image: &mut Pixmap, cx: f32, cy: f32, r: f32, color: Color) {
    let mut pb = PathBuilder::new();
    pb.move_to(cx * SCALE, (cy - r) * SCALE);
    pb.line_to((cx + r) * SCALE, cy * SCALE);
    pb.line_to(cx * SCALE, (cy + r) * SCALE);
    pb.line_to((cx - r) * SCALE, cy * SCALE);
    pb.close();
    if let Some(path) = pb.finish() {
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

pub(super) fn paint_triangle(image: &mut Pixmap, x: f32, y: f32, w: f32, h: f32, color: Color) {
    let mut pb = PathBuilder::new();
    pb.move_to(x * SCALE, y * SCALE);
    pb.line_to((x + w) * SCALE, (y + h * 0.5) * SCALE);
    pb.line_to(x * SCALE, (y + h) * SCALE);
    pb.close();
    if let Some(path) = pb.finish() {
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

pub(super) fn paint_check(image: &mut Pixmap, x: f32, cy: f32, color: Color) {
    fill_rect(image, x, cy - 0.5, 3.0, 2.0, color);
    fill_rect(image, x + 3.0, cy - 3.5, 2.0, 5.0, color);
}

pub(super) fn paint_multiply_rect(
    image: &mut Pixmap,
    x: f32,
    y: f32,
    w: f32,
    h: f32,
    color: Color,
) {
    let (dw, dh) = (image.width() as i32, image.height() as i32);
    let x0 = (x * SCALE).round().max(0.0) as i32;
    let y0 = (y * SCALE).round().max(0.0) as i32;
    let x1 = ((x + w) * SCALE).round().min(dw as f32) as i32;
    let y1 = ((y + h) * SCALE).round().min(dh as f32) as i32;
    let (mr, mg, mb) = (color.r() as u32, color.g() as u32, color.b() as u32);
    let data = image.data_mut();
    for py in y0..y1 {
        for px in x0..x1 {
            let idx = ((py * dw + px) * 4) as usize;
            data[idx] = ((data[idx] as u32 * mr + 127) / 255) as u8;
            data[idx + 1] = ((data[idx + 1] as u32 * mg + 127) / 255) as u8;
            data[idx + 2] = ((data[idx + 2] as u32 * mb + 127) / 255) as u8;
        }
    }
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
