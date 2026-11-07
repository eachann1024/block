//! Keep the quick-note panel out of AppKit's main-window selection.
//! GPUI's shared GPUIPanel class opts into becoming main; only our panel opts out.
use std::ffi::{c_char, c_void};
type Id = *mut c_void;
type Sel = *const c_void;

#[allow(deprecated)]
unsafe extern "C" {
    fn objc_getClass(name: *const c_char) -> cocoa::base::id;
    fn objc_allocateClassPair(superclass: Id, name: *const c_char, extra_bytes: usize) -> Id;
    fn objc_registerClassPair(class: Id);
    fn objc_disposeClassPair(class: Id);
    fn class_addMethod(class: Id, name: Sel, imp: *const c_void, types: *const c_char) -> u8;
    fn object_setClass(object: Id, class: Id) -> Id;
    fn sel_registerName(name: *const c_char) -> Sel;
}

extern "C" fn cannot_become_main(_: Id, _: Sel) -> cocoa::base::BOOL { cocoa::base::NO }

/// Main-thread only. The subclass adds no storage and inherits GPUI's key/input handling.
pub(super) unsafe fn install(window: Id) {
    unsafe {
        let name = c"BlockQuickNotePanel";
        let mut class: Id = objc_getClass(name.as_ptr()).cast();
        if class.is_null() {
            let parent: Id = objc_getClass(c"GPUIPanel".as_ptr()).cast();
            if parent.is_null() { return; }
            class = objc_allocateClassPair(parent, name.as_ptr(), 0);
            if class.is_null() { return; }
            let signature = if cfg!(target_arch = "aarch64") { c"B@:" } else { c"c@:" };
            if class_addMethod(class, sel_registerName(c"canBecomeMainWindow".as_ptr()),
                cannot_become_main as *const c_void, signature.as_ptr()) == 0 {
                objc_disposeClassPair(class);
                crate::log::error("Could not configure the independent quick-note panel class");
                return;
            }
            objc_registerClassPair(class);
        }
        object_setClass(window, class);
    }
}
