//! Hand-made marks: Thinkly's washi tape and the wavy pen underline.
use cosmic_text::Color;
use tiny_skia::{Paint, PathBuilder, Pixmap, Stroke, Transform};

use super::layout::SCALE;

/// Washi tape centered on `(cx, cy)`: translucent color, faint white stripes, zigzag torn ends.
pub(super) fn paint_washi(
    image: &mut Pixmap,
    cx: f32,
    cy: f32,
    w: f32,
    h: f32,
    angle_deg: f32,
    color: Color,
) {
    let (dw, dh) = (image.width() as i32, image.height() as i32);
    let (cx, cy) = (cx * SCALE, cy * SCALE);
    let (hw, hh) = (w * 0.5 * SCALE, h * 0.5 * SCALE);
    let notch = 4.0 * SCALE;
    let (stripe_on, period) = (5.0 * SCALE, 11.0 * SCALE);
    let rad = angle_deg.to_radians();
    let (cos_a, sin_a) = (rad.cos(), rad.sin());
    let bound_x = (hw * cos_a.abs() + hh * sin_a.abs()).ceil() as i32 + 2;
    let bound_y = (hw * sin_a.abs() + hh * cos_a.abs()).ceil() as i32 + 2;
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
            if ly.abs() > hh {
                continue;
            }
            // Three teeth per end: a triangle wave over the tape height.
            let t = ((ly + hh) / (2.0 * hh) * 3.0).fract();
            let tooth = notch * (1.0 - (t * 2.0 - 1.0).abs());
            if lx.abs() > hw - tooth {
                continue;
            }
            let striped = (lx + hw).rem_euclid(period) < stripe_on;
            let idx = ((py * dw + px) * 4) as usize;
            blend(&mut data[idx..idx + 3], color);
            if striped {
                blend(&mut data[idx..idx + 3], Color::rgba(255, 255, 255, 51));
            }
        }
    }
}

fn blend(px: &mut [u8], c: Color) {
    let a = c.a() as u32;
    for (dst, src) in px.iter_mut().zip([c.r(), c.g(), c.b()]) {
        *dst = ((src as u32 * a + *dst as u32 * (255 - a) + 127) / 255) as u8;
    }
}

/// The pen underline: five smooth waves across `w`, like Thinkly's 124×10 stroke.
pub(super) fn stroke_wavy(image: &mut Pixmap, x: f32, y: f32, w: f32, h: f32, color: Color) {
    let s = SCALE;
    let mid = y + h * 0.6;
    let step = (w - 4.0) / 6.0;
    let mut pb = PathBuilder::new();
    pb.move_to((x + 2.0) * s, mid * s);
    for i in 0..6 {
        let x0 = x + 2.0 + step * i as f32;
        let crest = if i % 2 == 0 { y + h * 0.1 } else { y + h * 1.1 };
        pb.quad_to((x0 + step * 0.5) * s, crest * s, (x0 + step) * s, mid * s);
    }
    let Some(path) = pb.finish() else { return };
    let mut paint = Paint::default();
    paint.anti_alias = true;
    paint.set_color_rgba8(color.r(), color.g(), color.b(), color.a());
    let stroke = Stroke {
        width: 2.4 * s,
        line_cap: tiny_skia::LineCap::Round,
        ..Stroke::default()
    };
    image.stroke_path(&path, &paint, &stroke, Transform::identity(), None);
}
