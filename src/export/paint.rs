//! Draw only the buffers used to compute layout; no second wrapping pass.
use cosmic_text::{Buffer, FontSystem, SwashCache};
use tiny_skia::{Color, Paint, Pixmap, Rect, Transform};

use super::layout::{CODE, Layout, Row, SCALE, STRIKE};
use super::{Cancellation, ExportError};
use crate::editor::model::Kind;

pub(super) fn rasterize(
    layout: &Layout,
    fonts: &mut FontSystem,
    cache: &mut SwashCache,
    cancel: &Cancellation,
) -> Result<Pixmap, ExportError> {
    cancel.check()?;
    let mut image = Pixmap::new(layout.width, layout.height)
        .ok_or(ExportError::Rendering("pixel allocation failed"))?;
    image.fill(Color::WHITE);
    for row in &layout.rows {
        cancel.check()?;
        if row.code {
            rect(
                &mut image,
                row.x - 8.0,
                row.y,
                row.width + 16.0,
                row.height,
                [242, 244, 247],
            );
        }
        marker(&mut image, row, fonts, cache, cancel)?;
        if let Some(label) = &row.label {
            text(&mut image, label, row.x, row.y + 8.0, fonts, cache, cancel)?;
        }
        for run in row.buffer.layout_runs() {
            for glyph in run.glyphs {
                if glyph.metadata & CODE != 0 {
                    rect(
                        &mut image,
                        row.x + glyph.x,
                        row.text_y + run.line_top + 1.0,
                        glyph.w,
                        run.line_height - 2.0,
                        [237, 240, 245],
                    );
                }
            }
        }
        text(
            &mut image,
            &row.buffer,
            row.x,
            row.text_y,
            fonts,
            cache,
            cancel,
        )?;
        for run in row.buffer.layout_runs() {
            for glyph in run.glyphs {
                if glyph.metadata & STRIKE != 0 {
                    rect(
                        &mut image,
                        row.x + glyph.x,
                        row.text_y + run.line_y - glyph.font_size * 0.3,
                        glyph.w,
                        1.0,
                        [32, 36, 44],
                    );
                }
            }
        }
    }
    Ok(image)
}

fn rect(image: &mut Pixmap, x: f32, y: f32, w: f32, h: f32, color: [u8; 3]) {
    let mut paint = Paint::default();
    paint.set_color_rgba8(color[0], color[1], color[2], 255);
    if let Some(rect) = Rect::from_xywh(x * SCALE, y * SCALE, w * SCALE, h * SCALE) {
        image.fill_rect(rect, &paint, Transform::identity(), None);
    }
}

fn marker(
    image: &mut Pixmap,
    row: &Row,
    fonts: &mut FontSystem,
    cache: &mut SwashCache,
    cancel: &Cancellation,
) -> Result<(), ExportError> {
    let x = row.marker_x;
    let y = row.text_y + row.line_height / 2.0;
    match row.kind {
        Kind::Bullet => rect(image, x + 5.0, y - 2.5, 5.0, 5.0, [82, 96, 113]),
        Kind::Quote => rect(image, x, row.y, 3.0, row.height, [197, 208, 230]),
        Kind::Todo => {
            rect(image, x + 2.0, y - 7.0, 14.0, 14.0, [84, 125, 208]);
            if !row.checked {
                rect(image, x + 3.0, y - 6.0, 12.0, 12.0, [255, 255, 255]);
            } else {
                rect(image, x + 5.0, y, 3.0, 2.0, [255, 255, 255]);
                rect(image, x + 8.0, y - 3.0, 2.0, 5.0, [255, 255, 255]);
            }
        }
        _ => {}
    }
    if let Some(marker) = &row.marker {
        text(image, marker, x, row.text_y, fonts, cache, cancel)?;
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn text(
    image: &mut Pixmap,
    buffer: &Buffer,
    x: f32,
    y: f32,
    fonts: &mut FontSystem,
    cache: &mut SwashCache,
    cancel: &Cancellation,
) -> Result<(), ExportError> {
    let width = image.width() as i32;
    let height = image.height() as i32;
    let pixels = image.data_mut();
    for run in buffer.layout_runs() {
        cancel.check()?;
        for glyph in run.glyphs {
            let physical = glyph.physical((x * SCALE, (y + run.line_y) * SCALE), SCALE);
            cache.with_pixels(
                fonts,
                physical.cache_key,
                cosmic_text::Color::rgb(32, 36, 44),
                |dx, dy, color| {
                    let px = physical.x + dx;
                    let py = physical.y + dy;
                    if px < 0 || py < 0 || px >= width || py >= height {
                        return;
                    }
                    let index = ((py * width + px) * 4) as usize;
                    let alpha = color.a() as u32;
                    // The canvas remains opaque. Direct source-over avoids a tiny-skia path per pixel.
                    for (channel, source) in
                        [color.r(), color.g(), color.b()].into_iter().enumerate()
                    {
                        pixels[index + channel] = ((source as u32 * alpha
                            + pixels[index + channel] as u32 * (255 - alpha)
                            + 127)
                            / 255) as u8;
                    }
                },
            );
        }
    }
    Ok(())
}
