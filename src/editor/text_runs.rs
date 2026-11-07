//! Shared rich-text runs for ordinary blocks and table cell shaping.
use gpui::{Font, FontFallbacks, FontFeatures, FontStyle, FontWeight, HighlightStyle, Hsla, Rgba,
    StrikethroughStyle, StyledText, TextRun, UnderlineStyle, px, rgb};
use super::model::{Block, MarkKind};

pub(crate) fn css<'a>(attrs: &'a [(String, String)], name: &str) -> Option<&'a str> {
    attrs.iter().find(|(key,_)| key == "style").and_then(|(_,value)|
        value.split(';').find_map(|part| {
            let (key,value) = part.split_once(':')?;
            (key.trim().eq_ignore_ascii_case(name)).then_some(value.trim())
        }))
}
pub(super) fn named_color_hex(value: &str, background: bool, dark: bool) -> Option<&'static str> {
    let key = value.trim();
    let name = match (key, background) {
        ("gray" | "#5e5d59" | "#c3c2b7" | "#6e6e73", false) => "gray",
        ("brown" | "#64473a" | "#e7c1ad" | "#9f6b53", false) => "brown",
        ("red" | "#93342c" | "#b42318" | "#ffb4ab" | "#d93025", false) => "red",
        ("orange" | "#974e08" | "#ffc38f" | "#d9730d", false) => "orange",
        ("yellow" | "#714f17" | "#855600" | "#f3d17c" | "#b06000", false) => "yellow",
        ("green" | "#326a23" | "#086b42" | "#8ce0b1" | "#188038", false) => "green",
        ("teal" | "#2f6259" | "#7fd8c8", false) => "teal",
        ("blue" | "#245bb2" | "#a8c8ff" | "#1a73e8", false) => "blue",
        ("purple" | "#5036b1" | "#6940a5" | "#d0baf8" | "#7627bb", false) => "purple",
        ("pink" | "#ad1a72" | "#f1b6d7" | "#c14c8a", false) => "pink",
        ("gray" | "grey" | "#ebeced" | "#e1e1de" | "#3d3d3a", true) => "gray",
        ("brown" | "#e9e5e3" | "#49352c", true) => "brown",
        ("red" | "#fbe4e4" | "#512b31", true) => "red",
        ("orange" | "#f6e9d9" | "#50351f", true) => "orange",
        ("yellow" | "#f9e8c1" | "#fbf3db" | "#fde6b3" | "#453a1e", true) => "yellow",
        ("green" | "#ddedea" | "#c7eac5" | "#253f34", true) => "green",
        ("blue" | "#ddebf1" | "#cfe2f9" | "#223f52", true) => "blue",
        ("purple" | "#eae4f2" | "#e0dcf8" | "#3b3055", true) => "purple",
        ("pink" | "#f4dfeb" | "#f7d3e1" | "#4b2c42", true) => "pink",
        _ => return None,
    };
    Some(match (name, background, dark) {
        ("gray", false, false) => "#5e5d59", ("gray", false, true) => "#c3c2b7",
        ("brown", false, false) => "#64473a", ("brown", false, true) => "#e7c1ad",
        ("red", false, false) => "#93342c", ("red", false, true) => "#ffb4ab",
        ("orange", false, false) => "#974e08", ("orange", false, true) => "#ffc38f",
        ("yellow", false, false) => "#714f17", ("yellow", false, true) => "#f3d17c",
        ("green", false, false) => "#326a23", ("green", false, true) => "#8ce0b1",
        ("teal", false, false) => "#2f6259", ("teal", false, true) => "#7fd8c8",
        ("blue", false, false) => "#245bb2", ("blue", false, true) => "#a8c8ff",
        ("purple", false, false) => "#5036b1", ("purple", false, true) => "#d0baf8",
        ("pink", false, false) => "#ad1a72", ("pink", false, true) => "#f1b6d7",
        ("gray", true, false) => "#e1e1de", ("gray", true, true) => "#3d3d3a",
        ("brown", true, false) => "#e9e5e3", ("brown", true, true) => "#49352c",
        ("red", true, false) => "#fbe4e4", ("red", true, true) => "#512b31",
        ("orange", true, false) => "#f6e9d9", ("orange", true, true) => "#50351f",
        ("yellow", true, false) => "#fde6b3", ("yellow", true, true) => "#453a1e",
        ("green", true, false) => "#c7eac5", ("green", true, true) => "#253f34",
        ("blue", true, false) => "#cfe2f9", ("blue", true, true) => "#223f52",
        ("purple", true, false) => "#e0dcf8", ("purple", true, true) => "#3b3055",
        ("pink", true, false) => "#f7d3e1", ("pink", true, true) => "#4b2c42",
        _ => return None,
    })
}
pub(crate) fn color(value: &str, background: bool) -> Option<Hsla> {
    let trimmed = value.trim();
    if matches!(trimmed, "default" | "transparent" | "none" | "") { return None; }
    let dark = crate::theme::is_dark();
    let named = if let Some(hex) = named_color_hex(trimmed, background, dark) {
        hex
    } else {
        match trimmed {
            "black" => "#000000",
            "white" => "#ffffff",
            other => other,
        }
    };
    if let Ok(rgba) = Rgba::try_from(named) { return Some(rgba.into()); }
    let (body, alpha) = if let Some(body) = named.strip_prefix("rgb(").and_then(|s| s.strip_suffix(')')) {
        (body, false)
    } else if let Some(body) = named.strip_prefix("rgba(").and_then(|s| s.strip_suffix(')')) {
        (body, true)
    } else { return None; };
    let parts: Vec<_> = body.split(',').map(str::trim).collect();
    if parts.len() != if alpha {4} else {3} { return None; }
    let component = |value: &str| -> Option<f32> {
        if let Some(percent) = value.strip_suffix('%') { Some(percent.parse::<f32>().ok()?.clamp(0.,100.) / 100.) }
        else { Some(value.parse::<f32>().ok()?.clamp(0.,255.) / 255.) }
    };
    Some(Rgba { r:component(parts[0])?, g:component(parts[1])?, b:component(parts[2])?,
        a:if alpha { parts[3].parse::<f32>().ok()?.clamp(0.,1.) } else {1.} }.into())
}
pub(crate) fn attribute_color(attrs: &[(String,String)], background: bool) -> Option<Hsla> {
    let property = if background { "background-color" } else { "color" };
    // The serialized CSS contains the resolved color and has precedence over named data attributes.
    if let Some(value) = css(attrs, property).and_then(|value| color(value, background)) { return Some(value); }
    if background {
        if let Some(value) = css(attrs, "background").and_then(|value| color(value, true)) { return Some(value); }
    }
    let html_attr = if background { "bgcolor" } else { "color" };
    if let Some((_, value)) = attrs.iter().find(|(k, _)| k.eq_ignore_ascii_case(html_attr))
        && let Some(c) = color(value, background)
    {
        return Some(c);
    }
    let suffix = if background { "background-color" } else { "text-color" };
    attrs.iter().find_map(|(key,value)| ((key == &format!("data-thinkly-{suffix}"))
        || key == &format!("data-goose-{suffix}")).then(|| color(value, background)).flatten())
}
pub(super) fn runs(block: &Block, text: &str, color: Hsla) -> Vec<TextRun> {
    let mut boundaries = vec![0, text.len()];
    for span in &block.spans { boundaries.extend([span.start,span.end]); }
    for tag in &block.meta.inline { boundaries.extend([tag.start,tag.end]); }
    boundaries.retain(|at| *at <= text.len() && text.is_char_boundary(*at));
    boundaries.sort_unstable(); boundaries.dedup();
    let mut result = Vec::new();
    for edge in boundaries.windows(2) {
        let (start,end) = (edge[0],edge[1]); let mut run = plain(end-start, color);
        let marks = block.spans.iter().find(|span| span.start <= start && end <= span.end)
            .map_or(&[][..], |span| span.marks.as_slice());
        if marks.contains(&MarkKind::Bold) { run.font.weight = FontWeight::SEMIBOLD; }
        if marks.contains(&MarkKind::Italic) { run.font.style = FontStyle::Italic; }
        if marks.contains(&MarkKind::Code) { run.font.family = crate::theme::mono_font_family().into(); run.font.fallbacks = None; run.font.weight = FontWeight::MEDIUM; run.color = super::inline_code::code_text_color(); }
        if marks.contains(&MarkKind::Strike) { run.strikethrough = Some(StrikethroughStyle { color:Some(color), thickness:px(1.) }); }
        for tag in block.meta.inline.iter().filter(|tag| tag.start <= start && end <= tag.end) {
            if tag.name == "a" {
                run.color = gpui::rgb(0x256abf).into();
            }
            if matches!(tag.name.as_str(), "u" | "ins") || css(&tag.attrs,"text-decoration").is_some_and(|value| value.contains("underline")) {
                run.underline = Some(UnderlineStyle { color:None, thickness:px(1.), wavy:false });
            }
            if css(&tag.attrs,"text-decoration").is_some_and(|value| value.contains("line-through")) {
                run.strikethrough = Some(StrikethroughStyle { color:Some(run.color), thickness:px(1.) });
            }
            if tag.name == "mark" && run.background_color.is_none() {
                run.background_color = super::text_runs::color("yellow", true);
            }
            if let Some(color) = attribute_color(&tag.attrs, false) { run.color = color; }
            if let Some(color) = attribute_color(&tag.attrs, true) { run.background_color = Some(color); }
            if matches!(css(&tag.attrs,"font-weight"), Some("bold"|"600"|"700"|"800"|"900")) { run.font.weight = FontWeight::SEMIBOLD; }
            if css(&tag.attrs,"font-style") == Some("italic") { run.font.style = FontStyle::Italic; }
        }
        result.push(run);
    }
    result
}
pub(super) fn plain(len: usize, color: Hsla) -> TextRun {
    let (family, fallbacks) = crate::theme::editor_font();
    TextRun { len, font:Font { family:family.into(), weight:FontWeight::NORMAL,
        style:FontStyle::Normal, features:FontFeatures::default(),
        fallbacks:(!fallbacks.is_empty()).then(|| FontFallbacks::from_fonts(fallbacks.iter().map(|s| (*s).to_string()).collect())) },
        color, background_color:None, underline:None, strikethrough:None }
}
pub(super) fn paint_tinted_lines(layout: &super::view::layout::TextLayout, color: Hsla, window: &mut gpui::Window) {
    for cell in &layout.cells { paint_tinted_lines(&cell.text, color, window); }
    let mask = window.content_mask().bounds;
    if !layout.bounds.intersects(&mask) { return; }
    let code_col = super::inline_code::code_text_color();
    let mut y = layout.bounds.top();
    for (line_start, line) in &layout.lines {
        let h = line.size(layout.line_height).height;
        if y + h > mask.top() && y < mask.bottom() {
            let raw = &line.unwrapped_layout;
            let baseline = (layout.line_height - raw.ascent - raw.descent) * 0.5 + raw.ascent;
            let mut wraps = line.wrap_boundaries.iter().peekable();
            let (mut visual, mut start_x, mut row_x, mut row_y) = (0, px(0.), layout.bounds.left() + layout.aligned_x(line, 0), y);
            for (run_ix, run) in raw.runs.iter().enumerate() {
                for (glyph_ix, g) in run.glyphs.iter().enumerate() {
                    if wraps.peek() == Some(&&gpui::WrapBoundary { run_ix, glyph_ix }) {
                        wraps.next(); visual += 1; start_x = g.position.x;
                        row_x = layout.bounds.left() + layout.aligned_x(line, visual);
                        row_y += layout.line_height;
                    }
                    if !g.is_emoji && row_y + layout.line_height > mask.top() && row_y < mask.bottom() {
                        let doc_off = super::inline_code::shaped_to_doc(*line_start + g.index, &layout.code_spans);
                        let in_code = layout.code_spans.iter().any(|s| s.start <= doc_off && doc_off < s.end);
                        let pos = gpui::point(row_x + g.position.x - start_x, row_y + baseline + g.position.y);
                        let _ = window.paint_glyph(pos, run.font_id, g.id, raw.font_size, if in_code { code_col } else { color });
                    }
                }
            }
        }
        y += h;
    }
}

