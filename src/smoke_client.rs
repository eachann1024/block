//! CLI entry point; clients never initialize logging or GPUI.
#[cfg(unix)]
use super::ipc;
#[cfg(unix)]
#[path = "smoke_queue.rs"] mod queue;

pub(super) fn handle() -> bool {
    let args: Vec<_> = std::env::args().collect();
    #[cfg(not(unix))]
    if args.iter().any(|a| a == "--smoke-once") { return false; }
    let worker = args.iter().any(|a| a == "--smoke-worker");
    let command = if worker { "WORKER" }
        else if args.iter().any(|a| a == "--smoke-status") { "STATUS" }
        else if args.iter().any(|a| a == "--smoke-stop") { "STOP" }
        else if args.iter().any(|a| matches!(a.as_str(), "--smoke" | "--smoke-test" | "--smoke-once")) { "RUN" }
        else { return false };
    #[cfg(unix)]
    {
        let result = if worker { ipc::start().map(|started| if started { 0 } else { 1 }) }
            else { submit(command) };
        match result {
            Ok(0) if worker => return false,
            Ok(code) => std::process::exit(code),
            Err(error) => { eprintln!("[FAIL] smoke worker: {error}"); std::process::exit(1); }
        }
    }
    #[cfg(not(unix))]
    { let _ = (worker, command); eprintln!("Resident smoke workers require Unix; use --smoke-once"); std::process::exit(1); }
}

#[cfg(unix)]
fn submit(command: &str) -> std::io::Result<i32> {
    use std::{io::Write, os::unix::net::UnixStream, time::Duration};
    let path = ipc::endpoint()?;
    // Keep the ticket until the result arrives: version changes and STOP cannot
    // overtake earlier runs, including jobs submitted from another executable.
    let _ticket = if command == "STATUS" { None } else { Some(queue::Ticket::wait(&path)?) };
    if command == "STOP" {
        drain_legacy_workers(&path)?;
        stop_and_wait(&path)?;
        println!("OK worker stopped after earlier jobs");
        return Ok(0);
    }
    let version = if command == "RUN" { Some(ipc::fingerprint()?) } else { None };
    if let Some(version) = &version {
        drain_legacy_workers(&path)?;
        if let Ok(mut stream) = UnixStream::connect(&path) {
            stream.set_read_timeout(Some(Duration::from_secs(5)))?;
            stream.set_write_timeout(Some(Duration::from_secs(5)))?;
            writeln!(stream, "STATUS")?;
            let status = ipc::line(&mut stream)?;
            if !status.split_whitespace().any(|part| part == format!("executable={version}")) {
                eprintln!("Waiting for the previous worker before loading this build...");
                stop_and_wait(&path)?;
            }
        }
    }
    let mut connection = UnixStream::connect(&path);
    if connection.is_err() && command == "RUN" {
        connection = start_worker(&path);
    }
    let mut stream = connection?;
    stream.set_write_timeout(Some(Duration::from_secs(5)))?;
    stream.set_read_timeout(Some(Duration::from_secs(if command == "RUN" { 3600 } else { 5 })))?;
    if command == "RUN" {
        writeln!(stream, "RUN {}", version.expect("RUN fingerprint"))?;
    } else { writeln!(stream, "{command}")?; }
    let response = ipc::line(&mut stream)?;
    if command == "STATUS" {
        println!("{response} pending={}", queue::pending(&path)?);
    } else { println!("{response}"); }
    Ok(if response.starts_with("OK ") { 0 } else { 1 })
}

#[cfg(unix)]
fn stop_and_wait(path: &std::path::Path) -> std::io::Result<()> {
    use std::{fs::{self, OpenOptions}, io::{self, Write},
        os::unix::{fs::OpenOptionsExt, net::UnixStream}, time::{Duration, Instant}};
    let mut stream = match UnixStream::connect(path) {
        Ok(stream) => stream,
        Err(e) if matches!(e.kind(), io::ErrorKind::NotFound | io::ErrorKind::ConnectionRefused) => return Ok(()),
        Err(e) => return Err(e),
    };
    stream.set_read_timeout(Some(Duration::from_secs(5)))?;
    stream.set_write_timeout(Some(Duration::from_secs(5)))?;
    writeln!(stream, "STOP")?;
    if !ipc::line(&mut stream)?.starts_with("OK ") { return Err(io::Error::other("worker refused to stop")); }
    drop(stream);
    let lock = OpenOptions::new().create(true).truncate(false).read(true).write(true)
        .mode(0o600).open(path.with_extension("lock"))?;
    let deadline = Instant::now() + Duration::from_secs(3600);
    loop {
        match lock.try_lock() {
            Ok(()) => return Ok(()),
            Err(fs::TryLockError::Error(e)) => return Err(e),
            Err(fs::TryLockError::WouldBlock) => (),
        }
        if Instant::now() >= deadline { return Err(io::Error::other("previous worker did not finish; refusing another GUI")); }
        std::thread::sleep(Duration::from_millis(100));
    }
}

