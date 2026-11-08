use std::time::{Duration, SystemTime};

#[allow(dead_code)]
#[derive(Clone, Debug)]
pub struct ProcessRow {
    pub pid: u32,
    pub parent_pid: u32,
    pub name: String,
    pub is_main: bool,
    pub rss_bytes: Option<u64>,
    pub cpu_percent: Option<f64>,
    pub started_at: Option<SystemTime>,
    pub uptime: Option<Duration>,
    pub thread_count: Option<u32>,
}

#[derive(Default)]
pub struct ProcessSnapshot {
    pub processes: Vec<ProcessRow>,
    pub errors: Vec<String>,
}

#[cfg(target_os = "macos")]
#[path = "process_macos.rs"]
mod platform;

#[cfg(target_os = "macos")]
pub use platform::ProcessSampler;

#[cfg(not(target_os = "macos"))]
#[derive(Default)]
pub struct ProcessSampler;

#[cfg(not(target_os = "macos"))]
impl ProcessSampler {
    pub fn new() -> Self { Self }
    pub fn sample(&mut self) -> Result<ProcessSnapshot, String> {
        Err("Process metrics are not supported on this platform".into())
    }
}
