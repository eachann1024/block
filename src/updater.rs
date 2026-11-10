//! Silent background update checker, bilingual changelog modal, and installer updates.
//! Installers come from the GitHub release. When the asset digest is present, the download is checked against it.

use std::{
    path::Path,
    process::Command,
    sync::{
        Mutex,
        atomic::{AtomicBool, Ordering},
    },
    time::Duration,
};

use gpui::{
    AnyElement, App, Context, FontWeight, MouseButton, div, prelude::*, px,
};

use crate::{
    app::Workspace,
    theme::{Theme, tr},
    ui::modal,
};

static OPEN: AtomicBool = AtomicBool::new(false);
static HAS_UPDATE: AtomicBool = AtomicBool::new(false);
static ENTRANCE_DONE: AtomicBool = AtomicBool::new(false);
static DOWNLOADING: AtomicBool = AtomicBool::new(false);
static CANCELLED: AtomicBool = AtomicBool::new(false);
static UPDATE_INFO: Mutex<Option<UpdateInfo>> = Mutex::new(None);
static PROGRESS: Mutex<DownloadProgress> = Mutex::new(DownloadProgress {
    downloaded_bytes: 0,
    total_bytes: 0,
    pct: 0,
    active: false,
});

#[derive(Clone, Debug, Default)]
pub struct ReleaseSection {
    pub highlights: String,
    pub features: Vec<String>,
    pub fixes: Vec<String>,
}

#[derive(Clone, Debug)]
pub struct DayLog {
    pub date: String,
    pub version_tag: String,
    pub zh: ReleaseSection,
    pub en: ReleaseSection,
}

#[derive(Clone, Debug)]
pub struct UpdateInfo {
    pub latest_version: String,
    pub latest_tag: String,
    pub days: Vec<DayLog>,
    pub download_url: String,
    pub asset_name: String,
    pub total_bytes: u64,
    pub sha256: Option<String>,
    pub manual_build: bool,
}

#[derive(Clone, Debug, Default)]
pub struct DownloadProgress {
    pub downloaded_bytes: u64,
    pub total_bytes: u64,
    pub pct: u8,
    pub active: bool,
}

#[derive(serde::Deserialize, Clone, Debug)]
struct GhAsset {
    name: String,
    browser_download_url: String,
    #[serde(default)]
    size: u64,
    #[serde(default)]
    digest: Option<String>,
}

const SOURCE_URL: &str = "https://github.com/eachann1024/block";

#[allow(dead_code)]
enum UpdatePackage {
    Installer(&'static str),
    BuildFromSource,
}

fn sha256_from_digest(digest: Option<&str>) -> Option<String> {
    let hex = digest?.trim().strip_prefix("sha256:")?.trim();
    if hex.len() == 64 && hex.bytes().all(|byte| byte.is_ascii_hexdigit()) {
        Some(hex.to_ascii_lowercase())
    } else {
        None
    }
}

#[cfg(all(target_os = "linux", target_arch = "x86_64"))]
fn linux_from_appimage() -> bool {
    if std::env::var_os("APPIMAGE").is_some() || std::env::var_os("APPDIR").is_some() {
        return true;
    }
    std::env::current_exe()
        .ok()
        .and_then(|path| path.to_str().map(|text| text.contains("/tmp/.mount_")))
        .unwrap_or(false)
}

#[cfg(all(target_os = "linux", target_arch = "x86_64"))]
fn linux_from_rpm() -> bool {
    let Ok(exe) = std::env::current_exe() else { return false };
    let Ok(out) = Command::new("rpm").args(["-qf", exe.to_string_lossy().as_ref()]).output() else {
        return false;
    };
    if !out.status.success() {
        return false;
    }
    let text = String::from_utf8_lossy(&out.stdout);
    let line = text.lines().next().unwrap_or("").trim();
    !line.is_empty() && !line.to_ascii_lowercase().contains("not owned")
}

fn update_package() -> UpdatePackage {
    #[cfg(all(target_os = "macos", target_arch = "aarch64"))]
    { return UpdatePackage::Installer("block-macos-arm64.dmg"); }
    #[cfg(all(target_os = "macos", target_arch = "x86_64"))]
    { return UpdatePackage::Installer("block-macos-x64.dmg"); }
    #[cfg(all(target_os = "windows", target_arch = "x86_64"))]
    { return UpdatePackage::Installer("block-windows-x64-setup.exe"); }
    #[cfg(all(target_os = "windows", target_arch = "aarch64"))]
    { return UpdatePackage::BuildFromSource; }
    #[cfg(all(target_os = "linux", target_arch = "x86_64"))]
    {
        if linux_from_rpm() {
            return UpdatePackage::BuildFromSource;
        }
        if linux_from_appimage() {
            return UpdatePackage::Installer("block-linux-x64.AppImage");
        }
        return UpdatePackage::Installer("block-linux-x64.deb");
    }
    #[cfg(all(target_os = "linux", target_arch = "aarch64"))]
    { return UpdatePackage::BuildFromSource; }
    #[cfg(not(any(
        all(target_os = "macos", any(target_arch = "aarch64", target_arch = "x86_64")),
        all(target_os = "windows", any(target_arch = "x86_64", target_arch = "aarch64")),
        all(target_os = "linux", any(target_arch = "x86_64", target_arch = "aarch64")),
    )))]
    { return UpdatePackage::BuildFromSource; }
}

