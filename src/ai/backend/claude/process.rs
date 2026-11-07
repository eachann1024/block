use std::{collections::HashMap, io::{BufRead, BufReader, Write}, process::{ChildStdin, Stdio},
    sync::{Arc, Mutex, atomic::{AtomicBool, Ordering}, mpsc::Sender}, thread};

use serde_json::{Value, json};
use super::super::{ApprovalResponse, BackendError, BackendEvent, BackendEventKind, RunningTurn, ToolBridge, TurnRequest};
use super::mcp;
#[path = "../codex/process_group.rs"] mod process_group;

pub(super) type Input = Option<ChildStdin>;
type Pending = HashMap<String, Value>;

pub(super) fn write_frame(input: &Arc<Mutex<Input>>, value: &Value) -> Result<(), BackendError> {
    let mut guard = input.lock().map_err(|_| BackendError::new("process_error", "Claude stdin lock failed"))?;
    let stdin = guard.as_mut().ok_or_else(|| BackendError::new("turn_finished", "Claude process has closed"))?;
    writeln!(stdin, "{value}").and_then(|_| stdin.flush()).map_err(|error| BackendError::new("process_error", error.to_string()))
}

struct Turn {
    child: process_group::SharedChild,
    input: Arc<Mutex<Input>>,
    pending: Arc<Mutex<Pending>>,
    cancelled: Arc<AtomicBool>,
    finished: Arc<AtomicBool>,
    tools: ToolBridge,
}

impl RunningTurn for Turn {
    fn cancel(&mut self) -> Result<(), BackendError> {
        if self.finished.load(Ordering::Acquire) { return Ok(()); }
        self.cancelled.store(true, Ordering::Release);
        self.tools.cancel();
        process_group::stop(&self.child);
        Ok(())
    }

    fn respond(&mut self, request_id: &str, response: ApprovalResponse) -> Result<(), BackendError> {
        if matches!(response, ApprovalResponse::AllowSession) {
            return Err(BackendError::new("unsupported", "Claude session-wide approval is not supported"));
        }
        let original = self.pending.lock().map_err(|_| BackendError::new("process_error", "Approval lock failed"))?
            .remove(request_id).ok_or_else(|| BackendError::new("approval_expired", "Approval request is no longer pending"))?;
        let decision = match response {
            ApprovalResponse::AllowOnce => json!({"behavior":"allow","updatedInput":original}),
            ApprovalResponse::Deny => json!({"behavior":"deny","message":"User denied this request"}),
            ApprovalResponse::Cancel => json!({"behavior":"deny","message":"User cancelled this request","interrupt":true}),
            ApprovalResponse::AllowSession => unreachable!(),
        };
        write_frame(&self.input, &json!({"type":"control_response","response":{"subtype":"success","request_id":request_id,"response":decision}}))
    }
}

impl Drop for Turn {
    fn drop(&mut self) {
        if !self.finished.load(Ordering::Acquire) { let _ = self.cancel(); }
    }
}

