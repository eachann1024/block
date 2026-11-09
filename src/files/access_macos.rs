//! Core Foundation owns the bookmark URLs; no AppKit or main-thread calls here.
use std::{ffi::c_void, io, os::unix::ffi::OsStrExt, path::Path, ptr};
use super::Bookmark;

type Ref = *const c_void;
const WITH_SECURITY_SCOPE: usize = 1 << 11;
const RESOLVE_WITH_SECURITY_SCOPE: usize = 1 << 10;
const RESOLVE_QUIETLY: usize = (1 << 8) | (1 << 9) | (1 << 15);

#[link(name = "CoreFoundation", kind = "framework")]
unsafe extern "C" {
    fn CFRelease(value: Ref);
    fn CFURLCreateFromFileSystemRepresentation(allocator: Ref, bytes: *const u8, length: isize, is_directory: u8) -> Ref;
    fn CFURLCreateBookmarkData(allocator: Ref, url: Ref, options: usize, properties: Ref, relative: Ref, error: *mut Ref) -> Ref;
    fn CFURLCreateByResolvingBookmarkData(allocator: Ref, data: Ref, options: usize, relative: Ref, properties: Ref, stale: *mut u8, error: *mut Ref) -> Ref;
    fn CFURLStartAccessingSecurityScopedResource(url: Ref) -> u8;
    fn CFURLStopAccessingSecurityScopedResource(url: Ref);
    fn CFDataCreate(allocator: Ref, bytes: *const u8, length: isize) -> Ref;
    fn CFDataGetLength(data: Ref) -> isize;
    fn CFDataGetBytePtr(data: Ref) -> *const u8;
}

struct Owned(Ref);

impl Owned {
    fn checked(value: Ref, message: &'static str) -> io::Result<Self> {
        if value.is_null() { Err(io::Error::other(message)) } else { Ok(Self(value)) }
    }
}

impl Drop for Owned {
    fn drop(&mut self) { unsafe { CFRelease(self.0); } }
}

pub(super) struct Scope { url: Owned, accessing: bool }

// CFURL is immutable and its security scope belongs to the process, not a thread.
// All scopes are accessed under the manager's mutex.
unsafe impl Send for Scope {}

impl Drop for Scope {
    fn drop(&mut self) {
        if self.accessing { unsafe { CFURLStopAccessingSecurityScopedResource(self.url.0); } }
    }
}

impl Scope {
    pub(super) fn bookmark(&self) -> io::Result<Bookmark> { bookmark(self.url.0) }
}

pub(super) fn capture(path: &Path) -> io::Result<Bookmark> {
    let bytes = path.as_os_str().as_bytes();
    let url = Owned::checked(unsafe {
        CFURLCreateFromFileSystemRepresentation(ptr::null(), bytes.as_ptr(), bytes.len() as isize, 1)
    }, "could not create folder URL")?;
    bookmark(url.0)
}

fn bookmark(url: Ref) -> io::Result<Bookmark> {
    // Explicit app-scoped bookmarks require App Sandbox. Unsandboxed builds
    // retain the implicit scope in a regular bookmark instead.
    for security_scoped in [true, false] {
        let options = if security_scoped { WITH_SECURITY_SCOPE } else { 0 };
        let mut error = ptr::null();
        let data = unsafe { CFURLCreateBookmarkData(ptr::null(), url, options, ptr::null(), ptr::null(), &mut error) };
        if !error.is_null() { drop(Owned(error)); }
        if data.is_null() { continue; }
        let data = Owned(data);
        let length = unsafe { CFDataGetLength(data.0) };
        let bytes = unsafe { CFDataGetBytePtr(data.0) };
        if length > 0 && !bytes.is_null() {
            return Ok(Bookmark { data: unsafe { std::slice::from_raw_parts(bytes, length as usize) }.to_vec(), security_scoped });
        }
    }
    Err(io::Error::other("could not preserve folder access"))
}

pub(super) fn resolve(bookmark: &Bookmark) -> io::Result<(Scope, bool)> {
    if bookmark.data.is_empty() { return Err(io::Error::other("empty folder bookmark")); }
    let data = Owned::checked(unsafe {
        CFDataCreate(ptr::null(), bookmark.data.as_ptr(), bookmark.data.len() as isize)
    }, "could not load folder bookmark")?;
    let options = RESOLVE_QUIETLY | if bookmark.security_scoped { RESOLVE_WITH_SECURITY_SCOPE } else { 0 };
    let mut stale = 0;
    let mut error = ptr::null();
    let url = unsafe {
        CFURLCreateByResolvingBookmarkData(ptr::null(), data.0, options, ptr::null(), ptr::null(), &mut stale, &mut error)
    };
    if !error.is_null() { drop(Owned(error)); }
    let url = Owned::checked(url, "folder access expired; select the folder again")?;
    let accessing = unsafe { CFURLStartAccessingSecurityScopedResource(url.0) } != 0;
    // False also means the resource needs no scope (ordinary unsandboxed paths).
    // The actual scan remains the authority on whether this folder is readable.
    Ok((Scope { url, accessing }, stale != 0))
}
