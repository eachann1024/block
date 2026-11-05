//! Font settings, installed/imported font lists, and fallback resolution.
use std::process::Command;
use std::sync::atomic::{AtomicBool, AtomicU8, AtomicU64, Ordering};
use std::sync::RwLock;
use gpui::SharedString;
pub use crate::files::state::{FontChoice, LineHeight};

static FONT_CHOICE: AtomicU8 = AtomicU8::new(0);
static SMOOTHING: AtomicBool = AtomicBool::new(true);
static CUSTOM_EDITOR_FONT: RwLock<Option<String>> = RwLock::new(None);
static CUSTOM_CODE_FONT: RwLock<Option<String>> = RwLock::new(None);
static LINE_HEIGHT: AtomicU8 = AtomicU8::new(1);
static INSTALLED_FONTS: RwLock<Vec<String>> = RwLock::new(Vec::new());
static IMPORTED_FONTS: RwLock<Vec<String>> = RwLock::new(Vec::new());
static FONT_CATALOG_VERSION: AtomicU64 = AtomicU64::new(1);

const SERIF_FALLBACKS: &[&str] = &["Songti SC", "Noto Serif CJK SC", "SimSun"];
const CJK_FALLBACKS: &[&str] = &["PingFang SC", "Songti SC", "Noto Sans SC", "Microsoft YaHei"];

