//! Per-style block backgrounds, heading highlights, quote chrome, and list markers.
use cosmic_text::{Buffer, Color, FontSystem, SwashCache};
use tiny_skia::Pixmap;

use super::decor_marks::paint_tape;
use super::decor_shapes::{
    fill_circle, fill_rect, fill_rounded_rect, paint_check, paint_dashed_rect, paint_diamond,
    paint_multiply_rect, paint_triangle, stroke_rect, stroke_rounded_rect,
};
use super::layout::{Layout, Row};
use super::paint::text;
use super::palette::{ExportTheme, Palette};
use super::{Cancellation, ExportError};
use crate::editor::model::Kind;

pub(super) fn paint_block_bg(image: &mut Pixmap, row: &Row, layout: &Layout, pal: &Palette) {
    let theme = layout.options.theme;
    let x = row.marker_x;
    let w = (layout.margin_left + layout.content_width - x).max(row.width);
    if row.code {
        match theme {
            ExportTheme::Mono | ExportTheme::Ink | ExportTheme::Film => {
                fill_rounded_rect(image, x, row.y, w, row.height, 4.0, pal.code_bg);
            }
            ExportTheme::Journal | ExportTheme::Kraft => {
                fill_rounded_rect(image, x, row.y, w, row.height, 8.0, pal.code_bg);
                paint_dashed_rect(image, x, row.y, w, row.height, 1.5, pal.code_border);
            }
            ExportTheme::Riso => {
                fill_rect(image, x + 5.0, row.y + 5.0, w, row.height, pal.accent);
                fill_rect(image, x, row.y, w, row.height, pal.code_bg);
                stroke_rect(image, x, row.y, w, row.height, 2.0, pal.code_border);
            }
            ExportTheme::Neon => {
                fill_rect(image, x, row.y, w, row.height, pal.code_bg);
                stroke_rect(image, x, row.y, w, row.height, 2.0, pal.code_border);
            }
            ExportTheme::Press => {
                fill_rect(image, x, row.y, w, row.height, pal.code_bg);
                stroke_rect(image, x, row.y, w, row.height, 1.0, pal.code_border);
            }
        }
        return;
    }

    match row.kind {
        Kind::Quote => match theme {
            ExportTheme::Mono | ExportTheme::Ink => {
                fill_rect(image, x, row.y, 4.0, row.height, pal.quote_bar);
            }
            ExportTheme::Film => {
                fill_rect(image, x, row.y, 2.0, row.height, pal.quote_bar);
            }
            ExportTheme::Journal => {
                let qw = (buffer_max_line_w(&row.buffer) + 44.0).clamp(240.0, w);
                fill_rounded_rect(
                    image,
                    x + 2.0,
                    row.y + 3.0,
                    qw,
                    row.height,
                    4.0,
                    Color::rgba(90, 70, 30, 30),
                );
                fill_rounded_rect(image, x, row.y, qw, row.height, 4.0, pal.quote_bg);
                paint_tape(
                    image,
                    x + qw * 0.5 - 48.0,
                    row.y - 10.0,
                    96.0,
                    20.0,
                    2.0,
                    false,
                    6.0,
                    Color::rgba(116, 160, 150, 148),
                    Color::rgba(186, 214, 204, 148),
                );
            }
            ExportTheme::Kraft => {
                let qw = (buffer_max_line_w(&row.buffer) + 44.0).clamp(240.0, w);
                fill_rounded_rect(
                    image,
                    x + 3.0,
                    row.y + 4.0,
                    qw,
                    row.height,
                    4.0,
                    Color::rgba(0, 0, 0, 90),
                );
                fill_rounded_rect(image, x, row.y, qw, row.height, 4.0, pal.quote_bg);
                stroke_rounded_rect(image, x, row.y, qw, row.height, 4.0, 1.0, pal.quote_bar);
                paint_tape(
                    image,
                    x + qw * 0.5 - 48.0,
                    row.y - 10.0,
                    96.0,
                    20.0,
                    3.0,
                    false,
                    5.0,
                    Color::rgba(92, 166, 150, 184),
                    Color::rgba(64, 124, 112, 184),
                );
            }
            ExportTheme::Riso | ExportTheme::Neon => {
                fill_rect(image, x + 6.0, row.y + 6.0, w, row.height, pal.quote_bar);
                fill_rect(image, x, row.y, w, row.height, pal.quote_bg);
            }
            ExportTheme::Press => {
                fill_rect(image, x, row.y, w, 1.0, pal.quote_bar);
                fill_rect(image, x, row.y + row.height - 1.0, w, 1.0, pal.quote_bar);
            }
        },
        Kind::H1 => match theme {
            ExportTheme::Journal => {
                let tw = buffer_max_line_w(&row.buffer);
                fill_rounded_rect(
                    image,
                    x - 2.0,
                    row.y + row.height * 0.56,
                    (tw + 8.0).min(w),
                    row.height * 0.34,
                    2.0,
                    Color::rgba(245, 215, 122, 215),
                );
            }
            ExportTheme::Kraft => {
                let tw = buffer_max_line_w(&row.buffer);
                fill_rounded_rect(
                    image,
                    x - 2.0,
                    row.y + row.height * 0.56,
                    (tw + 8.0).min(w),
                    row.height * 0.34,
                    2.0,
                    Color::rgba(240, 195, 90, 92),
                );
            }
            ExportTheme::Riso => {
                paint_multiply_rect(
                    image,
                    x + 6.0,
                    row.y + 6.0,
                    w,
                    row.height,
                    Color::rgb(255, 90, 168),
                );
                stroke_rect(image, x, row.y, w, row.height, 3.0, pal.accent2);
            }
            ExportTheme::Press => {
                let ry = row.y + row.height - 8.0;
                let mid = x + w * 0.5;
                fill_rect(image, mid - 94.0, ry, 74.0, 1.0, pal.divider);
                fill_rect(image, mid + 20.0, ry, 74.0, 1.0, pal.divider);
                paint_diamond(image, mid, ry + 0.5, 4.5, pal.accent);
            }
            _ => {}
        },
        Kind::H2 => {
            let tw = buffer_max_line_w(&row.buffer);
            match theme {
                ExportTheme::Riso => {
                    fill_rect(
                        image,
                        x,
                        row.y + row.height * 0.52,
                        (tw + 14.0).min(w),
                        row.height * 0.42,
                        Color::rgba(255, 90, 168, 115),
                    );
                }
                ExportTheme::Neon => {
                    fill_rect(
                        image,
                        x,
                        row.y + row.height * 0.52,
                        (tw + 14.0).min(w),
                        row.height * 0.42,
                        Color::rgba(255, 106, 61, 140),
                    );
                }
                ExportTheme::Press => {
                    fill_rect(image, x, row.y + row.height - 2.0, w, 1.0, pal.divider);
                }
                _ => {}
            }
        }
        _ => {}
    }
}

