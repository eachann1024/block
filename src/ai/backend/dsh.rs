mod process;

use std::{sync::{mpsc::Sender, Arc, Mutex, atomic::{AtomicBool, Ordering}}, process::{Child, Command, Stdio}, thread, time::{Duration, Instant}};
use super::{mcp_bridge as bridge, ApprovalResponse, BackendAdapter, BackendError, BackendEvent, BackendEventKind, BackendStatus, RunningTurn, ToolBridge, TurnRequest, cli_env};

#[derive(Default)]
pub(crate) struct DshAdapter;

impl BackendAdapter for DshAdapter {
    fn probe(&self) -> BackendStatus {
        let mut child = match cli_env::command("dsh").arg("--version").stdin(Stdio::null())
            .stdout(Stdio::piped()).stderr(Stdio::null()).spawn() {
            Ok(child) => child,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return BackendStatus::Missing,
            Err(error) => return BackendStatus::Error { message: error.to_string() },
        };
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            match child.try_wait() {
                Ok(Some(status)) if status.success() => {
                    use std::io::Read;
                    let mut version = String::new();
                    if let Some(mut output) = child.stdout.take() {
                        if let Err(error) = output.read_to_string(&mut version) {
                            return BackendStatus::Error { message: error.to_string() };
                        }
                    }
                    return BackendStatus::Ready { version: version.trim().to_owned() };
                }
                Ok(Some(status)) => return BackendStatus::Error { message: format!("dsh --version exited with {status}") },
                Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(25)),
                Ok(None) => {
                    let _ = child.kill();
                    let _ = child.wait();
                    return BackendStatus::Error { message: "dsh --version timed out".into() };
                }
                Err(error) => {
                    let _ = child.kill();
                    let _ = child.wait();
                    return BackendStatus::Error { message: error.to_string() };
                }
            }
        }
    }

    fn start(&mut self, request: TurnRequest, events: Sender<BackendEvent>, tools: ToolBridge)
        -> Result<Box<dyn RunningTurn>, BackendError> {
        let cancelled = Arc::new(AtomicBool::new(false));
        let (child, active, worker) = process::start(request.clone(), events.clone(), tools, cancelled.clone())?;
        thread::spawn(worker);
        Ok(Box::new(DshTurn { child, active, events, request, cancelled }))
    }
}

struct DshTurn {
    child: Arc<Mutex<Child>>,
    active: Arc<AtomicBool>,
    events: Sender<BackendEvent>,
    request: TurnRequest,
    cancelled: Arc<AtomicBool>,
}

impl RunningTurn for DshTurn {
    fn cancel(&mut self) -> Result<(), BackendError> {
        if self.cancelled.load(Ordering::Acquire) { return Ok(()); }
        let mut child = self.child.lock().map_err(|error| BackendError::new("dsh_lock", error.to_string()))?;
        if child.try_wait().map_err(|error| BackendError::new("dsh_wait", error.to_string()))?.is_none() {
            self.cancelled.store(true, Ordering::Release);
            self.active.store(false, Ordering::Release);
            #[cfg(unix)] {
                let status = Command::new("/bin/kill").args(["-TERM", "--", &format!("-{}", child.id())])
                    .stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null()).status();
                if !status.is_ok_and(|status| status.success()) { let _ = child.kill(); }
                let process = self.child.clone();
                thread::spawn(move || {
                    thread::sleep(Duration::from_secs(3));
                    if let Ok(mut child) = process.lock() {
                        if child.try_wait().ok().flatten().is_none() { let _ = child.kill(); }
                    }
                });
            }
            #[cfg(not(unix))]
            child.kill().map_err(|error| BackendError::new("dsh_cancel", error.to_string()))?;
            let _ = self.events.send(BackendEvent { conversation_id: self.request.conversation_id.clone(),
                turn_id: self.request.turn_id.clone(), kind: BackendEventKind::Cancelled });
        }
        Ok(())
    }

    fn respond(&mut self, _request_id: &str, _response: ApprovalResponse) -> Result<(), BackendError> {
        Err(BackendError::new("dsh_approval_via_tool", "Respond to Block tool approvals through the tool bridge"))
    }
}
