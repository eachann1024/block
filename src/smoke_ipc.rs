//! Owner-only resident smoke IPC. Binding and the lifetime lock precede GUI startup.
use std::{fs::{self, File, OpenOptions}, io::{self, BufRead, BufReader, Read, Write},
    os::unix::{fs::{DirBuilderExt, MetadataExt, OpenOptionsExt, PermissionsExt}, net::{UnixListener, UnixStream}},
    path::PathBuf, sync::{Mutex, OnceLock, atomic::{AtomicBool, AtomicUsize, Ordering}, mpsc}, time::Duration};

pub(super) struct Job { pub stream: UnixStream }
pub(super) enum Request { Run(Job), Stop }
static QUEUE: OnceLock<Mutex<Option<mpsc::Receiver<Request>>>> = OnceLock::new();
static QUEUED: AtomicUsize = AtomicUsize::new(0);
static ACTIVE: AtomicBool = AtomicBool::new(false);

pub(super) fn endpoint() -> io::Result<PathBuf> {
    let home = std::env::var_os("HOME").ok_or_else(|| io::Error::other("HOME is unavailable"))?;
    let uid = fs::metadata(home)?.uid();
    let dir = PathBuf::from(format!("/tmp/block-smoke-worker-{uid}"));
    match fs::DirBuilder::new().mode(0o700).create(&dir) {
        Ok(()) => (), Err(e) if e.kind() == io::ErrorKind::AlreadyExists => (), Err(e) => return Err(e),
    }
    let meta = fs::symlink_metadata(&dir)?;
    if !meta.is_dir() || meta.uid() != uid || meta.permissions().mode() & 0o077 != 0 {
        return Err(io::Error::other("smoke IPC directory must be owned by the user with mode 0700"));
    }
    // All launch paths and build profiles share one worker and admission queue.
    Ok(dir.join("resident.sock"))
}

pub(super) fn fingerprint() -> io::Result<String> {
    fingerprint_at(&std::env::current_exe()?)
}

pub(super) fn fingerprint_at(path: &std::path::Path) -> io::Result<String> {
    use std::hash::Hasher;
    let mut file = File::open(path)?;
    let mut hash = std::collections::hash_map::DefaultHasher::new();
    let mut buffer = [0u8; 65536];
    loop {
        let size = file.read(&mut buffer)?;
        if size == 0 { break; }
        hash.write(&buffer[..size]);
    }
    Ok(format!("{:016x}", hash.finish()))
}

pub(super) fn line(stream: &mut UnixStream) -> io::Result<String> {
    let mut text = String::new();
    BufReader::new(stream).take(1024).read_line(&mut text)?;
    if !text.ends_with('\n') { return Err(io::Error::other("invalid or oversized smoke response")); }
    Ok(text.trim().to_owned())
}

pub(super) fn start() -> io::Result<bool> {
    let path = endpoint()?;
    let lock: File = OpenOptions::new().create(true).truncate(false).read(true).write(true)
        .mode(0o600).open(path.with_extension("lock"))?;
    if lock.try_lock().is_err() { return Ok(false); }
    match UnixStream::connect(&path) {
        Ok(_) => return Ok(false),
        Err(e) if matches!(e.kind(), io::ErrorKind::NotFound | io::ErrorKind::ConnectionRefused) => (),
        Err(e) => return Err(e),
    }
    match fs::remove_file(&path) {
        Ok(()) => (), Err(e) if e.kind() == io::ErrorKind::NotFound => (), Err(e) => return Err(e),
    }
    let listener = UnixListener::bind(&path)?;
    fs::set_permissions(&path, fs::Permissions::from_mode(0o600))?;
    let version = std::env::var("BLOCK_SMOKE_SOURCE_VERSION").map(Ok).unwrap_or_else(|_| fingerprint())?;
    let (tx, rx) = mpsc::channel();
    QUEUE.set(Mutex::new(Some(rx))).map_err(|_| io::Error::other("worker already started"))?;
    std::thread::spawn(move || {
        let _lock = lock;
        let mut stopping = false;
        for connection in listener.incoming() {
            let Ok(mut stream) = connection else { continue };
            let _ = stream.set_read_timeout(Some(Duration::from_secs(2)));
            let _ = stream.set_write_timeout(Some(Duration::from_secs(2)));
            let Ok(request) = line(&mut stream) else { continue };
            let parts: Vec<_> = request.split_whitespace().collect();
            match parts.as_slice() {
                ["STATUS"] => { let _ = writeln!(stream, "OK pid={} active={} queued={} executable={version}",
                    std::process::id(), ACTIVE.load(Ordering::SeqCst), QUEUED.load(Ordering::SeqCst)); }
                ["STOP"] => {
                    if !stopping { let _ = tx.send(Request::Stop); stopping = true; }
                    let _ = writeln!(stream, "OK stopping after queued jobs");
                }
                ["RUN", ..] if stopping => { let _ = writeln!(stream, "ERR worker is stopping"); }
                ["RUN", client_version] if *client_version == version => {
                    QUEUED.fetch_add(1, Ordering::SeqCst);
                    if tx.send(Request::Run(Job { stream })).is_err() {
                        QUEUED.fetch_sub(1, Ordering::SeqCst);
                        break;
                    }
                }
                ["RUN", ..] => { let _ = writeln!(stream, "ERR executable changed; run --smoke-stop, then retry after worker exits"); }
                _ => { let _ = writeln!(stream, "ERR unknown smoke command"); }
            }
        }
        let _ = fs::remove_file(path);
    });
    Ok(true)
}

pub(super) fn receiver() -> mpsc::Receiver<Request> {
    QUEUE.get().expect("worker listener").lock().unwrap().take().expect("single worker runner")
}
pub(super) fn active(value: bool) {
    ACTIVE.store(value, Ordering::SeqCst);
    if value { QUEUED.fetch_sub(1, Ordering::SeqCst); }
}
pub(super) fn reply(mut job: Job, outcome: &Result<(), String>) {
    match outcome {
        Ok(()) => { let _ = writeln!(job.stream, "OK smoke passed"); }
        Err(error) => { let _ = writeln!(job.stream, "ERR {}", error.replace(['\n', '\r'], " ")); }
    }
}
