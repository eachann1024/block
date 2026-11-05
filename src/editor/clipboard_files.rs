//! gpui only surfaces text and bitmaps from the pasteboard, so Finder-copied files are read directly.
use std::path::PathBuf;

pub(super) const IMAGE_EXTENSIONS: [&str; 7] = ["png", "jpg", "jpeg", "gif", "webp", "bmp", "svg"];

pub(super) fn is_image(path: &std::path::Path) -> bool {
    path.extension().and_then(|e| e.to_str())
        .is_some_and(|e| IMAGE_EXTENSIONS.contains(&e.to_ascii_lowercase().as_str()))
}

/// Image files copied as file paths on the system clipboard.
/// macOS reads Finder `NSFilenamesPboardType`. Windows and Linux return an empty list:
/// text and bitmap paste still go through gpui; pasting a copied file path does not.
#[cfg(target_os = "macos")]
#[allow(deprecated)]
pub(super) fn image_paths() -> Vec<PathBuf> {
    use cocoa::{appkit::NSPasteboard, base::{id, nil}, foundation::{NSArray, NSString}};
    use std::sync::OnceLock;
    static KIND: OnceLock<usize> = OnceLock::new();
    unsafe {
        let kind = *KIND.get_or_init(|| NSString::alloc(nil).init_str("NSFilenamesPboardType") as usize) as id;
        let list = NSPasteboard::generalPasteboard(nil).propertyListForType(kind);
        if list == nil { return Vec::new(); }
        (0..NSArray::count(list)).filter_map(|i| {
            let item = NSArray::objectAtIndex(list, i);
            if item == nil { return None; }
            let raw = NSString::UTF8String(item);
            if raw.is_null() { return None; }
            let path = PathBuf::from(std::ffi::CStr::from_ptr(raw).to_string_lossy().into_owned());
            is_image(&path).then_some(path)
        }).collect()
    }
}
#[cfg(not(target_os = "macos"))]
pub(super) fn image_paths() -> Vec<PathBuf> { Vec::new() }