#[derive(serde::Deserialize, Clone, Debug)]
struct GhRelease {
    tag_name: String,
    published_at: Option<String>,
    body: Option<String>,
    prerelease: Option<bool>,
    draft: Option<bool>,
    #[serde(default)]
    assets: Vec<GhAsset>,
}

/// Standard FIPS 180-4 SHA-256 implementation (zero external dependency).
pub fn sha256_hex(bytes: &[u8]) -> String {
    let mut h: [u32; 8] = [
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
        0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
    ];

    const K: [u32; 64] = [
        0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
        0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
        0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
        0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
        0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
        0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
        0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
        0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
        0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
        0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
        0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
        0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
        0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
        0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
        0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
        0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
    ];

    let bit_len = (bytes.len() as u64).wrapping_mul(8);
    // Full blocks are hashed in place; only the final partial block is copied and padded.
    let full = bytes.len() / 64 * 64;
    let mut tail = bytes[full..].to_vec();
    tail.push(0x80);
    while (tail.len() % 64) != 56 {
        tail.push(0x00);
    }
    tail.extend_from_slice(&bit_len.to_be_bytes());

    for chunk in bytes[..full].chunks_exact(64).chain(tail.chunks_exact(64)) {
        let mut w = [0u32; 64];
        for i in 0..16 {
            let s = i * 4;
            w[i] = u32::from_be_bytes([chunk[s], chunk[s + 1], chunk[s + 2], chunk[s + 3]]);
        }
        for i in 16..64 {
            let s0 = w[i - 15].rotate_right(7) ^ w[i - 15].rotate_right(18) ^ (w[i - 15] >> 3);
            let s1 = w[i - 2].rotate_right(17) ^ w[i - 2].rotate_right(19) ^ (w[i - 2] >> 10);
            w[i] = w[i - 16].wrapping_add(s0).wrapping_add(w[i - 7]).wrapping_add(s1);
        }

        let mut a = h[0];
        let mut b = h[1];
        let mut c = h[2];
        let mut d = h[3];
        let mut e = h[4];
        let mut f = h[5];
        let mut g = h[6];
        let mut h_val = h[7];

        for i in 0..64 {
            let s1 = e.rotate_right(6) ^ e.rotate_right(11) ^ e.rotate_right(25);
            let ch = (e & f) ^ ((!e) & g);
            let temp1 = h_val
                .wrapping_add(s1)
                .wrapping_add(ch)
                .wrapping_add(K[i])
                .wrapping_add(w[i]);
            let s0 = a.rotate_right(2) ^ a.rotate_right(13) ^ a.rotate_right(22);
            let maj = (a & b) ^ (a & c) ^ (b & c);
            let temp2 = s0.wrapping_add(maj);

            h_val = g;
            g = f;
            f = e;
            e = d.wrapping_add(temp1);
            d = c;
            c = b;
            b = a;
            a = temp1.wrapping_add(temp2);
        }

        h[0] = h[0].wrapping_add(a);
        h[1] = h[1].wrapping_add(b);
        h[2] = h[2].wrapping_add(c);
        h[3] = h[3].wrapping_add(d);
        h[4] = h[4].wrapping_add(e);
        h[5] = h[5].wrapping_add(f);
        h[6] = h[6].wrapping_add(g);
        h[7] = h[7].wrapping_add(h_val);
    }

    format!(
        "{:08x}{:08x}{:08x}{:08x}{:08x}{:08x}{:08x}{:08x}",
        h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7]
    )
}

fn parse_version(s: &str) -> Option<(u64, u64, u64)> {
    let s = s.trim().trim_start_matches(|c| c == 'v' || c == 'V');
    let parts: Vec<&str> = s.split('.').collect();
    if parts.len() < 3 {
        return None;
    }
    let major = parts[0].parse::<u64>().ok()?;
    let minor = parts[1].parse::<u64>().ok()?;
    let patch = parts[2].split(|c: char| !c.is_ascii_digit()).next()?.parse::<u64>().ok()?;
    Some((major, minor, patch))
}

fn looks_chinese(text: &str) -> bool {
    text.contains("## 中文") || text.contains("本次更新重点") || text.contains("### 新功能") || text.contains("✨ 新功能")
}

fn split_zh_en(body: &str) -> (&str, &str) {
    let (first, second) = if let Some((left, right)) = body.split_once("\n---\n") {
        (left.trim(), right.trim())
    } else if let Some((left, right)) = body.split_once("\n---") {
        (left.trim(), right.trim())
    } else {
        return (body.trim(), body.trim());
    };
    if second.is_empty() {
        return (first, first);
    }
    if looks_chinese(second) && !looks_chinese(first) {
        (second, first)
    } else {
        (first, second)
    }
}

