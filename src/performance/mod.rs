mod disk;
mod colors;
mod process;
mod view;

pub use disk::{DiskPaths, DiskSnapshot, scan_disk_with_cancel};
pub use process::{ProcessSampler, ProcessSnapshot};
pub use view::view;

use std::{path::PathBuf, sync::{Arc, Mutex, atomic::{AtomicBool, AtomicU64, Ordering}}, time::Duration};
use gpui::Context;
use crate::app::Workspace;

static GENERATION: AtomicU64 = AtomicU64::new(0);
static DISK_BUSY: AtomicBool = AtomicBool::new(false);
static STATE: Mutex<State> = Mutex::new(State::new());

struct State {
    open: bool,
    generation: u64,
    cancel: Option<Arc<AtomicBool>>,
    process: Option<Result<ProcessSnapshot, String>>,
    process_samples: u64,
    disk: Option<Result<DiskSnapshot, String>>,
    disk_at: Option<std::time::SystemTime>,
    disk_loading: bool,
    notebooks: Vec<PathBuf>,
    selected: Option<String>,
}

impl State {
    const fn new() -> Self {
        Self { open: false, generation: 0, cancel: None, process: None, process_samples: 0, disk: None,
            disk_at: None, disk_loading: false, notebooks: Vec::new(), selected: None }
    }
}

pub fn close() -> bool {
    let mut state = STATE.lock().unwrap_or_else(|e| e.into_inner());
    if !state.open { return false; }
    state.open = false;
    if let Some(cancel) = state.cancel.take() { cancel.store(true, Ordering::Release); }
    GENERATION.fetch_add(1, Ordering::AcqRel);
    true
}

pub fn open(notebooks: Vec<PathBuf>, cx: &mut Context<Workspace>) {
    if STATE.lock().unwrap_or_else(|e| e.into_inner()).open { return; }
    let generation = GENERATION.fetch_add(1, Ordering::AcqRel) + 1;
    let cancel = Arc::new(AtomicBool::new(false));
    {
        let mut state = STATE.lock().unwrap_or_else(|e| e.into_inner());
        if let Some(old) = state.cancel.take() { old.store(true, Ordering::Release); }
        *state = State { open: true, generation, cancel: Some(cancel.clone()), notebooks: notebooks.clone(), disk_loading: true, ..State::new() };
    }
    let paths = disk_paths(notebooks);
    let process_cancel = cancel.clone();
    std::thread::spawn(move || {
        let mut sampler = ProcessSampler::new();
        while !process_cancel.load(Ordering::Acquire) {
            let result = sampler.sample();
            let mut state = STATE.lock().unwrap_or_else(|e| e.into_inner());
            if state.open && state.generation == generation {
                if result.is_ok() { state.process_samples += 1; }
                state.process = Some(result);
            }
            drop(state);
            for _ in 0..10 {
                if process_cancel.load(Ordering::Acquire) { return; }
                std::thread::sleep(Duration::from_millis(100));
            }
        }
    });
    start_disk(paths, generation, cancel.clone());
    cx.spawn(async move |this, cx| {
        while !cancel.load(Ordering::Acquire) {
            cx.background_executor().timer(Duration::from_secs(1)).await;
            let _ = this.update(cx, |_, cx| cx.notify());
        }
    }).detach();
    cx.notify();
}

