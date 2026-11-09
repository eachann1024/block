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
mod media;
mod paint;
mod palette;
mod palette_colors;
mod rich;
mod preferences;
mod spec;
mod source;
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
pub(crate) use view::{ExportSlot, ExportView, Snapshot, active_view, close_active};

pub use budget::{ExportError, preflight};
pub use format::ExportFormat;
pub use job::{Cancellation, ExportJob, Timings, write_bytes, write_png};
pub use palette::{ExportTheme, RenderOptions};
pub use preferences::ExportPreferences;
pub(crate) use source::{BlockLayout, SourceContext, TableGeometry};

pub(super) const WATERMARK_LABEL: &str = "Block App";

use crate::editor::model::Block;
use cosmic_text::{FontSystem, SwashCache};
use tiny_skia::Pixmap;
use std::{sync::Mutex, time::Duration};

#[cfg(target_family = "wasm")]
pub(crate) use web_cjk::metric_font;

// One font database for all jobs; all glyphs in a job share one raster cache.
// The glyph cache is dropped between jobs so arbitrary documents cannot grow it forever.
static FONTS: Mutex<Option<FontSystem>> = Mutex::new(None);

pub fn reset_font_system() {
    if let Ok(mut guard) = FONTS.lock() {
        *guard = None;
    }
}

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
fn new_font_system() -> FontSystem {
    let mut fs = FontSystem::new();
    for data in crate::fonts::load_disk_export_fonts() {
        fs.db_mut().load_font_data(data);
    }
    for data in crate::fonts::load_custom_fonts_data() {
        fs.db_mut().load_font_data(data);
    }
    fs
}
/// The browser has no system fonts for cosmic-text; use the CDN fonts fetched at runtime.
#[cfg(target_family = "wasm")]
fn new_font_system() -> FontSystem {
    let mut db = cosmic_text::fontdb::Database::new();
    for data in crate::fonts::cached_export_fonts() {
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

/// Encode full PNG plus vertical strips (`<= 4096` px tall) so GPU texture atlases never overflow.
pub(super) fn render_preview_with_cancel(
    blocks: &[Block],
    options: RenderOptions,
    cancel: &Cancellation,
) -> Result<(Vec<u8>, Vec<(Vec<u8>, u32, u32)>, Timings), ExportError> {
    const TILE_H: u32 = 4096;
    let (image, mut timings) = render_pixmap(blocks, options, cancel)?;
    let start = Stopwatch::start();
    let bytes = image
        .encode_png()
        .map_err(|error| ExportError::Encoding(error.to_string()))?;
    let (w, h) = (image.width(), image.height());
    let mut tiles = Vec::new();
    if h <= TILE_H {
        tiles.push((bytes.clone(), w, h));
    } else {
        let row_bytes = (w as usize) * 4;
        let data = image.data();
        let mut y = 0;
        while y < h {
            cancel.check()?;
            let th = (h - y).min(TILE_H);
            let start_byte = (y as usize) * row_bytes;
            let end_byte = start_byte + (th as usize) * row_bytes;
            if let Some(sub) = tiny_skia::PixmapRef::from_bytes(&data[start_byte..end_byte], w, th)
                && let Ok(tile_png) = sub.to_owned().encode_png()
            {
                tiles.push((tile_png, w, th));
            }
            y += th;
        }
    }
    timings.encoding = start.elapsed();
    cancel.check()?;
    Ok((bytes, tiles, timings))
}
