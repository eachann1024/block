mod budget;
mod chrome;
mod decor_blocks;
mod decor_marks;
mod decor_paper;
mod decor_shapes;
mod export_docx;
mod export_docx_xml;
mod export_html;
mod export_html_blocks;
mod export_pdf;
mod export_zip;
mod format;
mod job;
mod layout;
mod paint;
mod palette;
mod palette_colors;
mod rich;
mod spec;
mod style;
mod table;
mod view;
mod view_actions;
mod view_stage;
mod view_ui;
mod view_widgets;
#[cfg(target_family = "wasm")]
mod web_cjk;
#[cfg(target_family = "wasm")]
mod web_save;

pub(crate) use budget::validate_snapshot;
pub use view::{ExportSlot, ExportView, Snapshot, active_view};

pub use budget::{ExportError, preflight};
pub use format::ExportFormat;
pub use job::{Cancellation, ExportJob, Timings, write_bytes, write_png};
pub use palette::{ExportTheme, RenderOptions};

use crate::editor::model::Block;
use cosmic_text::{FontSystem, SwashCache};
use tiny_skia::Pixmap;
use std::{sync::Mutex, time::Duration};

// One font database for all jobs; all glyphs in a job share one raster cache.
// The glyph cache is dropped between jobs so arbitrary documents cannot grow it forever.
static FONTS: Mutex<Option<FontSystem>> = Mutex::new(None);

/// `Stopwatch::start()` panics on wasm32, so timings are only measured natively.
struct Stopwatch(#[cfg(not(target_family = "wasm"))] std::time::Instant);
impl Stopwatch {
    fn start() -> Self {
        #[cfg(not(target_family = "wasm"))]
        { Self(std::time::Instant::now()) }
        #[cfg(target_family = "wasm")]
        { Self() }
    }
    fn elapsed(&self) -> Duration {
        #[cfg(not(target_family = "wasm"))]
        { self.0.elapsed() }
        #[cfg(target_family = "wasm")]
        { Duration::ZERO }
    }
}

#[cfg(not(target_family = "wasm"))]
fn new_font_system() -> FontSystem { FontSystem::new() }
/// The browser has no system fonts for cosmic-text; use the fonts bundled with the web build.
#[cfg(target_family = "wasm")]
fn new_font_system() -> FontSystem {
    let mut db = cosmic_text::fontdb::Database::new();
    for data in crate::WEB_FONTS {
        db.load_font_data(data.to_vec());
    }
    for fam in ["PingFang SC", "Songti SC", "Kaiti SC"] {
        db.load_font_data(web_cjk::metric_font(fam));
    }
    db.set_sans_serif_family("PingFang SC");
    db.set_serif_family("Songti SC");
    db.set_monospace_family("JetBrains Mono");
    FontSystem::new_with_locale_and_db("zh-CN".to_string(), db)
}

pub(super) fn render_pixmap(
    blocks: &[Block],
    options: RenderOptions,
    cancel: &Cancellation,
) -> Result<(Pixmap, Timings), ExportError> {
    cancel.check()?;
    let start = Stopwatch::start();
    let mut storage = FONTS.try_lock().map_err(|_| ExportError::Busy)?;
    let fonts = storage.get_or_insert_with(new_font_system);
    let mut timings = Timings {
        font_setup: start.elapsed(),
        ..Timings::default()
    };
    cancel.check()?;
    let start = Stopwatch::start();
    let layout = layout::build(blocks, fonts, cancel, options)?;
    timings.layout = start.elapsed();
    let start = Stopwatch::start();
    let image = paint::rasterize(&layout, fonts, &mut SwashCache::new(), cancel)?;
    timings.raster = start.elapsed();
    // Release font/layout resources before encoding; pixels were budgeted before allocation.
    drop(layout);
    drop(storage);
    cancel.check()?;
    Ok((image, timings))
}

pub fn render_with_cancel(
    blocks: &[Block],
    options: RenderOptions,
    cancel: &Cancellation,
) -> Result<(Vec<u8>, Timings), ExportError> {
    let (image, mut timings) = render_pixmap(blocks, options, cancel)?;
    let start = Stopwatch::start();
    let bytes = image
        .encode_png()
        .map_err(|error| ExportError::Encoding(error.to_string()))?;
    timings.encoding = start.elapsed();
    cancel.check()?;
    Ok((bytes, timings))
}
