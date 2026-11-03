//! Deterministic paper textures, frames, halftones, and background geometry per style.
use cosmic_text::Color;
use tiny_skia::{FillRule, Paint, PathBuilder, Pixmap, Transform};

use super::decor_shapes::{fill_circle, fill_rect, fill_rounded_rect, stroke_circle, stroke_rect};
use super::layout::{Layout, SCALE};
use super::palette::{ExportTheme, Palette};
use super::{Cancellation, ExportError};

pub(super) fn paint_paper(
    image: &mut Pixmap,
    layout: &Layout,
    pal: &Palette,
    cancel: &Cancellation,
) -> Result<(), ExportError> {
    cancel.check()?;
    let c = pal.canvas;
    image.fill(tiny_skia::Color::from_rgba8(c.r(), c.g(), c.b(), 255));
    let w = layout.width as f32 / SCALE;
    let h = layout.height as f32 / SCALE;

    match layout.options.theme {
        ExportTheme::Mono | ExportTheme::Ink => {
            stroke_rect(image, 24.0, 24.0, w - 48.0, h - 48.0, 1.0, pal.divider);
            fill_rect(image, 24.0, 24.0, 10.0, 10.0, pal.accent);
            fill_rect(image, w - 34.0, h - 34.0, 10.0, 10.0, pal.accent);
        }
        ExportTheme::Journal => {
            paint_dots(image, w, h, 24.0, 12.0, 1.2, Color::rgb(216, 204, 176));
            fill_rect(image, 64.0, 0.0, 2.0, h, pal.divider);
        }
        ExportTheme::Kraft => {
            paint_grid(image, w, h, 24.0, Color::rgba(240, 228, 200, 18));
            fill_rect(image, 64.0, 0.0, 2.0, h, pal.divider);
            let holes = ((h - 80.0) / 170.0).round().max(3.0) as usize;
            let step = (h - 80.0) / holes.max(1) as f32;
            for i in 0..=holes {
                let cy = 40.0 + i as f32 * step;
                fill_circle(image, 35.0, cy + 0.8, 7.0, Color::rgba(255, 255, 255, 22));
                fill_circle(image, 35.0, cy, 7.0, Color::rgb(11, 10, 9));
            }
        }
        ExportTheme::Riso => {
            paint_grain(image, Color::rgba(40, 30, 20, 20), 0x517c_c1b7);
            paint_halftone_circle(
                image,
                w - 90.0,
                90.0,
                150.0,
                12.0,
                3.2,
                Color::rgba(255, 90, 168, 225),
            );
            let by = (h - 280.0).max(160.0);
            paint_halftone_rect(
                image,
                -20.0,
                by,
                160.0,
                160.0,
                10.0,
                2.2,
                Color::rgba(47, 95, 208, 140),
            );
        }
        ExportTheme::Press => {
            paint_grain(image, Color::rgba(60, 45, 25, 13), 0x9e37_79b9);
            stroke_rect(image, 14.0, 14.0, w - 28.0, h - 28.0, 1.0, pal.divider);
            stroke_rect(image, 19.0, 19.0, w - 38.0, h - 38.0, 1.0, pal.divider);
            stroke_rect(image, 22.0, 22.0, w - 44.0, h - 44.0, 1.0, pal.divider);
        }
        ExportTheme::Neon => {
            paint_grain(image, Color::rgba(255, 255, 255, 15), 0x85eb_ca6b);
            let by = (h - 280.0).max(160.0);
            paint_halftone_rect(
                image,
                -20.0,
                by,
                160.0,
                160.0,
                10.0,
                2.2,
                Color::rgba(63, 224, 208, 115),
            );
            stroke_circle(image, w - 94.0, 123.0, 56.0, 3.0, pal.accent2);
            stroke_circle(image, w - 100.0, 118.0, 56.0, 3.0, pal.accent);
            fill_circle(image, w - 100.0, 118.0, 34.0, pal.accent2);
            paint_diagonal_band(image, 0.0, 20.0, w, pal.accent2);
            paint_diagonal_band(image, h - 14.0, 14.0, w, Color::rgba(255, 106, 61, 180));
        }
        ExportTheme::Film => {
            let hole = Color::rgba(232, 224, 208, 42);
            let mut x = 12.0;
            while x + 14.0 <= w - 8.0 {
                fill_rounded_rect(image, x, 10.0, 14.0, 18.0, 3.0, hole);
                fill_rounded_rect(image, x, h - 28.0, 14.0, 18.0, 3.0, hole);
                x += 30.0;
            }
            fill_rounded_rect(
                image,
                20.0,
                56.0,
                w - 40.0,
                h - 112.0,
                14.0,
                pal.inner_canvas,
            );
            paint_film_corners(image, 36.0, 72.0, w - 36.0, h - 72.0, 18.0, pal.accent);
        }
    }
    Ok(())
}

