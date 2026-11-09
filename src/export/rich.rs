//! Shape semantic spans once, sharing exactly the same buffers with the raster pass.
use super::{
    ExportError,
    layout::{CODE, STRIKE, require_glyphs},
    style,
};
use crate::editor::model::{Block, MarkKind};
use cosmic_text::{
    Align, Attrs, Buffer, CacheKeyFlags, Color, Family, FontSystem, Metrics, Shaping, Weight, Wrap,
};

pub(super) const UNDERLINE: usize = 16;
pub(super) const LINK: usize = 32;
pub(super) const HIGHLIGHT: usize = 64;
pub(super) const BACKGROUND_SHIFT: usize = 8;
pub(super) struct Shaped {
    pub buffer: Buffer,
    pub backgrounds: Vec<Color>,
}

pub(super) fn shape(
    block: &Block,
    fonts: &mut FontSystem,
    size: f32,
    line: f32,
    width: f32,
    alignment: Option<Align>,
    base: Attrs<'_>,
) -> Result<Shaped, ExportError> {
    shape_with_wrap(block, fonts, size, line, width, alignment, base, Wrap::WordOrGlyph)
}

pub(super) fn shape_with_wrap(
    block: &Block,
    fonts: &mut FontSystem,
    size: f32,
    line: f32,
    width: f32,
    alignment: Option<Align>,
    base: Attrs<'_>,
    wrap: Wrap,
) -> Result<Shaped, ExportError> {
    let mut boundaries = vec![0, block.text.len()];
    for span in &block.spans {
        boundaries.extend([span.start, span.end]);
    }
    for tag in &block.meta.inline {
        boundaries.extend([tag.start, tag.end]);
    }
    boundaries.sort_unstable();
    boundaries.dedup();
    let mut spans = Vec::new();
    let mut backgrounds = Vec::new();
    for pair in boundaries.windows(2) {
        let (start, end) = (pair[0], pair[1]);
        let mut attrs = base.clone();
        let mut flags = attrs.metadata;
        let marks = block
            .spans
            .iter()
            .find(|s| s.start <= start && end <= s.end)
            .map_or(&[][..], |s| s.marks.as_slice());
        if marks.contains(&MarkKind::Bold) {
            attrs = attrs.weight(Weight::BOLD);
        }
        if marks.contains(&MarkKind::Italic) {
            attrs = attrs.cache_key_flags(CacheKeyFlags::FAKE_ITALIC);
        }
        if marks.contains(&MarkKind::Strike) {
            flags |= STRIKE;
        }
        let mut background = None;
        if marks.contains(&MarkKind::Code) {
            flags |= CODE;
            // Monospace fallback has no CJK glyphs; keep the body face for non-ASCII code.
            if block.text[start..end].is_ascii() {
                attrs = attrs.family(Family::Monospace);
            }
        }
        for tag in block
            .meta
            .inline
            .iter()
            .filter(|tag| tag.start <= start && end <= tag.end)
        {
            if tag.name == "a" {
                flags |= UNDERLINE | LINK;
            }
            if matches!(tag.name.as_str(), "u" | "ins")
                || style::css(&tag.attrs, "text-decoration")
                    .is_some_and(|value| value.contains("underline"))
            {
                flags |= UNDERLINE;
            }
            if style::css(&tag.attrs, "text-decoration").is_some_and(|v| v.contains("line-through")) {
                flags |= STRIKE;
            }
            if tag.name == "mark" {
                flags |= HIGHLIGHT;
            }
            if let Some(color) = style::attribute_color(&tag.attrs, false) {
                attrs = attrs.color(color);
            }
            if let Some(color) = style::attribute_color(&tag.attrs, true) {
                background = Some(color);
            }
            if style::bold(&tag.attrs) {
                attrs = attrs.weight(Weight::BOLD);
            }
            if style::italic(&tag.attrs) {
                attrs = attrs.cache_key_flags(CacheKeyFlags::FAKE_ITALIC);
            }
        }
        if let Some(color) = background {
            let index = backgrounds
                .iter()
                .position(|value| *value == color)
                .unwrap_or_else(|| {
                    backgrounds.push(color);
                    backgrounds.len() - 1
                });
            flags |= (index + 1) << BACKGROUND_SHIFT;
        }
        spans.push((&block.text[start..end], attrs.metadata(flags)));
    }
    if spans.is_empty() {
        spans.push(("", base.clone()));
    }
    let mut buffer = Buffer::new(fonts, Metrics::new(size, line));
    buffer.set_size(Some(width), None);
    buffer.set_wrap(wrap);
    buffer.set_rich_text(spans, &base, Shaping::Advanced, alignment);
    buffer.shape_until_scroll(fonts, false);
    require_glyphs(&buffer)?;
    Ok(Shaped {
        buffer,
        backgrounds,
    })
}
