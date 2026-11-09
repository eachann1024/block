//! Paper, card frame, and quiet background textures per style. Nothing here sits under body text
//! except faint textures, so every style keeps full text contrast.
use cosmic_text::Color;
use tiny_skia::Pixmap;

use super::decor_marks::paint_washi;
use super::decor_shapes::{fill_rounded_rect, stroke_rounded_rect};
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
    let theme = layout.options.theme;

    match theme {
        ExportTheme::Riso => paint_grain(image, Color::rgba(40, 30, 20, 14), 0x517c_c1b7),
        ExportTheme::Press => paint_grain(image, Color::rgba(60, 45, 25, 11), 0x9e37_79b9),
        _ => {}
    }

    let Some(card) = theme.spec().card else { return Ok(()) };
    let (cx, cy, cw, ch) = (card.inset, card.inset, w - card.inset * 2.0, h - card.inset * 2.0);
    if card.shadow.a() > 0 {
        let s = card.shadow;
        for (dy, grow, alpha) in [(2.0, 0.0, 1.0), (8.0, 4.0, 0.6), (18.0, 10.0, 0.3)] {
            let a = (s.a() as f32 * alpha) as u8;
            fill_rounded_rect(
                image,
                cx - grow,
                cy + dy - grow * 0.5,
                cw + grow * 2.0,
                ch + grow,
                card.radius + grow,
                Color::rgba(s.r(), s.g(), s.b(), a),
            );
        }
    }
    fill_rounded_rect(image, cx, cy, cw, ch, card.radius, pal.inner_canvas);
    if let Some(border) = card.border {
        stroke_rounded_rect(image, cx + 0.5, cy + 0.5, cw - 1.0, ch - 1.0, card.radius, 1.0, border);
    }

    match theme {
        ExportTheme::Journal => {
            paint_washi(image, w * 0.5, cy, 142.0, 34.0, -2.0, pal.accent2);
        }
        ExportTheme::Kraft => {
            let dot = Color::rgba(240, 228, 200, 22);
            let gap = 22.0;
            let inner = card.radius;
            let mut y = cy + gap * 0.5;
            while y < cy + ch - inner * 0.5 {
                let mut x = cx + gap * 0.5;
                while x < cx + cw - inner * 0.5 {
                    fill_dot(image, x, y, 1.3, dot);
                    x += gap;
                }
                y += gap;
            }
            paint_washi(image, w * 0.5, cy, 142.0, 34.0, -2.0, pal.accent2);
        }
        ExportTheme::Film => {
            let hole = Color::rgba(232, 224, 208, 40);
            let mut x = 14.0;
            while x + 14.0 <= w - 10.0 {
                fill_rounded_rect(image, x, 11.0, 14.0, 18.0, 3.0, hole);
                fill_rounded_rect(image, x, h - 29.0, 14.0, 18.0, 3.0, hole);
                x += 30.0;
            }
        }
        _ => {}
    }
    Ok(())
}

/// Direct antialiased dot stamp to avoid per-dot path tessellation across tall canvases.
fn fill_dot(image: &mut Pixmap, cx: f32, cy: f32, r: f32, color: Color) {
    let (w, h) = (image.width() as i32, image.height() as i32);
    let (cx, cy, r) = (cx * SCALE, cy * SCALE, r * SCALE);
    let x0 = (cx - r - 1.0).floor().max(0.0) as i32;
    let x1 = (cx + r + 1.0).ceil().min((w - 1) as f32) as i32;
    let y0 = (cy - r - 1.0).floor().max(0.0) as i32;
    let y1 = (cy + r + 1.0).ceil().min((h - 1) as f32) as i32;
    let data = image.data_mut();
    let (sr, sg, sb, sa) = (color.r() as u32, color.g() as u32, color.b() as u32, color.a() as f32);
    for py in y0..=y1 {
        let dy = py as f32 + 0.5 - cy;
        for px in x0..=x1 {
            let dx = px as f32 + 0.5 - cx;
            let cov = (r + 0.5 - (dx * dx + dy * dy).sqrt()).clamp(0.0, 1.0);
            let a = (sa * cov).round() as u32;
            if a == 0 {
                continue;
            }
            let idx = ((py * w + px) * 4) as usize;
            data[idx] = ((sr * a + data[idx] as u32 * (255 - a) + 127) / 255) as u8;
            data[idx + 1] = ((sg * a + data[idx + 1] as u32 * (255 - a) + 127) / 255) as u8;
            data[idx + 2] = ((sb * a + data[idx + 2] as u32 * (255 - a) + 127) / 255) as u8;
        }
    }
}

/// Sparse deterministic speckle, sampled every 3 device pixels.
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