pub(crate) fn styled_text(block: &Block) -> StyledText {
    let mut points = vec![0, block.text.len()];
    for span in &block.spans { points.extend([span.start, span.end]); }
    for tag in &block.meta.inline { points.extend([tag.start, tag.end]); }
    points.retain(|at| *at <= block.text.len() && block.text.is_char_boundary(*at));
    points.sort_unstable(); points.dedup();
    let (mut highlights, dark) = (Vec::new(), crate::theme::is_dark());
    for range in points.windows(2).map(|pair| pair[0]..pair[1]).filter(|r| !r.is_empty()) {
        let (mut style, mut changed) = (HighlightStyle::default(), false);
        for span in block.spans.iter().filter(|s| s.start <= range.start && range.end <= s.end) {
            if span.marks.contains(&MarkKind::Bold) { style.font_weight = Some(FontWeight(650.)); changed = true; }
            if span.marks.contains(&MarkKind::Italic) { style.font_style = Some(FontStyle::Italic); changed = true; }
            if span.marks.contains(&MarkKind::Strike) { style.strikethrough = Some(StrikethroughStyle { color: None, thickness: px(1.) }); changed = true; }
            if span.marks.contains(&MarkKind::Code) { style.background_color = Some(rgb(if dark { 0x2c2c2e } else { 0xf1efe9 }).into()); changed = true; }
        }
        for tag in block.meta.inline.iter().filter(|t| t.start <= range.start && range.end <= t.end) {
            if tag.name == "a" { style.color = Some(rgb(0x256abf).into()); changed = true; }
            if matches!(tag.name.as_str(), "u" | "ins") || css(&tag.attrs, "text-decoration").is_some_and(|v| v.contains("underline")) {
                style.underline = Some(UnderlineStyle { color: None, thickness: px(1.), wavy: false }); changed = true;
            }
            if css(&tag.attrs, "text-decoration").is_some_and(|v| v.contains("line-through")) {
                style.strikethrough = Some(StrikethroughStyle { color: style.color, thickness: px(1.) }); changed = true;
            }
            if tag.name == "mark" && style.background_color.is_none() { style.background_color = color("yellow", true); changed = true; }
            if let Some(fg) = attribute_color(&tag.attrs, false) { style.color = Some(fg); changed = true; }
            if let Some(bg) = attribute_color(&tag.attrs, true) { style.background_color = Some(bg); changed = true; }
            if matches!(css(&tag.attrs, "font-weight"), Some("bold" | "600" | "700" | "800" | "900")) { style.font_weight = Some(FontWeight(650.)); changed = true; }
            if css(&tag.attrs, "font-style") == Some("italic") { style.font_style = Some(FontStyle::Italic); changed = true; }
        }
        if changed { highlights.push((range, style)); }
    }
    StyledText::new(block.text.clone()).with_highlights(highlights)
}

