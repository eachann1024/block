//! Attribute and document-size checks for the SVG scanner.

use super::svg_scan::SvgSize;

pub(super) fn local_name(name: &str) -> &str {
    name.rsplit(':').next().unwrap_or(name)
}

pub(super) fn forbidden(local: &str) -> bool {
    matches!(
        local,
        "script" | "foreignobject" | "iframe" | "object" | "embed" | "audio" | "video" | "canvas"
            | "animate" | "animatetransform" | "animatemotion" | "set" | "handler" | "mpath"
            | "image" | "feimage" | "link" | "discard" | "font-face-uri" | "font-face-src"
    )
}

pub(super) fn check_attr(name: &str, value: &str) -> Result<(), String> {
    let name = name.to_ascii_lowercase();
    if name.starts_with("on") {
        return Err(super::copy("不支持事件属性", "Event attributes are not supported"));
    }
    if name == "externalresourcesrequired" && value.trim().eq_ignore_ascii_case("true") {
        return Err(super::copy("不支持外部或嵌入资源", "External or embedded resources are not supported"));
    }
    if name == "xmlns" || name.starts_with("xmlns:") {
        return Ok(());
    }
    if name == "href" || name.ends_with(":href") || name == "src" || name.ends_with(":src") {
        let refer = value.trim();
        if refer.is_empty() || (refer.starts_with('#') && !refer.contains(':') && !refer.contains('/') && !refer.contains('\\')) {
            return Ok(());
        }
        return Err(super::copy("不支持外部或嵌入资源", "External or embedded resources are not supported"));
    }
    check_resource(value)
}

pub(super) fn check_resource(value: &str) -> Result<(), String> {
    let lower = value.to_ascii_lowercase();
    let compact: String = lower.chars().filter(|ch| !ch.is_whitespace()).collect();
    if compact.contains("javascript:") || compact.contains("data:") || compact.contains("@import") || compact.contains("expression(") {
        return Err(super::copy("不支持外部或嵌入资源", "External or embedded resources are not supported"));
    }
    // usvg decodes CSS escapes before resolving url(). A backslash can hide data:, url, or a path.
    if value.contains('\\') || value.contains('\u{0000}') {
        return Err(super::copy("不支持外部或嵌入资源", "External or embedded resources are not supported"));
    }
    let mut rest = lower.as_str();
    while let Some(at) = rest.find("url(") {
        let after = &rest[at + 4..];
        let end = after.find(')').ok_or_else(|| super::copy("不支持外部或嵌入资源", "External or embedded resources are not supported"))?;
        let inner = after[..end].trim().trim_matches(|ch: char| ch == '"' || ch == '\'' || ch.is_whitespace());
        if !(inner.starts_with('#') && !inner.contains(':') && !inner.contains('/') && !inner.contains('\\') && !inner.contains('%')) {
            return Err(super::copy("不支持外部或嵌入资源", "External or embedded resources are not supported"));
        }
        rest = &after[end + 1..];
    }
    Ok(())
}

pub(super) fn root_size(attrs: &[(String, String)]) -> Result<SvgSize, String> {
    let find = |key: &str| attrs.iter().find(|(name, _)| name.eq_ignore_ascii_case(key)).map(|(_, value)| value.as_str());
    let width = find("width").and_then(css_px);
    let height = find("height").and_then(css_px);
    let view = find("viewBox").or_else(|| find("viewbox")).and_then(view_box);
    let (width, height) = match (width, height, view) {
        (Some(width), Some(height), _) => (width, height),
        (Some(width), None, Some((box_w, box_h))) => (width, width * box_h / box_w),
        (None, Some(height), Some((box_w, box_h))) => (height * box_w / box_h, height),
        (None, None, Some((box_w, box_h))) => (box_w, box_h),
        _ => return Err(super::copy("SVG 缺少可计算的宽高", "SVG has no usable width and height")),
    };
    Ok(SvgSize { width, height })
}

fn view_box(raw: &str) -> Option<(f32, f32)> {
    let parts = raw
        .split(|ch: char| ch == ',' || ch.is_whitespace())
        .filter(|part| !part.is_empty())
        .map(|part| part.parse::<f32>().ok())
        .collect::<Option<Vec<_>>>()?;
    if parts.len() != 4 || !(parts[2] > 0.0 && parts[3] > 0.0 && parts[2].is_finite() && parts[3].is_finite()) {
        None
    } else {
        Some((parts[2], parts[3]))
    }
}

fn css_px(raw: &str) -> Option<f32> {
    let raw = raw.trim();
    if raw.is_empty() || raw.ends_with('%') { return None; }
    let split = raw.find(|ch: char| ch.is_ascii_alphabetic()).unwrap_or(raw.len());
    let value: f32 = raw[..split].trim().parse().ok()?;
    if !value.is_finite() || value <= 0.0 { return None; }
    let px = match raw[split..].trim() {
        "" | "px" => value,
        "pt" => value * 96.0 / 72.0,
        "pc" => value * 16.0,
        "in" => value * 96.0,
        "cm" => value * 96.0 / 2.54,
        "mm" => value * 96.0 / 25.4,
        _ => return None,
    };
    px.is_finite().then_some(px)
}
