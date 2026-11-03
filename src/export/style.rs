//! The supported inline CSS subset and per-style block typography/spacing metrics.
use std::collections::BTreeMap;

use cosmic_text::Color;

use super::palette::{ExportTheme, Palette};
use crate::editor::model::Kind;

pub(super) fn row_text_color(theme: ExportTheme, kind: Kind, code: bool, pal: &Palette) -> Color {
    if code {
        return if theme == ExportTheme::Neon {
            pal.accent
        } else {
            pal.text
        };
    }
    match kind {
        Kind::Quote => pal.quote_fg,
        Kind::H1 | Kind::H3 => pal.title,
        Kind::H2 => match theme {
            ExportTheme::Riso => pal.text,
            ExportTheme::Neon | ExportTheme::Journal => pal.accent,
            ExportTheme::Kraft
            | ExportTheme::Mono
            | ExportTheme::Ink
            | ExportTheme::Press
            | ExportTheme::Film => pal.title,
        },
        _ => pal.text,
    }
}

pub(super) fn block_font_size(theme: ExportTheme, kind: Kind, code: bool) -> f32 {
    if code {
        return 14.0;
    }
    match kind {
        Kind::H1 if matches!(theme, ExportTheme::Journal | ExportTheme::Kraft) => 36.0,
        Kind::H1 => 38.0,
        Kind::H2
            if matches!(
                theme,
                ExportTheme::Mono | ExportTheme::Ink | ExportTheme::Riso | ExportTheme::Neon
            ) =>
        {
            19.0
        }
        Kind::H2 => 21.0,
        Kind::H3 => 18.0,
        Kind::Quote
            if matches!(
                theme,
                ExportTheme::Mono | ExportTheme::Ink | ExportTheme::Press | ExportTheme::Film
            ) =>
        {
            20.0
        }
        Kind::Quote => 17.5,
        _ => 17.0,
    }
}

pub(super) fn block_box_metrics(
    theme: ExportTheme,
    kind: Kind,
    code: bool,
    has_marker: bool,
    indent: usize,
    gutters: &BTreeMap<usize, f32>,
) -> (f32, f32, f32, f32) {
    if code {
        return (0.0, 16.0, 12.0, 12.0);
    }
    match kind {
        Kind::Quote
            if matches!(
                theme,
                ExportTheme::Mono | ExportTheme::Ink | ExportTheme::Film
            ) =>
        {
            (20.0, 0.0, 4.0, 4.0)
        }
        Kind::Quote if theme == ExportTheme::Press => (0.0, 24.0, 14.0, 14.0),
        Kind::Quote => (0.0, 20.0, 14.0, 14.0),
        Kind::H1 if theme == ExportTheme::Riso => (0.0, 18.0, 14.0, 14.0),
        Kind::H1 if theme == ExportTheme::Press => (0.0, 0.0, 4.0, 22.0),
        Kind::H2 if matches!(theme, ExportTheme::Riso | ExportTheme::Neon) => (0.0, 0.0, 2.0, 4.0),
        Kind::H2 if theme == ExportTheme::Press => (0.0, 0.0, 0.0, 6.0),
        Kind::H2 => (0.0, 0.0, 0.0, 2.0),
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
        _ if code => 16.0,
        Kind::H1 => 18.0,
        Kind::H2 | Kind::H3 => 12.0,
        Kind::Quote => 18.0,
        _ => 8.0,
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

fn color(value: &str, background: bool) -> Option<Color> {
    let lower = value.trim().to_ascii_lowercase();
    let named = match (lower.as_str(), background) {
        ("default", _) => return None,
        ("transparent", _) => return Some(Color::rgba(0, 0, 0, 0)),
        ("gray", false) => "#787774",
        ("brown", false) => "#9f6b53",
        ("red", false) => "#d44c47",
        ("orange", false) => "#d9730d",
        ("yellow", false) => "#cb912f",
        ("green", false) => "#448361",
        ("blue", false) => "#337ea9",
        ("purple", false) => "#9065b0",
        ("pink", false) => "#c14c8a",
        ("gray", true) => "#ebeced",
        ("brown", true) => "#e9e5e3",
        ("red", true) => "#fbe4e4",
        ("orange", true) => "#f6e9d9",
        ("yellow", true) => "#fbf3db",
        ("green", true) => "#ddedea",
        ("blue", true) => "#ddebf1",
        ("purple", true) => "#eae4f2",
        ("pink", true) => "#f4dfeb",
        ("black", _) => "#000000",
        ("white", _) => "#ffffff",
        _ => lower.as_str(),
    };
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
