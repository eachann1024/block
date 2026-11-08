use std::{
    fs,
    io::{BufRead, BufReader},
    path::PathBuf,
    process::Stdio,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
        mpsc::Sender,
    },
    thread,
};
use serde_json::{Value, json};
use super::PiTurn;
use super::super::{
    BackendError, BackendEvent, BackendEventKind, RunningTurn, ToolBridge, TurnRequest, cli_env,
    dsh::bridge,
};
#[path = "../codex/process_group.rs"]
mod process_group;

pub(super) use process_group::SharedChild;
pub(super) fn stop_child(child: &SharedChild) {
    process_group::stop(child);
}

struct TempDir(PathBuf);
impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}
struct BridgeGuard(Arc<AtomicBool>);
impl Drop for BridgeGuard {
    fn drop(&mut self) {
        self.0.store(false, Ordering::Release);
    }
}

fn io_error(error: impl std::fmt::Display) -> BackendError {
    BackendError::new("pi_io", error.to_string())
}

fn emit(events: &Sender<BackendEvent>, request: &TurnRequest, kind: BackendEventKind) {
    let _ = events.send(BackendEvent {
        conversation_id: request.conversation_id.clone(),
        turn_id: request.turn_id.clone(),
        kind,
    });
}

pub(super) fn start(
    request: TurnRequest,
    events: Sender<BackendEvent>,
    tools: ToolBridge,
) -> Result<Box<dyn RunningTurn>, BackendError> {
    let token = bridge::secret()?;
    let dir = TempDir(std::env::temp_dir().join(format!("block-pi-{}", &token[..24])));
    fs::create_dir_all(&dir.0).map_err(io_error)?;
    let ext_file = dir.0.join("ext.js");
    fs::write(&ext_file, include_str!("ext.js")).map_err(io_error)?;
    let (port, bridge_active, _) = bridge::listen(&request, tools.clone(), token.clone())?;
    let guard = BridgeGuard(bridge_active);
    let tool_specs = super::super::super::agent::tools()
        .as_array()
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .filter_map(|entry| {
            let function = entry.get("function")?;
            Some(json!({
                "name": function.get("name")?,
                "description": function.get("description")?,
                "inputSchema": function.get("parameters")?,
            }))
        })
        .collect::<Vec<_>>();
    let base = dirs::data_local_dir()
        .ok_or_else(|| BackendError::new("pi_storage", "No application data directory"))?
        .join("Block")
        .join("pi-runtime");
    let storage = base.join("sessions");
    let workspace = base.join("workspace");
    fs::create_dir_all(&storage).map_err(io_error)?;
    fs::create_dir_all(&workspace).map_err(io_error)?;
    let cwd = request
        .context
        .notebook_root
        .clone()
        .filter(|path| path.is_dir())
        .unwrap_or(workspace);
    let context = serde_json::to_string(&request.context).map_err(io_error)?;
    let prompt = format!("{}\n\nBlock context: {}", request.message, context);
    let mut command = cli_env::command("pi");
    command.args(["--mode", "json"]);
    if request.config.get("fullPermissions").and_then(Value::as_bool) == Some(true) {
        command.arg("--approve");
    }
    command
        .arg("--session-dir")
        .arg(&storage)
        .arg("-e")
        .arg(&ext_file)
        .args([
            "--append-system-prompt",
            crate::editor::ai::agent::SYSTEM_PROMPT_HINT,
        ]);
    if let Some(session_id) = request.native_session_id.as_deref().filter(|id| !id.is_empty()) {
        command.args(["--session-id", session_id]);
    }
    if let Some(model) = request.config.get("model").and_then(Value::as_str).filter(|m| !m.is_empty()) {
        command.args(["--model", model]);
    }
    command
        .arg("--")
        .arg(&prompt)
        .env("BLOCK_PI_PORT", port.to_string())
        .env("BLOCK_PI_TOKEN", &token)
        .env("BLOCK_PI_TOOLS", serde_json::to_string(&tool_specs).map_err(io_error)?)
        .current_dir(&cwd)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null());
    process_group::configure(&mut command);
    let mut child = command.spawn().map_err(io_error)?;
    let stdout = child.stdout.take().ok_or_else(|| BackendError::new("pi_stdio", "Missing stdout"))?;
    let child = process_group::managed(child);
    let cancelled = Arc::new(AtomicBool::new(false));
    let finished = Arc::new(AtomicBool::new(false));
    let turn = PiTurn {
        child: child.clone(),
        cancelled: cancelled.clone(),
        finished: finished.clone(),
        tools: tools.clone(),
    };
    thread::spawn(move || {
        let mut terminal = None;
        let mut streamed = false;
        let mut last_error: Option<String> = None;
        for line in BufReader::new(stdout).lines() {
            let Ok(line) = line else { break };
            if !line.trim_start().starts_with('{') {
                continue;
            }
            let Ok(frame) = serde_json::from_str::<Value>(&line) else { continue };
            match frame["type"].as_str().unwrap_or("") {
                "session" => {
                    if let Some(id) = frame["id"].as_str() {
                        emit(&events, &request, BackendEventKind::Session { native_session_id: id.to_owned() });
                    }
                }
                "message_start" => streamed = false,
                "message_update" => {
                    let sub = &frame["assistantMessageEvent"];
                    match sub["type"].as_str().unwrap_or("") {
                        "text_delta" => if let Some(delta) = sub["delta"].as_str() {
                            streamed = true;
                            emit(&events, &request, BackendEventKind::Delta(delta.to_owned()));
                        },
                        "thinking_delta" => if let Some(delta) = sub["delta"].as_str() {
                            emit(&events, &request, BackendEventKind::Thinking(delta.to_owned()));
                        },
                        _ => {}
                    }
                }
                "tool_execution_start" => {
                    let name = frame["toolName"].as_str().unwrap_or("tool").to_owned();
                    emit(&events, &request, BackendEventKind::ToolActivity { name, detail: frame["args"].to_string() });
                }
                "message_end" if frame["message"]["role"] == "assistant" => {
                    if let Some(err) = frame["message"]["errorMessage"].as_str().filter(|s| !s.is_empty()) {
                        last_error = Some(err.to_owned());
                    }
                    if !streamed {
                        if let Some(blocks) = frame["message"]["content"].as_array() {
                            for block in blocks {
                                if block["type"] == "text" {
                                    if let Some(text) = block["text"].as_str() {
                                        emit(&events, &request, BackendEventKind::Delta(text.to_owned()));
                                    }
                                }
                            }
                        }
                    }
                }
                "auto_retry_end" if frame["success"] == false => {
                    if let Some(err) = frame["finalError"].as_str().filter(|s| !s.is_empty()) {
                        last_error = Some(err.to_owned());
                    }
                }
                "agent_settled" => {
                    if frame["aborted"] == true {
                        terminal = Some(BackendEventKind::Cancelled);
                    } else if let Some(err) = last_error.take() {
                        terminal = Some(BackendEventKind::Error(BackendError::new("pi_error", err)));
                    } else {
                        terminal = Some(BackendEventKind::Completed);
                    }
                    break;
                }
                _ => {}
            }
        }
        tools.cancel();
        drop(guard);
        process_group::stop(&child);
        finished.store(true, Ordering::Release);
        let kind = if cancelled.load(Ordering::Acquire) {
            BackendEventKind::Cancelled
        } else if let Some(kind) = terminal {
            kind
        } else if let Some(err) = last_error {
            BackendEventKind::Error(BackendError::new("pi_error", err))
        } else {
            BackendEventKind::Completed
        };
        emit(&events, &request, kind);
        drop(dir);
    });
    Ok(Box::new(turn))
}
