//! Non-activating macOS `NSPanel` configuration so the Quick Note window operates
//! independently without waking, unhiding, or focusing the main window.
use std::sync::{OnceLock, atomic::{AtomicU64, Ordering}};
use std::time::Instant;
use global_hotkey::hotkey::{Code, HotKey, Modifiers};

static START_INSTANT: OnceLock<Instant> = OnceLock::new();
static LAST_QUICK_MS: AtomicU64 = AtomicU64::new(0);
static LAST_SEARCH_MS: AtomicU64 = AtomicU64::new(0);

fn now_ms() -> u64 {
    START_INSTANT.get_or_init(Instant::now).elapsed().as_millis() as u64 + 1000
}

pub(crate) fn mark_quick_triggered() -> bool {
    let now = now_ms();
    let prev = LAST_QUICK_MS.swap(now, Ordering::Relaxed);
    now.saturating_sub(prev) >= 250
}

pub(crate) fn record_search_triggered() {
    LAST_SEARCH_MS.store(now_ms(), Ordering::Relaxed);
}

pub(crate) fn mark_search_triggered() -> bool {
    let now = now_ms();
    let prev = LAST_SEARCH_MS.swap(now, Ordering::Relaxed);
    now.saturating_sub(prev) >= 250
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
fn app_windows(app: cocoa::base::id) -> cocoa::base::id {
    use cocoa::base::{SEL, id, selector};
    unsafe extern "C" {
        fn objc_msgSend();
    }
    unsafe {
        let send: unsafe extern "C" fn(id, SEL) -> id = std::mem::transmute(objc_msgSend as *const ());
        send(app, selector("windows"))
    }
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(crate) unsafe fn is_quick_panel(win: cocoa::base::id) -> bool {
    use cocoa::base::{BOOL, SEL, YES, id, selector};
    unsafe extern "C" { fn objc_msgSend(); fn objc_getClass(name: *const std::ffi::c_char) -> id; }
    unsafe {
        let kind: unsafe extern "C" fn(id, SEL, id) -> BOOL = std::mem::transmute(objc_msgSend as *const ());
        let mask: unsafe extern "C" fn(id, SEL) -> usize = std::mem::transmute(objc_msgSend as *const ());
        kind(win, selector("isKindOfClass:"), objc_getClass(c"GPUIPanel".as_ptr())) == YES && (mask(win, selector("styleMask")) & 2) != 0
    }
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(super) fn configure_quick_panel() {
    use cocoa::appkit::{NSApplication, NSPanel, NSWindow};
    use cocoa::base::{NO, SEL, YES, id, nil, selector};
    use cocoa::foundation::NSArray;
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let app = NSApplication::sharedApplication(nil);
        let windows = app_windows(app);
        let send_void: unsafe extern "C" fn(id, SEL) = std::mem::transmute(objc_msgSend as *const ());
        for i in 0..NSArray::count(windows) {
            let win = NSArray::objectAtIndex(windows, i);
            if is_quick_panel(win) {
                super::panel_class::install(win.cast());
                NSWindow::setHidesOnDeactivate_(win, NO);
                NSWindow::setCanHide_(win, NO);
                NSPanel::setFloatingPanel(win, YES);
                NSPanel::setBecomesKeyOnlyIfNeeded(win, NO);
                super::ime_bridge::install_ime_bridge(win as *mut std::ffi::c_void);
                crate::window_visibility::prepare_background_window(win);
                send_void(win, selector("orderFrontRegardless"));
                if !crate::smoke::is_worker() { send_void(win, selector("makeKeyWindow")); }
            }
        }
    }
}

#[cfg(not(target_os = "macos"))]
pub(super) fn configure_quick_panel() {}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(crate) fn order_out_quick_panel() {
    use cocoa::appkit::{NSApplication, NSWindow};
    use cocoa::base::{SEL, id, nil, selector};
    use cocoa::foundation::NSArray;
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let app = NSApplication::sharedApplication(nil);
        let windows = app_windows(app);
        let resign: unsafe extern "C" fn(id, SEL) = std::mem::transmute(objc_msgSend as *const ());
        for i in 0..NSArray::count(windows) {
            let win = NSArray::objectAtIndex(windows, i);
            if is_quick_panel(win) {
                resign(win, selector("resignKeyWindow"));
                NSWindow::orderOut_(win, nil);
            }
        }
    }
}

#[cfg(not(target_os = "macos"))]
pub(crate) fn order_out_quick_panel() {}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(crate) fn is_quick_panel_active() -> bool {
    use cocoa::{appkit::NSApplication, base::{BOOL, SEL, YES, id, nil, selector}, foundation::NSArray};
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let windows = app_windows(NSApplication::sharedApplication(nil));
        let flag: unsafe extern "C" fn(id, SEL) -> BOOL = std::mem::transmute(objc_msgSend as *const ());
        (0..NSArray::count(windows)).any(|i| {
            let win = NSArray::objectAtIndex(windows, i);
            is_quick_panel(win) && flag(win, selector("isVisible")) == YES && flag(win, selector("isKeyWindow")) == YES
        })
    }
}

