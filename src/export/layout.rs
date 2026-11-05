//! Single-column document layout parameterized by paper style margins and typography.
use cosmic_text::{
    Attrs, Buffer, CacheKeyFlags, Color, Family, FontSystem, Metrics, Shaping, Weight,
};

use super::palette::{ExportTheme, RenderOptions};
use super::style::{block_box_metrics, block_font_size, block_gap_after, line_factor};
use super::{Cancellation, ExportError, budget};
use crate::editor::model::{Block, Kind, MarkKind};

pub(super) const PAGE_WIDTH: f32 = 816.0;
pub(super) const SCALE: f32 = 2.0;
pub(super) const STRIKE: usize = 4;
pub(super) const CODE: usize = 8;

pub(super) struct Row {
    pub buffer: Buffer,
    pub backgrounds: Vec<Color>,
    pub frame: Option<super::table::Frame>,
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
    pub chrome: super::chrome::ChromeLayout,
    pub options: RenderOptions,
    pub margin_top: f32,
    pub margin_left: f32,
    pub content_width: f32,
    pub footer_y: f32,
    pub width: u32,
    pub height: u32,
}

pub(super) fn attributes(
    theme: ExportTheme,
    kind: Kind,
    marks: &[MarkKind],
    code_block: bool,
) -> Attrs<'static> {
    let flags = marks.iter().fold(0, |bits, mark| {
        bits | match mark {
            MarkKind::Bold => 1,
            MarkKind::Italic => 2,
            MarkKind::Strike => STRIKE,
            MarkKind::Code => CODE,
        }
    });
    let family = if code_block || flags & CODE != 0 {
        Family::Monospace
    } else if matches!(kind, Kind::H1 | Kind::H2 | Kind::H3) {
        family_of(theme.heading_family())
    } else {
        family_of(theme.body_family())
    };
    let weight = if flags & 1 != 0 || matches!(kind, Kind::H1 | Kind::H2 | Kind::H3) {
        Weight::BOLD
    } else {
        Weight::NORMAL
    };
    let italic = if flags & 2 != 0 {
        CacheKeyFlags::FAKE_ITALIC
    } else {
        CacheKeyFlags::empty()
    };
    Attrs::new()
        .family(family)
        .weight(weight)
        .cache_key_flags(italic)
        .metadata(flags)
}

