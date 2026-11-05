//! Desktop background font downloader and atomic disk cache (`~/Library/Caches/Block/fonts/`).

use std::{
    borrow::Cow,
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    process::Command,
    sync::atomic::{AtomicU64, Ordering},
};
use gpui::{App, Entity};
use crate::app::Workspace;
use super::{
    FontSpec, JETBRAINS_MONO_400, JETBRAINS_MONO_500, STIX_TWO_MATH_400,
    cached_export_fonts, is_valid_font_header, is_valid_sfnt,
    mark_math_failed, register_export_font, set_math_font, woff::ensure_sfnt,
};

static NEXT_TMP: AtomicU64 = AtomicU64::new(0);

pub fn cache_dir() -> PathBuf {
    dirs::cache_dir()
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join("Library/Caches")))
        .unwrap_or_else(std::env::temp_dir)
        .join("Block/fonts")
}

fn read_cached(path: &Path) -> Option<Vec<u8>> {
    let raw = fs::read(path).ok()?;
    let sfnt = ensure_sfnt(raw)?;
    Some(sfnt)
}

fn write_atomic(dir: &Path, filename: &str, bytes: &[u8]) -> std::io::Result<()> {
    fs::create_dir_all(dir)?;
    let target = dir.join(filename);
    let seq = NEXT_TMP.fetch_add(1, Ordering::Relaxed);
    let tmp = dir.join(format!(".{filename}.{}.{seq}.tmp", std::process::id()));
    let res = (|| -> std::io::Result<()> {
        let mut f = OpenOptions::new().write(true).create_new(true).open(&tmp)?;
        f.write_all(bytes)?;
        f.sync_all()?;
        drop(f);
        fs::rename(&tmp, &target)
    })();
    if res.is_err() {
        let _ = fs::remove_file(&tmp);
    }
    res
}

fn download_url(url: &str) -> Result<Vec<u8>, String> {
    let out = Command::new("/usr/bin/curl")
        .args([
            "-fsSL",
            "--connect-timeout",
            "8",
            "--max-time",
            "25",
            "-A",
            "Block/0.1",
            url,
        ])
        .output()
        .map_err(|e| format!("curl exec error: {e}"))?;
    if !out.status.success() {
        return Err(format!("curl status {} for {url}", out.status));
    }
    if !is_valid_font_header(&out.stdout) {
        return Err(format!("invalid font header from {url}"));
    }
    ensure_sfnt(out.stdout).ok_or_else(|| format!("failed to decode font from {url}"))
}

fn load_or_download(dir: &Path, spec: FontSpec, errors: &mut Vec<String>) -> Option<Vec<u8>> {
    let path = dir.join(spec.filename);
    if let Some(bytes) = read_cached(&path) {
        return Some(bytes);
    }
    let mut last_err = String::new();
    for url in spec.urls() {
        match download_url(&url) {
            Ok(bytes) if is_valid_sfnt(&bytes) => {
                if let Err(err) = write_atomic(dir, spec.filename, &bytes) {
                    errors.push(format!("cache write {}: {err}", spec.filename));
                }
                return Some(bytes);
            }
            Ok(_) => last_err = format!("invalid sfnt for {}", spec.filename),
            Err(err) => last_err = err,
        }
    }
    if !last_err.is_empty() {
        errors.push(format!("{}: {last_err}", spec.filename));
    }
    None
}

/// Synchronously reads any already-cached export fonts from disk if memory registration hasn't completed yet.
pub fn load_disk_export_fonts() -> Vec<Vec<u8>> {
    let mem = cached_export_fonts();
    if !mem.is_empty() {
        return mem.into_iter().map(|s| s.to_vec()).collect();
    }
    let dir = cache_dir();
    [JETBRAINS_MONO_400, JETBRAINS_MONO_500]
        .into_iter()
        .filter_map(|spec| read_cached(&dir.join(spec.filename)))
        .collect()
}

/// Spawns a background task to load or download CDN fonts, register them with GPUI / cosmic-text / latex-rust,
/// and refresh open windows while preserving priority for system-installed fonts.
pub fn spawn(workspace: Entity<Workspace>, cx: &mut App) {
    let system_has_jetbrains = cx
        .text_system()
        .all_font_names()
        .iter()
        .any(|name| name == "JetBrains Mono");
    let weak = workspace.downgrade();
    cx.spawn(async move |cx| {
        let (mono_fonts, math_ready) = cx
            .background_executor()
            .spawn(async move {
                let dir = cache_dir();
                let mut errors = Vec::new();
                let mut mono = Vec::new();
                for spec in [JETBRAINS_MONO_400, JETBRAINS_MONO_500] {
                    if let Some(bytes) = load_or_download(&dir, spec, &mut errors) {
                        mono.push(register_export_font(bytes));
                    }
                }
                let math_ok = match load_or_download(&dir, STIX_TWO_MATH_400, &mut errors) {
                    Some(bytes) => set_math_font(bytes),
                    None => {
                        mark_math_failed();
                        false
                    }
                };
                if !errors.is_empty() {
                    crate::log::warn(&format!("CDN font fetch incomplete: {}", errors.join("; ")));
                }
                if !mono.is_empty() {
                    crate::export::reset_font_system();
                }
                (mono, math_ok)
            })
            .await;

        cx.update(|cx| {
            let mut updated_mono = false;
            if !system_has_jetbrains && !mono_fonts.is_empty() {
                let cows = mono_fonts.into_iter().map(Cow::Borrowed).collect();
                if cx.text_system().add_fonts(cows).is_ok() {
                    crate::theme::init_fonts(&cx.text_system().all_font_names());
                    updated_mono = true;
                }
            }
            if updated_mono || math_ready || super::math_font_failed() {
                if let Some(ws) = weak.upgrade() {
                    ws.update(cx, |w, cx| {
                        if updated_mono {
                            w.editor.update(cx, |e, cx| {
                                e.reset_layouts();
                                cx.notify();
                            });
                        } else {
                            w.editor.update(cx, |_, cx| cx.notify());
                        }
                        cx.notify();
                    });
                }
                if updated_mono {
                    crate::quick::refresh_style(cx);
                }
            }
        });
    })
    .detach();
}