static RESOLVED_FONTS: RwLock<(&'static str, &'static str, &'static str)> =
    RwLock::new(("PingFang SC", "Songti SC", "Menlo"));

#[allow(dead_code)]
pub fn font_choice() -> FontChoice {
    match FONT_CHOICE.load(Ordering::Relaxed) { 1 => FontChoice::Serif, _ => FontChoice::Sans }
}
pub fn set_font_choice(font: FontChoice) {
    FONT_CHOICE.store(match font { FontChoice::Sans => 0, FontChoice::Serif => 1 }, Ordering::Relaxed);
}

pub fn custom_editor_font() -> Option<String> { CUSTOM_EDITOR_FONT.read().ok()?.clone() }
pub fn set_custom_editor_font(font: Option<String>) { if let Ok(mut g) = CUSTOM_EDITOR_FONT.write() { *g = font; } }
pub fn custom_code_font() -> Option<String> { CUSTOM_CODE_FONT.read().ok()?.clone() }
pub fn set_custom_code_font(font: Option<String>) { if let Ok(mut g) = CUSTOM_CODE_FONT.write() { *g = font; } }

pub fn line_height() -> LineHeight {
    match LINE_HEIGHT.load(Ordering::Relaxed) { 0 => LineHeight::Tight, 2 => LineHeight::Loose, _ => LineHeight::Normal }
}
pub fn set_line_height(lh: LineHeight) {
    LINE_HEIGHT.store(match lh { LineHeight::Tight => 0, LineHeight::Normal => 1, LineHeight::Loose => 2 }, Ordering::Relaxed);
}
pub fn body_line_factor() -> f32 { line_height().factor() }

pub fn font_settings_hash() -> u64 {
    use std::hash::{Hash, Hasher};
    let mut h = std::collections::hash_map::DefaultHasher::new();
    CUSTOM_EDITOR_FONT.read().ok().as_deref().hash(&mut h);
    CUSTOM_CODE_FONT.read().ok().as_deref().hash(&mut h);
    LINE_HEIGHT.load(Ordering::Relaxed).hash(&mut h);
    (FONT_CHOICE.load(Ordering::Relaxed)).hash(&mut h);
    h.finish()
}

pub fn is_monospace_name(name: &str) -> bool {
    let l = name.to_ascii_lowercase();
    l.contains("mono") || l.contains("code") || l.contains("consolas") || l.contains("menlo") || l.contains("monaco") || l.contains("courier")
}

pub fn installed_fonts() -> Vec<String> { INSTALLED_FONTS.read().map(|g| g.clone()).unwrap_or_default() }
pub fn imported_fonts() -> Vec<String> { IMPORTED_FONTS.read().map(|g| g.clone()).unwrap_or_default() }
pub fn font_catalog_version() -> u64 { FONT_CATALOG_VERSION.load(Ordering::Relaxed) }
pub fn add_imported_fonts(fonts: &[String]) {
    if let Ok(mut guard) = IMPORTED_FONTS.write() {
        for f in fonts {
            let trimmed = f.trim();
            if !trimmed.is_empty() && !guard.iter().any(|g| g.eq_ignore_ascii_case(trimmed)) {
                guard.push(trimmed.to_string());
            }
        }
        guard.sort_by(|a, b| a.to_lowercase().cmp(&b.to_lowercase()));
        guard.dedup();
        FONT_CATALOG_VERSION.fetch_add(1, Ordering::Relaxed);
    }
}

pub fn resolved_body_font() -> &'static str { resolved().0 }
pub fn resolved_serif_font() -> &'static str { resolved().1 }
pub fn resolved_mono_font() -> &'static str { resolved().2 }

pub fn active_editor_font() -> String {
    match custom_editor_font().as_deref() {
        None | Some("") | Some("system-sans") => resolved_body_font().to_string(),
        Some("system-serif") => resolved_serif_font().to_string(),
        Some(name) => name.to_string(),
    }
}

pub fn active_code_font() -> String {
    match custom_code_font().as_deref() {
        None | Some("") | Some("auto") => resolved_mono_font().to_string(),
        Some(name) => name.to_string(),
    }
}

pub fn init_fonts(installed: &[String]) {
    let mut valid: Vec<String> = installed.iter().filter(|s| !s.starts_with('.')).cloned().collect();
    valid.sort_by(|a, b| a.to_lowercase().cmp(&b.to_lowercase()));
    valid.dedup();
    if let Ok(mut guard) = INSTALLED_FONTS.write() {
        *guard = valid;
        FONT_CATALOG_VERSION.fetch_add(1, Ordering::Relaxed);
    }
    let pick = |candidates: &[&'static str], last: &'static str| {
        candidates.iter().copied().find(|c| installed.iter().any(|n| n == c)).unwrap_or(last)
    };
    let body = pick(
        &["PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", "Noto Sans SC", "Hiragino Sans GB", "Helvetica Neue", "Segoe UI", "DejaVu Sans"],
        if cfg!(target_os = "windows") { "Microsoft YaHei" } else if cfg!(target_os = "linux") { "Noto Sans CJK SC" } else { "PingFang SC" },
    );
    let serif = pick(
        &["LXGW WenKai", "LXGW WenKai Screen", "Kaiti SC", "Songti SC", "Source Han Serif SC", "Noto Serif CJK SC", "SimSun"],
        if cfg!(target_os = "windows") { "SimSun" } else if cfg!(target_os = "linux") { "Noto Serif CJK SC" } else { "Songti SC" },
    );
    let mono = pick(
        &["JetBrains Mono", "Menlo", "Monaco", "Cascadia Mono", "Consolas", "DejaVu Sans Mono", "Liberation Mono"],
        if cfg!(target_os = "windows") { "Consolas" } else if cfg!(target_os = "linux") { "DejaVu Sans Mono" } else { "Menlo" },
    );
    if let Ok(mut guard) = RESOLVED_FONTS.write() { *guard = (body, serif, mono); }
}

fn resolved() -> (&'static str, &'static str, &'static str) {
    RESOLVED_FONTS.read().map(|g| *g).unwrap_or(("PingFang SC", "Songti SC", "Menlo"))
}

pub fn mono_font_family() -> SharedString {
    SharedString::from(active_code_font())
}

pub fn editor_font() -> (SharedString, &'static [&'static str]) {
    let (_, serif, _) = resolved();
    let custom = CUSTOM_EDITOR_FONT.read().ok().and_then(|g| g.clone());
    match custom.as_deref() {
        None | Some("") | Some("system-sans") => (".SystemUIFont".into(), &[]),
        Some("system-serif") if serif == "Songti SC" => (serif.into(), &[]),
        Some("system-serif") => (serif.into(), SERIF_FALLBACKS),
        Some(name) => (SharedString::from(name.to_owned()), CJK_FALLBACKS),
    }
}

pub fn set_font_smoothing(on: bool) {
    SMOOTHING.store(on, Ordering::Relaxed);
    if !cfg!(target_os = "macos") { return; }
    let val = if on { "2" } else { "0" };
    std::thread::spawn(move || {
        let _ = Command::new("defaults").args(["write", "dev.eachann.block", "AppleFontSmoothing", "-int", val]).status();
    });
}