#[cfg(not(target_os = "macos"))]
pub(crate) fn is_quick_panel_active() -> bool { false }

#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(crate) fn is_quick_panel_visible() -> bool {
    use cocoa::{appkit::NSApplication, base::{BOOL, SEL, YES, id, nil, selector}, foundation::NSArray};
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let windows = app_windows(NSApplication::sharedApplication(nil));
        let flag: unsafe extern "C" fn(id, SEL) -> BOOL = std::mem::transmute(objc_msgSend as *const ());
        (0..NSArray::count(windows)).any(|i| {
            let win = NSArray::objectAtIndex(windows, i);
            is_quick_panel(win) && flag(win, selector("isVisible")) == YES
        })
    }
}

#[cfg(not(target_os = "macos"))]
pub(crate) fn is_quick_panel_visible() -> bool { false }

pub fn parse_hotkey(spec: &str) -> Option<HotKey> {
    let mut mods = Modifiers::empty();
    let mut code = None;
    for part in spec.split('-').filter(|s| !s.is_empty()) {
        match part.to_ascii_lowercase().as_str() {
            "ctrl" | "control" => mods |= Modifiers::CONTROL,
            "alt" | "opt" | "option" => mods |= Modifiers::ALT,
            "shift" => mods |= Modifiers::SHIFT,
            "cmd" | "super" | "meta" => mods |= Modifiers::META,
            "space" => code = Some(Code::Space),
            "comma" | "," => code = Some(Code::Comma),
            "period" | "." => code = Some(Code::Period),
            "slash" | "/" => code = Some(Code::Slash),
            "semicolon" | ";" => code = Some(Code::Semicolon),
            "quote" | "'" => code = Some(Code::Quote),
            "minus" => code = Some(Code::Minus),
            "equal" | "=" => code = Some(Code::Equal),
            "backquote" | "`" => code = Some(Code::Backquote),
            s if s.len() == 1 => {
                code = match s.as_bytes()[0] {
                    b'a' => Some(Code::KeyA), b'b' => Some(Code::KeyB), b'c' => Some(Code::KeyC), b'd' => Some(Code::KeyD),
                    b'e' => Some(Code::KeyE), b'f' => Some(Code::KeyF), b'g' => Some(Code::KeyG), b'h' => Some(Code::KeyH),
                    b'i' => Some(Code::KeyI), b'j' => Some(Code::KeyJ), b'k' => Some(Code::KeyK), b'l' => Some(Code::KeyL),
                    b'm' => Some(Code::KeyM), b'n' => Some(Code::KeyN), b'o' => Some(Code::KeyO), b'p' => Some(Code::KeyP),
                    b'q' => Some(Code::KeyQ), b'r' => Some(Code::KeyR), b's' => Some(Code::KeyS), b't' => Some(Code::KeyT),
                    b'u' => Some(Code::KeyU), b'v' => Some(Code::KeyV), b'w' => Some(Code::KeyW), b'x' => Some(Code::KeyX),
                    b'y' => Some(Code::KeyY), b'z' => Some(Code::KeyZ),
                    b'0' => Some(Code::Digit0), b'1' => Some(Code::Digit1), b'2' => Some(Code::Digit2), b'3' => Some(Code::Digit3),
                    b'4' => Some(Code::Digit4), b'5' => Some(Code::Digit5), b'6' => Some(Code::Digit6), b'7' => Some(Code::Digit7),
                    b'8' => Some(Code::Digit8), b'9' => Some(Code::Digit9),
                    _ => None,
                };
            }
            _ => return None,
        }
    }
    if spec.ends_with("--") { code = Some(Code::Minus); }
    (!mods.is_empty()).then_some(HotKey::new(Some(mods), code?))
}