pub(super) fn family_of(name: &'static str) -> Family<'static> {
    if name == super::spec::MONO { Family::Monospace } else { Family::Name(name) }
}

pub(super) fn require_glyphs(buffer: &Buffer) -> Result<(), ExportError> {
    let _ = buffer;
    Ok(())
}

pub(super) fn plain(
    fonts: &mut FontSystem,
    theme: ExportTheme,
    text: &str,
    size: f32,
    width: f32,
) -> Result<Buffer, ExportError> {
    let mut buffer = Buffer::new(fonts, Metrics::new(size, size * 1.4));
    buffer.set_size(Some(width), None);
    buffer.set_wrap(cosmic_text::Wrap::WordOrGlyph);
    let attrs = attributes(theme, Kind::Paragraph, &[], false);
    buffer.set_text(text, &attrs, Shaping::Advanced, None);
    buffer.shape_until_scroll(fonts, false);
    require_glyphs(&buffer)?;
    Ok(buffer)
}

pub(super) fn build(
    blocks: &[Block],
    fonts: &mut FontSystem,
    cancel: &Cancellation,
    options: RenderOptions,
) -> Result<Layout, ExportError> {
    budget::validate(blocks)?;
    let theme = options.theme;
    let (margin_top, margin_right, margin_bottom, margin_left) = theme.margins();
    let content_width = PAGE_WIDTH - margin_left - margin_right;
    let chrome = super::chrome::build_chrome(fonts, &options, margin_left, content_width);
    let mut rows = Vec::with_capacity(blocks.len());
    let mut y = margin_top + chrome.header_h;

    // The big title already shows the file name; drop a leading H1 that repeats it.
    let title = options.title.trim();
    let blocks = match blocks.first() {
        Some(first)
            if options.show_title
                && !title.is_empty()
                && first.kind == Kind::H1
                && first.text.split_whitespace().eq(title.split_whitespace()) =>
        {
            &blocks[1..]
        }
        _ => blocks,
    };
    let markers = build_markers(blocks, fonts, theme, content_width)?;
    let mut gutters = std::collections::BTreeMap::<usize, f32>::new();
    let default_g = 28.0;
    for (block, marker) in blocks.iter().zip(&markers) {
        if let Some(marker) = marker {
            let w = marker.layout_runs().map(|r| r.line_w).fold(0.0, f32::max);
            let gutter = gutters.entry(block.meta.indent).or_insert(default_g);
            *gutter = gutter.max(w + 12.0);
        }
    }

    for (block, marker) in blocks.iter().zip(markers) {
        cancel.check()?;
        if let Some(table) = &block.meta.table {
            y = super::table::append(
                table,
                &mut rows,
                y,
                margin_left,
                content_width,
                theme,
                fonts,
                cancel,
            )? + 10.0;
            continue;
        }
        let code = block.meta.code_language.is_some();
        let size = block_font_size(theme, block.kind, code);
        let line_height = size * line_factor(theme, block.kind, code);
        let indent = (block.meta.indent as f32 * 8.5).min((content_width - 80.0).max(0.0));
        let (gutter, pad_x, pad_top, pad_bottom) = block_box_metrics(
            theme,
            block.kind,
            code,
            marker.is_some(),
            block.meta.indent,
            &gutters,
        );
        let width = (content_width - indent - gutter - pad_x * 2.0).max(32.0);
        let label = block
            .meta
            .code_language
            .as_deref()
            .filter(|s| !s.is_empty())
            .map(|s| if matches!(s.to_ascii_lowercase().as_str(), "yaml-frontmatter" | "frontmatter") { "yaml" } else { s })
            .map(|s| plain(fonts, theme, s, 13.0, width))
            .transpose()?;
        let label_h = label
            .as_ref()
            .map(|b| {
                b.layout_runs()
                    .map(|r| r.line_top + r.line_height)
                    .fold(0.0, f32::max)
                    + 4.0
            })
            .unwrap_or(0.0);
        let shaped = super::rich::shape(
            block,
            fonts,
            size,
            line_height,
            width,
            None,
            attributes(theme, block.kind, &[], code),
        )?;
        let text_h = shaped
            .buffer
            .layout_runs()
            .map(|r| r.line_top + r.line_height)
            .fold(line_height, f32::max);
        let height = text_h + pad_top + pad_bottom + label_h;
        budget::canvas(PAGE_WIDTH * SCALE, (y + height + margin_bottom) * SCALE)?;
        rows.push(Row {
            buffer: shaped.buffer,
            backgrounds: shaped.backgrounds,
            frame: None,
            marker,
            label,
            kind: block.kind,
            checked: block.checked,
            code,
            x: margin_left + indent + gutter + pad_x,
            marker_x: margin_left + indent,
            y,
            text_y: y + pad_top + label_h,
            height,
            width,
            line_height,
        });
        y += height + block_gap_after(block.kind, code);
    }

    let footer_y = y + 28.0;
    let end = footer_y + chrome.footer_h + margin_bottom;
    let (width, height) = budget::canvas(PAGE_WIDTH * SCALE, end * SCALE)?;
    Ok(Layout {
        rows,
        chrome,
        options,
        margin_top,
        margin_left,
        content_width,
        footer_y,
        width,
        height,
    })
}

fn build_markers(
    blocks: &[Block],
    fonts: &mut FontSystem,
    theme: ExportTheme,
    content_width: f32,
) -> Result<Vec<Option<Buffer>>, ExportError> {
    let numbers = crate::editor::model::list_numbers(blocks.iter());
    let size = theme.spec().body;
    blocks
        .iter()
        .zip(numbers)
        .map(|(_, ord)| {
            ord.map(|v| plain(fonts, theme, &format!("{v}."), size, content_width))
                .transpose()
        })
        .collect()
}
