//! Open a local path in the system app or the default https browser. Arguments are
//! passed as argv, never through a shell.
use std::io;
use std::path::Path;
use std::process::Command;
use crate::theme::tr;

pub fn open_default(path: &Path) -> io::Result<()> {
    #[cfg(target_family = "wasm")]
    { let _ = path; return Err(io::Error::other(tr("浏览器里无法打开本地应用", "Cannot open a local app in the browser"))); }
    #[cfg(target_os = "macos")]
    { spawn(Command::new("/usr/bin/open").arg(path)) }
    #[cfg(target_os = "windows")]
    { spawn(Command::new("explorer.exe").arg(path)) }
    #[cfg(not(any(target_os = "macos", target_os = "windows", target_family = "wasm")))]
    { spawn(Command::new("xdg-open").arg(path)) }
}

/// Open a local HTML file with the application that handles https, not the file association.
pub fn open_in_browser(path: &Path) -> io::Result<()> {
    #[cfg(target_family = "wasm")]
    { let _ = path; return Err(io::Error::other(tr("浏览器里无法再次打开本地页面", "Cannot launch a browser from the browser build"))); }
    #[cfg(target_os = "macos")]
    { open_mac_browser(path) }
    #[cfg(target_os = "windows")]
    {
        // Explorer uses the file association. There is no shell-free https-handler lookup
        // in the current dependency set; see the implementation report.
        spawn(Command::new("explorer.exe").arg(path))
    }
    #[cfg(not(any(target_os = "macos", target_os = "windows", target_family = "wasm")))]
    { open_linux_browser(path) }
}

fn spawn(command: &mut Command) -> io::Result<()> {
    command.spawn().map(|_| ()).map_err(|e| io::Error::other(e.to_string()))
}

#[cfg(target_os = "macos")]
#[allow(deprecated)]
fn open_mac_browser(path: &Path) -> io::Result<()> {
    use cocoa::base::{SEL, id, nil, selector};
    use cocoa::foundation::{NSAutoreleasePool, NSString};
    unsafe extern "C" {
        fn objc_msgSend();
        fn objc_getClass(name: *const std::ffi::c_char) -> id;
    }
    let app = unsafe {
        let pool = NSAutoreleasePool::new(nil);
        let get_id: unsafe extern "C" fn(id, SEL) -> id = std::mem::transmute(objc_msgSend as *const ());
        let get_url: unsafe extern "C" fn(id, SEL, id) -> id = std::mem::transmute(objc_msgSend as *const ());
        let utf8_fn: unsafe extern "C" fn(id, SEL) -> *const std::ffi::c_char =
            std::mem::transmute(objc_msgSend as *const ());
        let ws = get_id(objc_getClass(c"NSWorkspace".as_ptr()), selector("sharedWorkspace"));
        let https = NSString::alloc(nil).init_str("https://localhost/").autorelease();
        let url = get_url(objc_getClass(c"NSURL".as_ptr()), selector("URLWithString:"), https);
        let app_url = if ws != nil && url != nil {
            get_url(ws, selector("URLForApplicationToOpenURL:"), url)
        } else {
            nil
        };
        let app_path = if app_url != nil {
            let ns_path = get_id(app_url, selector("path"));
            let cstr = if ns_path != nil { utf8_fn(ns_path, selector("UTF8String")) } else { std::ptr::null() };
            if !cstr.is_null() { std::ffi::CStr::from_ptr(cstr).to_string_lossy().trim().to_owned() } else { String::new() }
        } else {
            String::new()
        };
        pool.drain();
        app_path
    };
    if app.is_empty() || (!app.ends_with(".app") && !std::path::Path::new(&app).exists()) {
        return Err(io::Error::other(tr("找不到默认浏览器", "No default browser")));
    }
    spawn(Command::new("/usr/bin/open").arg("-a").arg(app).arg(path))
}

#[cfg(not(any(target_os = "macos", target_os = "windows", target_family = "wasm")))]
fn open_linux_browser(path: &Path) -> io::Result<()> {
    let query = Command::new("xdg-mime").args(["query", "default", "x-scheme-handler/https"]).output();
    if let Ok(output) = query {
        let desktop = String::from_utf8_lossy(&output.stdout).trim().to_owned();
        if desktop_id_ok(&desktop) {
            let status = Command::new("gio").arg("launch").arg(&desktop).arg(path).status();
            if status.is_ok_and(|s| s.success()) { return Ok(()); }
        }
    }
    // xdg-open follows the HTML file association, which may be a browser. Reported as a limitation.
    spawn(Command::new("xdg-open").arg(path))
}

#[cfg(not(any(target_os = "macos", target_os = "windows", target_family = "wasm")))]
fn desktop_id_ok(id: &str) -> bool {
    id.ends_with(".desktop") && id.len() < 128
        && id.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'-' | b'_'))
}