fn parse_section(text: &str) -> ReleaseSection {
    let mut highlights = String::new();
    let mut features = Vec::new();
    let mut fixes = Vec::new();

    enum Mode {
        None,
        Features,
        Fixes,
    }
    let mut mode = Mode::None;

    for line in text.lines() {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }

        if (trimmed.starts_with("🌈") || trimmed.contains("本次更新重点") || trimmed.contains("Highlights"))
            && highlights.is_empty()
        {
            let content = trimmed
                .trim_start_matches(|c| c == '🌈' || c == '#' || c == ' ')
                .trim();
            let content = content
                .strip_prefix("本次更新重点：")
                .or_else(|| content.strip_prefix("本次更新重点:"))
                .or_else(|| content.strip_prefix("Highlights:"))
                .or_else(|| content.strip_prefix("Highlights："))
                .unwrap_or(content)
                .trim();
            highlights = content.to_string();
            continue;
        }

        let heading_line = trimmed.starts_with('#')
            || trimmed.starts_with('✨')
            || trimmed.starts_with('🪐')
            || trimmed.starts_with('🐛');
        if heading_line {
            let heading = trimmed.trim_start_matches('#').trim();
            if heading.eq_ignore_ascii_case("Other") || heading.contains("其他") {
                mode = Mode::None;
                continue;
            }
            if heading.eq_ignore_ascii_case("Download")
                || heading.eq_ignore_ascii_case("Install")
                || heading.contains("下载")
                || heading.contains("安装")
                || heading.contains("官网")
                || heading.to_ascii_lowercase().starts_with("website:")
            {
                mode = Mode::None;
                continue;
            }
            if heading.contains("新功能")
                || heading.eq_ignore_ascii_case("New")
                || heading.eq_ignore_ascii_case("✨ New")
                || heading.contains("改进")
                || heading.contains("优化")
                || heading.eq_ignore_ascii_case("Improvements")
                || heading.eq_ignore_ascii_case("Improved")
                || (trimmed.starts_with("✨") && (trimmed.contains("New") || trimmed.contains("新")))
                || trimmed.starts_with("🪐")
            {
                mode = Mode::Features;
                continue;
            }
            if heading.contains("修复")
                || heading.eq_ignore_ascii_case("Fixes")
                || heading.eq_ignore_ascii_case("Fixed")
                || heading.eq_ignore_ascii_case("🐛 Fixes")
                || heading.eq_ignore_ascii_case("🐛 Fixed")
                || (trimmed.starts_with("🐛") && (trimmed.contains("Fix") || trimmed.contains("修")))
            {
                mode = Mode::Fixes;
                continue;
            }
        }

        if let Some(item) = trimmed
            .strip_prefix('-')
            .or_else(|| trimmed.strip_prefix('*'))
            .or_else(|| trimmed.strip_prefix('•'))
        {
            let item = item.trim();
            if !item.is_empty() {
                match mode {
                    Mode::Features => {
                        if !features.contains(&item.to_string()) {
                            features.push(item.to_string());
                        }
                    }
                    Mode::Fixes => {
                        if !fixes.contains(&item.to_string()) {
                            fixes.push(item.to_string());
                        }
                    }
                    Mode::None => {}
                }
            }
        }
    }

    ReleaseSection { highlights, features, fixes }
}

struct ParsedRelease {
    ver: (u64, u64, u64),
    tag: String,
    date: String,
    zh_sec: ReleaseSection,
    en_sec: ReleaseSection,
    assets: Vec<GhAsset>,
}

fn fetch_releases_json() -> Option<Vec<GhRelease>> {
    let api_urls = [
        "https://api.github.com/repos/eachann1024/block/releases?per_page=30",
        "https://ghfast.top/https://api.github.com/repos/eachann1024/block/releases?per_page=30",
        "https://gh-proxy.com/https://api.github.com/repos/eachann1024/block/releases?per_page=30",
    ];

    for url in api_urls {
        let output = Command::new("curl")
            .args(["-sSL", "--connect-timeout", "4", "--max-time", "6", "-H", "User-Agent: Block-App", url])
            .output();
        if let Ok(out) = output
            && out.status.success()
            && !out.stdout.is_empty()
            && let Ok(releases) = serde_json::from_slice::<Vec<GhRelease>>(&out.stdout)
        {
            return Some(releases);
        }
    }
    None
}

