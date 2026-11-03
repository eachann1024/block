use super::InlineTag;
use std::ops::Range;

/// Semantic HTML ranges use UTF-8 byte offsets, just like ordinary mark spans.
/// Attributes are retained even when the native renderer only understands a subset.
pub(super) fn slice(tags: &[InlineTag], range: Range<usize>) -> Vec<InlineTag> {
    tags.iter().filter(|tag| tag.start < range.end && tag.end > range.start).map(|tag| {
        let mut tag = tag.clone();
        tag.start = tag.start.max(range.start) - range.start;
        tag.end = tag.end.min(range.end) - range.start;
        tag
    }).collect()
}

pub(super) fn replace(tags: &mut Vec<InlineTag>, range: Range<usize>, inserted: usize) {
    tags.retain_mut(|tag| {
        let inherit = if range.is_empty() && range.start > 0 {
            tag.start < range.start && range.start <= tag.end
        } else { tag.start <= range.start && range.start < tag.end };
        let before = tag.start < range.start;
        let after = tag.end > range.end;
        let start = if before { tag.start } else if inherit { range.start }
            else { tag.start.max(range.end) - range.end + range.start + inserted };
        let end = if after { tag.end - range.end + range.start + inserted }
            else if inherit { range.start + inserted } else { tag.end.min(range.start) };
        tag.start = start; tag.end = end;
        start < end && (before || after || inherit)
    });
}

impl InlineTag {
    pub fn attr(&self, name: &str) -> Option<&str> {
        self.attrs.iter().find(|(key, _)| key == name).map(|(_, value)| value.as_str())
    }
}

// Inline formatting that thinkly stores as HTML: `<u>`, `<span style="color:..;background-color:..">`
// and links. Each helper edits tags for one byte range and splits tags that cross its edges.
type Tags = Vec<InlineTag>;
fn tag(start: usize, end: usize, name: &str, attrs: Vec<(String, String)>) -> InlineTag {
    InlineTag { start, end, name: name.into(), attrs }
}
fn has_style(tag: &InlineTag, prop: &str) -> bool {
    tag.name == "span" && crate::editor::text_runs::css(&tag.attrs, prop).is_some()
}
fn covered(tags: &[InlineTag], range: &Range<usize>, pred: &dyn Fn(&InlineTag) -> bool) -> bool {
    let mut hits: Vec<_> = tags.iter().filter(|t| pred(t)).collect();
    hits.sort_by_key(|t| t.start);
    let mut at = range.start;
    for t in hits { if t.start <= at { at = at.max(t.end); } }
    at >= range.end
}
/// Split matching tags at the range edges; the inside part is rewritten by `inner` or dropped.
fn carve(tags: &mut Tags, range: &Range<usize>, pred: &dyn Fn(&InlineTag) -> bool,
    inner: &dyn Fn(&InlineTag) -> Option<Vec<(String, String)>>) {
    let mut out = Vec::with_capacity(tags.len() + 2);
    for t in std::mem::take(tags) {
        if !(pred(&t) && t.start < range.end && t.end > range.start) { out.push(t); continue; }
        if t.start < range.start { out.push(InlineTag { end: range.start, ..t.clone() }); }
        if t.end > range.end { out.push(InlineTag { start: range.end, ..t.clone() }); }
        if let Some(attrs) = inner(&t) { out.push(tag(t.start.max(range.start), t.end.min(range.end), &t.name, attrs)); }
    }
    *tags = out;
}
fn tidy(tags: &mut Tags) {
    tags.sort_by_key(|t| (t.start, std::cmp::Reverse(t.end)));
    let mut out: Tags = Vec::new();
    for t in std::mem::take(tags) {
        if let Some(p) = out.iter_mut().rev().find(|p| p.end >= t.start && p.name == t.name && p.attrs == t.attrs) {
            p.end = p.end.max(t.end);
        } else { out.push(t); }
    }
    *tags = out;
}
fn without_style(attrs: &[(String, String)], prop: &str) -> Vec<(String, String)> {
    attrs.iter().filter_map(|(key, value)| {
        if key != "style" { return Some((key.clone(), value.clone())); }
        let kept: Vec<_> = value.split(';').map(str::trim).filter(|part| !part.is_empty()
            && part.split_once(':').is_none_or(|(k, _)| !k.trim().eq_ignore_ascii_case(prop))).collect();
        (!kept.is_empty()).then(|| (key.clone(), kept.join("; ")))
    }).collect()
}
fn keep_nonempty(attrs: Vec<(String, String)>) -> Option<Vec<(String, String)>> { (!attrs.is_empty()).then_some(attrs) }

pub(super) fn is_underlined(tags: &[InlineTag], range: &Range<usize>) -> bool { covered(tags, range, &|t| t.name == "u") }
pub(super) fn toggle_underline(tags: &mut Tags, range: Range<usize>) {
    let on = !is_underlined(tags, &range);
    carve(tags, &range, &|t| t.name == "u", &|_| None);
    if on { tags.push(tag(range.start, range.end, "u", Vec::new())); }
    tidy(tags);
}
/// `prop` is `color` or `background-color`; `None` removes it. Values are thinkly colour names.
pub(super) fn set_style(tags: &mut Tags, range: Range<usize>, prop: &str, value: Option<&str>) {
    carve(tags, &range, &|t| has_style(t, prop), &|t| keep_nonempty(without_style(&t.attrs, prop)));
    if let Some(value) = value { tags.push(tag(range.start, range.end, "span", vec![("style".into(), format!("{prop}:{value}"))])); }
    tidy(tags);
}
pub(super) fn set_link(tags: &mut Tags, range: Range<usize>, href: Option<&str>) {
    carve(tags, &range, &|t| t.name == "a", &|_| None);
    if let Some(href) = href { tags.push(tag(range.start, range.end, "a", vec![("href".into(), href.into())])); }
    tidy(tags);
}
pub(super) fn link_at(tags: &[InlineTag], offset: usize) -> Option<(Range<usize>, String)> {
    tags.iter().find(|t| t.name == "a" && t.start <= offset && offset < t.end)
        .map(|t| (t.start..t.end, t.attr("href").unwrap_or_default().to_owned()))
}
/// Drops underline and colour wrappers. Links are content, so they stay.
pub(super) fn clear_format(tags: &mut Tags, range: Range<usize>) {
    let pred = |t: &InlineTag| t.name == "u" || (t.name == "span" && t.attrs.iter().any(|(k, _)| k == "style"));
    carve(tags, &range, &pred, &|t| if t.name == "u" { None } else { keep_nonempty(t.attrs.iter().filter(|(k, _)| k != "style").cloned().collect()) });
    tidy(tags);
}