#[cfg(unix)]
fn drain_legacy_workers(endpoint: &std::path::Path) -> std::io::Result<()> {
    for entry in std::fs::read_dir(endpoint.parent().expect("smoke directory"))? {
        let path = entry?.path();
        let legacy = path.file_stem().and_then(|s| s.to_str())
            .is_some_and(|s| s.len() == 16 && s.bytes().all(|c| c.is_ascii_hexdigit()));
        if legacy && path.extension().is_some_and(|ext| ext == "sock") { stop_and_wait(&path)?; }
    }
    Ok(())
}

#[cfg(unix)]
fn start_worker(path: &std::path::Path) -> std::io::Result<std::os::unix::net::UnixStream> {
    use std::{fs::{self, OpenOptions}, io, os::unix::{fs::OpenOptionsExt, net::UnixStream},
        time::{Duration, Instant}};
    let lock = OpenOptions::new().create(true).truncate(false).read(true).write(true)
        .mode(0o600).open(path.with_extension("startup.lock"))?;
    let deadline = Instant::now() + Duration::from_secs(15);
    loop {
        if let Ok(stream) = UnixStream::connect(path) { return Ok(stream); }
        match lock.try_lock() {
            Ok(()) => break,
            Err(fs::TryLockError::WouldBlock) => (),
            Err(fs::TryLockError::Error(error)) => return Err(error),
        }
        if Instant::now() >= deadline { return Err(io::Error::other("smoke worker startup lock timed out; retry")); }
        std::thread::sleep(Duration::from_millis(50));
    }
    // Another client may have finished startup before we acquired the lock.
    if let Ok(stream) = UnixStream::connect(path) { return Ok(stream); }
    let mut child = spawn_worker_copy(path)?;
    let deadline = Instant::now() + Duration::from_secs(30);
    loop {
        if let Ok(stream) = UnixStream::connect(path) { return Ok(stream); }
        if let Some(status) = child.try_wait()? {
            return Err(io::Error::other(format!("smoke worker exited during startup: {status}; retry")));
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            return Err(io::Error::other("smoke worker startup timed out; retry"));
        }
        std::thread::sleep(Duration::from_millis(50));
    }
}

#[cfg(unix)]
fn spawn_worker_copy(endpoint: &std::path::Path) -> std::io::Result<std::process::Child> {
    use std::{fs, hash::{Hash, Hasher}, io, os::unix::{fs::{DirBuilderExt, MetadataExt}, process::CommandExt},
        process::{Command, Stdio}, time::{SystemTime, UNIX_EPOCH}};
    let source = fs::canonicalize(std::env::current_exe()?)?;
    let version = ipc::fingerprint_at(&source)?;
    let stamp = |m: fs::Metadata| (m.dev(), m.ino(), m.len(), m.mtime(), m.mtime_nsec(), m.ctime(), m.ctime_nsec());
    let before = stamp(fs::metadata(&source)?);
    let mut hash = std::collections::hash_map::DefaultHasher::new();
    version.hash(&mut hash);
    let nonce = SystemTime::now().duration_since(UNIX_EPOCH).map_err(io::Error::other)?.as_nanos();
    let directory = endpoint.parent().ok_or_else(|| io::Error::other("invalid smoke endpoint"))?
        .join(format!("worker-{:016x}-{}-{nonce}", hash.finish(), std::process::id()));
    fs::DirBuilder::new().mode(0o700).create(&directory)?;
    let executable = directory.join("block");
    let result = (|| {
        fs::copy(&source, &executable)?;
        if before != stamp(fs::metadata(&source)?) || version != ipc::fingerprint_at(&source)? {
            return Err(io::Error::other("smoke source executable changed while copying; retry"));
        }
        Command::new(&executable).arg("--smoke-worker").process_group(0)
            .env("BLOCK_BACKGROUND_LAUNCH", "1").env_remove("__CFBundleIdentifier")
            .env("BLOCK_SMOKE_SOURCE_EXE", &source).env("BLOCK_SMOKE_SOURCE_VERSION", version)
            .stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null()).spawn()
    })();
    if result.is_err() { let _ = fs::remove_dir_all(directory); }
    result
}