fn mock_update_info() -> UpdateInfo {
    let day1 = DayLog {
        date: "2025-05-18".to_string(),
        version_tag: "v1.0.999".to_string(),
        zh: ReleaseSection {
            highlights: "支持原位无感增量更新与实时下载进度显示。".to_string(),
            features: vec![
                "新增后台静默检查更新与侧边栏更新提示图标。".to_string(),
                "双语更新日志弹窗按天汇总改动。".to_string(),
            ],
            fixes: vec![
                "修复切换主题时偶发的界面重绘闪烁问题。".to_string(),
                "修复深色模式下部分图标对比度不足。".to_string(),
            ],
        },
        en: ReleaseSection {
            highlights: "Seamless incremental updates with real-time download progress.".to_string(),
            features: vec![
                "Background silent update check and sidebar update badge.".to_string(),
                "Bilingual changelog modal grouped by day.".to_string(),
            ],
            fixes: vec![
                "Fixed rare redraw flicker when switching appearance.".to_string(),
                "Fixed low contrast for some icons in dark mode.".to_string(),
            ],
        },
    };

    let day2 = DayLog {
        date: "2025-05-17".to_string(),
        version_tag: "v1.0.998".to_string(),
        zh: ReleaseSection {
            highlights: "优化长文档排版与代码块高亮性能。".to_string(),
            features: vec![
                "行内代码块新增快捷复制反馈浮层。".to_string(),
            ],
            fixes: vec![
                "修复按键提示标签在紧凑布局下的对齐偏差。".to_string(),
            ],
        },
        en: ReleaseSection {
            highlights: "Optimized long document layout and code block highlighting performance.".to_string(),
            features: vec![
                "Added instant copy feedback toast for inline code chips.".to_string(),
            ],
            fixes: vec![
                "Fixed alignment offset of keyboard shortcut labels in tight layouts.".to_string(),
            ],
        },
    };

    let asset_name = match update_package() {
        UpdatePackage::Installer(name) => name.to_string(),
        UpdatePackage::BuildFromSource => "block-mock".to_string(),
    };

    UpdateInfo {
        latest_version: "1.0.999".to_string(),
        latest_tag: "v1.0.999".to_string(),
        days: vec![day1, day2],
        download_url: format!("https://github.com/eachann1024/block/releases/download/v1.0.999/{asset_name}"),
        asset_name,
        total_bytes: 4_404_019,
        sha256: None,
        manual_build: false,
    }
}

pub fn check_for_updates() -> bool {
    if std::env::var("BLOCK_MOCK_UPDATE").as_deref() == Ok("1") {
        set_test_update(mock_update_info());
        return true;
    }

    let Some(releases) = fetch_releases_json() else {
        return false;
    };

    let current_ver = parse_version(crate::version::VERSION).unwrap_or((1, 0, 0));
    let mut parsed: Vec<ParsedRelease> = Vec::new();

    for r in releases {
        if r.prerelease == Some(true) || r.draft == Some(true) {
            continue;
        }
        let Some(ver) = parse_version(&r.tag_name) else {
            continue;
        };
        if ver <= current_ver {
            continue;
        }
        let date = r
            .published_at
            .as_deref()
            .and_then(|p| p.get(..10))
            .unwrap_or("unknown")
            .to_string();

        let raw_body = r.body.as_deref().unwrap_or_default();
        let (zh_text, en_text) = split_zh_en(raw_body);
        let zh_sec = parse_section(zh_text);
        let en_sec = parse_section(en_text);

        parsed.push(ParsedRelease {
            ver,
            tag: r.tag_name,
            date,
            zh_sec,
            en_sec,
            assets: r.assets,
        });
    }

    if parsed.is_empty() {
        return false;
    }

    // Sort descending by semantic version
    parsed.sort_by(|a, b| b.ver.cmp(&a.ver));

    let latest = &parsed[0];
    let latest_version = format!("{}.{}.{}", latest.ver.0, latest.ver.1, latest.ver.2);
    let latest_tag = latest.tag.clone();

    // Group by date (aggregate upwards)
    let mut days: Vec<DayLog> = Vec::new();
    for item in &parsed {
        if let Some(existing) = days.iter_mut().find(|d| d.date == item.date) {
            existing.version_tag = format!("{} · {}", existing.version_tag, item.tag);
            if existing.zh.highlights.is_empty() {
                existing.zh.highlights = item.zh_sec.highlights.clone();
            }
            if existing.en.highlights.is_empty() {
                existing.en.highlights = item.en_sec.highlights.clone();
            }
            for f in &item.zh_sec.features {
                if !existing.zh.features.contains(f) {
                    existing.zh.features.push(f.clone());
                }
            }
            for f in &item.zh_sec.fixes {
                if !existing.zh.fixes.contains(f) {
                    existing.zh.fixes.push(f.clone());
                }
            }
            for f in &item.en_sec.features {
                if !existing.en.features.contains(f) {
                    existing.en.features.push(f.clone());
                }
            }
            for f in &item.en_sec.fixes {
                if !existing.en.fixes.contains(f) {
                    existing.en.fixes.push(f.clone());
                }
            }
        } else {
            days.push(DayLog {
                date: item.date.clone(),
                version_tag: item.tag.clone(),
                zh: item.zh_sec.clone(),
                en: item.en_sec.clone(),
            });
        }
    }

    let mut download_url = String::new();
    let mut asset_name = String::new();
    let mut total_bytes = 0;
    let mut sha256 = None;
    let mut manual_build = false;

    match update_package() {
        UpdatePackage::BuildFromSource => manual_build = true,
        UpdatePackage::Installer(installer) => {
            if let Some(asset) = latest.assets.iter().find(|item| item.name == installer) {
                download_url = asset.browser_download_url.clone();
                asset_name = asset.name.clone();
                total_bytes = asset.size;
                sha256 = sha256_from_digest(asset.digest.as_deref());
            } else {
                asset_name = installer.to_string();
                manual_build = true;
            }
        }
    }

    let info = UpdateInfo {
        latest_version,
        latest_tag,
        days,
        download_url,
        asset_name,
        total_bytes,
        sha256,
        manual_build,
    };

    *UPDATE_INFO.lock().unwrap() = Some(info);
    HAS_UPDATE.store(true, Ordering::Relaxed);
    ENTRANCE_DONE.store(false, Ordering::SeqCst);
    true
}

