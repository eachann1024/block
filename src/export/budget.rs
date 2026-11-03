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
    Rendering(&'static str),
    Encoding(String),
    Write(std::io::Error),
}

impl fmt::Display for ExportError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Cancelled => write!(f, "Export cancelled"),
            Self::Dialog(error) => write!(f, "Export dialog failed: {error}"),
            Self::Empty => write!(f, "Nothing selected to export"),
            Self::Busy => write!(
                f,
                "An export is already running; try again after it finishes"
            ),
            Self::ResourceLimit(reason) => write!(
                f,
                "Export exceeds the image budget ({reason}). Select a smaller range; nothing was exported"
            ),
            Self::InvalidInput(reason) => {
                write!(f, "Export contains invalid text formatting: {reason}")
            }
            Self::Rendering(reason) => write!(f, "Image rendering failed: {reason}"),
            Self::Encoding(error) => write!(f, "PNG encoding failed: {error}"),
            Self::Write(error) => write!(f, "Could not save exported image: {error}"),
        }
    }
}
impl std::error::Error for ExportError {}

pub fn preflight(count: usize, text_bytes: usize, spans: usize) -> Result<(), ExportError> {
    if count == 0 {
        return Err(ExportError::Empty);
    }
    if count > MAX_BLOCKS {
        return Err(ExportError::ResourceLimit("maximum 256 blocks"));
    }
    if text_bytes > MAX_TEXT_BYTES {
        return Err(ExportError::ResourceLimit("maximum 128 KiB of text"));
    }
    if spans > MAX_SPANS {
        return Err(ExportError::ResourceLimit("maximum 8192 formatting ranges"));
    }
    Ok(())
}

pub(super) fn validate(blocks: &[Block]) -> Result<(), ExportError> {
    preflight(blocks.len(), 0, 0)?;
    let (mut bytes, mut spans) = (0usize, 0usize);
    for block in blocks {
        bytes = bytes
            .saturating_add(block.text.len())
            .saturating_add(block.meta.code_language.as_ref().map_or(0, String::len))
            .saturating_add(block.meta.raw_source.as_ref().map_or(0, String::len));
        spans = spans.saturating_add(block.spans.len());
        preflight(blocks.len(), bytes, spans)?;
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
                    "ranges must be ordered, disjoint UTF-8 boundaries",
                ));
            }
            end = span.end;
        }
    }
    Ok(())
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
            "maximum 16 megapixels / 64 MiB pixel buffer",
        ));
    }
    Ok((width.ceil() as u32, height.ceil() as u32))
}
