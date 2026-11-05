mod protocol;
mod worker;
pub(crate) mod models;

use std::{path::PathBuf, process::{Command, Stdio}, sync::{Arc, Mutex, mpsc::Sender}, thread};

use super::{ApprovalResponse, BackendAdapter, BackendError, BackendEvent, BackendStatus, RunningTurn, ToolBridge, TurnRequest};
use protocol::Control;

#[derive(Default)]
pub(crate) struct CodexAdapter;

fn executable() -> PathBuf {
    if let Some(path) = std::env::var_os("PATH") {
        for directory in std::env::split_paths(&path) {
            let candidate = directory.join("codex");
            if candidate.is_file() { return candidate; }
        }
    }
    let bundled = PathBuf::from("/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex");
    if bundled.is_file() { bundled } else { PathBuf::from("codex") }
}

impl BackendAdapter for CodexAdapter {
    fn probe(&self) -> BackendStatus {
        let version = match Command::new(executable()).arg("--version").output() {
            Ok(output) if output.status.success() => String::from_utf8_lossy(&output.stdout).trim().to_owned(),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return BackendStatus::Missing,
            _ => return BackendStatus::Error { message: "无法检测 Codex CLI 版本".into() },
        };
        match Command::new(executable()).args(["login", "status"]).stdout(Stdio::null()).stderr(Stdio::null()).status() {
            Ok(status) if status.success() => BackendStatus::Ready { version: format!("{version}；将禁用外部 MCP") },
            Ok(_) => BackendStatus::NotAuthenticated { message: "请先使用 codex login 登录".into() },
            Err(error) => BackendStatus::Error { message: format!("无法检查 Codex 登录状态：{error}") },
        }
    }

    fn start(&mut self, request: TurnRequest, events: Sender<BackendEvent>, tools: ToolBridge) -> Result<Box<dyn RunningTurn>, BackendError> {
        let control = Arc::new(Mutex::new(Control::default()));
        let state = control.clone();
        thread::Builder::new().name("block-codex".into()).spawn(move || {
            worker::run(request, events, tools, state);
        }).map_err(|error| BackendError::new("codex_spawn", error.to_string()))?;
        Ok(Box::new(CodexTurn { control }))
    }
}

struct CodexTurn { control: Arc<Mutex<Control>> }

impl RunningTurn for CodexTurn {
    fn cancel(&mut self) -> Result<(), BackendError> {
        self.control.lock().map_err(|_| BackendError::new("codex_control", "Codex 控制锁已失效"))?.cancel()
    }

    fn respond(&mut self, _request_id: &str, _response: ApprovalResponse) -> Result<(), BackendError> {
        Err(BackendError::new("codex_approval_denied", "Codex 沙箱权限不可提升；笔记变更请通过 Block 计划审批"))
    }
}

impl Drop for CodexTurn {
    fn drop(&mut self) { let _ = self.cancel(); }
}