/// Install a synthetic update and arm the badge entrance again.
pub fn set_test_update(info: UpdateInfo) {
    *UPDATE_INFO.lock().unwrap() = Some(info);
    HAS_UPDATE.store(true, Ordering::SeqCst);
    ENTRANCE_DONE.store(false, Ordering::SeqCst);
}

pub fn entrance_done() -> bool {
    ENTRANCE_DONE.load(Ordering::SeqCst)
}

pub fn mark_entrance_done() {
    ENTRANCE_DONE.store(true, Ordering::SeqCst);
}

/// Scheme B entrance clock. Thresholds match the sidebar badge animation.
#[derive(Clone, Copy, Debug)]
pub(crate) struct EntranceFrame {
    pub bubbles: bool,
    pub bubble_t: f32,
    pub bubble_fade: f32,
    pub btn_opacity: f32,
    pub y_offset: f32,
    pub tangle: bool,
    pub tangle_opacity: f32,
    pub jx: f32,
    pub jy: f32,
    pub complete: bool,
}

pub(crate) fn entrance_frame(delta: f32) -> EntranceFrame {
    let bubbles = delta < 0.42;
    let bubble_t = (delta / 0.42).clamp(0.0, 1.0);
    let bubble_fade = (1.0 - bubble_t) * (1.0 - bubble_t);

    let (btn_opacity, y_offset) = if delta < 0.14 {
        (0.0, 8.0)
    } else if delta < 0.42 {
        let t = (delta - 0.14) / (0.42 - 0.14);
        let y = 8.0 * (1.0 - t) - 5.0 * (t * std::f32::consts::PI).sin();
        (t, y)
    } else {
        (1.0, 0.0)
    };

    let mut tangle = false;
    let mut tangle_opacity = 0.0;
    let mut jx = 0.0;
    let mut jy = 0.0;
    if delta >= 0.32 && delta < 1.0 {
        let (opacity, x, y) = if delta < 0.78 {
            let phase = (delta - 0.32) / (0.78 - 0.32);
            let opacity = if delta < 0.38 { (delta - 0.32) / 0.06 } else { 1.0 };
            let x = (phase * std::f32::consts::TAU * 7.0).sin() * 1.5;
            let y = (phase * std::f32::consts::TAU * 9.0).cos() * 1.2;
            (opacity, x, y)
        } else {
            let fade = 1.0 - (delta - 0.78) / (1.0 - 0.78);
            (fade.max(0.0), 0.0, 0.0)
        };
        if opacity > 0.0 {
            tangle = true;
            tangle_opacity = opacity;
            jx = x;
            jy = y;
        }
    }

    EntranceFrame {
        bubbles,
        bubble_t,
        bubble_fade,
        btn_opacity,
        y_offset,
        tangle,
        tangle_opacity,
        jx,
        jy,
        complete: delta >= 0.98,
    }
}

/// One animation tick. The finishing frame latches `entrance_done`.
pub(crate) fn drive_entrance(delta: f32) -> EntranceFrame {
    let frame = entrance_frame(delta);
    if frame.complete {
        mark_entrance_done();
    }
    frame
}

/// `#updater-badge-button` press, without a window notify.
pub(crate) fn accept_badge_click() {
    mark_entrance_done();
    OPEN.store(true, Ordering::Relaxed);
}

/// `None` when `bytes` match `expected_hex`; otherwise the actual digest.
fn sha256_mismatch(bytes: &[u8], expected_hex: &str) -> Option<String> {
    let actual = sha256_hex(bytes).to_lowercase();
    if actual == expected_hex.to_lowercase() { None } else { Some(actual) }
}

pub fn init(cx: &mut App) {
    cx.spawn(async move |cx| {
        // Delay 1s after launch to avoid competing with main window startup
        cx.background_executor().timer(Duration::from_secs(1)).await;
        let found = cx.background_executor().spawn(async move {
            check_for_updates()
        }).await;
        if found {
            let _ = cx.update(|cx| {
                if let Some((_, workspace)) = crate::search::window::main_window(cx) {
                    workspace.update(cx, |_, cx| cx.notify());
                }
            });
        }
    }).detach();
}

pub fn has_update() -> bool {
    HAS_UPDATE.load(Ordering::Relaxed)
}

pub fn open(cx: &mut Context<Workspace>) {
    accept_badge_click();
    cx.notify();
}

pub fn close_modal() -> bool {
    OPEN.swap(false, Ordering::Relaxed)
}

pub fn cancel_and_close() {
    CANCELLED.store(true, Ordering::SeqCst);
    DOWNLOADING.store(false, Ordering::SeqCst);
    OPEN.store(false, Ordering::Relaxed);
    if let Ok(mut prog) = PROGRESS.lock() {
        prog.active = false;
    }
}

