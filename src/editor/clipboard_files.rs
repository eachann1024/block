//! System file and image clipboard bridge: reads and writes files (`NSFilenamesPboardType` / `public.file-url`)
//! and image payloads without placing file path strings into `public.utf8-plain-text`.
use std::path::{Path, PathBuf};

pub(super) const IMAGE_EXTENSIONS: [&str; 7] = ["png", "jpg", "jpeg", "gif", "webp", "bmp", "svg"];

pub(crate) fn is_image(path: &Path) -> bool {
    path.extension().and_then(|e| e.to_str())
        .is_some_and(|e| IMAGE_EXTENSIONS.contains(&e.to_ascii_lowercase().as_str()))
}

pub(crate) fn image_format(path: &Path) -> Option<gpui::ImageFormat> {
    let ext = path.extension()?.to_str()?.to_ascii_lowercase();
    match ext.as_str() {
        "png" => Some(gpui::ImageFormat::Png),
        "jpg" | "jpeg" => Some(gpui::ImageFormat::Jpeg),
        "gif" => Some(gpui::ImageFormat::Gif),
        "webp" => Some(gpui::ImageFormat::Webp),
        "bmp" => Some(gpui::ImageFormat::Bmp),
        "svg" => Some(gpui::ImageFormat::Svg),
        "ico" => Some(gpui::ImageFormat::Ico),
        "tif" | "tiff" => Some(gpui::ImageFormat::Tiff),
        _ => None,
    }
}

/// Extract only `ClipboardEntry::String` entries, ignoring `ClipboardEntry::ExternalPaths`
/// so copied files never fall back to pasting their file path string.
pub(crate) fn string_text(item: &gpui::ClipboardItem) -> Option<String> {
    let mut out = String::new();
    for entry in item.entries() {
        if let gpui::ClipboardEntry::String(s) = entry {
            out.push_str(&s.text());
        }
    }
    (!out.is_empty()).then_some(out)
}

