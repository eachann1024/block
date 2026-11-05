//! Runtime CDN font loader and disk cache (`~/Library/Caches/Block/fonts/`).
//! Desktop caches JetBrains Mono (400/500) and STIX Two Math (400); web additionally fetches
//! IBM Plex Sans (400/600) from the same Fontsource CDN endpoints.

pub mod woff;
#[cfg(not(target_family = "wasm"))]
mod cache;
#[cfg(target_family = "wasm")]
mod web;

#[cfg(not(target_family = "wasm"))]
pub use cache::{load_disk_export_fonts, spawn};
#[cfg(target_family = "wasm")]
pub use web::{load_ui_fonts, spawn};

use std::sync::{
    Mutex, OnceLock,
    atomic::{AtomicBool, Ordering},
};
use latex_rust::MathFont;

/// Pinned Fontsource package version (`npm view @fontsource/jetbrains-mono version`).
pub const FONTSOURCE_VERSION: &str = "5.3.0";

#[derive(Clone, Copy)]
pub struct FontSpec {
    pub id: &'static str,
    pub weight: u16,
    pub filename: &'static str,
}

pub const JETBRAINS_MONO_400: FontSpec = FontSpec {
    id: "jetbrains-mono",
    weight: 400,
    filename: "jetbrains-mono-latin-400-normal.ttf",
};

pub const JETBRAINS_MONO_500: FontSpec = FontSpec {
    id: "jetbrains-mono",
    weight: 500,
    filename: "jetbrains-mono-latin-500-normal.ttf",
};

pub const STIX_TWO_MATH_400: FontSpec = FontSpec {
    id: "stix-two-math",
    weight: 400,
    filename: "stix-two-math-latin-400-normal.ttf",
};

#[cfg(target_family = "wasm")]
pub const IBM_PLEX_SANS_400: FontSpec = FontSpec {
    id: "ibm-plex-sans",
    weight: 400,
    filename: "ibm-plex-sans-latin-400-normal.ttf",
};

#[cfg(target_family = "wasm")]
pub const IBM_PLEX_SANS_600: FontSpec = FontSpec {
    id: "ibm-plex-sans",
    weight: 600,
    filename: "ibm-plex-sans-latin-600-normal.ttf",
};

impl FontSpec {
    /// Primary jsDelivr Fontsource TTF URL, Fastly mirror, and npmmirror `@fontsource/*` fallback.
    pub fn urls(&self) -> [String; 3] {
        let id = self.id;
        let w = self.weight;
        let v = FONTSOURCE_VERSION;
        [
            format!("https://cdn.jsdelivr.net/fontsource/fonts/{id}@{v}/latin-{w}-normal.ttf"),
            format!("https://fastly.jsdelivr.net/fontsource/fonts/{id}@{v}/latin-{w}-normal.ttf"),
            format!("https://registry.npmmirror.com/@fontsource/{id}/{v}/files/files/{id}-latin-{w}-normal.woff"),
        ]
    }
}

/// Validates TrueType (`\0\1\0\0`), OpenType CFF (`OTTO`), WOFF2 (`wOF2`), or WOFF (`wOFF`) magic bytes.
pub fn is_valid_font_header(bytes: &[u8]) -> bool {
    if bytes.len() < 12 {
        return false;
    }
    matches!(
        &bytes[..4],
        b"\x00\x01\x00\x00" | b"OTTO" | b"wOF2" | b"wOFF"
    )
}

/// Validates that a buffer is a raw SFNT face (`\0\1\0\0` or `OTTO`) ready for CoreText / cosmic-text / skrifa.
pub fn is_valid_sfnt(bytes: &[u8]) -> bool {
    bytes.len() >= 12 && matches!(&bytes[..4], b"\x00\x01\x00\x00" | b"OTTO")
}

static MATH_FONT: OnceLock<MathFont> = OnceLock::new();
static MATH_FAILED: AtomicBool = AtomicBool::new(false);
static EXPORT_FONTS: Mutex<Vec<&'static [u8]>> = Mutex::new(Vec::new());

pub fn math_font() -> Option<&'static MathFont> {
    MATH_FONT.get()
}

pub fn math_font_failed() -> bool {
    MATH_FAILED.load(Ordering::Relaxed)
}

pub fn set_math_font(bytes: Vec<u8>) -> bool {
    if MATH_FONT.get().is_some() {
        return true;
    }
    let leaked: &'static [u8] = Box::leak(bytes.into_boxed_slice());
    match MathFont::from_bytes(leaked) {
        Ok(font) => {
            let _ = MATH_FONT.set(font);
            MATH_FAILED.store(false, Ordering::Relaxed);
            true
        }
        Err(err) => {
            MATH_FAILED.store(true, Ordering::Relaxed);
            crate::log::warn(&format!("failed to parse STIX Two Math font: {err}"));
            false
        }
    }
}

pub fn mark_math_failed() {
    if MATH_FONT.get().is_none() {
        MATH_FAILED.store(true, Ordering::Relaxed);
    }
}

pub fn register_export_font(bytes: Vec<u8>) -> &'static [u8] {
    let leaked: &'static [u8] = Box::leak(bytes.into_boxed_slice());
    if let Ok(mut list) = EXPORT_FONTS.lock() {
        list.push(leaked);
    }
    leaked
}

pub fn cached_export_fonts() -> Vec<&'static [u8]> {
    EXPORT_FONTS
        .lock()
        .map(|list| list.clone())
        .unwrap_or_default()
}
