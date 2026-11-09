//! The big title (the file name) with its per-style mark, and the optional one-line watermark.
#[cfg(not(target_family = "wasm"))]
use std::time::{SystemTime, UNIX_EPOCH};

use cosmic_text::{Align, Attrs, Buffer, Color, FontSystem, Metrics, Shaping, SwashCache, Weight, Wrap};
use tiny_skia::Pixmap;

use super::decor_blocks::buffer_max_line_w;
use super::decor_marks::stroke_wavy;
use super::decor_shapes::{fill_rect, fill_rounded_rect};
use super::layout::{Layout, PAGE_WIDTH, family_of};
use super::paint::text;
use super::palette::{Palette, RenderOptions};
use super::spec::TitleMark;
use super::{Cancellation, ExportError, WATERMARK_LABEL};

/// Watermark text size: the smallest text on any export.
const FOOT: f32 = 14.0;

pub(super) struct ChromeLayout {
    /// Space the title takes above the body, mark and breathing room included.
    pub header_h: f32,
    pub footer_h: f32,
    title: Option<Buffer>,
    title_h: f32,
    footer_items: Vec<(Buffer, f32)>,
}

pub(super) fn build_chrome(
    fonts: &mut FontSystem,
    options: &RenderOptions,
    left: f32,
    width: f32,
) -> ChromeLayout {
    let spec = options.theme.spec();
    let name = options.title.trim();
    let name = if name.is_empty() { crate::theme::tr("未命名", "Untitled") } else { name };
    let (title, title_h, header_h) = if options.show_title {
        let mut buf = Buffer::new(fonts, Metrics::new(spec.title, spec.title * 1.3));
        buf.set_size(Some(width), None);
        buf.set_wrap(Wrap::WordOrGlyph);
        let weight = if spec.title_bold { Weight::BOLD } else { Weight::NORMAL };
        let family = if name.is_ascii() { spec.title_family } else { spec.heading_family };
        let attrs = Attrs::new().family(family_of(family)).weight(weight);
        let align = (spec.mark == TitleMark::DoubleRule).then_some(Align::Center);
        buf.set_rich_text([(name, attrs.clone())], &attrs, Shaping::Advanced, align);
        buf.shape_until_scroll(fonts, false);
        let th = buf.layout_runs().map(|r| r.line_top + r.line_height).fold(0.0, f32::max);
        let below = match spec.mark {
            TitleMark::Rule => 20.0 + 2.0 + 36.0,
            TitleMark::Bar => 18.0 + 4.0 + 34.0,
            TitleMark::Wavy => 8.0 + 12.0 + 30.0,
            TitleMark::Highlight | TitleMark::Cursor => 36.0,
            TitleMark::DoubleRule => 18.0 + 7.0 + 34.0,
            TitleMark::Band => 44.0 + 52.0,
        };
        (Some(buf), th, th + below)
    } else {
        (None, 0.0, 0.0)
    };

    let mut footer_items = Vec::new();
    let footer_h = if options.footer {
        let d = today();
        let date = format!("{:04}.{:02}.{:02}", d.0, d.1, d.2);
        for (s, anchor_right) in [(WATERMARK_LABEL, false), (date.as_str(), true)] {
            let mut buf = Buffer::new(fonts, Metrics::new(FOOT, FOOT * 1.4));
            buf.set_size(Some(width), None);
            let attrs = Attrs::new().family(family_of(spec.body_family));
            buf.set_text(s, &attrs, Shaping::Advanced, None);
            buf.shape_until_scroll(fonts, false);
            let x = if anchor_right { left + width - buffer_max_line_w(&buf) } else { left };
            footer_items.push((buf, x));
        }
        1.0 + 16.0 + FOOT * 1.4
    } else {
        0.0
    };
    ChromeLayout { header_h, footer_h, title, title_h, footer_items }
}

pub(super) fn paint_chrome(
    image: &mut Pixmap,
    layout: &Layout,
    pal: &Palette,
    fonts: &mut FontSystem,
    cache: &mut SwashCache,
    cancel: &Cancellation,
) -> Result<(), ExportError> {
    let spec = layout.options.theme.spec();
    let (x, w, hy) = (layout.margin_left, layout.content_width, layout.margin_top);
    let chrome = &layout.chrome;
    if let Some(title) = &chrome.title {
        let th = chrome.title_h;
        let mut color = pal.title;
        match spec.mark {
            TitleMark::Rule => fill_rect(image, x, hy + th + 20.0, w, 2.0, pal.title),
            TitleMark::Bar => fill_rounded_rect(image, x, hy + th + 18.0, 48.0, 4.0, 2.0, pal.accent),
            TitleMark::Wavy => stroke_wavy(image, x, hy + th + 8.0, 150.0, 12.0, pal.accent),
            TitleMark::DoubleRule => {
                fill_rect(image, x, hy + th + 18.0, w, 3.0, pal.divider);
                fill_rect(image, x, hy + th + 24.0, w, 1.0, pal.divider);
            }
            TitleMark::Band => {
                fill_rect(image, 0.0, 0.0, PAGE_WIDTH, hy + th + 44.0, pal.accent);
                color = pal.accent2;
            }
            TitleMark::Highlight => {
                let ink = Color::rgba(pal.accent.r(), pal.accent.g(), pal.accent.b(), 105);
                for run in title.layout_runs() {
                    let x0 = run.glyphs.first().map_or(0.0, |g| g.x);
                    let top = hy + run.line_top + run.line_height * 0.5;
                    fill_rect(image, x + x0 - 4.0, top, run.line_w + 8.0, run.line_height * 0.38, ink);
                }
            }
            TitleMark::Cursor => {
                if let Some(run) = title.layout_runs().last() {
                    let top = hy + run.line_top + run.line_height * 0.16;
                    let cw = (spec.title * 0.55).round();
                    fill_rect(image, x + run.line_w + 10.0, top, cw, run.line_height * 0.7, pal.accent);
                }
            }
        }
        text(image, title, x, hy, (pal, color), fonts, cache, cancel)?;
    }
    if layout.options.footer {
        let fy = layout.footer_y;
        fill_rect(image, x, fy, w, 1.0, pal.divider);
        for (buf, bx) in &chrome.footer_items {
            text(image, buf, *bx, fy + 16.0, (pal, pal.secondary), fonts, cache, cancel)?;
        }
    }
    Ok(())
}

/// Today's `(year, month, day)` in UTC.
pub(super) fn today() -> (i64, u32, u32) {
    #[cfg(not(target_family = "wasm"))]
    let secs = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0);
    #[cfg(target_family = "wasm")]
    let secs = (js_sys::Date::now() / 1000.) as u64; // SystemTime::now() panics on wasm32
    let days = (secs / 86_400) as i64;
    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u32;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146_096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    (if m <= 2 { y + 1 } else { y }, m, d)
}
