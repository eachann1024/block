//! Ocean paper theme (design A) in Light and Dark, plus process-wide appearance,
//! typography, font-smoothing and shortcut settings backed by `state.json`.

use std::process::Command;
use std::sync::{Mutex, OnceLock};
use std::sync::atomic::{AtomicBool, AtomicU8, Ordering};
use gpui::{Hsla, Pixels, px};
pub use crate::files::state::{FontChoice, LanguageMode, ThemeMode};
use crate::icons::IconColors;
pub use crate::ui::tooltip::{build_tooltip, tooltip};

#[cfg(target_os = "macos")]
pub const DEFAULT_SEARCH_SHORTCUT: &str = "cmd-shift-k";
#[cfg(not(target_os = "macos"))]
pub const DEFAULT_SEARCH_SHORTCUT: &str = "ctrl-shift-k";
pub const DEFAULT_QUICK_SHORTCUT: &str = "alt-n";

static THEME_MODE: AtomicU8 = AtomicU8::new(0);
static IS_DARK: AtomicBool = AtomicBool::new(false);
static FONT_CHOICE: AtomicU8 = AtomicU8::new(0);
static SMOOTHING: AtomicBool = AtomicBool::new(true);
static SHORTCUTS: Mutex<Option<(String, String)>> = Mutex::new(None);
static LANGUAGE_MODE: AtomicU8 = AtomicU8::new(0);
static IS_EN: AtomicBool = AtomicBool::new(false);

fn system_is_en() -> bool {
    if let Ok(out) = Command::new("defaults").args(["read", "-g", "AppleLanguages"]).output() {
        let s = String::from_utf8_lossy(&out.stdout).to_ascii_lowercase();
        for line in s.lines() {
            let t = line.trim().trim_matches(|c: char| c == '"' || c == ',' || c == '(' || c == ')').trim();
            if t.starts_with("zh") { return false; }
            if t.starts_with("en") { return true; }
        }
    }
    std::env::var("LC_ALL").or_else(|_| std::env::var("LANG"))
        .map(|v| v.to_ascii_lowercase().starts_with("en")).unwrap_or(false)
}

pub fn language_mode() -> LanguageMode {
    match LANGUAGE_MODE.load(Ordering::Relaxed) { 1 => LanguageMode::Zh, 2 => LanguageMode::En, _ => LanguageMode::System }
}

pub fn set_language_mode(mode: LanguageMode) {
    let (code, en) = match mode {
        LanguageMode::System => (0, system_is_en()),
        LanguageMode::Zh => (1, false),
        LanguageMode::En => (2, true),
    };
    LANGUAGE_MODE.store(code, Ordering::Relaxed);
    IS_EN.store(en, Ordering::Relaxed);
}

pub fn is_en() -> bool { IS_EN.load(Ordering::Relaxed) }
pub fn tr(zh: &'static str, en: &'static str) -> &'static str { if is_en() { en } else { zh } }

fn system_is_dark() -> bool {
    Command::new("defaults")
        .args(["read", "-g", "AppleInterfaceStyle"])
        .output()
        .is_ok_and(|out| String::from_utf8_lossy(&out.stdout).trim().eq_ignore_ascii_case("dark"))
}

pub fn apply_state(state: &crate::files::state::State) {
    set_language_mode(state.language);
    set_theme_mode(state.theme);
    set_font_choice(state.font_family);
    set_font_smoothing(state.font_smoothing.unwrap_or(true));
    let search = state.shortcut_search.as_deref()
        .filter(|s| !s.eq_ignore_ascii_case("cmd-k"))
        .unwrap_or(DEFAULT_SEARCH_SHORTCUT)
        .to_owned();
    let quick = state.shortcut_quick.clone().unwrap_or_else(|| DEFAULT_QUICK_SHORTCUT.into());
    set_shortcuts(search, quick);
}

pub fn theme_mode() -> ThemeMode {
    match THEME_MODE.load(Ordering::Relaxed) { 1 => ThemeMode::Dark, 2 => ThemeMode::System, _ => ThemeMode::Light }
}

pub fn set_theme_mode(mode: ThemeMode) {
    let code = match mode { ThemeMode::Light => 0, ThemeMode::Dark => 1, ThemeMode::System => 2 };
    THEME_MODE.store(code, Ordering::Relaxed);
    let dark = match mode { ThemeMode::Light => false, ThemeMode::Dark => true, ThemeMode::System => system_is_dark() };
    IS_DARK.store(dark, Ordering::Relaxed);
}

pub fn is_dark() -> bool { IS_DARK.load(Ordering::Relaxed) }

