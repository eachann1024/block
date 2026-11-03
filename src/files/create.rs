//! Bounded candidate allocation is separate from exclusive disk creation so the
//! workspace can reject paths still owned by its tree or current document.
use std::fs::OpenOptions;
use std::io;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

static NEXT: AtomicU64 = AtomicU64::new(0);

pub fn note_candidates(dir: &Path) -> impl Iterator<Item = PathBuf> {
    let dir = dir.to_owned();
    let stamp = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos();
    (0..64).map(move |attempt| {
        let name = if attempt == 0 { "未命名.md".to_owned() } else {
            format!("未命名 {stamp:x}-{}-{}.md", std::process::id(), NEXT.fetch_add(1, Ordering::Relaxed))
        };
        dir.join(name)
    })
}

pub fn create_note_at(path: &Path) -> io::Result<()> {
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
    Err(io::Error::new(io::ErrorKind::AlreadyExists, "无法分配唯一笔记名称，请重试"))
}