fn disk_paths(notebooks: Vec<PathBuf>) -> DiskPaths {
    if crate::smoke::is_requested() {
        if let Some(root) = notebooks.first().and_then(|path| path.parent()).map(|parent| parent.join("performance-fixture")) {
            if root.is_dir() {
                return DiskPaths { app: Some(root.join("app")), data: vec![root.join("data")],
                    notebooks: vec![root.join("notebook")], cache: vec![root.join("cache")] };
            }
        }
    }
    let app = std::env::current_exe().ok().map(|path| {
        path.ancestors().find(|p| p.extension().is_some_and(|e| e == "app")).unwrap_or(&path).to_path_buf()
    });
    let mut data = vec![crate::files::state::app_data_dir("Block")];
    if !crate::smoke::is_requested() {
        if let Some(state) = dirs::state_dir() { data.push(state.join("Block")); }
        if let Some(path) = std::env::var_os("BLOCK_LOG_FILE") { data.push(PathBuf::from(path)); }
        if let Some(path) = dirs::data_local_dir() { data.push(path.join("Block")); }
    }
    data.retain(|path| !matches!(std::fs::symlink_metadata(path), Err(error) if error.kind() == std::io::ErrorKind::NotFound));
    let cache = if crate::smoke::is_requested() { Vec::new() } else {
        let mut paths = dirs::cache_dir().map(|path| vec![path.join("Block")]).unwrap_or_default();
        paths.push(std::env::temp_dir().join("block-remote-images"));
        paths
    };
    DiskPaths { app, data, notebooks, cache }
}

fn start_disk(paths: DiskPaths, generation: u64, cancel: Arc<AtomicBool>) {
    let app_missing = paths.app.is_none();
    std::thread::spawn(move || {
        while !cancel.load(Ordering::Acquire) {
            if DISK_BUSY.compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire).is_ok() {
                let result = scan_disk_with_cancel(paths, cancel.clone()).join()
                    .map(|mut snapshot| {
                        if app_missing { snapshot.partial = true; snapshot.error_count += 1; snapshot.errors.push("无法定位应用文件 / Application path unavailable".into()); }
                        snapshot
                    })
                    .map_err(|_| "磁盘扫描线程异常退出 / Disk scan failed".to_owned());
                DISK_BUSY.store(false, Ordering::Release);
                let mut state = STATE.lock().unwrap_or_else(|e| e.into_inner());
                if state.open && state.generation == generation && !cancel.load(Ordering::Acquire) {
                    state.disk = Some(result);
                    state.disk_at = Some(std::time::SystemTime::now());
                    state.disk_loading = false;
                }
                return;
            }
            std::thread::sleep(Duration::from_millis(100));
        }
    });
}

pub(crate) struct SmokeState {
    pub open: bool,
    pub generation: u64,
    pub selected: Option<String>,
    pub process_samples: u64,
    pub process_ok: bool,
    pub cpu_ready: bool,
    pub disk_loading: bool,
    pub disk_categories: Vec<(String, u64)>,
    pub disk_error: Option<String>,
}

pub(crate) fn smoke_state() -> SmokeState {
    let state = STATE.lock().unwrap_or_else(|error| error.into_inner());
    let (disk_categories, disk_error) = match &state.disk {
        Some(Ok(snapshot)) => (snapshot.categories.iter().map(|category| (category.id.to_owned(), category.bytes)).collect(),
            if snapshot.partial { Some(snapshot.errors.join("; ")) } else { None }),
        Some(Err(error)) => (Vec::new(), Some(error.clone())),
        None => (Vec::new(), None),
    };
    let main = state.process.as_ref().and_then(|result| result.as_ref().ok())
        .and_then(|snapshot| snapshot.processes.iter().find(|row| row.is_main));
    SmokeState { open: state.open, generation: state.generation,
        selected: state.selected.clone(),
        process_samples: state.process_samples, process_ok: main.and_then(|row| row.rss_bytes).is_some_and(|rss| rss > 0),
        cpu_ready: main.and_then(|row| row.cpu_percent).is_some(), disk_loading: state.disk_loading, disk_categories, disk_error }
}

fn refresh_disk() {
    let (paths, generation, cancel) = {
        let mut state = STATE.lock().unwrap_or_else(|e| e.into_inner());
        if !state.open || state.disk_loading { return; }
        let Some(cancel) = state.cancel.clone() else { return };
        state.disk = None;
        state.disk_at = None;
        state.disk_loading = true;
        (disk_paths(state.notebooks.clone()), state.generation, cancel)
    };
    start_disk(paths, generation, cancel);
}
