use std::fmt;

use crate::editor::model::Block;

// Bound input before cloning/shaping; bound the actual canvas again before allocation.
// Pixel storage is <=64 MiB. Encoding may temporarily require another full image.
pub const MAX_BLOCKS: usize = 256;
pub const MAX_TEXT_BYTES: usize = 128 * 1024;
pub const MAX_SPANS: usize = 8192;
pub const MAX_PIXELS: u64 = 16 * 1024 * 1024;

#[derive(Debug)]
pub enum ExportError {
    Cancelled,
    Dialog(String),
    Empty,
    Busy,
    ResourceLimit(&'static str),
    InvalidInput(&'static str),
    UnsupportedTable(&'static str),
    Rendering(&'static str),
    Encoding(String),
    Write(std::io::Error),
}

impl fmt::Display for ExportError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        use crate::theme::tr;
        match self {
            Self::Cancelled => write!(f, "{}", tr("已取消生成", "Export canceled")),
            Self::Dialog(error) => write!(f, "{}: {error}", tr("保存对话框出错", "Save dialog error")),
            Self::Empty => write!(f, "{}", tr("没有可生成的内容", "Nothing to export")),
            Self::Busy => write!(f, "{}", tr("已有生成任务在进行，请稍后再试", "An export is already in progress")),
            Self::ResourceLimit(reason) => write!(
                f,
                "{} ({reason})",
                tr("内容超出图片限制，请缩小范围", "Content exceeds image limit; narrow the scope")
            ),
            Self::InvalidInput(reason) => write!(f, "{}: {reason}", tr("内容格式无效", "Invalid content format")),
            Self::UnsupportedTable(reason) => write!(
                f,
                "{} ({reason})",
                tr("表格无法完整生成图片；仅支持基础结构化的 Markdown / HTML 表格", "Table cannot be exported; only basic Markdown/HTML tables are supported")
            ),
            Self::Rendering(reason) => write!(f, "{}: {reason}", tr("图片渲染失败", "Image rendering failed")),
            Self::Encoding(error) => write!(f, "{}: {error}", tr("PNG 编码失败", "PNG encoding failed")),
            Self::Write(error) => write!(f, "{}: {error}", tr("无法保存图片", "Failed to save image")),
        }
    }
}
impl std::error::Error for ExportError {}

pub fn preflight(count: usize, text_bytes: usize, spans: usize) -> Result<(), ExportError> {
    if count == 0 {
        return Err(ExportError::Empty);
    }
    if count > MAX_BLOCKS {
        return Err(ExportError::ResourceLimit("最多 256 个块"));
    }
    if text_bytes > MAX_TEXT_BYTES {
        return Err(ExportError::ResourceLimit("最多 128 KiB 文本"));
    }
    if spans > MAX_SPANS {
        return Err(ExportError::ResourceLimit("最多 8192 个格式范围"));
    }
    Ok(())
}

pub(super) fn validate(blocks: &[Block]) -> Result<(), ExportError> {
    validate_snapshot(blocks.len(), blocks.iter().map(|b| (b, 0..b.text.len())))
}

/// Account for everything retained by a snapshot before cloning, including original HTML.
/// Ordinary blocks are sliced; table slices retain source, originals and empty grid cells.
pub(crate) fn validate_snapshot<'a>(
    count: usize,
    blocks: impl Iterator<Item = (&'a Block, std::ops::Range<usize>)>,
) -> Result<(), ExportError> {
    preflight(count, 0, 0)?;
    let mut tally = Tally {
        count,
        bytes: 0,
        spans: 0,
        cells: 0,
    };
    for (block, range) in blocks {
        if range.start > range.end
            || !block.text.is_char_boundary(range.start)
            || !block.text.is_char_boundary(range.end)
        {
            return Err(ExportError::InvalidInput("选区必须位于 UTF-8 字符边界"));
        }
        tally.block(block, Some(range))?;
    }
    Ok(())
}

struct Tally {
    count: usize,
    bytes: usize,
    spans: usize,
    cells: usize,
}
impl Tally {
    fn add(&mut self, bytes: usize, spans: usize) -> Result<(), ExportError> {
        self.bytes = self.bytes.saturating_add(bytes);
        self.spans = self.spans.saturating_add(spans);
        preflight(self.count, self.bytes, self.spans)
    }
    fn attrs(&mut self, attrs: &[(String, String)]) -> Result<(), ExportError> {
        for (name, value) in attrs {
            self.add(name.len().saturating_add(value.len()), 1)?;
        }
        Ok(())
    }
    fn block(
        &mut self,
        block: &Block,
        range: Option<std::ops::Range<usize>>,
    ) -> Result<(), ExportError> {
        let range = range.unwrap_or(0..block.text.len());
        let table = block.meta.table.as_ref();
        self.add(
            if table.is_some() {
                block.text.len()
            } else {
                range.len()
            },
            0,
        )?;
        for value in [&block.meta.code_language, &block.meta.unsupported_reason] {
            self.add(value.as_ref().map_or(0, String::len), 0)?;
        }
        self.add(
            block.meta.raw_source.as_ref().map_or(0, |s| {
                if table.is_some() || range == (0..block.text.len()) {
                    s.len()
                } else {
                    range.len()
                }
            }),
            0,
        )?;
        self.add(
            block
                .meta
                .html_paragraph
                .as_ref()
                .map_or(0, |shell| shell.source.len()),
            0,
        )?;
        let mut end = 0;
        for span in &block.spans {
            if span.start < end
                || span.start >= span.end
                || span.end > block.text.len()
                || !block.text.is_char_boundary(span.start)
                || !block.text.is_char_boundary(span.end)
                || span.marks.len() > 4
            {
                return Err(ExportError::InvalidInput(
                    "格式范围必须有序、不重叠且位于 UTF-8 边界",
                ));
            }
            end = span.end;
            if span.start < range.end && span.end > range.start {
                self.add(0, 1)?;
            }
        }
        for tag in &block.meta.inline {
            if tag.start >= tag.end
                || tag.end > block.text.len()
                || !block.text.is_char_boundary(tag.start)
                || !block.text.is_char_boundary(tag.end)
            {
                return Err(ExportError::InvalidInput("HTML 范围必须位于 UTF-8 边界"));
            }
            if tag.start < range.end && tag.end > range.start {
                self.add(tag.name.len(), 1)?;
                self.attrs(&tag.attrs)?;
            }
        }
        if let Some(table) = table {
            super::table::validate(table)?;
            self.add(table.source.len(), 0)?;
            self.attrs(&table.attrs)?;
            for width in &table.column_widths {
                self.add(width.as_ref().map_or(0, String::len), 1)?;
            }
            for row in &table.rows {
                for cell in &row.cells {
                    self.cells += 1;
                    if self.cells > 2048 {
                        return Err(ExportError::ResourceLimit("最多 2048 个表格单元格"));
                    }
                    if cell.content.meta.table.is_some() || cell.original.meta.table.is_some() {
                        return Err(ExportError::UnsupportedTable("嵌套表格"));
                    }
                    self.attrs(&cell.attrs)?;
                    self.block(&cell.content, None)?;
                    self.block(&cell.original, None)?;
                }
            }
        } else if block.meta.raw_source.is_some()
            && block.meta.unsupported_reason.is_some()
            && block
                .text
                .as_bytes()
                .windows(6)
                .any(|part| part.eq_ignore_ascii_case(b"<table"))
        {
            return Err(ExportError::UnsupportedTable("不支持的非结构化 HTML 表格"));
        }
        Ok(())
    }
}

pub(super) fn canvas(width: f32, height: f32) -> Result<(u32, u32), ExportError> {
    if !width.is_finite()
        || !height.is_finite()
        || width <= 0.0
        || height <= 0.0
        || (width.ceil() as u64)
            .checked_mul(height.ceil() as u64)
            .is_none_or(|pixels| pixels > MAX_PIXELS)
    {
        return Err(ExportError::ResourceLimit(
            "最多 1600 万像素 / 64 MiB 像素缓冲",
        ));
    }
    Ok((width.ceil() as u32, height.ceil() as u32))
}
