//! Main-window native visibility. Quick notes never participate in these transitions.
#[cfg(target_os = "macos")]
#[allow(deprecated)]
fn app_windows() -> cocoa::base::id {
    use cocoa::{appkit::NSApplication, base::{SEL, id, nil, selector}};
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let app = NSApplication::sharedApplication(nil);
        let send: unsafe extern "C" fn(id, SEL) -> id = std::mem::transmute(objc_msgSend as *const ());
        send(app, selector("windows"))
    }
}
#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub fn hide_main_window_on_close() -> bool {
    use cocoa::{appkit::NSWindow, base::nil, foundation::NSArray};
    unsafe {
        let windows = app_windows();
        let mut hidden = false;
        for i in 0..NSArray::count(windows) {
            let win = NSArray::objectAtIndex(windows, i);
            if is_main_window(win) {
                NSWindow::orderOut_(win, nil);
                hidden = true;
            }
        }
        hidden
    }
}
/// Without Cocoa window ordering, closing the main window quits instead of hiding it.
#[cfg(not(target_os = "macos"))]
pub fn hide_main_window_on_close() -> bool { false }
#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(crate) unsafe fn is_main_window(win: cocoa::base::id) -> bool {
    use cocoa::base::{BOOL, SEL, YES, id, selector};
    unsafe extern "C" { fn objc_msgSend(); fn objc_getClass(name: *const std::ffi::c_char) -> id; }
    unsafe {
        let kind: unsafe extern "C" fn(id, SEL, id) -> BOOL = std::mem::transmute(objc_msgSend as *const ());
        kind(win, selector("isKindOfClass:"), objc_getClass(c"GPUIWindow".as_ptr())) == YES
    }
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(crate) fn is_main_window_visible() -> bool {
    use cocoa::{appkit::NSApplication, base::{BOOL, SEL, YES, id, nil, selector}, foundation::NSArray};
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let app = NSApplication::sharedApplication(nil);
        let flag: unsafe extern "C" fn(id, SEL) -> BOOL = std::mem::transmute(objc_msgSend as *const ());
        if flag(app, selector("isHidden")) == YES { return false; }
        let windows = app_windows();
        (0..NSArray::count(windows)).any(|i| {
            let win = NSArray::objectAtIndex(windows, i);
            is_main_window(win) && (flag(win, selector("isVisible")) == YES)
        })
    }
}

#[cfg(not(target_os = "macos"))]
pub(crate) fn is_main_window_visible() -> bool { true }

#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(crate) fn show_native_main_window() {
    use cocoa::{appkit::{NSApplication, NSWindow}, base::{BOOL, SEL, YES, id, nil, selector}, foundation::NSArray};
    unsafe extern "C" { fn objc_msgSend(); }
    unsafe {
        let app = NSApplication::sharedApplication(nil);
        let flag: unsafe extern "C" fn(id, SEL) -> BOOL = std::mem::transmute(objc_msgSend as *const ());
        if flag(app, selector("isHidden")) == YES {
            if crate::smoke::is_worker() {
                let windows = app_windows();
                for i in 0..NSArray::count(windows) {
                    prepare_background_window(NSArray::objectAtIndex(windows, i));
                }
                let unhide: unsafe extern "C" fn(id, SEL) = std::mem::transmute(objc_msgSend as *const ());
                unhide(app, selector("unhideWithoutActivation"));
            } else {
                let unhide: unsafe extern "C" fn(id, SEL, id) = std::mem::transmute(objc_msgSend as *const ());
                unhide(app, selector("unhide:"), nil);
            }
        }
        let set_bool: unsafe extern "C" fn(id, SEL, BOOL) = std::mem::transmute(objc_msgSend as *const ());
        if !crate::smoke::is_worker() { set_bool(app, selector("activateIgnoringOtherApps:"), YES); }
        let send_void: unsafe extern "C" fn(id, SEL) = std::mem::transmute(objc_msgSend as *const ());
        let send_id: unsafe extern "C" fn(id, SEL) -> id = std::mem::transmute(objc_msgSend as *const ());
        let windows = app_windows();
        for i in 0..NSArray::count(windows) {
            let win = NSArray::objectAtIndex(windows, i);
            if is_main_window(win) {
                prepare_background_window(win);
                if flag(win, selector("isMiniaturized")) == YES { NSWindow::deminiaturize_(win, nil); }
                let content = send_id(win, selector("contentView"));
                if content != nil { set_bool(content, selector("setNeedsDisplay:"), YES); }
                send_void(win, selector("orderFrontRegardless"));
                if !crate::smoke::is_worker() {
                    send_void(win, selector("makeMainWindow"));
                    NSWindow::makeKeyAndOrderFront_(win, nil);
                }
            }
        }
    }
}

#[cfg(not(target_os = "macos"))]
pub(crate) fn show_native_main_window() {}

/// Exercise native ordering in the isolated worker without pixels, mouse interception or focus.
#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(crate) unsafe fn prepare_background_window(win: cocoa::base::id) {
    if !crate::smoke::is_worker() { return; }
    use cocoa::appkit::NSWindow;
    use cocoa::base::YES;
    unsafe {
        NSWindow::setAlphaValue_(win, 0.);
        NSWindow::setIgnoresMouseEvents_(win, YES);
    }
}
