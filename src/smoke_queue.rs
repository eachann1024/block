//! FIFO admission across binaries. Ticket locks disappear when a client crashes.
use std::{fs::{self, File, OpenOptions}, io::{self, Write},
    os::unix::fs::OpenOptionsExt, path::{Path, PathBuf}, time::{Duration, Instant}};

pub(super) struct Ticket { path: PathBuf, _lock: File }

fn open(path: &Path) -> io::Result<File> {
    OpenOptions::new().create(true).truncate(false).read(true).write(true).mode(0o600).open(path)
}

fn admission(directory: &Path) -> io::Result<File> {
    let lock = open(&directory.join("admission.lock"))?;
    lock.lock()?;
    Ok(lock)
}

// Call only while holding admission.lock; ticket creation must never appear stale.
fn live(directory: &Path) -> io::Result<Vec<PathBuf>> {
    let mut tickets = Vec::new();
    for entry in fs::read_dir(directory)? {
        let path = entry?.path();
        if path.extension().is_none_or(|ext| ext != "ticket") { continue; }
        let file = open(&path)?;
        match file.try_lock() {
            Ok(()) => { fs::remove_file(path)?; }
            Err(fs::TryLockError::WouldBlock) => tickets.push(path),
            Err(fs::TryLockError::Error(error)) => return Err(error),
        }
    }
    tickets.sort();
    Ok(tickets)
}

pub(super) fn pending(endpoint: &Path) -> io::Result<usize> {
    let directory = endpoint.parent().ok_or_else(|| io::Error::other("invalid endpoint"))?;
    let _guard = admission(directory)?;
    Ok(live(directory)?.len())
}

impl Ticket {
    pub(super) fn wait(endpoint: &Path) -> io::Result<Self> {
        let directory = endpoint.parent().ok_or_else(|| io::Error::other("invalid endpoint"))?;
        let ticket = {
            let _guard = admission(directory)?;
            let sequence = directory.join("sequence");
            let previous = match fs::read_to_string(&sequence) {
                Ok(text) => text.trim().parse::<u64>().map_err(io::Error::other)?,
                Err(e) if e.kind() == io::ErrorKind::NotFound => 0,
                Err(e) => return Err(e),
            };
            let next = previous.checked_add(1).ok_or_else(|| io::Error::other("smoke queue exhausted"))?;
            // Atomic publication survives a client being terminated while enrolling.
            let staged = directory.join("sequence.next");
            let mut file = open(&staged)?;
            file.set_len(0)?;
            writeln!(file, "{next}")?;
            fs::rename(staged, sequence)?;
            let path = directory.join(format!("{next:020}.ticket"));
            let lock = open(&path)?;
            lock.lock()?;
            Self { path, _lock: lock }
        };
        let deadline = Instant::now() + Duration::from_secs(3600);
        let mut announced = false;
        loop {
            let first = {
                let _guard = admission(directory)?;
                live(directory)?.first().cloned()
            };
            if first.as_ref() == Some(&ticket.path) { return Ok(ticket); }
            if !announced { eprintln!("Waiting for earlier smoke jobs..."); announced = true; }
            if Instant::now() >= deadline { return Err(io::Error::other("smoke queue wait timed out")); }
            std::thread::sleep(Duration::from_millis(100));
        }
    }
}

impl Drop for Ticket {
    fn drop(&mut self) { let _ = fs::remove_file(&self.path); }
}
