//! Shared semantic projection for editable exports; geometry comes from the paper renderer.
use super::palette::Palette;
use super::{ExportError, RenderOptions, style};
use crate::editor::model::{Block, Kind, MarkKind};

pub(super) fn blocks<'a>(blocks: &'a [Block], options: &RenderOptions) -> &'a [Block] {
    match blocks.first() {
        Some(first)
            if options.show_title
                && !options.title.trim().is_empty()
                && first.kind == Kind::H1
                && first
                    .text
                    .split_whitespace()
                    .eq(options.title.split_whitespace()) =>
        {
            &blocks[1..]
        }
        _ => blocks,
    }
}

pub(super) fn title(options: &RenderOptions) -> &str {
    let title = options.title.trim();
    if title.is_empty() {
        crate::theme::tr("未命名", "Untitled")
    } else {
        title
    }
}

pub(super) fn safe_href(href: &str) -> bool {
    let compact: String = href
        .chars()
        .filter(|c| !c.is_control() && !c.is_whitespace())
        .collect();
    !["javascript:", "data:", "vbscript:"]
        .iter()
        .any(|s| compact.to_ascii_lowercase().starts_with(s))
}

pub(super) struct Segment<'a> {
    pub text: &'a str,
    pub bold: bool,
    pub italic: bool,
    pub strike: bool,
    pub underline: bool,
    pub code: bool,
    pub color: Option<cosmic_text::Color>,
    pub background: Option<cosmic_text::Color>,
    pub script: Option<&'static str>,
    pub href: Option<&'a str>,
}

pub(super) fn segments<'a>(block: &'a Block, pal: &Palette) -> Vec<Segment<'a>> {
    let mut cuts = vec![0, block.text.len()];
    for span in &block.spans {
        cuts.extend([span.start, span.end]);
    }
    for tag in &block.meta.inline {
        cuts.extend([tag.start, tag.end]);
    }
    cuts.sort_unstable();
    cuts.dedup();
    cuts.windows(2)
        .filter_map(|pair| {
            let (start, end) = (pair[0], pair[1]);
            let text = block.text.get(start..end)?;
            let marks = block
                .spans
                .iter()
                .find(|s| s.start <= start && end <= s.end)
                .map_or(&[][..], |s| s.marks.as_slice());
            let code = marks.contains(&MarkKind::Code);
            let mut s = Segment {
                text,
                bold: marks.contains(&MarkKind::Bold),
                italic: marks.contains(&MarkKind::Italic),
                strike: marks.contains(&MarkKind::Strike),
                underline: false,
                code,
                color: None,
                background: code.then_some(pal.code_bg),
                script: None,
                href: None,
            };
            for tag in block
                .meta
                .inline
                .iter()
                .filter(|t| t.start <= start && end <= t.end)
            {
                match tag.name.as_str() {
                    "a" => {
                        s.color = Some(pal.link_fg());
                        s.underline = true;
                        s.href = tag
                            .attrs
                            .iter()
                            .find(|(k, _)| k == "href")
                            .map(|(_, v)| v.as_str())
                            .filter(|h| safe_href(h));
                    }
                    "u" | "ins" => s.underline = true,
                    "mark" if s.background.is_none() => s.background = Some(pal.highlight_bg()),
                    "sup" => s.script = Some("superscript"),
                    "sub" => s.script = Some("subscript"),
                    _ => {}
                }
                if let Some(v) = style::css(&tag.attrs, "text-decoration") {
                    s.underline |= v.contains("underline");
                    s.strike |= v.contains("line-through");
                }
                if let Some(c) = style::attribute_color(&tag.attrs, false) {
                    s.color = Some(pal.fg(c));
                }
                if let Some(c) = style::attribute_color(&tag.attrs, true) {
                    s.background = Some(pal.bg(c));
                }
                s.bold |= style::bold(&tag.attrs);
                s.italic |= style::italic(&tag.attrs);
            }
            Some(s)
        })
        .collect()
}

pub(super) fn indent(block: &Block, width: f32) -> f32 {
    (block.meta.indent as f32 * 8.5).min((width - 80.).max(0.))
}

