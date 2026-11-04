//! Per-style header, masthead, film edge markings, and watermark footer.
use std::time::{SystemTime, UNIX_EPOCH};

use cosmic_text::{Attrs, Buffer, Color, Family, FontSystem, Metrics, Shaping, SwashCache, Weight};
use tiny_skia::Pixmap;

use super::decor_blocks::buffer_max_line_w;
use super::decor_marks::paint_stamp;
use super::decor_shapes::{fill_rect, paint_diamond, stroke_rounded_rect};
use super::layout::{Layout, SCALE};
use super::paint::text;
use super::palette::{ExportTheme, Palette, RenderOptions};
use super::{Cancellation, ExportError};

pub(super) struct ChromeLayout {
    pub header_h: f32,
    pub footer_h: f32,
    header_items: Vec<ChromeText>,
    footer_items: Vec<ChromeText>,
    film_edges: Vec<ChromeText>,
    stamp: Option<([Buffer; 3], Color)>,
}

struct ChromeText {
    buf: Buffer,
    x: f32,
    dy: f32,
    color: Color,
}

#[derive(Clone, Copy)]
enum Anchor { L(f32), C(f32), R(f32) }

type Spec<'a> = (&'a str, &'static str, f32, bool, Anchor, f32, Color);

struct DateInfo { year: i64, month: u32, day: u32, weekday: &'static str }

#[rustfmt::skip]
pub(super) fn build_chrome(
    fonts: &mut FontSystem,
    options: &RenderOptions,
    pal: &Palette,
    left: f32,
    width: f32,
) -> ChromeLayout {
    let d = today();
    let theme = options.theme;
    let (right, mid) = (left + width, left + width * 0.5);
    let title = if options.title.trim().is_empty() { crate::theme::tr("随手记录", "Notes") } else { options.title.trim() };
    let sec = pal.secondary;

    let (header_h, header_items) = match theme {
        ExportTheme::Mono | ExportTheme::Ink => {
            let l = if options.show_title && !options.title.trim().is_empty() {
                format!("THINKLY FAST · {title}")
            } else {
                "THINKLY FAST".to_string()
            };
            let r = format!("{:04} · {:02} · {:02}", d.year, d.month, d.day);
            (64.0, items(fonts, &[
                (&l, "SF Mono", 12.0, false, Anchor::L(left), 0.0, sec),
                (&r, "SF Mono", 12.0, false, Anchor::R(right), 0.0, sec),
            ]))
        }
        ExportTheme::Journal | ExportTheme::Kraft => {
            let date = format!("{:04} / {:02} / {:02}", d.year, d.month, d.day);
            let mut specs = vec![
                (d.weekday, "SF Mono", 12.0, false, Anchor::C(left + 22.0), 0.0, sec),
                (date.as_str(), "SF Mono", 13.0, false, Anchor::L(left + 54.0), 0.0, sec),
            ];
            if options.show_title {
                specs.push((title, "PingFang SC", 13.0, false, Anchor::R(right), 0.0, sec));
            }
            (36.0, items(fonts, &specs))
        }
        ExportTheme::Riso => {
            let l = if options.show_title {
                format!("NO. {:02}{:02} / {}", d.month, d.day, title)
            } else {
                format!("NO. {:02}{:02}", d.month, d.day)
            };
            (38.0, items(fonts, &[
                (&l, "SF Mono", 12.0, true, Anchor::L(left), 0.0, pal.text),
                ("RISO · 2-COLOR", "SF Mono", 12.0, true, Anchor::R(right), 0.0, pal.accent),
            ]))
        }
        ExportTheme::Press => {
            let l = if crate::theme::is_en() { format!("NO. {:02}{:02}", d.month, d.day) } else { format!("第 {:02}{:02} 期", d.month, d.day) };
            let r = if crate::theme::is_en() { format!("{:04}.{:02}.{:02}", d.year, d.month, d.day) } else { format!("{:04} 年 {} 月 {} 日", d.year, d.month, d.day) };
            (106.0, items(fonts, &[
                (&l, "Songti SC", 12.0, false, Anchor::L(left), 0.0, sec),
                ("THINKLY FAST DAILY", "Songti SC", 12.0, false, Anchor::C(mid), 0.0, sec),
                (&r, "Songti SC", 12.0, false, Anchor::R(right), 0.0, sec),
                (crate::theme::tr("思 考 快 报", "THINKLY PRESS"), "Songti SC", 34.0, true, Anchor::C(mid), 32.0, pal.title),
            ]))
        }
        ExportTheme::Neon => {
            let l = if options.show_title {
                format!("ISSUE #{:02}{:02} · {}", d.month, d.day, title)
            } else {
                format!("ISSUE #{:02}{:02} · NIGHT SHIFT", d.month, d.day)
            };
            (38.0, items(fonts, &[
                (&l, "SF Mono", 12.0, true, Anchor::L(left), 0.0, pal.accent),
                ("SILKSCREEN · 2-INK", "SF Mono", 12.0, true, Anchor::R(right), 0.0, pal.accent2),
            ]))
        }
        ExportTheme::Film => {
            let l = format!("FRAME 12 · {:04}.{:02}.{:02}", d.year, d.month, d.day);
            (38.0, items(fonts, &[(&l, "SF Mono", 13.0, false, Anchor::L(left), 0.0, pal.accent)]))
        }
    };

    let film_edges = if theme == ExportTheme::Film {
        let ac = pal.accent;
        items(fonts, &[
            ("THINKLY 400", "SF Mono", 11.0, false, Anchor::L(24.0), 32.0, ac),
            ("▸ 11A", "SF Mono", 11.0, false, Anchor::C(310.0), 32.0, ac),
            ("▸ 12", "SF Mono", 11.0, false, Anchor::C(535.0), 32.0, ac),
            ("▸ 12A", "SF Mono", 11.0, false, Anchor::R(792.0), 32.0, ac),
            ("◂ 12", "SF Mono", 11.0, false, Anchor::L(24.0), -48.0, ac),
            ("SAFETY FILM", "SF Mono", 11.0, false, Anchor::C(408.0), -48.0, ac),
            ("13 ▸", "SF Mono", 11.0, false, Anchor::R(792.0), -48.0, ac),
        ])
    } else {
        Vec::new()
    };

    let (footer_h, footer_items, stamp) = if options.footer {
        build_footer(fonts, theme, pal, &d, left, right)
    } else {
        (0.0, Vec::new(), None)
    };

    ChromeLayout { header_h, footer_h, header_items, footer_items, film_edges, stamp }
}

#[rustfmt::skip]
fn build_footer(
    fonts: &mut FontSystem,
    theme: ExportTheme,
    pal: &Palette,
    d: &DateInfo,
    left: f32,
    right: f32,
) -> (f32, Vec<ChromeText>, Option<([Buffer; 3], Color)>) {
    let sec = pal.secondary;
    match theme {
        ExportTheme::Mono | ExportTheme::Ink => {
            let r = if theme == ExportTheme::Ink { "DARK EDITION · 01 / 01" } else { "PAGE 01 / 01" };
            (54.0, items(fonts, &[
                ("THINKLY FAST", "SF Mono", 12.0, false, Anchor::L(left + 16.0), 32.0, sec),
                (r, "SF Mono", 12.0, false, Anchor::R(right), 32.0, sec),
            ]), None)
        }
        ExportTheme::Journal | ExportTheme::Kraft => {
            let is_j = theme == ExportTheme::Journal;
            let l = if is_j { crate::theme::tr("thinkly fast · 随手记", "thinkly fast · journal") } else { crate::theme::tr("thinkly fast · 黑卡手账", "thinkly fast · kraft") };
            let sc = if is_j { Color::rgba(181, 70, 47, 215) } else { Color::rgba(224, 122, 95, 220) };
            let s1 = make_buf(fonts, "THINKLY", "SF Mono", 10.0, true);
            let s2 = make_buf(fonts, &format!("{:02}·{:02}", d.month, d.day), "SF Mono", 15.0, true);
            let s3 = make_buf(fonts, &format!("{:04}", d.year), "SF Mono", 10.0, true);
            (76.0, items(fonts, &[(l, "Kaiti SC", 14.0, false, Anchor::L(left), 38.0, sec)]), Some(([s1, s2, s3], sc)))
        }
        ExportTheme::Riso | ExportTheme::Neon => {
            let r = format!("PRINTED {:04}-{:02}-{:02}", d.year, d.month, d.day);
            let (lc, rc) = if theme == ExportTheme::Riso { (pal.text, pal.accent) } else { (pal.accent, pal.accent2) };
            (56.0, items(fonts, &[
                ("THINKLY FAST", "SF Mono", 12.0, true, Anchor::L(left), 34.0, lc),
                (&r, "SF Mono", 12.0, true, Anchor::R(right), 34.0, rc),
            ]), None)
        }
        ExportTheme::Press => {
            let r = format!("{:04}-{:02}-{:02}", d.year, d.month, d.day);
            (52.0, items(fonts, &[
                (crate::theme::tr("THINKLY FAST 印行", "THINKLY FAST PRESS"), "Songti SC", 12.0, false, Anchor::L(left), 30.0, sec),
                (&r, "Songti SC", 12.0, false, Anchor::R(right), 30.0, sec),
            ]), None)
        }
        ExportTheme::Film => {
            let r = format!("DEVELOPED {:04}-{:02}-{:02}", d.year, d.month, d.day);
            (46.0, items(fonts, &[
                ("THINKLY FAST", "SF Mono", 12.0, false, Anchor::L(left), 24.0, sec),
                (&r, "SF Mono", 12.0, false, Anchor::R(right), 24.0, sec),
            ]), None)
        }
    }
}

pub(super) fn paint_chrome(
    image: &mut Pixmap,
    layout: &Layout,
    pal: &Palette,
    fonts: &mut FontSystem,
    cache: &mut SwashCache,
    cancel: &Cancellation,
) -> Result<(), ExportError> {
    let theme = layout.options.theme;
    let (x, w, hy) = (layout.margin_left, layout.content_width, layout.margin_top);
    let h_page = layout.height as f32 / SCALE;
    match theme {
        ExportTheme::Mono | ExportTheme::Ink => fill_rect(image, x, hy + 28.0, w, 3.0, pal.title),
        ExportTheme::Journal | ExportTheme::Kraft => {
            stroke_rounded_rect(image, x, hy - 2.0, 44.0, 20.0, 10.0, 1.5, pal.secondary);
        }
        ExportTheme::Press => {
            fill_rect(image, x, hy + 22.0, w, 3.0, pal.divider);
            fill_rect(image, x, hy + 80.0, w, 1.0, pal.divider);
            fill_rect(image, x, hy + 84.0, w, 1.0, pal.divider);
        }
        _ => {}
    }
    for it in &layout.chrome.header_items {
        text(image, &it.buf, it.x, hy + it.dy, (pal, it.color), fonts, cache, cancel)?;
    }
    for edge in &layout.chrome.film_edges {
        let ey = if edge.dy >= 0.0 { edge.dy } else { h_page + edge.dy };
        text(image, &edge.buf, edge.x, ey, (pal, edge.color), fonts, cache, cancel)?;
    }
    if layout.options.footer {
        let fy = layout.footer_y;
        match theme {
            ExportTheme::Mono => {
                fill_rect(image, x, fy + 16.0, w, 1.0, pal.divider);
                fill_rect(image, x, fy + 35.0, 8.0, 8.0, pal.title);
            }
            ExportTheme::Ink => {
                fill_rect(image, x, fy + 16.0, w, 1.0, Color::rgb(42, 42, 42));
                fill_rect(image, x, fy + 35.0, 8.0, 8.0, pal.title);
            }
            ExportTheme::Press => {
                fill_rect(image, x, fy + 14.0, w, 1.0, pal.divider);
                fill_rect(image, x, fy + 17.0, w, 1.0, pal.divider);
                paint_diamond(image, x + w * 0.5, fy + 36.0, 4.5, pal.accent);
            }
            ExportTheme::Riso | ExportTheme::Neon => fill_rect(image, x, fy + 16.0, w, 2.0, pal.divider),
            _ => {}
        }
        for it in &layout.chrome.footer_items {
            text(image, &it.buf, it.x, fy + it.dy, (pal, it.color), fonts, cache, cancel)?;
        }
        if let Some((lines, color)) = &layout.chrome.stamp {
            paint_stamp(image, x + w - 42.0, fy + 34.0, *color, lines, pal, fonts, cache, cancel)?;
        }
    }
    Ok(())
}

fn make_buf(fonts: &mut FontSystem, s: &str, family: &str, size: f32, bold: bool) -> Buffer {
    let mut buf = Buffer::new(fonts, Metrics::new(size, size * 1.35));
    buf.set_size(Some(720.0), None);
    let mut attrs = Attrs::new().family(Family::Name(family));
    if bold {
        attrs = attrs.weight(Weight::BOLD);
    }
    buf.set_text(s, &attrs, Shaping::Advanced, None);
    buf.shape_until_scroll(fonts, false);
    buf
}

fn items(fonts: &mut FontSystem, specs: &[Spec<'_>]) -> Vec<ChromeText> {
    specs
        .iter()
        .map(|&(s, fam, sz, bold, anchor, dy, color)| {
            let buf = make_buf(fonts, s, fam, sz, bold);
            let w = buffer_max_line_w(&buf);
            let x = match anchor {
                Anchor::L(x) => x,
                Anchor::C(cx) => cx - w * 0.5,
                Anchor::R(r) => r - w,
            };
            ChromeText { buf, x, dy, color }
        })
        .collect()
}

fn today() -> DateInfo {
    let secs = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0);
    let days = (secs / 86_400) as i64;
    let weekday = ["THU", "FRI", "SAT", "SUN", "MON", "TUE", "WED"][days.rem_euclid(7) as usize];
    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u32;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146_096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let year = if m <= 2 { y + 1 } else { y };
    DateInfo { year, month: m, day: d, weekday }
}
