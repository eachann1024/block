//! Non-activating macOS `NSPanel` configuration so the Quick Note window operates
//! independently without waking, unhiding, or focusing the main window.

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
pub(super) fn prepare_quick_summon() {
    use cocoa::appkit::{NSApplication, NSWindow};
    use cocoa::base::{BOOL, SEL, YES, id, nil, selector};
    use cocoa::foundation::NSArray;
    unsafe extern "C" {
        fn objc_msgSend();
    }
    unsafe {
        let app = NSApplication::sharedApplication(nil);
        let is_hidden: unsafe extern "C" fn(id, SEL) -> BOOL = std::mem::transmute(objc_msgSend as *const ());
        if is_hidden(app, selector("isHidden")) == YES {
            let windows = app_windows(app);
            for i in 0..NSArray::count(windows) {
                let win = NSArray::objectAtIndex(windows, i);
                if NSWindow::level(win) == 0 {
                    NSWindow::orderOut_(win, nil);
                }
            }
            let unhide: unsafe extern "C" fn(id, SEL) = std::mem::transmute(objc_msgSend as *const ());
            unhide(app, selector("unhideWithoutActivation"));
        }
    }
}

/// Windows and Linux have no non-activating panel: the quick note stays a normal GPUI popup.
#[cfg(not(target_os = "macos"))]
pub(super) fn prepare_quick_summon() {}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(super) fn configure_quick_panel() {
    use cocoa::appkit::{NSApplication, NSPanel, NSWindow};
    use cocoa::base::{NO, YES, nil};
    use cocoa::foundation::NSArray;
    unsafe {
        let app = NSApplication::sharedApplication(nil);
        let windows = app_windows(app);
        for i in 0..NSArray::count(windows) {
            let win = NSArray::objectAtIndex(windows, i);
            if NSWindow::level(win) != 0 {
                NSWindow::setHidesOnDeactivate_(win, NO);
                NSWindow::setCanHide_(win, NO);
                NSPanel::setFloatingPanel(win, YES);
                NSPanel::setBecomesKeyOnlyIfNeeded(win, NO);
                super::ime_bridge::install_ime_bridge(win as *mut std::ffi::c_void);
                NSWindow::makeKeyAndOrderFront_(win, nil);
            }
        }
    }
}

#[cfg(not(target_os = "macos"))]
pub(super) fn configure_quick_panel() {}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(super) fn order_out_quick_panel() {
    use cocoa::appkit::{NSApplication, NSWindow};
    use cocoa::base::nil;
    use cocoa::foundation::NSArray;
    unsafe {
        let app = NSApplication::sharedApplication(nil);
        let windows = app_windows(app);
        for i in 0..NSArray::count(windows) {
            let win = NSArray::objectAtIndex(windows, i);
            if NSWindow::level(win) != 0 {
                NSWindow::orderOut_(win, nil);
            }
        }
    }
}

#[cfg(not(target_os = "macos"))]
pub(super) fn order_out_quick_panel() {}
