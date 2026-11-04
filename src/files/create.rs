//! Bounded candidate allocation is separate from exclusive disk creation so the
//! workspace can reject paths still owned by its tree or current document.
use std::fs::OpenOptions;
use std::io;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

const SEQUENTIAL: usize = 1000;
static NEXT: AtomicU64 = AtomicU64::new(0);

pub fn note_candidates(dir: &Path) -> impl Iterator<Item = PathBuf> {
    let dir = dir.to_owned();
    #[cfg(not(target_family = "wasm"))]
    let stamp = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos();
    #[cfg(target_family = "wasm")]
    let stamp = 0u128;
    let base = crate::theme::tr("未命名", "Untitled");
    // Sequential names ("未命名", "未命名 2", ...) first; the unique-stamp form is only a collision fallback.
    (0..SEQUENTIAL + 16).map(move |attempt| {
        let name = if attempt == 0 { format!("{base}.md") } else if attempt < SEQUENTIAL {
            format!("{base} {}.md", attempt + 1)
        } else {
            format!("{base} {stamp:x}-{}.md", NEXT.fetch_add(1, Ordering::Relaxed))
        };
        dir.join(name)
    })
}

pub fn create_note_at(path: &Path) -> io::Result<()> {
    #[cfg(target_family = "wasm")]
    { return super::vfs::create_note_exclusive(path); }
    #[cfg(not(target_family = "wasm"))]
    OpenOptions::new().write(true).create_new(true).open(path).map(|_| ())
}

pub fn create_note(dir: &Path) -> io::Result<PathBuf> {
    for path in note_candidates(dir) {
        match create_note_at(&path) {
            Ok(()) => return Ok(path),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
    Err(io::Error::new(io::ErrorKind::AlreadyExists, crate::theme::tr("无法分配唯一笔记名称，请重试", "Could not allocate a unique note name; please retry")))
}
