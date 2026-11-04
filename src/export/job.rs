use std::{
    fs::{File, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    sync::{
        Arc,
        atomic::{AtomicBool, AtomicU64, Ordering},
    },
    time::Duration,
};

use super::ExportError;

#[derive(Clone, Default)]
pub struct Cancellation(Arc<AtomicBool>);
impl Cancellation {
    pub fn cancel(&self) {
        self.0.store(true, Ordering::Release);
    }
    pub fn check(&self) -> Result<(), ExportError> {
        if self.0.load(Ordering::Acquire) {
            Err(ExportError::Cancelled)
        } else {
            Ok(())
        }
    }
}

static ACTIVE: AtomicBool = AtomicBool::new(false);
static NEXT_FILE: AtomicU64 = AtomicU64::new(1);

/// Held from preflight through dialog completion and background work.
/// Prevents several otherwise bounded exports from exhausting memory together.
pub struct ExportJob {
    pub cancellation: Cancellation,
}
impl ExportJob {
    pub fn begin() -> Result<Self, ExportError> {
        ACTIVE
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .map_err(|_| ExportError::Busy)?;
        Ok(Self {
            cancellation: Cancellation::default(),
        })
    }
}
impl Drop for ExportJob {
    fn drop(&mut self) {
        ACTIVE.store(false, Ordering::Release);
    }
}

#[derive(Debug, Default)]
#[allow(dead_code)] // kept for diagnostics via Debug
pub struct Timings {
    pub font_setup: Duration,
    pub layout: Duration,
    pub raster: Duration,
    pub encoding: Duration,
}

struct Temporary(PathBuf);
impl Drop for Temporary {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.0);
    }
}

fn temporary(path: &Path) -> Result<(Temporary, File), ExportError> {
    let directory = path
        .parent()
        .filter(|p| !p.as_os_str().is_empty())
        .unwrap_or(Path::new("."));
    if path.file_name().is_none() {
        return Err(ExportError::Write(std::io::Error::new(
            std::io::ErrorKind::InvalidInput,
            crate::theme::tr("缺少文件名", "missing filename"),
        )));
    }
    for _ in 0..64 {
        let id = NEXT_FILE.fetch_add(1, Ordering::Relaxed);
        let candidate = directory.join(format!(".thinkly-export-{}-{id}.tmp", std::process::id()));
        match OpenOptions::new()
            .create_new(true)
            .write(true)
            .open(&candidate)
        {
            Ok(file) => return Ok((Temporary(candidate), file)),
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(ExportError::Write(error)),
        }
    }
    Err(ExportError::Write(std::io::Error::new(
        std::io::ErrorKind::AlreadyExists,
        crate::theme::tr("临时文件名已用尽", "temporary file names exhausted"),
    )))
}

/// The confirmed path is never rewritten, including its extension. A failure before
/// the atomic rename preserves the previous destination and removes our staging file.
pub fn write_png(path: &Path, bytes: &[u8], cancel: &Cancellation) -> Result<(), ExportError> {
    write_bytes(path, bytes, cancel)
}

pub fn write_bytes(path: &Path, bytes: &[u8], cancel: &Cancellation) -> Result<(), ExportError> {
    cancel.check()?;
    let (temporary, mut file) = temporary(path)?;
    for chunk in bytes.chunks(64 * 1024) {
        cancel.check()?;
        file.write_all(chunk).map_err(ExportError::Write)?;
    }
    file.sync_all().map_err(ExportError::Write)?;
    drop(file);
    cancel.check()?;
    std::fs::rename(&temporary.0, path).map_err(ExportError::Write)?;
    Ok(())
}
