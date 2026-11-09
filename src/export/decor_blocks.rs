//! Per-style block chrome: code panels, quote bars, heading accents, and list markers.
use cosmic_text::{Buffer, Color, FontSystem, SwashCache};
use tiny_skia::Pixmap;

use super::decor_shapes::{fill_circle, fill_rect, fill_rounded_rect, paint_check, stroke_rounded_rect};
use super::layout::{Layout, Row};
use super::paint::text;
use super::palette::{ExportTheme, Palette};
use super::{Cancellation, ExportError};
use crate::editor::model::Kind;

/// Corner radius for code panels and quote boxes: soft on card styles, crisp on print styles.
fn radius(theme: ExportTheme) -> f32 {
    match theme {
        ExportTheme::Journal | ExportTheme::Kraft | ExportTheme::Mist | ExportTheme::Night => 10.0,
        ExportTheme::Press | ExportTheme::Riso | ExportTheme::Terminal => 2.0,
        _ => 6.0,
    }
}

pub(super) fn paint_block_bg(image: &mut Pixmap, row: &Row, layout: &Layout, pal: &Palette) {
    let theme = layout.options.theme;
    let x = row.marker_x;
    let w = (layout.margin_left + layout.content_width - x).max(row.width);
    let r = radius(theme);
    if row.rule {
        fill_rect(image, x, row.y + row.height / 2., w, 1., pal.divider);
        return;
    }
    if let Some(kind) = row.callout {
        let (bg, border, accent, _, _) = crate::ui::callout_controls::tone(kind, pal.dark);
        let color = |hex: u32| Color::rgb((hex >> 16) as u8, (hex >> 8) as u8, hex as u8);
        fill_rounded_rect(image, x, row.y, w, row.height, 8., color(bg));
        stroke_rounded_rect(image, x, row.y, w, row.height, 8., 1., color(border));
        fill_rounded_rect(image, x, row.y, 4., row.height, 2., color(accent));
        return;
    }
    if row.code {
        fill_rounded_rect(image, x, row.y, w, row.height, r, pal.code_bg);
        let sw = if theme == ExportTheme::Riso { 1.5 } else { 1.0 };
        stroke_rounded_rect(image, x, row.y, w, row.height, r, sw, pal.code_border);
        return;
    }

    match row.kind {
        Kind::Quote if theme == ExportTheme::Press => {
            fill_rect(image, x, row.y, w, 1.0, pal.quote_bar);
            fill_rect(image, x, row.y + row.height - 1.0, w, 1.0, pal.quote_bar);
        }
        Kind::Quote => {
            if pal.quote_bg.a() > 0 {
                fill_rounded_rect(image, x, row.y, w, row.height, r, pal.quote_bg);
            }
            let bar = if matches!(theme, ExportTheme::Journal | ExportTheme::Film) { 3.0 } else { 4.0 };
            fill_rounded_rect(image, x, row.y, bar, row.height, bar * 0.5, pal.quote_bar);
        }
        Kind::H2 if theme == ExportTheme::Riso => {
            let tw = buffer_max_line_w(&row.buffer);
            let top = row.text_y + row.line_height * 0.55;
            let ink = Color::rgba(pal.accent.r(), pal.accent.g(), pal.accent.b(), 80);
            fill_rect(image, row.x - 3.0, top, (tw + 6.0).min(w), row.line_height * 0.35, ink);
        }
        Kind::H2 if theme == ExportTheme::Press => {
            fill_rect(image, x, row.y + row.height - 1.0, w, 1.0, pal.divider);
        }
        Kind::H2 if theme == ExportTheme::Poster => {
            let h = row.line_height * 0.62;
            fill_rect(image, x - 16.0, row.text_y + (row.line_height - h) * 0.5, 5.0, h, pal.accent);
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
    let square = matches!(theme, ExportTheme::Poster | ExportTheme::Terminal | ExportTheme::Riso);
    match row.kind {
        Kind::Todo => {
            if row.checked {
                fill_rounded_rect(image, x, cy - 7.5, 15.0, 15.0, 3.5, pal.accent);
                paint_check(image, x + 7.5, cy, pal.inner_canvas);
            } else {
                stroke_rounded_rect(image, x + 0.75, cy - 6.75, 13.5, 13.5, 3.0, 1.5, pal.accent);
            }
        }
        Kind::Numbered => {
            if let Some(buf) = &row.marker {
                let lh = buf.layout_runs().next().map_or(row.line_height, |r| r.line_height);
                let y = row.text_y + (row.line_height - lh) * 0.5;
                text(image, buf, x, y, (pal, pal.title), fonts, cache, cancel)?;
            }
        }
        Kind::Bullet if square => fill_rect(image, x + 2.0, cy - 3.5, 7.0, 7.0, pal.accent),
        Kind::Bullet => fill_circle(image, x + 5.0, cy, 3.2, pal.accent),
        _ => {}
    }
    Ok(())
}

pub(super) fn buffer_max_line_w(buf: &Buffer) -> f32 {
    buf.layout_runs().map(|r| r.line_w).fold(0.0, f32::max)
}
