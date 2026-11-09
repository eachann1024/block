use std::{
    io::{BufRead, BufReader, Read, Write},
    process::{ChildStdin, Stdio},
    sync::{Arc, Mutex, mpsc::{self, Receiver}},
    thread,
    time::Duration,
};
use serde_json::{Value, json};
use super::super::{BackendError, cli_env};
#[path = "../codex/process_group.rs"]
mod process_group;
pub(super) use process_group::SharedChild;
pub(super) fn stop(child: &SharedChild) { process_group::stop(child); }

pub(super) type Input = Arc<Mutex<Option<ChildStdin>>>;

pub(super) fn send(input: &Input, frame: Value) -> Result<(), BackendError> {
    let mut guard = input.lock().map_err(|_| error("agy stdin lock failed"))?;
    let stdin = guard.as_mut().ok_or_else(|| error("agy process has closed"))?;
    writeln!(stdin, "{frame}").and_then(|_| stdin.flush()).map_err(error)
}

pub(super) fn request(input: &Input, id: u64, method: &str, params: Value) -> Result<(), BackendError> {
    send(input, json!({"jsonrpc":"2.0", "id":id, "method":method, "params":params}))
}

pub(super) fn error(message: impl std::fmt::Display) -> BackendError {
    BackendError::new("agy_process", message.to_string())
}

pub(super) fn initialize(input: &Input) -> Result<(), BackendError> {
    request(input, 1, "initialize", json!({"protocolVersion":1,
        "clientInfo":{"name":"Block", "version":env!("CARGO_PKG_VERSION")},
        "clientCapabilities":{}}))
}

pub(super) struct Connection {
    pub child: process_group::SharedChild,
    pub input: Input,
    pub frames: Receiver<Result<Value, BackendError>>,
    stderr: Arc<Mutex<String>>,
}

impl Connection {
    pub fn spawn(full_permissions: bool) -> Result<Self, BackendError> {
        let mut command = cli_env::command("agy");
        command.args(["acp", "--disable-telemetry"]);
        if full_permissions { command.arg("--dangerously-skip-permissions"); }
        command.stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::piped());
        process_group::configure(&mut command);
        let mut child = command.spawn().map_err(|err| BackendError::new(
            if err.kind() == std::io::ErrorKind::NotFound { "missing" } else { "agy_process" }, err.to_string()))?;
        let input = Arc::new(Mutex::new(child.stdin.take()));
        let stdout = child.stdout.take().ok_or_else(|| error("agy stdout unavailable"))?;
        let stderr = Arc::new(Mutex::new(String::new()));
        if let Some(mut output) = child.stderr.take() {
            let tail = stderr.clone();
            thread::spawn(move || {
                let mut bytes = [0; 1024];
                while let Ok(count) = output.read(&mut bytes) {
                    if count == 0 { break; }
                    if let Ok(mut tail) = tail.lock() {
                        tail.push_str(&String::from_utf8_lossy(&bytes[..count]));
                        if tail.len() > 8192 {
                            let mut start = tail.len() - 8192;
                            while !tail.is_char_boundary(start) { start += 1; }
                            tail.drain(..start);
                        }
                    }
                }
            });
        }
        let (sender, frames) = mpsc::channel();
        thread::spawn(move || {
            for line in BufReader::new(stdout).lines() {
                let frame = line.map_err(error).and_then(|line| serde_json::from_str(&line)
                    .map_err(|err| BackendError::new("agy_protocol", err.to_string())));
                if sender.send(frame).is_err() { break; }
            }
        });
        Ok(Self { child: process_group::managed(child), input, frames, stderr })
    }

    pub fn receive(&self, timeout: Duration) -> Result<Value, BackendError> {
        self.frames.recv_timeout(timeout).map_err(|err| {
            let detail = self.stderr.lock().ok().map(|text| text.trim().to_owned()).unwrap_or_default();
            error(if detail.is_empty() { format!("agy: {err}") } else { detail })
        })?
    }
}

impl Drop for Connection {
    fn drop(&mut self) {
        if let Ok(mut input) = self.input.lock() { input.take(); }
        process_group::stop(&self.child);
    }
}
