use cosmic_text::{
    Attrs, Buffer, CacheKeyFlags, Family, FontSystem, Metrics, Shaping, Weight, Wrap,
};

use super::{Cancellation, ExportError, budget};
use crate::editor::model::{Block, Kind, MarkKind};

pub(super) const CONTENT: f32 = 720.0;
pub(super) const MARGIN: f32 = 48.0;
pub(super) const SCALE: f32 = 2.0;
pub(super) const STRIKE: usize = 4;
pub(super) const CODE: usize = 8;

pub(super) struct Row {
    pub buffer: Buffer,
    pub marker: Option<Buffer>,
    pub label: Option<Buffer>,
    pub kind: Kind,
    pub checked: bool,
    pub code: bool,
    pub x: f32,
    pub marker_x: f32,
    pub y: f32,
    pub text_y: f32,
    pub height: f32,
    pub width: f32,
    pub line_height: f32,
}

pub(super) struct Layout {
    pub rows: Vec<Row>,
    pub width: u32,
    pub height: u32,
}

fn attributes(kind: Kind, marks: &[MarkKind], code_block: bool) -> Attrs<'static> {
    let flags = marks.iter().fold(0, |bits, mark| {
        bits | match mark {
            MarkKind::Bold => 1,
            MarkKind::Italic => 2,
            MarkKind::Strike => STRIKE,
            MarkKind::Code => CODE,
        }
    });
    Attrs::new()
        .family(if code_block || flags & CODE != 0 {
            Family::Monospace
        } else {
            Family::Name("PingFang SC")
        })
        .weight(
            if flags & 1 != 0 || matches!(kind, Kind::H1 | Kind::H2 | Kind::H3) {
                Weight::BOLD
            } else {
                Weight::NORMAL
            },
        )
        // Keep the covering upright face: cosmic-text 0.14's italic fallback
        // can select SF Italic for CJK, yielding .notdef instead of PingFang.
        // Swash applies the oblique transform only to these marked ranges.
        .cache_key_flags(if flags & 2 != 0 {
            CacheKeyFlags::FAKE_ITALIC
        } else {
            CacheKeyFlags::empty()
        })
        .metadata(flags)
}

fn require_glyphs(buffer: &Buffer) -> Result<(), ExportError> {
    if buffer
        .layout_runs()
        .any(|run| run.glyphs.iter().any(|glyph| glyph.glyph_id == 0))
    {
        return Err(ExportError::Rendering(
            "installed fonts do not cover all characters; no image was exported",
        ));
    }
    Ok(())
}

fn plain(fonts: &mut FontSystem, text: &str, size: f32, width: f32) -> Result<Buffer, ExportError> {
    let mut buffer = Buffer::new(fonts, Metrics::new(size, size * 1.5));
    buffer.set_size(fonts, Some(width), None);
    buffer.set_text(
        fonts,
        text,
        &attributes(Kind::Paragraph, &[], false),
        Shaping::Advanced,
    );
    buffer.shape_until_scroll(fonts, false);
    require_glyphs(&buffer)?;
    Ok(buffer)
}

