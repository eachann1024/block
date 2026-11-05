use std::{process::{Child, Command}, sync::{Arc, Mutex}, thread, time::{Duration, Instant}};

pub(super) type SharedChild = Arc<Mutex<Process>>;

pub(super) struct Process {
    child: Child,
    stopped: bool,
}

pub(super) fn configure(command: &mut Command) {
    #[cfg(unix)]
    {
        use std::os::unix::process::CommandExt;
        command.process_group(0);
    }
    #[cfg(not(unix))]
    let _ = command;
}

pub(super) fn managed(child: Child) -> SharedChild {
    Arc::new(Mutex::new(Process { child, stopped: false }))
}

pub(super) fn stop(shared: &SharedChild) {
    let Ok(mut process) = shared.lock() else { return };
    if process.stopped { return; }
    process.stopped = true;
    #[cfg(unix)]
    {
        unsafe extern "C" { fn kill(pid: i32, signal: i32) -> i32; }
        if let Ok(pid) = i32::try_from(process.child.id()) {
            unsafe { kill(-pid, 9); }
        }
    }
    let _ = process.child.kill();
    let deadline = Instant::now() + Duration::from_secs(1);
    loop {
        match process.child.try_wait() {
            Ok(Some(_)) => return,
            Err(_) => return,
            Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(10)),
            Ok(None) => break,
        }
    }
    drop(process);
    let process = shared.clone();
    thread::spawn(move || { if let Ok(mut process) = process.lock() { let _ = process.child.wait(); } });
}
