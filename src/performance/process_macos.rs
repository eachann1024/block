use super::{ProcessRow, ProcessSnapshot};
use std::{collections::{HashMap, HashSet}, ffi::{c_char, c_int, c_void}, mem::{size_of, MaybeUninit}, process::{Command, Stdio}, time::{Duration, Instant, SystemTime}};

#[repr(C)]
struct BsdInfo {
    flags: u32, status: u32, xstatus: u32, pid: u32, ppid: u32,
    uid: u32, gid: u32, ruid: u32, rgid: u32, svuid: u32, svgid: u32, reserved: u32,
    comm: [c_char; 16], name: [c_char; 32],
    nfiles: u32, pgid: u32, jobc: u32, tdev: u32, tpgid: u32, nice: i32,
    start_sec: u64, start_usec: u64,
}

#[repr(C)]
struct TaskInfo {
    virtual_size: u64, resident_size: u64, total_user: u64, total_system: u64,
    threads_user: u64, threads_system: u64, policy: i32, faults: i32, pageins: i32,
    cow_faults: i32, messages_sent: i32, messages_received: i32, syscalls_mach: i32,
    syscalls_unix: i32, csw: i32, thread_count: i32, num_running: i32, priority: i32,
}

#[repr(C)]
struct TaskAllInfo { bsd: BsdInfo, task: TaskInfo }

unsafe extern "C" {
    fn proc_pidinfo(pid: c_int, flavor: c_int, arg: u64, buffer: *mut c_void, size: c_int) -> c_int;
    fn sysctlbyname(name: *const c_char, old: *mut c_void, old_len: *mut usize, new: *mut c_void, new_len: usize) -> c_int;
    fn kill(pid: c_int, signal: c_int) -> c_int;
}

struct Previous { start: (u64, u64), ticks: u64, at: Instant }

#[derive(Default)]
pub struct ProcessSampler { previous: HashMap<u32, Previous> }

impl ProcessSampler {
    pub fn new() -> Self { Self::default() }

    pub fn sample(&mut self) -> Result<ProcessSnapshot, String> {
        let child = Command::new("/bin/ps").args(["-axo", "pid=,ppid="]).stdout(Stdio::piped()).stderr(Stdio::piped()).spawn().map_err(|error| format!("ps: {error}"))?;
        let ps_pid = child.id();
        let output = child.wait_with_output().map_err(|error| format!("ps: {error}"))?;
        if !output.status.success() { return Err(format!("ps: {}", String::from_utf8_lossy(&output.stderr))); }
        let mut relations = Vec::new();
        for line in String::from_utf8_lossy(&output.stdout).lines() {
            let mut fields = line.split_whitespace();
            if let (Some(Ok(pid)), Some(Ok(parent))) = (fields.next().map(str::parse::<u32>), fields.next().map(str::parse::<u32>)) {
                if pid != ps_pid { relations.push((pid, parent)); }
            }
        }
        let main = std::process::id();
        let mut descendants = HashSet::from([main]);
        loop {
            let before = descendants.len();
            for &(pid, parent) in &relations { if descendants.contains(&parent) { descendants.insert(pid); } }
            if descendants.len() == before { break; }
        }
        let frequency = frequency();
        let mut result = ProcessSnapshot::default();
        if frequency.is_none() { result.errors.push("hw.tbfrequency unavailable; CPU percentage unavailable".into()); }
        let mut next = HashMap::new();
        let mut ids: Vec<_> = descendants.into_iter().collect();
        ids.sort_unstable();
        for pid in ids {
            let mut data = MaybeUninit::<TaskAllInfo>::uninit();
            let size = size_of::<TaskAllInfo>();
            let returned = unsafe { proc_pidinfo(pid as c_int, 2, 0, data.as_mut_ptr().cast(), size as c_int) };
            if returned != size as c_int {
                if pid == main { self.previous.clear(); return Err(format!("proc_pidinfo: {}", std::io::Error::last_os_error())); }
                if unsafe { kill(pid as c_int, 0) } != 0 && std::io::Error::last_os_error().raw_os_error() == Some(3) { continue; }
                result.errors.push(format!("Process {pid} unavailable or exited"));
                continue;
            }
            let data = unsafe { data.assume_init() };
            if pid != main && !relations.iter().any(|&(candidate, parent)| candidate == pid && parent == data.bsd.ppid) { continue; }
            let at = Instant::now();
            let start = (data.bsd.start_sec, data.bsd.start_usec);
            let ticks = data.task.total_user.checked_add(data.task.total_system);
            let cpu_percent = ticks.and_then(|ticks| self.previous.get(&pid).and_then(|previous| {
                if previous.start != start { return None; }
                let delta = ticks.checked_sub(previous.ticks)?;
                let seconds = at.checked_duration_since(previous.at)?.as_secs_f64();
                let frequency = frequency?;
                (seconds > 0.0).then_some(delta as f64 / frequency as f64 / seconds * 100.0)
            }));
            if let Some(ticks) = ticks { next.insert(pid, Previous { start, ticks, at }); }
            let started_at = (data.bsd.start_usec < 1_000_000).then(|| Duration::new(data.bsd.start_sec, (data.bsd.start_usec * 1000) as u32))
                .and_then(|duration| SystemTime::UNIX_EPOCH.checked_add(duration));
            let uptime = started_at.and_then(|start| SystemTime::now().duration_since(start).ok());
            let name_bytes: Vec<u8> = data.bsd.comm.iter().take_while(|&&byte| byte != 0).map(|&byte| byte as u8).collect();
            let name = String::from_utf8_lossy(&name_bytes).into_owned();
            result.processes.push(ProcessRow { pid, parent_pid: data.bsd.ppid, name, is_main: pid == main,
                rss_bytes: Some(data.task.resident_size), cpu_percent, started_at, uptime,
                thread_count: u32::try_from(data.task.thread_count).ok() });
        }
        self.previous = next;
        Ok(result)
    }
}

fn frequency() -> Option<u64> {
    let mut value = 0u64;
    let mut len = size_of::<u64>();
    let status = unsafe { sysctlbyname(c"hw.tbfrequency".as_ptr(), (&mut value as *mut u64).cast(), &mut len, std::ptr::null_mut(), 0) };
    (status == 0 && len == size_of::<u64>() && value > 0).then_some(value)
}