/// Same natural grid and scale in PNG, Word and HTML, including manually resized columns.
pub(super) fn table_columns(
    table: &crate::editor::model::Table,
    source: &super::SourceContext,
    id: u64,
    width: f32,
) -> Result<(Vec<f32>, f32), ExportError> {
    let geometry = source.block(id).and_then(|b| b.table.as_ref());
    let columns = geometry
        .filter(|g| {
            g.columns.len() == table.columns && g.columns.iter().all(|w| w.is_finite() && *w > 0.)
        })
        .map(|g| g.columns.clone());
    let columns = match columns {
        Some(columns) => columns,
        None => super::table::columns(table, width)?
            .windows(2)
            .map(|x| x[1] - x[0])
            .collect(),
    };
    let scale = (width / columns.iter().sum::<f32>().max(1.)).min(1.);
    Ok((columns.into_iter().map(|w| w * scale).collect(), scale))
}

pub(super) fn base64(bytes: &[u8]) -> String {
    const DIGITS: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut out = String::with_capacity(bytes.len().div_ceil(3) * 4);
    for chunk in bytes.chunks(3) {
        let value = ((chunk[0] as u32) << 16)
            | ((chunk.get(1).copied().unwrap_or(0) as u32) << 8)
            | chunk.get(2).copied().unwrap_or(0) as u32;
        for shift in [18, 12] {
            out.push(DIGITS[((value >> shift) & 63) as usize] as char);
        }
        out.push(if chunk.len() > 1 {
            DIGITS[((value >> 6) & 63) as usize] as char
        } else {
            '='
        });
        out.push(if chunk.len() > 2 {
            DIGITS[(value & 63) as usize] as char
        } else {
            '='
        });
    }
    out
}

/// Resolve the generic code font once against the exact database used by the preview.
pub(super) fn font_family(name: &str) -> String {
    if name != "monospace" {
        return name.to_owned();
    }
    let Ok(mut guard) = super::FONTS.lock() else {
        return "monospace".into();
    };
    guard
        .get_or_insert_with(super::new_font_system)
        .db()
        .family_name(&cosmic_text::Family::Monospace)
        .to_owned()
}

pub(super) fn list_gutters(
    blocks: &[Block],
    options: &RenderOptions,
) -> Result<std::collections::BTreeMap<usize, f32>, ExportError> {
    let mut guard = super::FONTS.lock().map_err(|_| ExportError::Busy)?;
    let fonts = guard.get_or_insert_with(super::new_font_system);
    let mut gutters = std::collections::BTreeMap::new();
    let numbers = crate::editor::model::list_numbers(blocks.iter());
    for (block, number) in blocks.iter().zip(numbers) {
        if let Some(number) = number {
            let buffer = super::layout::plain(
                fonts,
                options.theme,
                &format!("{number}."),
                options.theme.spec().body,
                super::layout::PAGE_WIDTH,
            )?;
            let width = buffer.layout_runs().map(|r| r.line_w).fold(0., f32::max);
            let gutter = gutters.entry(block.meta.indent).or_insert(28_f32);
            *gutter = gutter.max(width + 12.);
        }
    }
    Ok(gutters)
}

/// Source-mode code can opt out of wrapping; shrink it by the same measured width as PNG.
pub(super) fn block_metrics(
    block: &Block,
    options: &RenderOptions,
    available: f32,
) -> Result<(f32, f32), ExportError> {
    let code = block.meta.code_language.is_some();
    let mut size = style::block_font_size(options.theme, block, code);
    let mut line = size * style::line_factor(options.theme, block.kind, code);
    if code && options.source.block(block.id).is_some_and(|b| b.nowrap) {
        use cosmic_text::{Buffer, Metrics, Shaping, Wrap};
        let mut guard = super::FONTS.lock().map_err(|_| ExportError::Busy)?;
        let fonts = guard.get_or_insert_with(super::new_font_system);
        let width = (available - 36.).max(32.);
        let mut buffer = Buffer::new(fonts, Metrics::new(size, line));
        buffer.set_size(Some(width), None);
        buffer.set_wrap(Wrap::None);
        buffer.set_text(
            &block.text,
            &super::layout::attributes(options.theme, block.kind, &[], true),
            Shaping::Advanced,
            None,
        );
        buffer.shape_until_scroll(fonts, false);
        let natural = buffer.layout_runs().map(|r| r.line_w).fold(0., f32::max);
        if natural > width {
            size *= width / natural;
            line *= width / natural;
        }
    }
    Ok((size, line))
}