fn download_file_with_fallback(
    urls: &[String],
    dest: &Path,
    total_bytes: u64,
    progress_tx: &std::sync::mpsc::Sender<DownloadProgress>,
) -> bool {
    for url in urls {
        if CANCELLED.load(Ordering::SeqCst) {
            let _ = std::fs::remove_file(dest);
            return false;
        }

        let _ = std::fs::remove_file(dest);
        let dest_str = dest.to_string_lossy().into_owned();

        let mut child = match Command::new("curl")
            .args(["-fsSL", "--connect-timeout", "4", "--max-time", "180", "-o", &dest_str, url])
            .spawn()
        {
            Ok(c) => c,
            Err(_) => continue,
        };

        loop {
            std::thread::sleep(Duration::from_millis(100));
            if CANCELLED.load(Ordering::SeqCst) {
                let _ = child.kill();
                let _ = child.wait();
                let _ = std::fs::remove_file(dest);
                return false;
            }

            match child.try_wait() {
                Ok(Some(status)) => {
                    if status.success() {
                        let size = std::fs::metadata(dest).map(|m| m.len()).unwrap_or(0);
                        if size > 0 {
                            let prog = DownloadProgress {
                                downloaded_bytes: size,
                                total_bytes: if total_bytes > 0 { total_bytes } else { size },
                                pct: 100,
                                active: true,
                            };
                            if let Ok(mut p) = PROGRESS.lock() {
                                *p = prog.clone();
                            }
                            let _ = progress_tx.send(prog);
                            return true;
                        }
                    }
                    break;
                }
                Ok(None) => {
                    let size = std::fs::metadata(dest).map(|m| m.len()).unwrap_or(0);
                    let target_total = if total_bytes > 0 { total_bytes } else { (size + 1024 * 1024).max(size) };
                    let pct = if target_total > 0 {
                        ((size as f64 / target_total as f64) * 100.0).min(99.0) as u8
                    } else {
                        0
                    };
                    let prog = DownloadProgress {
                        downloaded_bytes: size,
                        total_bytes: target_total,
                        pct,
                        active: true,
                    };
                    if let Ok(mut p) = PROGRESS.lock() {
                        *p = prog.clone();
                    }
                    let _ = progress_tx.send(prog);
                }
                Err(_) => break,
            }
        }
    }
    false
}

fn perform_update(
    info: UpdateInfo,
    progress_tx: std::sync::mpsc::Sender<DownloadProgress>,
) -> Result<(), String> {
    if std::env::var("BLOCK_MOCK_UPDATE").as_deref() == Ok("1") {
        let total_bytes: u64 = 4_404_019;
        for step in 1..=20 {
            if CANCELLED.load(Ordering::SeqCst) {
                return Err("Cancelled".to_string());
            }
            let pct = (step * 5) as u8;
            let downloaded_bytes = (total_bytes * step as u64) / 20;
            let prog = DownloadProgress {
                downloaded_bytes,
                total_bytes,
                pct,
                active: true,
            };
            if let Ok(mut p) = PROGRESS.lock() {
                *p = prog.clone();
            }
            let _ = progress_tx.send(prog);
            std::thread::sleep(Duration::from_millis(60));
        }
        return Ok(());
    }

    let temp_dir = std::env::temp_dir();
    let temp_asset = temp_dir.join(format!("block-update-{}-{}", std::process::id(), info.asset_name));

    let download_candidates = [
        info.download_url.clone(),
        format!("https://ghfast.top/{}", info.download_url),
        format!("https://gh-proxy.com/{}", info.download_url),
        format!("https://mirror.ghproxy.com/{}", info.download_url),
    ];

    let ok = download_file_with_fallback(&download_candidates, &temp_asset, info.total_bytes, &progress_tx);
    if !ok {
        let _ = std::fs::remove_file(&temp_asset);
        return Err("Download failed or cancelled".to_string());
    }

    if CANCELLED.load(Ordering::SeqCst) {
        let _ = std::fs::remove_file(&temp_asset);
        return Err("Cancelled".to_string());
    }

    if let Some(expected) = info.sha256.as_deref() {
        let downloaded_bytes = std::fs::read(&temp_asset).map_err(|e| e.to_string())?;
        if let Some(actual_hash) = sha256_mismatch(&downloaded_bytes, expected) {
            let _ = std::fs::remove_file(&temp_asset);
            return Err(format!("SHA-256 mismatch: expected {expected}, got {actual_hash}"));
        }
    }

    #[cfg(target_os = "macos")]
    {
        let _ = Command::new("open").arg(&temp_asset).spawn();
    }
    #[cfg(target_os = "windows")]
    {
        let _ = Command::new(&temp_asset).spawn();
        std::process::exit(0);
    }
    #[cfg(target_os = "linux")]
    {
        if info.asset_name.ends_with(".deb") {
            let status = Command::new("xdg-open")
                .arg(&temp_asset)
                .status()
                .map_err(|err| format!("Could not open the deb installer: {err}"))?;
            if !status.success() {
                return Err("Could not open the deb installer".to_string());
            }
        } else {
            use std::os::unix::fs::PermissionsExt;
            let _ = std::fs::set_permissions(&temp_asset, std::fs::Permissions::from_mode(0o755));
            let _ = Command::new(&temp_asset).args(std::env::args().skip(1)).spawn();
            std::process::exit(0);
        }
    }

    Ok(())
}

