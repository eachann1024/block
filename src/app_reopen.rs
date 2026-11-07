//! Explicit macOS app-open requests target the workspace even while a panel is visible.
//! GPUI 0.3.11 skips its reopen callback when AppKit reports any visible window.
#[cfg(target_os = "macos")]
#[allow(deprecated)]
mod macos {
    use std::sync::OnceLock;
    use cocoa::{appkit::NSApplication, base::{BOOL, NO, SEL, YES, id, nil, selector}};

    type Reopen = unsafe extern "C-unwind" fn(id, SEL, id, BOOL) -> BOOL;
    static ORIGINAL: OnceLock<Reopen> = OnceLock::new();

    unsafe extern "C" {
        fn objc_msgSend();
        fn object_getClass(object: *mut std::ffi::c_void) -> *mut std::ffi::c_void;
        fn class_getInstanceMethod(class: id, selector: SEL) -> *mut std::ffi::c_void;
        fn method_getImplementation(method: *mut std::ffi::c_void) -> *const std::ffi::c_void;
        fn method_setImplementation(method: *mut std::ffi::c_void, implementation: *const std::ffi::c_void) -> *const std::ffi::c_void;
    }

    unsafe fn delegate(app: id) -> id {
        let get: unsafe extern "C" fn(id, SEL) -> id = unsafe { std::mem::transmute(objc_msgSend as *const ()) };
        unsafe { get(app, selector("delegate")) }
    }

    extern "C-unwind" fn reopen(this: id, command: SEL, app: id, _has_visible_windows: BOOL) -> BOOL {
        let Some(original) = ORIGINAL.get() else { return YES };
        // Let GPUI call the registered App handler regardless of auxiliary panels.
        unsafe { original(this, command, app, NO); }
        // The handler chose the main window. Do not let AppKit pick the last panel again.
        NO
    }

    pub(super) fn install() {
        if ORIGINAL.get().is_some() { return; }
        unsafe {
            let target = delegate(NSApplication::sharedApplication(nil));
            let method = if target == nil { std::ptr::null_mut() } else {
                class_getInstanceMethod(object_getClass(target.cast()).cast(), selector("applicationShouldHandleReopen:hasVisibleWindows:"))
            };
            if method.is_null() {
                crate::log::error("Could not install the main-window app-reopen handler");
                return;
            }
            let original = std::mem::transmute::<*const std::ffi::c_void, Reopen>(method_getImplementation(method));
            if ORIGINAL.set(original).is_ok() {
                method_setImplementation(method, reopen as *const std::ffi::c_void);
            }
        }
    }

    /// Invoke the real delegate entry point inside the isolated worker, outside an App borrow.
    pub(super) fn smoke_reopen(has_visible_windows: bool) -> Result<(), String> {
        if !crate::smoke::is_worker() { return Err("Reopen verification requires the isolated worker".into()); }
        unsafe {
            let app = NSApplication::sharedApplication(nil);
            let target = delegate(app);
            if target == nil { return Err("Application delegate is missing".into()); }
            let send: Reopen = std::mem::transmute(objc_msgSend as *const ());
            let default_handling = send(target, selector("applicationShouldHandleReopen:hasVisibleWindows:"), app,
                if has_visible_windows { YES } else { NO });
            if default_handling != NO { return Err("AppKit reopen still falls back to the last visible panel".into()); }
        }
        Ok(())
    }
}

pub(crate) fn install() {
    #[cfg(target_os = "macos")]
    macos::install();
}

pub(crate) fn smoke_reopen(has_visible_windows: bool) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    return macos::smoke_reopen(has_visible_windows);
    #[cfg(not(target_os = "macos"))]
    { let _ = has_visible_windows; Ok(()) }
}