pub fn font_choice() -> FontChoice {
    match FONT_CHOICE.load(Ordering::Relaxed) { 1 => FontChoice::Serif, _ => FontChoice::Sans }
}

pub fn set_font_choice(font: FontChoice) {
    FONT_CHOICE.store(match font { FontChoice::Sans => 0, FontChoice::Serif => 1 }, Ordering::Relaxed);
}

/// Resolved (serif, mono) primary families, picked from the installed font names.
static RESOLVED_FONTS: OnceLock<(&'static str, &'static str)> = OnceLock::new();

/// Picks the first installed candidate. gpui drops fallbacks and weight when the primary
/// family is missing, and naming "PingFang SC" as a fallback makes CoreText resolve the
/// duplicate PingFangUI.ttc (same PostScript names, different glyph IDs) and garble CJK.
pub fn init_fonts(installed: &[String]) {
    let pick = |candidates: &[&'static str], last: &'static str| {
        candidates.iter().copied()
            .find(|c| installed.iter().any(|n| n == c))
            .unwrap_or(last)
    };
    let serif = pick(&["LXGW WenKai", "LXGW WenKai Screen", "Kaiti SC", "Songti SC"], "Songti SC");
    let mono = pick(&["JetBrains Mono", "Menlo", "Monaco"], "Menlo");
    let _ = RESOLVED_FONTS.set((serif, mono));
}

fn resolved() -> (&'static str, &'static str) {
    RESOLVED_FONTS.get().copied().unwrap_or(("Songti SC", "Menlo"))
}

pub fn mono_font_family() -> &'static str { resolved().1 }

pub fn editor_font() -> (&'static str, &'static [&'static str]) {
    let (serif, _) = resolved();
    match font_choice() {
        FontChoice::Sans => (".SystemUIFont", &[]),
        FontChoice::Serif if serif == "Songti SC" => (serif, &[]),
        FontChoice::Serif => (serif, &["Songti SC"]),
    }
}

pub fn set_font_smoothing(on: bool) {
    SMOOTHING.store(on, Ordering::Relaxed);
    let val = if on { "2" } else { "0" };
    std::thread::spawn(move || {
        let _ = Command::new("defaults")
            .args(["write", "app.thinkly.fast", "AppleFontSmoothing", "-int", val])
            .status();
    });
}

pub fn search_shortcut() -> String {
    SHORTCUTS.lock().unwrap_or_else(|e| e.into_inner())
        .as_ref().map(|(s, _)| s.clone()).unwrap_or_else(|| DEFAULT_SEARCH_SHORTCUT.into())
}

pub fn quick_shortcut() -> String {
    SHORTCUTS.lock().unwrap_or_else(|e| e.into_inner())
        .as_ref().map(|(_, q)| q.clone()).unwrap_or_else(|| DEFAULT_QUICK_SHORTCUT.into())
}

pub fn set_shortcuts(search: String, quick: String) {
    *SHORTCUTS.lock().unwrap_or_else(|e| e.into_inner()) = Some((search, quick));
}

pub fn hotkey_parts(spec: &str) -> Vec<String> {
    let (mut ctrl, mut alt, mut shift, mut cmd, mut secondary, mut key) =
        (false, false, false, false, false, String::new());
    for part in spec.split('-').filter(|s| !s.is_empty()) {
        match part.to_ascii_lowercase().as_str() {
            "ctrl" | "control" => ctrl = true,
            "alt" | "opt" | "option" => alt = true,
            "shift" => shift = true,
            "cmd" | "super" | "meta" | "win" => cmd = true,
            "secondary" => secondary = true,
            "space" => key = "Space".into(),
            "comma" => key = ",".into(),
            "period" => key = ".".into(),
            "slash" => key = "/".into(),
            "minus" => key = "-".into(),
            "equal" => key = "=".into(),
            other => key = other.to_ascii_uppercase(),
        }
    }
    if spec.ends_with("--") { key = "-".into(); }
    let mut out = Vec::new();
    if cfg!(target_os = "macos") {
        if ctrl { out.push("⌃".into()); }
        if alt { out.push("⌥".into()); }
        if shift { out.push("⇧".into()); }
        if cmd || secondary { out.push("⌘".into()); }
    } else {
        if ctrl || secondary { out.push("Ctrl".into()); }
        if shift { out.push("Shift".into()); }
        if alt { out.push("Alt".into()); }
        if cmd {
            out.push(if cfg!(target_os = "windows") { "Win".into() } else { "Super".into() });
        }
    }
    if !key.is_empty() { out.push(key); }
    out
}

pub fn format_hotkey(spec: &str) -> String {
    let parts = hotkey_parts(spec);
    if cfg!(target_os = "macos") { parts.join(" ") } else { parts.join(" + ") }
}

const fn hex(value: u32) -> Hsla {
    let r = ((value >> 16) & 0xff) as f32 / 255.0;
    let g = ((value >> 8) & 0xff) as f32 / 255.0;
    let b = (value & 0xff) as f32 / 255.0;
    let max = if r >= g && r >= b { r } else if g >= b { g } else { b };
    let min = if r <= g && r <= b { r } else if g <= b { g } else { b };
    let (l, d) = ((max + min) / 2.0, max - min);
    let s = if d == 0.0 { 0.0 } else if l > 0.5 { d / (2.0 - max - min) } else { d / (max + min) };
    let h = if d == 0.0 { 0.0 } else if max == r { ((g - b) / d + if g < b { 6.0 } else { 0.0 }) / 6.0 }
        else if max == g { ((b - r) / d + 2.0) / 6.0 } else { ((r - g) / d + 4.0) / 6.0 };
    Hsla { h, s, l, a: 1.0 }
}

const OCEAN_LIGHT: Theme = Theme {
    shell: hex(0xf1f1f1), sidebar: hex(0xfafafa), settings_bg: hex(0xf8f8f8),
    paper: hex(0xffffff), menu: hex(0xffffff), text: hex(0x1f1e1d),
    text_secondary: hex(0x52514e), accent: hex(0x547dd0), icon_fill: hex(0xc9d8f3),
    hover: hex(0xeef3fc), selected: hex(0xe5edfc), hairline: hex(0xe4e6ea),
    code_bg: hex(0xf3f1ec), quote_bar: hex(0xdbd9d4), primary: hex(0x2f3540),
    primary_hover: hex(0x3b4250), btn_border: hex(0xdcdcdc), btn_hover: hex(0xf7f7f7),
    menu_border: hex(0xe4e4e4), sidebar_border: hex(0xe6e6e6), preview_bg: hex(0xe9e9e9),
    selection: hex(0xcde2fb), selection_text: hex(0x184f95), menu_item_height: px(32.),
    sidebar_width: px(260.), content_width: px(672.), editor_font: px(16.), ui_font: px(14.),
};

const OCEAN_DARK: Theme = Theme {
    shell: hex(0x343434), sidebar: hex(0x222222), settings_bg: hex(0x181818),
    paper: hex(0x181818), menu: hex(0x222222), text: hex(0xececec),
    text_secondary: hex(0xb4b4b4), accent: hex(0x779de8), icon_fill: hex(0x31456a),
    hover: hex(0x2c2c2c), selected: hex(0x343434), hairline: hex(0x2e2e2e),
    code_bg: hex(0x242424), quote_bar: hex(0x484848), primary: hex(0x445066),
    primary_hover: hex(0x52607a), btn_border: hex(0x444444), btn_hover: hex(0x2c2c2c),
    menu_border: hex(0x383838), sidebar_border: hex(0x333333), preview_bg: hex(0x262626),
    selection: hex(0x2d426b), selection_text: hex(0x9dbcf5), menu_item_height: px(32.),
    sidebar_width: px(260.), content_width: px(672.), editor_font: px(16.), ui_font: px(14.),
};

#[derive(Clone, Copy)]
pub struct Theme {
    pub shell: Hsla, pub sidebar: Hsla, pub settings_bg: Hsla, pub paper: Hsla,
    pub menu: Hsla, pub text: Hsla, pub text_secondary: Hsla, pub accent: Hsla,
    pub icon_fill: Hsla, pub hover: Hsla, pub selected: Hsla, pub hairline: Hsla,
    pub code_bg: Hsla, pub quote_bar: Hsla, pub primary: Hsla, pub primary_hover: Hsla,
    pub btn_border: Hsla, pub btn_hover: Hsla, pub menu_border: Hsla, pub sidebar_border: Hsla,
    pub preview_bg: Hsla, pub selection: Hsla, pub selection_text: Hsla,
    pub menu_item_height: Pixels, pub sidebar_width: Pixels, pub content_width: Pixels,
    pub editor_font: Pixels, pub ui_font: Pixels,
}

impl Theme {
    /// Icon tints: `line` is the stroke color the caller wants.
    pub fn icon_colors(&self, line: Hsla) -> IconColors {
        IconColors { line, fill: self.icon_fill, accent: self.accent }
    }

    pub fn ocean() -> Self {
        if is_dark() { OCEAN_DARK } else { OCEAN_LIGHT }
    }
}

