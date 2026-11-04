//! Rasterize the pre-shaped layout buffers and procedural style decorations onto a Pixmap.
use cosmic_text::{Buffer, Color, FontSystem, SwashCache};
use tiny_skia::Pixmap;

use super::decor_shapes::fill_rect;
use super::layout::{Layout, SCALE, STRIKE};
use super::palette::{ExportTheme, Palette};
use super::rich::{BACKGROUND_SHIFT, UNDERLINE};
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
        .ok_or(ExportError::Rendering(crate::theme::tr("像素缓冲分配失败", "Failed to allocate pixel buffer")))?;
    let pal = layout.options.theme.palette();

    super::decor_paper::paint_paper(&mut image, layout, &pal, cancel)?;
    super::chrome::paint_chrome(&mut image, layout, &pal, fonts, cache, cancel)?;

    for row in &layout.rows {
        cancel.check()?;
        if let Some(frame) = &row.frame {
            if let Some(color) = frame.background {
                fill_rect(
                    &mut image,
                    frame.x,
                    frame.y,
                    frame.width,
                    frame.height,
                    pal.bg(color),
                );
            }
            let border = pal.table_border;
            for (x, y, w, h) in [
                (frame.x, frame.y, frame.width, 0.5),
                (frame.x, frame.y, 0.5, frame.height),
                (frame.x + frame.width - 0.5, frame.y, 0.5, frame.height),
                (frame.x, frame.y + frame.height - 0.5, frame.width, 0.5),
            ] {
                fill_rect(&mut image, x, y, w, h, border);
            }
        } else {
            super::decor_blocks::paint_block_bg(&mut image, row, layout, &pal);
            super::decor_blocks::paint_list_marker(
                &mut image, row, layout, &pal, fonts, cache, cancel,
            )?;
        }

        if let Some(label) = &row.label {
            text(
                &mut image,
                label,
                row.x,
                row.y + 8.0,
                (&pal, pal.secondary),
                fonts,
                cache,
                cancel,
            )?;
        }

        for run in row.buffer.layout_runs() {
            for glyph in run.glyphs {
                let index = glyph.metadata >> BACKGROUND_SHIFT;
                if let Some(color) = index.checked_sub(1).and_then(|i| row.backgrounds.get(i)) {
                    fill_rect(
                        &mut image,
                        row.x + glyph.x,
                        row.text_y + run.line_top + 1.,
                        glyph.w,
                        run.line_height - 2.,
                        pal.bg(*color),
                    );
                }
            }
        }

        let fg = super::style::row_text_color(layout.options.theme, row.kind, row.code, &pal);
        if layout.options.theme == ExportTheme::Neon && row.kind == Kind::H1 {
            text(
                &mut image,
                &row.buffer,
                row.x + 3.0,
                row.text_y + 3.0,
                (&pal, pal.accent2),
                fonts,
                cache,
                cancel,
            )?;
        }
        text(
            &mut image,
            &row.buffer,
            row.x,
            row.text_y,
            (&pal, fg),
            fonts,
            cache,
            cancel,
        )?;

        for run in row.buffer.layout_runs() {
            for glyph in run.glyphs {
                if glyph.metadata & STRIKE != 0 {
                    fill_rect(
                        &mut image,
                        row.x + glyph.x,
                        row.text_y + run.line_y - glyph.font_size * 0.3,
                        glyph.w,
                        1.,
                        glyph.color_opt.map_or(fg, |c| pal.fg(c)),
                    );
                }
                if glyph.metadata & UNDERLINE != 0 {
                    fill_rect(
                        &mut image,
                        row.x + glyph.x,
                        row.text_y + run.line_y + 2.,
                        glyph.w,
                        1.,
                        glyph.color_opt.map_or(fg, |c| pal.fg(c)),
                    );
                }
            }
        }
    }

    super::decor_marks::paint_top_tapes(&mut image, layout);
    Ok(image)
}

#[allow(clippy::too_many_arguments)]
pub(super) fn text(
    image: &mut Pixmap,
    buffer: &Buffer,
    x: f32,
    y: f32,
    (pal, default): (&Palette, Color),
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
            if glyph.glyph_id == 0 {
                continue;
            }
            let glyph_color = glyph.color_opt.map_or(default, |c| pal.fg(c));
            let physical = glyph.physical((x * SCALE, (y + run.line_y) * SCALE), SCALE);
            let mut drew = false;
            cache.with_pixels(
                fonts,
                physical.cache_key,
                glyph_color,
                |dx, dy, color| {
                    drew = true;
                    let px = physical.x + dx;
                    let py = physical.y + dy;
                    if px < 0 || py < 0 || px >= width || py >= height {
                        return;
                    }
                    let index = ((py * width + px) * 4) as usize;
                    let alpha = color.a() as u32;
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
            #[cfg(target_family = "wasm")]
            if !drew {
                super::web_cjk::paint_glyph(
                    pixels, width, height, run.text, glyph, x, y + run.line_y, SCALE, glyph_color,
                );
            }
            let _ = drew;
        }
    }
    Ok(())
}