pub(super) fn start(request: TurnRequest, events: Sender<BackendEvent>, tools: ToolBridge)
    -> Result<Box<dyn RunningTurn>, BackendError> {
    let mut command = super::super::cli_env::command("claude");
    let args = request.config.get("launchArgs").and_then(Value::as_str).unwrap_or(super::super::BackendKind::Claude.default_launch_args());
    command.args(args.split_whitespace()).args([
        "--input-format", "stream-json",
        "--output-format", "stream-json",
        "--include-partial-messages",
        "--mcp-config", r#"{"mcpServers":{"block":{"type":"sdk","name":"block"}}}"#,
        "--append-system-prompt",
        "Use Block MCP tools (listNotebooks, listPages, searchNotes, readPage, executeBatchPlan, loadSkill, readSkillResource) when reading or batch-editing Block notes.",
    ])
        .env_remove("CLAUDECODE").stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::piped());
    if let Some(root) = request.context.notebook_root.as_ref() { command.current_dir(root); }
    if let Some(session) = request.native_session_id.as_deref().filter(|id| !id.is_empty()) { command.args(["--resume", session]); }
    if let Some(model) = request.config.get("model").and_then(Value::as_str).filter(|value| !value.is_empty()) { command.args(["--model", model]); }
    process_group::configure(&mut command);
    let mut child = command.spawn().map_err(|error| BackendError::new(if error.kind() == std::io::ErrorKind::NotFound { "missing" } else { "process_error" }, error.to_string()))?;
    let stdin = child.stdin.take().ok_or_else(|| BackendError::new("process_error", "Claude stdin unavailable"))?;
    let stdout = child.stdout.take().ok_or_else(|| BackendError::new("process_error", "Claude stdout unavailable"))?;
    let stderr = child.stderr.take();
    if let Some(stderr) = stderr { thread::spawn(move || { let _ = std::io::copy(&mut BufReader::new(stderr), &mut std::io::sink()); }); }
    let child = process_group::managed(child);
    let input = Arc::new(Mutex::new(Some(stdin)));
    let pending = Arc::new(Mutex::new(HashMap::new()));
    let cancelled = Arc::new(AtomicBool::new(false));
    let finished = Arc::new(AtomicBool::new(false));
    let turn = Turn { child: child.clone(), input: input.clone(), pending: pending.clone(), cancelled: cancelled.clone(), finished: finished.clone(), tools: tools.clone() };
    let init_input = input.clone();
    thread::spawn(move || {
        let mut terminal = None;
        let mut sent = false;
        let mut streamed = false;
        for line in BufReader::new(stdout).lines() {
            let line = match line { Ok(line) => line, Err(_) => break };
            if !line.trim_start().starts_with('{') { continue; }
            let frame: Value = match serde_json::from_str(&line) { Ok(frame) => frame, Err(_) => {
                terminal = Some(BackendEventKind::Error(BackendError::new("protocol_error", "Invalid Claude stream JSON"))); break;
            }};
            if frame["type"] == "control_response" && frame["response"]["request_id"] == "block_init" {
                if frame["response"]["subtype"] != "success" {
                    terminal = Some(BackendEventKind::Error(BackendError::new("protocol_error", "Claude initialization failed"))); break;
                }
                let context = json!({"page_id":request.context.page_id,"markdown":request.context.markdown,
                    "selection":request.context.selection,"revision":request.context.revision});
                let message = format!("{}\n\nBlock context (snapshot; use Block tools for live state): {}", request.message, context);
                if write_frame(&input, &json!({"type":"user","session_id":"","message":{"role":"user","content":message},"parent_tool_use_id":null})).is_err() {
                    terminal = Some(BackendEventKind::Error(BackendError::new("process_error", "Could not send message to Claude"))); break;
                }
                sent = true;
                continue;
            }
            if frame["type"] == "control_request" {
                control(&frame, &request, &tools, &input, &pending, &events);
                continue;
            }
            if frame["type"] == "control_cancel_request" {
                if let Some(id) = frame["request_id"].as_str() { if let Ok(mut pending) = pending.lock() { pending.remove(id); } }
                continue;
            }
            if let Some(kind) = event(&frame, &mut streamed) {
                if matches!(kind, BackendEventKind::Completed | BackendEventKind::Error(_)) { terminal = Some(kind); break; }
                emit(&events, &request, kind);
            }
        }
        tools.cancel();
        if let Ok(mut pending) = pending.lock() { pending.clear(); }
        if let Ok(mut guard) = input.lock() { guard.take(); }
        process_group::stop(&child);
        finished.store(true, Ordering::Release);
        let kind = if cancelled.load(Ordering::Acquire) { BackendEventKind::Cancelled } else {
            terminal.unwrap_or_else(|| BackendEventKind::Error(BackendError::new("process_exit", if sent { "Claude exited without a result" } else { "Claude exited before initialization" })))
        };
        emit(&events, &request, kind);
    });
    write_frame(&init_input, &json!({"type":"control_request","request_id":"block_init","request":{"subtype":"initialize","hooks":null}}))?;
    Ok(Box::new(turn))
}

fn emit(events: &Sender<BackendEvent>, request: &TurnRequest, kind: BackendEventKind) {
    let _ = events.send(BackendEvent { conversation_id: request.conversation_id.clone(), turn_id: request.turn_id.clone(), kind });
}

fn control(frame: &Value, request: &TurnRequest, tools: &ToolBridge, input: &Arc<Mutex<Input>>,
    _pending: &Arc<Mutex<Pending>>, events: &Sender<BackendEvent>) {
    let id = frame["request_id"].as_str().unwrap_or("");
    let data = &frame["request"];
    match data["subtype"].as_str() {
        Some("mcp_message") => mcp::handle(frame, request, tools, input, events),
        Some("can_use_tool") if !id.is_empty() => {
            let _ = write_frame(input, &json!({"type":"control_response","response":{"subtype":"success","request_id":id,"response":{"behavior":"allow","updatedInput":data["input"]}}}));
        }
        _ => { let _ = write_frame(input, &json!({"type":"control_response","response":{"subtype":"error","request_id":id,"error":"Unsupported control request"}})); }
    }
}

fn event(frame: &Value, streamed: &mut bool) -> Option<BackendEventKind> {
    match frame["type"].as_str()? {
        "system" if frame["subtype"] == "init" => frame["session_id"].as_str().map(|id| BackendEventKind::Session { native_session_id: id.to_owned() }),
        "stream_event" if frame["event"]["type"] == "message_start" => { *streamed = false; None }
        "stream_event" if frame["event"]["delta"]["type"] == "text_delta" => {
            *streamed = true; frame["event"]["delta"]["text"].as_str().map(|text| BackendEventKind::Delta(text.to_owned()))
        }
        "stream_event" if frame["event"]["delta"]["type"] == "thinking_delta" =>
            frame["event"]["delta"]["thinking"].as_str().map(|text| BackendEventKind::Thinking(text.to_owned())),
        "assistant" => {
            let blocks = frame["message"]["content"].as_array()?;
            if let Some(block) = blocks.iter().find(|block| block["type"] == "tool_use") {
                Some(BackendEventKind::ToolActivity { name: block["name"].as_str().unwrap_or("tool").to_owned(), detail: block["input"].to_string() })
            } else if !*streamed {
                blocks.iter().find_map(|block| block["text"].as_str()).map(|text| BackendEventKind::Delta(text.to_owned()))
            } else { None }
        }
        "result" if frame["is_error"].as_bool() == Some(true) || frame["subtype"] != "success" => {
            let message = frame["result"].as_str().or_else(|| frame["errors"][0].as_str()).unwrap_or("Claude turn failed");
            let lower = message.to_lowercase();
            let code = if lower.contains("auth") || lower.contains("api key") || lower.contains("login") { "authentication" } else { "claude_error" };
            Some(BackendEventKind::Error(BackendError::new(code, message)))
        }
        "result" => Some(BackendEventKind::Completed),
        _ => None,
    }
}
