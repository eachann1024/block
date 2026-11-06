use std::{io::{BufRead, BufReader, Write}, process::{ChildStdin, ChildStdout, Stdio}, sync::{Arc, Mutex}, thread, time::Duration};
use serde_json::{Value, json};
use super::super::{BackendError, ToolBridge, cli_env};
#[path = "process_group.rs"] mod process_group;

pub(super) type SharedControl = Arc<Mutex<Control>>;

#[derive(Default)]
pub(super) struct Control {
    pub writer: Option<Arc<Mutex<ChildStdin>>>,
    pub child: Option<process_group::SharedChild>,
    pub thread_id: Option<String>,
    pub turn_id: Option<String>,
    pub cancelled: bool,
    pub finished: bool,
    pub tools: Option<ToolBridge>,
}

impl Control {
    pub fn cancel(&mut self) -> Result<(), BackendError> {
        if self.finished || self.cancelled { return Ok(()); }
        self.cancelled = true;
        if let Some(tools) = &self.tools { tools.cancel(); }
        if let Some(child) = &self.child {
            let child = child.clone();
            if self.thread_id.is_some() && self.turn_id.is_some() {
                thread::spawn(move || { thread::sleep(Duration::from_secs(2)); process_group::stop(&child); });
            } else { process_group::stop(&child); }
        }
        if let (Some(writer), Some(thread_id), Some(turn_id)) = (&self.writer, &self.thread_id, &self.turn_id) {
            write(writer, &json!({"id": 9000, "method": "turn/interrupt", "params": {"threadId": thread_id, "turnId": turn_id}}))
        } else {
            Ok(())
        }
    }
}

pub(super) fn write(writer: &Arc<Mutex<ChildStdin>>, value: &Value) -> Result<(), BackendError> {
    let mut writer = writer.lock().map_err(|_| BackendError::new("codex_io", "Codex 输入已关闭"))?;
    serde_json::to_writer(&mut *writer, value).map_err(|error| BackendError::new("codex_io", error.to_string()))?;
    writeln!(writer).and_then(|_| writer.flush()).map_err(|error| BackendError::new("codex_io", error.to_string()))
}

pub(super) struct Server {
    pub writer: Arc<Mutex<ChildStdin>>,
    pub reader: BufReader<ChildStdout>,
    child: process_group::SharedChild,
}

impl Server {
    pub fn spawn(control: &SharedControl) -> Result<Self, BackendError> {
        let mut command = cli_env::command("codex");
        command.args(["--dangerously-bypass-approvals-and-sandbox", "--dangerously-bypass-hook-trust"]);
        process_group::configure(&mut command);
        let mut child = command.arg("app-server").stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::null()).spawn()
            .map_err(|error| BackendError::new("codex_spawn", error.to_string()))?;
        let writer = Arc::new(Mutex::new(child.stdin.take().ok_or_else(|| BackendError::new("codex_io", "Codex stdin 不可用"))?));
        let reader = BufReader::new(child.stdout.take().ok_or_else(|| BackendError::new("codex_io", "Codex stdout 不可用"))?);
        let child = process_group::managed(child);
        {
            let mut state = control.lock().map_err(|_| BackendError::new("codex_control", "Codex 控制锁已失效"))?;
            if state.cancelled { process_group::stop(&child); return Err(BackendError::new("cancelled", "已取消")); }
            state.writer = Some(writer.clone());
            state.child = Some(child.clone());
        }
        let mut server = Self { writer, reader, child };
        server.request(1, "initialize", json!({"clientInfo":{"name":"block","version":"1"},"capabilities":{"experimentalApi":true}}))?;
        write(&server.writer, &json!({"method":"initialized","params":{}}))?;
        Ok(server)
    }

    pub fn next(&mut self) -> Result<Value, BackendError> {
        let mut line = String::new();
        if self.reader.read_line(&mut line).map_err(|error| BackendError::new("codex_io", error.to_string()))? == 0 {
            return Err(BackendError::new("codex_exit", "Codex app-server 已退出"));
        }
        serde_json::from_str(&line).map_err(|error| BackendError::new("codex_protocol", error.to_string()))
    }

    pub fn request(&mut self, id: u64, method: &str, params: Value) -> Result<Value, BackendError> {
        write(&self.writer, &json!({"id":id,"method":method,"params":params}))?;
        loop {
            let message = self.next()?;
            if message.get("id") == Some(&json!(id)) && message.get("method").is_none() {
                if let Some(error) = message.get("error") { return Err(BackendError::new("codex_rpc", error.to_string())); }
                return message.get("result").cloned().ok_or_else(|| BackendError::new("codex_protocol", "Codex 响应缺少 result"));
            }
            if message.get("id").is_some() && message.get("method").is_some() {
                write(&self.writer, &json!({"id":message["id"],"result":{"decision":"accept"}}))?;
            }
        }
    }
}

impl Drop for Server {
    fn drop(&mut self) {
        process_group::stop(&self.child);
    }
}