pub(super) fn build(
    blocks: &[Block],
    fonts: &mut FontSystem,
    cancel: &Cancellation,
) -> Result<Layout, ExportError> {
    budget::validate(blocks)?;
    let mut rows = Vec::with_capacity(blocks.len());
    let mut y = MARGIN;
    let numbers = crate::editor::model::list_numbers(blocks.iter());
    let markers: Vec<_> = numbers
        .into_iter()
        .map(|number| {
            number
                .map(|n| plain(fonts, &format!("{n}."), 17.0, CONTENT))
                .transpose()
        })
        .collect::<Result<_, _>>()?;
    let mut gutters = std::collections::BTreeMap::<usize, f32>::new();
    for (block, marker) in blocks.iter().zip(&markers) {
        if let Some(marker) = marker {
            let width = marker.layout_runs().map(|r| r.line_w).fold(0.0, f32::max);
            let gutter = gutters.entry(block.meta.indent).or_insert(28.0);
            *gutter = gutter.max(width + 12.0);
        }
    }
    for (block, marker) in blocks.iter().zip(markers) {
        cancel.check()?;
        let size = match block.kind {
            Kind::H1 => 28.0,
            Kind::H2 => 22.0,
            Kind::H3 => 19.0,
            _ => 17.0,
        };
        let line_height = size * 1.5;
        let code = block.meta.code_language.is_some();
        let indent = block.meta.indent as f32 * 8.5;
        if indent > CONTENT - 80.0 {
            return Err(ExportError::ResourceLimit(
                "indent leaves insufficient text width",
            ));
        }
        let gutter = marker
            .as_ref()
            .map(|_| gutters[&block.meta.indent])
            .unwrap_or(match block.kind {
                Kind::Bullet | Kind::Todo => 28.0,
                Kind::Quote => 16.0,
                _ => 0.0,
            });
        let padding = if code { 8.0 } else { 0.0 };
        let width = CONTENT - indent - gutter - padding * 2.0;
        if width < 32.0 {
            return Err(ExportError::ResourceLimit(
                "list marker leaves insufficient text width",
            ));
        }
        let label = block
            .meta
            .code_language
            .as_deref()
            .filter(|s| !s.is_empty())
            .map(|s| plain(fonts, s, 11.0, width))
            .transpose()?;
        let label_height = label
            .as_ref()
            .map(|b| {
                b.layout_runs()
                    .map(|r| r.line_top + r.line_height)
                    .fold(0.0, f32::max)
                    + 4.0
            })
            .unwrap_or(0.0);
        let mut buffer = Buffer::new(fonts, Metrics::new(size, line_height));
        buffer.set_size(fonts, Some(width), None);
        buffer.set_wrap(fonts, Wrap::WordOrGlyph);
        let mut spans = Vec::with_capacity(block.spans.len() * 2 + 1);
        let mut cursor = 0;
        for span in &block.spans {
            if cursor < span.start {
                spans.push((
                    &block.text[cursor..span.start],
                    attributes(block.kind, &[], code),
                ));
            }
            spans.push((
                &block.text[span.start..span.end],
                attributes(block.kind, &span.marks, code),
            ));
            cursor = span.end;
        }
        if cursor < block.text.len() {
            spans.push((&block.text[cursor..], attributes(block.kind, &[], code)));
        }
        if spans.is_empty() {
            spans.push(("", attributes(block.kind, &[], code)));
        }
        buffer.set_rich_text(
            fonts,
            spans,
            &attributes(block.kind, &[], code),
            Shaping::Advanced,
            None,
        );
        buffer.shape_until_scroll(fonts, false);
        require_glyphs(&buffer)?;
        let text_height = buffer
            .layout_runs()
            .map(|run| run.line_top + run.line_height)
            .fold(line_height, f32::max);
        for run in buffer.layout_runs() {
            if run.line_w > width + 0.5 {
                return Err(ExportError::Rendering(
                    "a glyph cannot fit within the available width",
                ));
            }
        }
        let height = text_height + padding * 2.0 + label_height;
        // Actual layout, next block origin and canvas allocation share the same height.
        budget::canvas(
            (CONTENT + MARGIN * 2.0) * SCALE,
            (y + height + MARGIN) * SCALE,
        )?;
        rows.push(Row {
            buffer,
            marker,
            label,
            kind: block.kind,
            checked: block.checked,
            code,
            x: MARGIN + indent + gutter + padding,
            marker_x: MARGIN + indent,
            y,
            text_y: y + padding + label_height,
            height,
            width,
            line_height,
        });
        y += height
            + if matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3) {
                12.0
            } else {
                6.0
            };
    }
    let (width, height) = budget::canvas((CONTENT + MARGIN * 2.0) * SCALE, (y + MARGIN) * SCALE)?;
    Ok(Layout {
        rows,
        width,
        height,
    })
}