pub fn start_update(cx: &mut Context<Workspace>) {
    if DOWNLOADING.swap(true, Ordering::SeqCst) {
        return;
    }
    CANCELLED.store(false, Ordering::SeqCst);

    let info = {
        let lock = UPDATE_INFO.lock().unwrap();
        lock.clone()
    };

    let Some(info) = info else {
        DOWNLOADING.store(false, Ordering::SeqCst);
        return;
    };
    if info.manual_build || info.download_url.is_empty() {
        DOWNLOADING.store(false, Ordering::SeqCst);
        return;
    };

    if let Ok(mut prog) = PROGRESS.lock() {
        prog.active = true;
        prog.pct = 0;
        prog.downloaded_bytes = 0;
        prog.total_bytes = info.total_bytes;
    }
    cx.notify();

    let (tx, rx) = std::sync::mpsc::channel();
    let (res_tx, res_rx) = std::sync::mpsc::channel();

    cx.spawn(async move |workspace, cx| {
        let _handle = cx.background_executor().spawn(async move {
            let res = perform_update(info, tx);
            let _ = res_tx.send(res);
        });

        loop {
            let mut updated = false;
            while let Ok(prog) = rx.try_recv() {
                if let Ok(mut p) = PROGRESS.lock() {
                    *p = prog;
                }
                updated = true;
            }
            if updated {
                let _ = cx.update(|cx| {
                    let _ = workspace.update(cx, |_, cx| cx.notify());
                });
            }

            if let Ok(res) = res_rx.try_recv() {
                DOWNLOADING.store(false, Ordering::SeqCst);
                if let Ok(mut p) = PROGRESS.lock() {
                    p.active = false;
                }
                let _ = cx.update(|cx| {
                    let _ = workspace.update(cx, |_, cx| cx.notify());
                });
                if let Err(err) = res {
                    crate::log::error(&format!("Update error: {err}"));
                }
                break;
            }

            cx.background_executor().timer(Duration::from_millis(60)).await;
        }
    }).detach();
}

