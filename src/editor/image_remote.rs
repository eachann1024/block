//! Asynchronous download and disk caching for remote (`http://` / `https://`) image blocks.
use std::{
    collections::HashMap,
    fs,
    path::PathBuf,
    process::Command,
    sync::{LazyLock, Mutex},
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use gpui::{App, Entity};
use super::view::Editor;

#[derive(Clone, Debug)]
pub(super) enum RemoteImageState { Loading, Ready(PathBuf), Failed }

const EXTS: [&str; 6] = ["png", "jpg", "gif", "webp", "bmp", "svg"];
static CACHE: LazyLock<Mutex<HashMap<String, RemoteImageState>>> = LazyLock::new(|| Mutex::new(HashMap::new()));

fn cache_dir() -> PathBuf { std::env::temp_dir().join("block-remote-images") }

fn hash_url(url: &str) -> u64 {
    let mut h: u64 = 0xcbf29ce484222325;
    for &b in url.as_bytes() {
        h ^= u64::from(b);
        h = h.wrapping_mul(0x100000001b3);
    }
    h
}

fn find_on_disk(hash: u64) -> Option<PathBuf> {
    let dir = cache_dir();
    for ext in EXTS {
        let candidate = dir.join(format!("img_{hash:016x}.{ext}"));
        if fs::metadata(&candidate).is_ok_and(|m| m.is_file() && m.len() > 0) {
            return Some(candidate);
        }
    }
    None
}

pub(super) fn cached_path(url: &str) -> Option<PathBuf> {
    let url = url.trim();
    if let Ok(mut map) = CACHE.lock() {
        if let Some(state) = map.get(url) {
            return match state {
                RemoteImageState::Ready(p) => Some(p.clone()),
                _ => None,
            };
        }
        if let Some(path) = find_on_disk(hash_url(url)) {
            map.insert(url.to_owned(), RemoteImageState::Ready(path.clone()));
            return Some(path);
        }
    }
    None
}

pub(crate) fn ensure_cached(url: &str) -> Option<PathBuf> {
    if let Some(path) = cached_path(url).filter(|p| p.exists()) {
        return Some(path);
    }
    let key = url.trim().to_owned();
    let path = download_sync(&key).ok()?;
    if let Ok(mut map) = CACHE.lock() {
        map.insert(key, RemoteImageState::Ready(path.clone()));
    }
    Some(path)
}

pub(super) fn retry(url: &str) {
    if let Ok(mut map) = CACHE.lock() {
        map.remove(url.trim());
    }
}

fn detect_ext(bytes: &[u8], url: &str) -> Option<&'static str> {
    if bytes.is_empty() { return None; }
    if bytes.starts_with(b"\x89PNG\r\n\x1a\n") { return Some("png"); }
    if bytes.starts_with(b"\xff\xd8\xff") { return Some("jpg"); }
    if bytes.starts_with(b"GIF87a") || bytes.starts_with(b"GIF89a") { return Some("gif"); }
    if bytes.len() >= 12 && &bytes[0..4] == b"RIFF" && &bytes[8..12] == b"WEBP" { return Some("webp"); }
    if bytes.starts_with(b"BM") { return Some("bmp"); }
    let head = String::from_utf8_lossy(&bytes[..bytes.len().min(512)]).to_ascii_lowercase();
    if head.contains("<svg") { return Some("svg"); }
    if head.trim_start().starts_with("<!doctype html") || head.trim_start().starts_with("<html") {
        return None;
    }
    let clean = url.split(['?', '#']).next().unwrap_or(url).to_ascii_lowercase();
    if clean.ends_with(".png") { return Some("png"); }
    if clean.ends_with(".jpg") || clean.ends_with(".jpeg") { return Some("jpg"); }
    if clean.ends_with(".gif") { return Some("gif"); }
    if clean.ends_with(".webp") { return Some("webp"); }
    if clean.ends_with(".bmp") { return Some("bmp"); }
    if clean.ends_with(".svg") { return Some("svg"); }
    None
}

pub(crate) fn download_sync(url: &str) -> Result<PathBuf, String> {
    let dir = cache_dir();
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let hash = hash_url(url);
    if let Some(existing) = find_on_disk(hash) { return Ok(existing); }
    let curl = if cfg!(target_os = "macos") { "/usr/bin/curl" } else { "curl" };
    let out = Command::new(curl)
        .args([
            "-fsSL", "--compressed", "--max-time", "30",
            "-A", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
            url,
        ])
        .output()
        .map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err(format!("curl exit {}", out.status));
    }
    let ext = detect_ext(&out.stdout, url).ok_or_else(|| "unrecognized image payload".to_owned())?;
    let dest = dir.join(format!("img_{hash:016x}.{ext}"));
    let stamp = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos();
    let tmp = dir.join(format!("img_{hash:016x}.{stamp}.part"));
    fs::write(&tmp, &out.stdout).map_err(|e| e.to_string())?;
    fs::rename(&tmp, &dest).map_err(|e| e.to_string())?;
    Ok(dest)
}

fn refresh_image_rows(editor: &Entity<Editor>, id: u64, cx: &mut App) {
    editor.update(cx, |this, cx| {
        for (idx, block) in this.doc.blocks.iter().enumerate() {
            if block.id == id || super::image_source::spec_of(block).is_some() {
                if idx < this.list.item_count() {
                    this.list.splice(idx..idx + 1, 1);
                }
            }
        }
        cx.notify();
    });
}

pub(super) fn request(editor: Entity<Editor>, id: u64, url: &str, cx: &mut App) -> RemoteImageState {
    let key = url.trim().to_owned();
    if let Ok(mut map) = CACHE.lock() {
        if let Some(state) = map.get(&key) {
            return state.clone();
        }
        if let Some(path) = find_on_disk(hash_url(&key)) {
            let state = RemoteImageState::Ready(path);
            map.insert(key, state.clone());
            return state;
        }
        map.insert(key.clone(), RemoteImageState::Loading);
    }
    let url_bg = key.clone();
    cx.spawn(async move |cx| {
        let res = cx.background_executor().spawn(async move { download_sync(&url_bg) }).await;
        if let Ok(mut map) = CACHE.lock() {
            match res {
                Ok(path) => { map.insert(key, RemoteImageState::Ready(path)); }
                Err(err) => {
                    crate::log::warn(&format!("remote image download failed ({key}): {err}"));
                    map.insert(key, RemoteImageState::Failed);
                }
            }
        }
        cx.update(|cx| refresh_image_rows(&editor, id, cx));
        cx.background_executor().timer(Duration::from_millis(80)).await;
        cx.update(|cx| refresh_image_rows(&editor, id, cx));
    }).detach();
    RemoteImageState::Loading
}
