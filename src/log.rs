//! Crash, error, and warning diagnostics logging (`~/Library/Logs/Block/block.log`).
//! Only `WARN`, `ERROR`, and `PANIC` entries are ever written to the log file.
use std::{
    backtrace::Backtrace,
    fs::{self, File, OpenOptions},
    io::Write,
    path::PathBuf,
    sync::{Mutex, OnceLock},
    time::{SystemTime, UNIX_EPOCH},
};

const MAX_LOG_BYTES: u64 = 2 * 1024 * 1024;

struct Logger {
    path: Option<PathBuf>,
    file: Mutex<Option<File>>,
    context: Mutex<String>,
}

static LOGGER: OnceLock<Logger> = OnceLock::new();

fn resolve_log_path() -> Option<PathBuf> {
    if let Some(custom) = std::env::var_os("BLOCK_LOG_FILE") {
        return Some(PathBuf::from(custom));
    }
    #[cfg(target_os = "macos")]
    if let Some(home) = std::env::var_os("HOME") {
        return Some(PathBuf::from(home).join("Library/Logs/Block/block.log"));
    }
    Some(crate::files::app_data_dir("Block").join("logs/block.log"))
}

fn open_log_file(path: &PathBuf) -> Option<File> {
    if let Some(parent) = path.parent() {
        let _ = fs::create_dir_all(parent);
    }
    if let Ok(meta) = fs::metadata(path) && meta.len() > MAX_LOG_BYTES {
        let prev = path.with_file_name("block.prev.log");
        let _ = fs::rename(path, prev);
    }
    OpenOptions::new().create(true).append(true).open(path).ok()
}

fn timestamp_ms() -> String {
    let dur = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default();
    let secs = dur.as_secs();
    let millis = dur.subsec_millis();
    format!("{secs}.{millis:03}")
}

fn write_line(level: &str, msg: &str, flush_disk: bool) {
    let ts = timestamp_ms();
    let ctx = LOGGER
        .get()
        .and_then(|l| l.context.lock().ok().map(|c| c.clone()))
        .unwrap_or_default();
    let line = if ctx.is_empty() {
        format!("[{ts}] [{level}] {msg}\n")
    } else {
        format!("[{ts}] [{level}] ({ctx}) {msg}\n")
    };
    let _ = std::io::stderr().write_all(line.as_bytes());
    if let Some(logger) = LOGGER.get()
        && let Ok(mut guard) = logger.file.lock()
    {
        if guard.is_none() {
            *guard = logger.path.as_ref().and_then(open_log_file);
        }
        if let Some(file) = guard.as_mut() {
            let _ = file.write_all(line.as_bytes());
            let _ = file.flush();
            if flush_disk {
                let _ = file.sync_all();
            }
        }
    }
}

#[cfg(unix)]
fn ignore_hangup_signals() {
    const SIGHUP: i32 = 1;
    const SIGPIPE: i32 = 13;
    const SIG_IGN: usize = 1;
    unsafe extern "C" {
        fn signal(sig: i32, handler: usize) -> usize;
    }
    unsafe {
        signal(SIGHUP, SIG_IGN);
        signal(SIGPIPE, SIG_IGN);
    }
}

pub fn init() {
    #[cfg(unix)]
    ignore_hangup_signals();
    let _ = LOGGER.set(Logger {
        path: resolve_log_path(),
        file: Mutex::new(None),
        context: Mutex::new(String::from("startup")),
    });
    install_panic_hook();
}

#[cfg(target_os = "macos")]
fn main_image_base() -> String {
    unsafe extern "C" {
        static _mh_execute_header: u8;
    }
    format!("0x{:x}", (&raw const _mh_execute_header) as usize)
}

#[cfg(not(target_os = "macos"))]
fn main_image_base() -> String {
    String::from("<unavailable>")
}

fn install_panic_hook() {
    std::panic::set_hook(Box::new(|info| {
        let payload = if let Some(s) = info.payload().downcast_ref::<&str>() {
            (*s).to_owned()
        } else if let Some(s) = info.payload().downcast_ref::<String>() {
            s.clone()
        } else {
            String::from("<non-string panic payload>")
        };
        let location = info
            .location()
            .map(|l| format!("{}:{}:{}", l.file(), l.line(), l.column()))
            .unwrap_or_else(|| String::from("<unknown location>"));
        let thread = std::thread::current().name().unwrap_or("unnamed").to_owned();
        let image_base = main_image_base();
        let bt = Backtrace::force_capture();
        let report = format!(
            "=== CRASH / PANIC REPORT ===\nthread: {thread}\nlocation: {location}\nimage_base: {image_base}\nmessage: {payload}\nbacktrace:\n{bt}\n=== END CRASH REPORT ==="
        );
        write_line("PANIC", &report, true);
    }));
}

pub fn set_context(ctx: impl Into<String>) {
    if let Some(logger) = LOGGER.get()
        && let Ok(mut guard) = logger.context.lock()
    {
        *guard = ctx.into();
    }
}

pub fn warn(msg: &str) {
    write_line("WARN", msg, false);
}

pub fn error(msg: &str) {
    write_line("ERROR", msg, true);
}
