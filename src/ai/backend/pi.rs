#[path = "pi/process.rs"]
mod process;

use std::{process::Stdio, sync::mpsc::Sender, thread, time::{Duration, Instant}};
use super::{
    ApprovalResponse, BackendAdapter, BackendError, BackendEvent, BackendStatus, ModelOption,
    RunningTurn, ToolBridge, TurnRequest, cli_env,
};

#[derive(Default)]
pub(crate) struct PiAdapter;

impl BackendAdapter for PiAdapter {
    fn probe(&self) -> BackendStatus {
        let mut child = match cli_env::command("pi")
            .arg("--version")
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
        {
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
                        let _ = output.read_to_string(&mut version);
                    }
                    return BackendStatus::Ready { version: version.trim().to_owned() };
                }
                Ok(Some(status)) => {
                    return BackendStatus::Error { message: format!("pi --version exited with {status}") };
                }
                Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(25)),
                Ok(None) => {
                    let _ = child.kill();
                    let _ = child.wait();
                    return BackendStatus::Error { message: "pi --version timed out".into() };
                }
                Err(error) => {
                    let _ = child.kill();
                    let _ = child.wait();
                    return BackendStatus::Error { message: error.to_string() };
                }
            }
        }
    }

    fn start(
        &mut self,
        request: TurnRequest,
        events: Sender<BackendEvent>,
        tools: ToolBridge,
    ) -> Result<Box<dyn RunningTurn>, BackendError> {
        process::start(request, events, tools)
    }
}

pub(crate) fn list_models() -> Result<Vec<ModelOption>, String> {
    let output = cli_env::command("pi")
        .arg("--list-models")
        .stdin(Stdio::null())
        .output()
        .map_err(|error| error.to_string())?;
    if !output.status.success() {
        return Err("Pi model list failed".into());
    }
    let text = String::from_utf8_lossy(&output.stdout);
    let mut options = Vec::new();
    for line in text.lines() {
        let mut parts = line.split_whitespace();
        let (Some(provider), Some(model)) = (parts.next(), parts.next()) else { continue };
        if provider == "provider" && model == "model" {
            continue;
        }
        let value = format!("{provider}/{model}");
        options.push(ModelOption { label: value.clone(), value });
    }
    Ok(options)
}

pub(super) struct PiTurn {
    pub child: process::SharedChild,
    pub cancelled: std::sync::Arc<std::sync::atomic::AtomicBool>,
    pub finished: std::sync::Arc<std::sync::atomic::AtomicBool>,
    pub tools: ToolBridge,
}

impl RunningTurn for PiTurn {
    fn cancel(&mut self) -> Result<(), BackendError> {
        if self.finished.load(std::sync::atomic::Ordering::Acquire) {
            return Ok(());
        }
        self.cancelled.store(true, std::sync::atomic::Ordering::Release);
        self.tools.cancel();
        process::stop_child(&self.child);
        Ok(())
    }

    fn respond(&mut self, _request_id: &str, _response: ApprovalResponse) -> Result<(), BackendError> {
        Ok(())
    }
}

impl Drop for PiTurn {
    fn drop(&mut self) {
        if !self.finished.load(std::sync::atomic::Ordering::Acquire) {
            let _ = self.cancel();
        }
    }
}