pub fn view(cx: &mut Context<Workspace>) -> Option<AnyElement> {
    if !OPEN.load(Ordering::Relaxed) {
        return None;
    }

    let info = {
        let lock = UPDATE_INFO.lock().unwrap();
        lock.clone()
    }?;

    let progress = {
        let lock = PROGRESS.lock().unwrap();
        lock.clone()
    };

    let theme = Theme::ocean();
    let is_en = crate::theme::is_en();

    let mut day_elements = Vec::new();
    for (i, day) in info.days.iter().enumerate() {
        if i > 0 {
            day_elements.push(div().w_full().h(px(1.)).bg(theme.hairline).into_any_element());
        }

        let sec = if is_en { &day.en } else { &day.zh };
        let hl_prefix = if is_en { "🌈 Highlights: " } else { "🌈 本次更新重点：" };
        let feat_header = if is_en { "✨ New" } else { "✨ 新功能" };
        let fix_header = if is_en { "🐛 Fixes" } else { "🐛 修复" };

        let mut day_div = div()
            .w_full()
            .flex()
            .flex_col()
            .gap(px(8.))
            .child(
                div()
                    .flex()
                    .items_baseline()
                    .justify_between()
                    .child(
                        div()
                            .text_size(px(18.))
                            .font_weight(FontWeight(600.))
                            .text_color(theme.text)
                            .child(day.date.clone()),
                    )
                    .child(
                        div()
                            .font_family("JetBrains Mono")
                            .text_size(px(14.))
                            .text_color(theme.text_secondary)
                            .child(day.version_tag.clone()),
                    ),
            );

        if !sec.highlights.is_empty() {
            day_div = day_div.child(
                div()
                    .text_size(px(15.))
                    .font_weight(FontWeight(600.))
                    .text_color(theme.text)
                    .child(format!("{hl_prefix}{}", sec.highlights)),
            );
        }

        if !sec.features.is_empty() {
            let mut feat_list = div().flex().flex_col().gap(px(4.)).child(
                div()
                    .text_size(px(14.))
                    .font_weight(FontWeight(600.))
                    .text_color(theme.text)
                    .child(feat_header),
            );
            for item in &sec.features {
                feat_list = feat_list.child(
                    div()
                        .flex()
                        .items_start()
                        .gap(px(6.))
                        .text_size(px(14.))
                        .line_height(px(20.))
                        .text_color(theme.text)
                        .child(div().text_color(theme.text_secondary).child("•"))
                        .child(div().flex_1().child(item.clone())),
                );
            }
            day_div = day_div.child(feat_list);
        }

        if !sec.fixes.is_empty() {
            let mut fix_list = div().flex().flex_col().gap(px(4.)).child(
                div()
                    .text_size(px(14.))
                    .font_weight(FontWeight(600.))
                    .text_color(theme.text)
                    .child(fix_header),
            );
            for item in &sec.fixes {
                fix_list = fix_list.child(
                    div()
                        .flex()
                        .items_start()
                        .gap(px(6.))
                        .text_size(px(14.))
                        .line_height(px(20.))
                        .text_color(theme.text)
                        .child(div().text_color(theme.text_secondary).child("•"))
                        .child(div().flex_1().child(item.clone())),
                );
            }
            day_div = day_div.child(fix_list);
        }

        day_elements.push(day_div.into_any_element());
    }

    let scroll_content = div()
        .id("updater-changelog-scroll")
        .w_full()
        .max_h(px(380.))
        .overflow_y_scroll()
        .flex()
        .flex_col()
        .gap(px(16.))
        .children(day_elements);

    let progress_section = if progress.active {
        let downloaded_mb = progress.downloaded_bytes as f64 / (1024.0 * 1024.0);
        let total_mb = progress.total_bytes as f64 / (1024.0 * 1024.0);
        let left_text = if is_en {
            format!("Downloading update v{} ({:.1} MB / {:.1} MB)", info.latest_version, downloaded_mb, total_mb)
        } else {
            format!("正在下载更新 v{}（{:.1} MB / {:.1} MB）", info.latest_version, downloaded_mb, total_mb)
        };
        let right_text = format!("{}%", progress.pct);
        let progress_label = left_text.clone();

        Some(
            div()
                .w_full()
                .flex()
                .flex_col()
                .gap(px(6.))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .justify_between()
                        .child(
                            div()
                                .text_size(px(14.))
                                .text_color(theme.text)
                                .child(left_text),
                        )
                        .child(
                            div()
                                .font_family("JetBrains Mono")
                                .text_size(px(14.))
                                .text_color(theme.text_secondary)
                                .child(right_text),
                        ),
                )
                .child(crate::ui::progress::linear(
                    "updater-progress",
                    progress.pct as f32,
                    progress_label,
                )),
        )
    } else {
        None
    };

    let manual_note = if info.manual_build {
        Some(
            div()
                .flex_1()
                .text_size(px(14.))
                .text_color(theme.text)
                .child(if is_en {
                    format!("No installer is published for this system. Clone the source and build it yourself: {SOURCE_URL}")
                } else {
                    format!("请到 GitHub 拉取源码自行编译：{SOURCE_URL}")
                }),
        )
    } else {
        None
    };

    let actions = div()
        .w_full()
        .flex()
        .items_center()
        .justify_end()
        .gap(px(10.))
        .children(manual_note)
        .child(
            gpui_base::Button::new("updater-cancel")
                .focusable(false)
                .h(px(32.))
                .px(px(14.))
                .rounded(crate::ui::radius::CONTROL)
                .bg(theme.paper)
                .border_1()
                .border_color(theme.btn_border)
                .text_color(theme.text)
                .text_size(px(14.))
                .line_height(px(20.))
                .flex()
                .items_center()
                .justify_center()
                .cursor(gpui::CursorStyle::PointingHand)
                .hover(|s| s.bg(theme.hover))
                .on_mouse_down(gpui::MouseButton::Left, cx.listener(|this, _, window, cx| {
                    cancel_and_close();
                    this.restore_focus(window, cx);
                    cx.notify();
                }))
                .child(tr("先不更新", "Not Now")),
        )
        .when(!info.manual_build, |row| row.child(
            gpui_base::Button::new("updater-submit")
                .focusable(false)
                .disabled(progress.active)
                .h(px(32.))
                .px(px(16.))
                .rounded(crate::ui::radius::CONTROL)
                .bg(theme.accent)
                .text_color(gpui::white())
                .text_size(px(14.))
                .line_height(px(20.))
                .font_weight(FontWeight(500.))
                .flex()
                .items_center()
                .justify_center()
                .cursor(if progress.active { gpui::CursorStyle::Arrow } else { gpui::CursorStyle::PointingHand })
                .when(!progress.active, |b| b.hover(|s| s.opacity(0.9)))
                .when(progress.active, |b| b.opacity(0.7))
                .on_mouse_down(gpui::MouseButton::Left, cx.listener(|_, _, _, cx| {
                    start_update(cx);
                }))
                .child(if progress.active { tr("正在更新…", "Updating…") } else { tr("更新", "Update") }),
        ));

    let bottom_section = div()
        .w_full()
        .pt(px(14.))
        .border_t_1()
        .border_color(theme.hairline)
        .flex()
        .flex_col()
        .gap(px(12.))
        .children(progress_section)
        .child(actions);

    let card = modal::card("updater-card", px(580.))
        .relative()
        .pt(px(24.))
        .px(px(24.))
        .pb(px(20.))
        .gap(px(16.))
        .text_size(px(14.))
        .line_height(px(20.))
        .child(scroll_content)
        .child(bottom_section);

    Some(
        gpui::deferred(
            modal::backdrop("updater-overlay", card).on_mouse_down(
                MouseButton::Left,
                cx.listener(|this, _, window, cx| {
                    close_modal();
                    this.restore_focus(window, cx);
                    cx.notify();
                }),
            ),
        )
        .with_priority(6)
        .into_any_element(),
    )
}