fn paint_dots(image: &mut Pixmap, w: f32, h: f32, step: f32, offset: f32, r: f32, color: Color) {
    let mut y = offset;
    while y < h {
        let mut x = offset;
        while x < w {
            fill_circle(image, x, y, r, color);
            x += step;
        }
        y += step;
    }
}

fn paint_grid(image: &mut Pixmap, w: f32, h: f32, step: f32, color: Color) {
    let mut x = step;
    while x < w {
        fill_rect(image, x, 0.0, 1.0, h, color);
        x += step;
    }
    let mut y = step;
    while y < h {
        fill_rect(image, 0.0, y, w, 1.0, color);
        y += step;
    }
}

fn paint_grain(image: &mut Pixmap, color: Color, seed: u32) {
    let width = image.width();
    let height = image.height();
    let data = image.data_mut();
    let a = color.a() as u32;
    let (sr, sg, sb) = (color.r() as u32, color.g() as u32, color.b() as u32);
    for py in (0..height).step_by(3) {
        for px in (0..width).step_by(3) {
            let mut h =
                (px.wrapping_mul(374_761_393)).wrapping_add(py.wrapping_mul(668_265_263)) ^ seed;
            h = (h ^ (h >> 13)).wrapping_mul(1_274_126_177);
            if (h >> 24) < 52 {
                let idx = ((py * width + px) * 4) as usize;
                data[idx] = ((sr * a + data[idx] as u32 * (255 - a)) / 255) as u8;
                data[idx + 1] = ((sg * a + data[idx + 1] as u32 * (255 - a)) / 255) as u8;
                data[idx + 2] = ((sb * a + data[idx + 2] as u32 * (255 - a)) / 255) as u8;
            }
        }
    }
}

fn paint_halftone_circle(
    image: &mut Pixmap,
    cx: f32,
    cy: f32,
    radius: f32,
    step: f32,
    dot_r: f32,
    color: Color,
) {
    let r2 = radius * radius;
    let mut y = (cy - radius).max(0.0);
    while y <= cy + radius {
        let mut x = (cx - radius).max(0.0);
        while x <= cx + radius {
            let (dx, dy) = (x - cx, y - cy);
            if dx * dx + dy * dy <= r2 {
                fill_circle(image, x, y, dot_r, color);
            }
            x += step;
        }
        y += step;
    }
}

#[allow(clippy::too_many_arguments)]
fn paint_halftone_rect(
    image: &mut Pixmap,
    x0: f32,
    y0: f32,
    w: f32,
    h: f32,
    step: f32,
    dot_r: f32,
    color: Color,
) {
    let mut y = y0.max(0.0);
    while y <= y0 + h {
        let mut x = x0.max(0.0);
        while x <= x0 + w {
            fill_circle(image, x, y, dot_r, color);
            x += step;
        }
        y += step;
    }
}

fn paint_diagonal_band(image: &mut Pixmap, y0: f32, h: f32, w: f32, color: Color) {
    let mut paint = Paint::default();
    paint.anti_alias = true;
    paint.set_color_rgba8(color.r(), color.g(), color.b(), color.a());
    let mut x = -h;
    while x < w + h {
        let mut pb = PathBuilder::new();
        pb.move_to(x * SCALE, y0 * SCALE);
        pb.line_to((x + 11.0) * SCALE, y0 * SCALE);
        pb.line_to((x + 11.0 + h) * SCALE, (y0 + h) * SCALE);
        pb.line_to((x + h) * SCALE, (y0 + h) * SCALE);
        pb.close();
        if let Some(path) = pb.finish() {
            image.fill_path(
                &path,
                &paint,
                FillRule::Winding,
                Transform::identity(),
                None,
            );
        }
        x += 24.0;
    }
}

fn paint_film_corners(
    image: &mut Pixmap,
    left: f32,
    top: f32,
    right: f32,
    bottom: f32,
    arm: f32,
    color: Color,
) {
    for (x, y, dx, dy) in [
        (left, top, arm, arm),
        (right, top, -arm, arm),
        (left, bottom, arm, -arm),
        (right, bottom, -arm, -arm),
    ] {
        let rx = if dx > 0.0 { x } else { x + dx };
        let ry = if dy > 0.0 { y } else { y + dy };
        fill_rect(image, rx, y - 1.0, dx.abs(), 2.0, color);
        fill_rect(image, x - 1.0, ry, 2.0, dy.abs(), color);
    }
}