/// Copy file(s) to the system clipboard as file references (plus image bytes when copying an image),
/// never as a plain-text file path string.
pub(crate) fn copy_files(paths: &[PathBuf], cx: &mut gpui::App) -> bool {
    let valid: Vec<PathBuf> = paths.iter()
        .filter(|p| p.exists())
        .map(|p| std::fs::canonicalize(p).unwrap_or_else(|_| p.clone()))
        .collect();
    if valid.is_empty() { return false; }
    let image = valid.iter().find_map(|p| Some((image_format(p)?, std::fs::read(p).ok()?)));
    let mut entries = vec![gpui::ClipboardEntry::ExternalPaths(gpui::ExternalPaths(
        valid.iter().cloned().collect(),
    ))];
    if let Some((format, ref bytes)) = image {
        entries.push(gpui::ClipboardEntry::Image(gpui::Image::from_bytes(format, bytes.clone())));
    }
    cx.write_to_clipboard(gpui::ClipboardItem { entries });
    #[cfg(target_os = "macos")]
    write_pasteboard_files(&valid, image.as_ref());
    true
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
fn write_pasteboard_files(paths: &[PathBuf], image: Option<&(gpui::ImageFormat, Vec<u8>)>) {
    use cocoa::{
        appkit::NSPasteboard,
        base::{SEL, id, nil, selector},
        foundation::{NSArray, NSAutoreleasePool, NSData, NSString},
    };
    unsafe extern "C" {
        fn objc_msgSend();
        fn objc_getClass(name: *const std::ffi::c_char) -> id;
    }
    unsafe {
        let pool = NSAutoreleasePool::new(nil);
        let pb = NSPasteboard::generalPasteboard(nil);
        let ns_filenames_type = NSString::alloc(nil).init_str("NSFilenamesPboardType").autorelease();
        let file_url_type = NSString::alloc(nil).init_str("public.file-url").autorelease();
        let img_ut_type = image.map(|(format, _)| {
            let ut = match format {
                gpui::ImageFormat::Png => "public.png",
                gpui::ImageFormat::Jpeg => "public.jpeg",
                gpui::ImageFormat::Gif => "com.compuserve.gif",
                gpui::ImageFormat::Webp => "org.webmproject.webp",
                gpui::ImageFormat::Bmp => "com.microsoft.bmp",
                gpui::ImageFormat::Svg => "public.svg-image",
                gpui::ImageFormat::Tiff => "public.tiff",
                gpui::ImageFormat::Ico => "com.microsoft.ico",
                gpui::ImageFormat::Pnm => "public.pbm",
            };
            NSString::alloc(nil).init_str(ut).autorelease()
        });
        let mut declared = vec![ns_filenames_type, file_url_type];
        if let Some(ut) = img_ut_type { declared.push(ut); }
        let types_array = NSArray::arrayWithObjects(nil, &declared);
        pb.declareTypes_owner(types_array, nil);

        let ns_paths: Vec<id> = paths.iter()
            .map(|p| NSString::alloc(nil).init_str(&p.to_string_lossy()).autorelease())
            .collect();
        let paths_array = NSArray::arrayWithObjects(nil, &ns_paths);
        pb.setPropertyList_forType(paths_array, ns_filenames_type);

        if let Some(&first_ns_path) = ns_paths.first() {
            let get_url: unsafe extern "C" fn(id, SEL, id) -> id = std::mem::transmute(objc_msgSend as *const ());
            let get_id: unsafe extern "C" fn(id, SEL) -> id = std::mem::transmute(objc_msgSend as *const ());
            let url = get_url(objc_getClass(c"NSURL".as_ptr()), selector("fileURLWithPath:"), first_ns_path);
            if url != nil {
                let abs_str = get_id(url, selector("absoluteString"));
                if abs_str != nil {
                    let raw = NSString::UTF8String(abs_str);
                    if !raw.is_null() {
                        let bytes = std::ffi::CStr::from_ptr(raw).to_bytes();
                        let data = NSData::dataWithBytes_length_(nil, bytes.as_ptr() as *const _, bytes.len() as u64);
                        pb.setData_forType(data, file_url_type);
                    }
                }
            }
        }

        if let (Some((_, bytes)), Some(ut)) = (image, img_ut_type) {
            let data = NSData::dataWithBytes_length_(nil, bytes.as_ptr() as *const _, bytes.len() as u64);
            pb.setData_forType(data, ut);
        }
        pool.drain();
    }
}

/// Image files copied as file paths on the system clipboard.
pub(super) fn image_paths(item: Option<&gpui::ClipboardItem>) -> Vec<PathBuf> {
    let mut paths: Vec<PathBuf> = item.into_iter().flat_map(|item| item.entries().iter())
        .filter_map(|entry| match entry {
            gpui::ClipboardEntry::ExternalPaths(ext) => Some(ext.paths().iter().cloned().collect::<Vec<_>>()),
            _ => None,
        }).flatten().filter(|p| is_image(p)).collect();
    #[cfg(target_os = "macos")]
    if paths.is_empty() {
        #[allow(deprecated)]
        unsafe {
            use cocoa::{appkit::NSPasteboard, base::{id, nil}, foundation::{NSArray, NSString}};
            use std::sync::OnceLock;
            static KIND: OnceLock<usize> = OnceLock::new();
            let kind = *KIND.get_or_init(|| NSString::alloc(nil).init_str("NSFilenamesPboardType") as usize) as id;
            let list = NSPasteboard::generalPasteboard(nil).propertyListForType(kind);
            if list != nil {
                paths.extend((0..NSArray::count(list)).filter_map(|i| {
                    let item = NSArray::objectAtIndex(list, i);
                    if item == nil { return None; }
                    let raw = NSString::UTF8String(item);
                    if raw.is_null() { return None; }
                    let path = PathBuf::from(std::ffi::CStr::from_ptr(raw).to_string_lossy().into_owned());
                    is_image(&path).then_some(path)
                }));
            }
        }
    }
    paths
}

fn is_markdown(path: &Path) -> bool {
    path.extension().and_then(|e| e.to_str())
        .is_some_and(|e| matches!(e.to_ascii_lowercase().as_str(), "md" | "markdown" | "mdown" | "mkd"))
}

/// If the user copied `.md` file(s) in Finder, read their UTF-8 Markdown content.
pub(super) fn markdown_file_text(item: Option<&gpui::ClipboardItem>) -> Option<String> {
    let mut paths: Vec<PathBuf> = item.into_iter().flat_map(|item| item.entries().iter())
        .filter_map(|entry| match entry {
            gpui::ClipboardEntry::ExternalPaths(ext) => Some(ext.paths().iter().cloned().collect::<Vec<_>>()),
            _ => None,
        }).flatten().filter(|p| is_markdown(p)).collect();
    #[cfg(target_os = "macos")]
    if paths.is_empty() {
        #[allow(deprecated)]
        unsafe {
            use cocoa::{appkit::NSPasteboard, base::nil, foundation::{NSArray, NSString}};
            let kind = NSString::alloc(nil).init_str("NSFilenamesPboardType");
            let list = NSPasteboard::generalPasteboard(nil).propertyListForType(kind);
            if list != nil {
                paths.extend((0..NSArray::count(list)).filter_map(|i| {
                    let item = NSArray::objectAtIndex(list, i);
                    if item == nil { return None; }
                    let raw = NSString::UTF8String(item);
                    if raw.is_null() { return None; }
                    let path = PathBuf::from(std::ffi::CStr::from_ptr(raw).to_string_lossy().into_owned());
                    is_markdown(&path).then_some(path)
                }));
            }
        }
    }
    if paths.is_empty() { return None; }
    let mut combined = String::new();
    for path in paths {
        if let Ok(content) = std::fs::read_to_string(&path) {
            if !combined.is_empty() && !combined.ends_with('\n') { combined.push('\n'); }
            combined.push_str(&content);
        }
    }
    (!combined.is_empty()).then_some(combined)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn string_text_and_pasteboard_file_copy() {
        let item = gpui::ClipboardItem {
            entries: vec![gpui::ClipboardEntry::ExternalPaths(gpui::ExternalPaths(
                [PathBuf::from("/tmp/example.png")].into_iter().collect(),
            ))],
        };
        assert_eq!(string_text(&item), None);
        assert_eq!(image_paths(Some(&item)), vec![PathBuf::from("/tmp/example.png")]);

        #[cfg(target_os = "macos")]
        {
            let tmp = std::env::temp_dir().join("thinkly-clip-test.png");
            let png: &[u8] = &[
                0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48,
                0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00,
                0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, 0x78,
                0x9C, 0x62, 0x00, 0x00, 0x00, 0x02, 0x00, 0x01, 0xE5, 0x27, 0xDE, 0xFC, 0x00, 0x00,
                0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
            ];
            std::fs::write(&tmp, png).unwrap();
            let canon = std::fs::canonicalize(&tmp).unwrap();
            write_pasteboard_files(std::slice::from_ref(&canon), Some(&(gpui::ImageFormat::Png, png.to_vec())));
            assert_eq!(image_paths(None), vec![canon]);
            let _ = std::fs::remove_file(&tmp);
        }
    }
}
