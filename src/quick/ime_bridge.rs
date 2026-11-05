//! macOS input-method bridge for the Quick Note window.
//!
//! GPUI's native `GPUIView` forwards `keyDown:` and mouse events to
//! `NSTextInputContext`, but `flagsChanged:` only reaches GPUI's own modifier
//! handling. Input methods such as the system Pinyin IME use a lone Shift
//! press to toggle Chinese/English, so that toggle never fires. This module
//! gives only the Quick Note view a private subclass that runs GPUI's original
//! `flagsChanged:` first, then hands the event to the view's input context
//! once. Installation is idempotent and touches neither activation nor visuals.

use std::cell::Cell;
use std::ffi::{CStr, c_char, c_void};
use std::sync::atomic::{AtomicUsize, Ordering};

type Id = *mut c_void;
type Sel = *const c_void;

const BRIDGE_CLASS: &CStr = c"BlockQuickNoteGPUIView";
const GPUI_VIEW_CLASS: &CStr = c"GPUIView";
const MAX_VIEW_DEPTH: usize = 8;

/// Original `GPUIView -flagsChanged:` implementation, captured when the subclass is built.
static ORIGINAL_FLAGS_CHANGED: AtomicUsize = AtomicUsize::new(0);

thread_local! {
    static IN_BRIDGE: Cell<bool> = const { Cell::new(false) };
}

#[link(name = "objc")]
unsafe extern "C" {
    fn objc_msgSend();
    fn objc_getClass(name: *const c_char) -> Id;
    fn objc_allocateClassPair(superclass: Id, name: *const c_char, extra_bytes: usize) -> Id;
    fn objc_registerClassPair(class: Id);
    fn objc_disposeClassPair(class: Id);
    fn class_addMethod(class: Id, name: Sel, imp: *const c_void, types: *const c_char) -> u8;
    fn class_getMethodImplementation(class: Id, name: Sel) -> *const c_void;
    fn object_getClass(object: Id) -> Id;
    fn object_setClass(object: Id, class: Id) -> Id;
    fn sel_registerName(name: *const c_char) -> Sel;
}

unsafe fn sel(name: &CStr) -> Sel {
    unsafe { sel_registerName(name.as_ptr()) }
}

unsafe fn send_id(object: Id, name: &CStr) -> Id {
    unsafe {
        let send: unsafe extern "C" fn(Id, Sel) -> Id = std::mem::transmute(objc_msgSend as *const ());
        send(object, sel(name))
    }
}

unsafe fn send_void(object: Id, name: &CStr) {
    unsafe {
        let send: unsafe extern "C" fn(Id, Sel) = std::mem::transmute(objc_msgSend as *const ());
        send(object, sel(name))
    }
}

/// Replacement `-flagsChanged:`: original GPUI handling, then one IME delivery.
extern "C" fn flags_changed(this: Id, cmd: Sel, event: Id) {
    unsafe {
        // GPUI callbacks may close the window; keep the view alive until we finish.
        send_void(this, c"retain");
        let original = ORIGINAL_FLAGS_CHANGED.load(Ordering::Relaxed);
        if original != 0 {
            let original: unsafe extern "C" fn(Id, Sel, Id) = std::mem::transmute(original);
            original(this, cmd, event);
        }
        if !event.is_null() && !IN_BRIDGE.get() {
            IN_BRIDGE.set(true);
            // No GPUI lock is held here, so IME callbacks can lock window state freely.
            let input_context = send_id(this, c"inputContext");
            if !input_context.is_null() {
                let handle: unsafe extern "C" fn(Id, Sel, Id) -> u8 = std::mem::transmute(objc_msgSend as *const ());
                handle(input_context, sel(c"handleEvent:"), event);
            }
            IN_BRIDGE.set(false);
        }
        send_void(this, c"release");
    }
}

/// Returns the shared bridge subclass, building it on first use.
unsafe fn bridge_class(gpui_view: Id) -> Option<Id> {
    unsafe {
        let existing = objc_getClass(BRIDGE_CLASS.as_ptr());
        if !existing.is_null() {
            return (ORIGINAL_FLAGS_CHANGED.load(Ordering::Relaxed) != 0).then_some(existing);
        }
        let flags = sel(c"flagsChanged:");
        let original = class_getMethodImplementation(gpui_view, flags);
        if original.is_null() {
            return None;
        }
        let class = objc_allocateClassPair(gpui_view, BRIDGE_CLASS.as_ptr(), 0);
        if class.is_null() {
            return None;
        }
        if class_addMethod(class, flags, flags_changed as *const c_void, c"v@:@".as_ptr()) == 0 {
            objc_disposeClassPair(class);
            return None;
        }
        ORIGINAL_FLAGS_CHANGED.store(original as usize, Ordering::Relaxed);
        objc_registerClassPair(class);
        Some(class)
    }
}

/// Walks `view`'s subtree and switches the first plain `GPUIView` to the bridge class.
unsafe fn install_in_tree(view: Id, gpui_view: Id, depth: usize) -> bool {
    unsafe {
        if view.is_null() {
            return false;
        }
        let class = object_getClass(view);
        let bridge = objc_getClass(BRIDGE_CLASS.as_ptr());
        if !bridge.is_null() && class == bridge {
            return true;
        }
        if class == gpui_view {
            return match bridge_class(gpui_view) {
                Some(bridge) => {
                    object_setClass(view, bridge);
                    true
                }
                None => false,
            };
        }
        if depth >= MAX_VIEW_DEPTH {
            return false;
        }
        let subviews = send_id(view, c"subviews");
        if subviews.is_null() {
            return false;
        }
        let count_fn: unsafe extern "C" fn(Id, Sel) -> usize = std::mem::transmute(objc_msgSend as *const ());
        let at_fn: unsafe extern "C" fn(Id, Sel, usize) -> Id = std::mem::transmute(objc_msgSend as *const ());
        for index in 0..count_fn(subviews, sel(c"count")) {
            let child = at_fn(subviews, sel(c"objectAtIndex:"), index);
            if install_in_tree(child, gpui_view, depth + 1) {
                return true;
            }
        }
        false
    }
}

/// Installs the Shift/IME bridge on the GPUI view inside `window` (an `NSWindow *`).
/// Safe to call repeatedly; does nothing if the view is already bridged or not a plain `GPUIView`.
pub(super) fn install_ime_bridge(window: Id) {
    if window.is_null() {
        return;
    }
    unsafe {
        let gpui_view = objc_getClass(GPUI_VIEW_CLASS.as_ptr());
        if gpui_view.is_null() {
            return;
        }
        install_in_tree(send_id(window, c"contentView"), gpui_view, 0);
    }
}
