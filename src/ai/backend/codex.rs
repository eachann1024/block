mod protocol;
mod worker;
pub(crate) mod models;

use std::{process::Stdio, sync::{Arc, Mutex, mpsc::Sender}, thread};

use super::{ApprovalResponse, BackendAdapter, BackendError, BackendEvent, BackendStatus, RunningTurn, ToolBridge, TurnRequest, cli_env};
use protocol::Control;

#[derive(Default)]
pub(crate) struct CodexAdapter;

impl BackendAdapter for CodexAdapter {
    fn probe(&self) -> BackendStatus {
        let version = match cli_env::command("codex").arg("--version").output() {
            Ok(output) if output.status.success() => String::from_utf8_lossy(&output.stdout).trim().to_owned(),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return BackendStatus::Missing,
            _ => return BackendStatus::Error { message: "无法检测 Codex CLI 版本".into() },
        };
        match cli_env::command("codex").args(["login", "status"]).stdout(Stdio::null()).stderr(Stdio::null()).status() {
            Ok(status) if status.success() => BackendStatus::Ready { version },
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

    fn respond(&mut self, request_id: &str, response: ApprovalResponse) -> Result<(), BackendError> {
        let mut state = self.control.lock().map_err(|_| BackendError::new("codex_control", "Codex 控制锁已失效"))?;
        let rpc_id = state.pending_approvals.remove(request_id)
            .ok_or_else(|| BackendError::new("approval_expired", "审批请求已过期"))?;
        let writer = state.writer.as_ref()
            .ok_or_else(|| BackendError::new("codex_io", "Codex 输入已关闭"))?.clone();
        drop(state);
        let decision = match response {
            ApprovalResponse::AllowOnce | ApprovalResponse::AllowSession | ApprovalResponse::QuestionResponse(_) => "accept",
            ApprovalResponse::Deny => "decline",
            ApprovalResponse::Cancel => "cancel",
        };
        protocol::write(&writer, &serde_json::json!({"id": rpc_id, "result": {"decision": decision}}))
    }
}

impl Drop for CodexTurn {
    fn drop(&mut self) { let _ = self.cancel(); }
}
