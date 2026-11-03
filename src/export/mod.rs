mod budget;
mod chrome;
mod decor_blocks;
mod decor_marks;
mod decor_paper;
mod decor_shapes;
mod job;
mod layout;
mod paint;
mod palette;
mod rich;
mod style;
mod table;
mod view;
mod view_stage;
mod view_ui;
mod view_widgets;

pub(crate) use budget::validate_snapshot;
pub use view::{ExportSlot, ExportView, Snapshot, active_view};

pub use budget::{ExportError, preflight};
pub use job::{Cancellation, ExportJob, Timings, write_png};
pub use palette::{ExportTheme, RenderOptions};

use crate::editor::model::Block;
use cosmic_text::{FontSystem, SwashCache};
use std::{sync::Mutex, time::Instant};

// One font database for all jobs; all glyphs in a job share one raster cache.
// The glyph cache is dropped between jobs so arbitrary documents cannot grow it forever.
static FONTS: Mutex<Option<FontSystem>> = Mutex::new(None);

pub fn render_with_cancel(
    blocks: &[Block],
    options: RenderOptions,
    cancel: &Cancellation,
) -> Result<(Vec<u8>, Timings), ExportError> {
    cancel.check()?;
    let start = Instant::now();
    let mut storage = FONTS.try_lock().map_err(|_| ExportError::Busy)?;
    let fonts = storage.get_or_insert_with(FontSystem::new);
    let mut timings = Timings {
        font_setup: start.elapsed(),
        ..Timings::default()
    };
    cancel.check()?;
    let start = Instant::now();
    let layout = layout::build(blocks, fonts, cancel, options)?;
    timings.layout = start.elapsed();
    let start = Instant::now();
    let image = paint::rasterize(&layout, fonts, &mut SwashCache::new(), cancel)?;
    timings.raster = start.elapsed();
    // Release font/layout resources before encoding; pixels were budgeted before allocation.
    drop(layout);
    drop(storage);
    cancel.check()?;
    let start = Instant::now();
    let bytes = image
        .encode_png()
        .map_err(|error| ExportError::Encoding(error.to_string()))?;
    timings.encoding = start.elapsed();
    cancel.check()?;
    Ok((bytes, timings))
}
