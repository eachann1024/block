//! The supported inline CSS subset and per-style block typography/spacing metrics.
use std::collections::BTreeMap;

use cosmic_text::Color;

use super::palette::{ExportTheme, Palette};
use crate::editor::model::{Block, Kind, Table, TableCell};

pub(super) fn cell_background(cell: &TableCell, table: &Table, pal: &Palette) -> Option<Color> {
    attribute_color(&cell.attrs, true)
        .or_else(|| attribute_color(&table.attrs, true))
        .map(|color| pal.bg(color))
        .or_else(|| cell.header.then(|| pal.table_header_bg()))
}

pub(super) fn cell_foreground(cell: &TableCell, table: &Table, pal: &Palette) -> Option<Color> {
    attribute_color(&cell.attrs, false)
        .or_else(|| attribute_color(&table.attrs, false))
        .or_else(|| cell.header.then_some(pal.title))
}

pub(super) fn row_text_color(kind: Kind, code: bool, pal: &Palette) -> Color {
    if code {
        return pal.text;
    }
    match kind {
        Kind::Quote => pal.quote_fg,
        Kind::H1 | Kind::H2 | Kind::H3 => pal.title,
        _ => pal.text,
    }
}

pub(super) fn block_font_size(theme: ExportTheme, block: &Block, code: bool) -> f32 {
    let spec = theme.spec();
    if code {
        return 14.5;
    }
    match (block.kind, block.meta.heading_level) {
        (Kind::H1, _) => spec.heads[0],
        (Kind::H2, _) => spec.heads[1],
        (Kind::H3, Some(4)) => spec.body * 1.12,
        (Kind::H3, Some(5 | 6)) => spec.body,
        (Kind::H3, _) => spec.heads[2],
        _ => spec.body,
    }
}

/// Line height multiple: headings run tight, body text uses the style's leading.
pub(super) fn line_factor(theme: ExportTheme, kind: Kind, code: bool) -> f32 {
    match kind {
        _ if code => 1.6,
        Kind::H1 | Kind::H2 | Kind::H3 => 1.35,
        _ => theme.spec().leading,
    }
}

/// `(gutter, pad_x, pad_top, pad_bottom)` around a block's text.
pub(super) fn block_box_metrics(
    theme: ExportTheme,
    kind: Kind,
    code: bool,
    has_marker: bool,
    indent: usize,
    gutters: &BTreeMap<usize, f32>,
) -> (f32, f32, f32, f32) {
    if code {
        return (0.0, 18.0, 14.0, 14.0);
    }
    let boxed_quote = theme.palette().quote_bg.a() > 0;
    match kind {
        Kind::Quote if theme == ExportTheme::Press => (0.0, 4.0, 12.0, 12.0),
        Kind::Quote if boxed_quote => (0.0, 20.0, 12.0, 12.0),
        Kind::Quote => (0.0, 20.0, 2.0, 2.0),
        Kind::H1 => (0.0, 0.0, 18.0, 0.0),
        Kind::H2 => (0.0, 0.0, 12.0, if theme == ExportTheme::Press { 6.0 } else { 0.0 }),
        Kind::H3 => (0.0, 0.0, 8.0, 0.0),
        Kind::Bullet | Kind::Todo | Kind::Numbered => (
            if has_marker { gutters[&indent] } else { 26.0 },
            0.0,
            0.0,
            0.0,
        ),
        _ => (0.0, 0.0, 0.0, 0.0),
    }
}

pub(super) fn block_gap_after(kind: Kind, code: bool) -> f32 {
    match kind {
        _ if code => 18.0,
        Kind::H1 => 10.0,
        Kind::H2 | Kind::H3 => 8.0,
        Kind::Quote => 18.0,
        Kind::Bullet | Kind::Todo | Kind::Numbered => 4.0,
        _ => 10.0,
    }
}

pub(super) fn css<'a>(attrs: &'a [(String, String)], name: &str) -> Option<&'a str> {
    attrs
        .iter()
        .find(|(key, _)| key == "style")
        .and_then(|(_, value)| {
            value
                .split(';')
                .filter_map(|part| part.split_once(':'))
                .filter(|(key, _)| key.trim().eq_ignore_ascii_case(name))
                .map(|(_, value)| value.trim())
                .last()
        })
}

pub(super) fn attribute_color(attrs: &[(String, String)], background: bool) -> Option<Color> {
    let property = if background {
        "background-color"
    } else {
        "color"
    };
    if let Some(value) = css(attrs, property).and_then(|v| color(v, background)) {
        return Some(value);
    }
    if background && let Some(value) = css(attrs, "background").and_then(|v| color(v, true)) {
        return Some(value);
    }
    let html_attr = if background { "bgcolor" } else { "color" };
    if let Some(value) = attrs.iter().find(|(name, _)| name.eq_ignore_ascii_case(html_attr))
        .and_then(|(_, value)| color(value, background)) {
        return Some(value);
    }
    let suffix = if background {
        "background-color"
    } else {
        "text-color"
    };
    attrs.iter().find_map(|(key, value)| {
        (key == &format!("data-thinkly-{suffix}") || key == &format!("data-goose-{suffix}"))
            .then(|| color(value, background))
            .flatten()
    })
}

pub(super) fn color(value: &str, background: bool) -> Option<Color> {
    let lower = value.trim().to_ascii_lowercase();
    let named = crate::editor::text_runs::named_color_hex(&lower, background, false).unwrap_or(match lower.as_str() {
        "default" | "none" | "" => return None,
        "transparent" => return Some(Color::rgba(0, 0, 0, 0)),
        "black" => "#000000",
        "white" => "#ffffff",
        _ => lower.as_str(),
    });
    if let Ok(value) = gpui::Rgba::try_from(named) {
        return Some(Color::rgba(
            (value.r * 255.).round() as u8,
            (value.g * 255.).round() as u8,
            (value.b * 255.).round() as u8,
            (value.a * 255.).round() as u8,
        ));
    }
    let (body, alpha) =
        if let Some(body) = named.strip_prefix("rgb(").and_then(|s| s.strip_suffix(')')) {
            (body, false)
        } else {
            (named.strip_prefix("rgba(")?.strip_suffix(')')?, true)
        };
    let parts: Vec<_> = body.split(',').map(str::trim).collect();
    if parts.len() != if alpha { 4 } else { 3 } {
        return None;
    }
    let component = |value: &str| -> Option<u8> {
        let value = if let Some(percent) = value.strip_suffix('%') {
            percent.parse::<f32>().ok()? * 2.55
        } else {
            value.parse::<f32>().ok()?
        };
        value
            .is_finite()
            .then(|| value.clamp(0., 255.).round() as u8)
    };
    let a = if alpha {
        let value = parts[3].parse::<f32>().ok()?;
        if !value.is_finite() {
            return None;
        }
        (value.clamp(0., 1.) * 255.).round() as u8
    } else {
        255
    };
    Some(Color::rgba(
        component(parts[0])?,
        component(parts[1])?,
        component(parts[2])?,
        a,
    ))
}

pub(super) fn bold(attrs: &[(String, String)]) -> bool {
    matches!(
        css(attrs, "font-weight"),
        Some("bold" | "600" | "700" | "800" | "900")
    )
}

pub(super) fn italic(attrs: &[(String, String)]) -> bool {
    css(attrs, "font-style") == Some("italic")
}
