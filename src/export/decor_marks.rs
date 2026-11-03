//! Washi tapes and rotated circular date stamps for Journal and Kraft styles.
use cosmic_text::{Buffer, Color, FontSystem, SwashCache};
use tiny_skia::Pixmap;

use super::decor_shapes::stroke_circle;
use super::layout::{Layout, SCALE};
use super::paint::text;
use super::palette::{ExportTheme, Palette};
use super::{Cancellation, ExportError};

pub(super) fn paint_top_tapes(image: &mut Pixmap, layout: &Layout) {
    let w = layout.width as f32 / SCALE;
    match layout.options.theme {
        ExportTheme::Journal => {
            paint_tape(
                image,
                -18.0,
                26.0,
                190.0,
                36.0,
                -8.0,
                true,
                9.0,
                Color::rgba(226, 132, 104, 158),
                Color::rgba(244, 196, 170, 158),
            );
            paint_tape(
                image,
                w - 148.0,
                22.0,
                170.0,
                34.0,
                9.0,
                false,
                6.0,
                Color::rgba(116, 160, 150, 148),
                Color::rgba(186, 214, 204, 148),
            );
        }
        ExportTheme::Kraft => {
            paint_tape(
                image,
                120.0,
                16.0,
                170.0,
                34.0,
                -6.0,
                true,
                8.0,
                Color::rgba(232, 178, 64, 198),
                Color::rgba(196, 140, 40, 198),
            );
            paint_tape(
                image,
                w - 140.0,
                26.0,
                160.0,
                32.0,
                10.0,
                false,
                5.0,
                Color::rgba(92, 166, 150, 184),
                Color::rgba(64, 124, 112, 184),
            );
        }
        _ => {}
    }
}

pub(super) fn paint_stamp(
    image: &mut Pixmap,
    cx: f32,
    cy: f32,
    color: Color,
    lines: &[Buffer; 3],
    pal: &Palette,
    fonts: &mut FontSystem,
    cache: &mut SwashCache,
    cancel: &Cancellation,
) -> Result<(), ExportError> {
    let side = (96.0 * SCALE) as u32;
    let Some(mut scratch) = Pixmap::new(side, side) else {
        return Ok(());
    };
    let mid = 48.0;
    stroke_circle(&mut scratch, mid, mid, 41.0, 2.0, color);
    stroke_circle(&mut scratch, mid, mid, 34.0, 1.0, color);
    let ys = [23.0, 36.0, 58.0];
    for (buf, y) in lines.iter().zip(ys) {
        let w = buf.layout_runs().map(|r| r.line_w).fold(0.0, f32::max);
        text(
            &mut scratch,
            buf,
            mid - w * 0.5,
            y,
            (pal, color),
            fonts,
            cache,
            cancel,
        )?;
    }
    blit_rotated(
        image,
        &scratch,
        cx * SCALE,
        cy * SCALE,
        -12.0_f32.to_radians(),
    );
    Ok(())
}

fn blit_rotated(dst: &mut Pixmap, src: &Pixmap, cx: f32, cy: f32, angle: f32) {
    let (dw, dh) = (dst.width() as i32, dst.height() as i32);
    let (sw, sh) = (src.width() as i32, src.height() as i32);
    let (half_w, half_h) = (sw as f32 * 0.5, sh as f32 * 0.5);
    let (cos_a, sin_a) = (angle.cos(), angle.sin());
    let r = (half_w.hypot(half_h)).ceil() as i32;
    let dst_data = dst.data_mut();
    let src_data = src.data();
    for dy in -r..=r {
        for dx in -r..=r {
            let (px, py) = (cx as i32 + dx, cy as i32 + dy);
            if px < 0 || py < 0 || px >= dw || py >= dh {
                continue;
            }
            let sx = (dx as f32 * cos_a + dy as f32 * sin_a + half_w).round() as i32;
            let sy = (-dx as f32 * sin_a + dy as f32 * cos_a + half_h).round() as i32;
            if sx < 0 || sy < 0 || sx >= sw || sy >= sh {
                continue;
            }
            let s_idx = ((sy * sw + sx) * 4) as usize;
            let sa = src_data[s_idx + 3] as u32;
            if sa == 0 {
                continue;
            }
            let d_idx = ((py * dw + px) * 4) as usize;
            for ch in 0..3 {
                let s_val = src_data[s_idx + ch] as u32;
                let d_val = dst_data[d_idx + ch] as u32;
                dst_data[d_idx + ch] = ((s_val * sa + d_val * (255 - sa) + 127) / 255) as u8;
            }
        }
    }
}

#[allow(clippy::too_many_arguments)]
pub(super) fn paint_tape(
    image: &mut Pixmap,
    x: f32,
    y: f32,
    w: f32,
    h: f32,
    angle_deg: f32,
    diagonal: bool,
    stripe_w: f32,
    c1: Color,
    c2: Color,
) {
    let (dw, dh) = (image.width() as i32, image.height() as i32);
    let (cx, cy) = ((x + w * 0.5) * SCALE, (y + h * 0.5) * SCALE);
    let (hw, hh) = (w * 0.5 * SCALE, h * 0.5 * SCALE);
    let rad = angle_deg.to_radians();
    let (cos_a, sin_a) = (rad.cos(), rad.sin());
    let bound_x = (hw * cos_a.abs() + hh * sin_a.abs()).ceil() as i32 + 2;
    let bound_y = (hw * sin_a.abs() + hh * cos_a.abs()).ceil() as i32 + 2;
    let period = (stripe_w * SCALE).max(2.0);
    let data = image.data_mut();
    for dy in -bound_y..=bound_y {
        let py = cy as i32 + dy;
        if py < 0 || py >= dh {
            continue;
        }
        for dx in -bound_x..=bound_x {
            let px = cx as i32 + dx;
            if px < 0 || px >= dw {
                continue;
            }
            let lx = dx as f32 * cos_a + dy as f32 * sin_a;
            let ly = -dx as f32 * sin_a + dy as f32 * cos_a;
            if lx.abs() <= hw && ly.abs() <= hh {
                let coord = if diagonal {
                    (lx + hw + ly + hh) * 0.7071
                } else {
                    lx + hw
                };
                let c = if ((coord / period).floor() as i32).rem_euclid(2) == 0 {
                    c1
                } else {
                    c2
                };
                let a = c.a() as u32;
                let idx = ((py * dw + px) * 4) as usize;
                data[idx] = ((c.r() as u32 * a + data[idx] as u32 * (255 - a)) / 255) as u8;
                data[idx + 1] = ((c.g() as u32 * a + data[idx + 1] as u32 * (255 - a)) / 255) as u8;
                data[idx + 2] = ((c.b() as u32 * a + data[idx + 2] as u32 * (255 - a)) / 255) as u8;
            }
        }
    }
}