#[allow(clippy::too_many_arguments)]
pub(super) fn paint_list_marker(
    image: &mut Pixmap,
    row: &Row,
    layout: &Layout,
    pal: &Palette,
    fonts: &mut FontSystem,
    cache: &mut SwashCache,
    cancel: &Cancellation,
) -> Result<(), ExportError> {
    let x = row.marker_x;
    let cy = row.text_y + row.line_height * 0.5;
    let theme = layout.options.theme;
    match row.kind {
        Kind::Todo => {
            stroke_rounded_rect(image, x, cy - 6.5, 13.0, 13.0, 3.0, 1.5, pal.accent);
            if row.checked {
                paint_check(image, x + 3.5, cy + 0.5, pal.accent);
            }
        }
        Kind::Bullet | Kind::Numbered => match theme {
            ExportTheme::Riso => {
                fill_circle(image, x + 11.0, cy, 11.0, pal.accent);
                if let Some(buf) = &row.marker {
                    let mw = buffer_max_line_w(buf);
                    text(
                        image,
                        buf,
                        x + 11.0 - mw * 0.5,
                        cy - 8.5,
                        (pal, pal.text),
                        fonts,
                        cache,
                        cancel,
                    )?;
                }
            }
            ExportTheme::Neon => {
                fill_circle(image, x + 11.0, cy, 11.0, pal.accent2);
                if let Some(buf) = &row.marker {
                    let mw = buffer_max_line_w(buf);
                    let fg = Color::rgb(20, 21, 43);
                    text(
                        image,
                        buf,
                        x + 11.0 - mw * 0.5,
                        cy - 8.5,
                        (pal, fg),
                        fonts,
                        cache,
                        cancel,
                    )?;
                }
            }
            _ if row.kind == Kind::Numbered => {
                if let Some(buf) = &row.marker {
                    text(
                        image,
                        buf,
                        x,
                        row.text_y,
                        (pal, pal.accent),
                        fonts,
                        cache,
                        cancel,
                    )?;
                }
            }
            ExportTheme::Mono | ExportTheme::Ink | ExportTheme::Press => {
                fill_rect(image, x, cy - 3.5, 7.0, 7.0, pal.accent);
            }
            ExportTheme::Journal | ExportTheme::Kraft => {
                stroke_rounded_rect(image, x, cy - 6.0, 12.0, 12.0, 3.0, 1.8, pal.accent);
            }
            ExportTheme::Film => {
                paint_triangle(image, x + 1.0, cy - 4.0, 6.5, 8.0, pal.accent);
            }
        },
        _ => {}
    }
    Ok(())
}

pub(super) fn buffer_max_line_w(buf: &Buffer) -> f32 {
    buf.layout_runs().map(|r| r.line_w).fold(0.0, f32::max)
}
