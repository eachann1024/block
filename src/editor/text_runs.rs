//! Shared rich-text runs for ordinary blocks and table cell shaping.
use gpui::{Font, FontFallbacks, FontFeatures, FontStyle, FontWeight, Hsla, Rgba,
    StrikethroughStyle, TextRun, UnderlineStyle, px};
use super::model::{Block, MarkKind};
use crate::theme::Theme;

pub(super) fn css<'a>(attrs: &'a [(String, String)], name: &str) -> Option<&'a str> {
    attrs.iter().find(|(key,_)| key == "style").and_then(|(_,value)|
        value.split(';').find_map(|part| {
            let (key,value) = part.split_once(':')?;
            (key.trim().eq_ignore_ascii_case(name)).then_some(value.trim())
        }))
}
pub(super) fn color(value: &str, background: bool) -> Option<Hsla> {
    let named = match (value, background) {
        ("default",_) => return None,
        ("gray",false) => "#787774", ("brown",false) => "#9f6b53", ("red",false) => "#d44c47",
        ("orange",false) => "#d9730d", ("yellow",false) => "#cb912f", ("green",false) => "#448361",
        ("blue",false) => "#337ea9", ("purple",false) => "#9065b0", ("pink",false) => "#c14c8a",
        ("gray",true) => "#ebeced", ("brown",true) => "#e9e5e3", ("red",true) => "#fbe4e4",
        ("orange",true) => "#f6e9d9", ("yellow",true) => "#fbf3db", ("green",true) => "#ddedea",
        ("blue",true) => "#ddebf1", ("purple",true) => "#eae4f2", ("pink",true) => "#f4dfeb",
        ("black",_) => "#000000", ("white",_) => "#ffffff", _ => value,
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
pub(super) fn attribute_color(attrs: &[(String,String)], background: bool) -> Option<Hsla> {
    let property = if background { "background-color" } else { "color" };
    // The serialized CSS contains the resolved color and has precedence over named data attributes.
    if let Some(value) = css(attrs, property).and_then(|value| color(value, background)) { return Some(value); }
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
        if marks.contains(&MarkKind::Bold) { run.font.weight = FontWeight::BOLD; }
        if marks.contains(&MarkKind::Italic) { run.font.style = FontStyle::Italic; }
        if marks.contains(&MarkKind::Code) { run.font.family = "Menlo".into(); run.background_color = Some(Theme::ocean().code_bg); }
        if marks.contains(&MarkKind::Strike) { run.strikethrough = Some(StrikethroughStyle { color:Some(color), thickness:px(1.) }); }
        for tag in block.meta.inline.iter().filter(|tag| tag.start <= start && end <= tag.end) {
            if tag.name == "a" {
                run.color = gpui::rgb(0x3d6fd6).into();
                run.underline = Some(UnderlineStyle { color:Some(gpui::rgb(0x9dbcf5).into()), thickness:px(1.), wavy:false });
            }
            if tag.name == "u" || css(&tag.attrs,"text-decoration").is_some_and(|value| value.contains("underline")) {
                run.underline = Some(UnderlineStyle { color:None, thickness:px(1.), wavy:false });
            }
            if let Some(color) = attribute_color(&tag.attrs, false) { run.color = color; }
            if let Some(color) = attribute_color(&tag.attrs, true) { run.background_color = Some(color); }
            if matches!(css(&tag.attrs,"font-weight"), Some("bold"|"600"|"700"|"800"|"900")) { run.font.weight = FontWeight::BOLD; }
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
        fallbacks:Some(FontFallbacks::from_fonts(fallbacks.iter().map(|s| (*s).to_string()).collect())) },
        color, background_color:None, underline:None, strikethrough:None }
}
pub(super) fn tint_selection(text: &str, runs: Vec<TextRun>, sel: Option<std::ops::Range<usize>>, color: Hsla) -> Vec<TextRun> {
    let Some(sel) = sel else { return runs };
    let (mut s, mut e) = (sel.start.min(text.len()), sel.end.min(text.len()));
    while s > 0 && !text.is_char_boundary(s) { s -= 1; }
    while e < text.len() && !text.is_char_boundary(e) { e += 1; }
    if s >= e { return runs; }
    let mut out = Vec::with_capacity(runs.len() + 2);
    let mut at = 0;
    for run in runs {
        let end = at + run.len;
        if end <= s || at >= e { out.push(run); }
        else {
            if at < s { let mut r = run.clone(); r.len = s - at; out.push(r); }
            let a = at.max(s); let b = end.min(e);
            if a < b {
                let mut r = run.clone(); r.len = b - a; r.color = color;
                if let Some(st) = &mut r.strikethrough { st.color = Some(color); }
                out.push(r);
            }
            if end > e { let mut r = run; r.len = end - e; out.push(r); }
        }
        at = end;
    }
    out
}
