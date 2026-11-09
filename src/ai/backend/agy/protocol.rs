use std::{collections::HashMap, sync::{Arc, Mutex, atomic::{AtomicBool, Ordering}, mpsc::Sender}, time::{Duration, Instant}};
use serde_json::{Value, json};
use super::super::{ApprovalResponse, BackendError, BackendEvent, BackendEventKind, TurnRequest};
use super::transport::{self, Connection};

pub(super) type Pending = HashMap<String, Permission>;

pub(super) struct Permission { id: Value, options: Vec<Value> }

impl Permission {
    pub fn reply(self, response: ApprovalResponse) -> Value {
        let kinds: &[&str] = match response {
            ApprovalResponse::AllowOnce => &["allow_once"],
            ApprovalResponse::AllowSession => &["allow_always", "allow_once"],
            ApprovalResponse::Deny => &["reject_once", "reject_always"],
            _ => &[],
        };
        let option = kinds.iter().find_map(|kind| self.options.iter().find(|option| option["kind"] == *kind));
        let outcome = match option.and_then(|option| option.get("optionId")) {
            Some(id) => json!({"outcome":"selected", "optionId":id}),
            None => json!({"outcome":"cancelled"}),
        };
        json!({"jsonrpc":"2.0", "id":self.id, "result":{"outcome":outcome}})
    }
}

pub(super) fn response(frame: &Value) -> Result<&Value, BackendError> {
    if let Some(error) = frame.get("error") {
        let message = error["message"].as_str().unwrap_or("agy request failed");
        let lower = message.to_lowercase();
        let code = if lower.contains("auth") || lower.contains("login") || lower.contains("log in")
            || lower.contains("credential") { "authentication" } else { "agy_rpc" };
        return Err(BackendError::new(code, message));
    }
    frame.get("result").ok_or_else(|| BackendError::new("agy_protocol", "Missing agy response result"))
}

pub(super) fn emit(events: &Sender<BackendEvent>, request: &TurnRequest, kind: BackendEventKind) {
    let _ = events.send(BackendEvent { conversation_id: request.conversation_id.clone(), turn_id: request.turn_id.clone(), kind });
}

pub(super) fn run(connection: &Connection, turn: &TurnRequest, events: &Sender<BackendEvent>,
    pending: &Arc<Mutex<Pending>>, cancelled: &AtomicBool, servers: Value) -> Result<BackendEventKind, BackendError> {
    let workspace = crate::files::app_data_dir("Block").join("agy-runtime/workspace");
    std::fs::create_dir_all(&workspace).map_err(transport::error)?;
    let cwd = turn.context.notebook_root.as_ref().filter(|path| path.is_dir()).unwrap_or(&workspace);
    let input = &connection.input;
    transport::initialize(input)?;
    let deadline = Instant::now() + Duration::from_secs(45);
    let mut session = String::new();
    let mut thought_level = false;
    let mut prompting = false;
    loop {
        if cancelled.load(Ordering::Acquire) { return Ok(BackendEventKind::Cancelled); }
        if !prompting && Instant::now() >= deadline {
            return Err(BackendError::new("agy_timeout", "agy session initialization timed out"));
        }
        let frame = match connection.frames.recv_timeout(Duration::from_secs(1)) {
            Ok(frame) => frame?,
            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => continue,
            Err(_) => return Err(transport::error("agy exited before completing the turn; check agy login")),
        };
        if let Some(method) = frame["method"].as_str() {
            if method == "session/request_permission" {
                permission(connection, turn, events, pending, &frame)?;
            } else if method == "session/update" && prompting && frame["params"]["sessionId"] == session {
                update(events, turn, &frame["params"]["update"]);
            } else if let Some(id) = frame.get("id") {
                // Block does not advertise filesystem or terminal client capabilities.
                transport::send(input, json!({"jsonrpc":"2.0", "id":id,
                    "error":{"code":-32601, "message":"Unsupported Block client method"}}))?;
            }
            continue;
        }
        let Some(id) = frame["id"].as_u64() else { continue };
        let result = response(&frame)?;
        match id {
            1 => {
                if result["protocolVersion"] != 1 { return Err(BackendError::new("unsupported", "agy ACP protocol version 1 required")); }
                let mut params = json!({"cwd":cwd, "mcpServers":servers,
                    "disableBuiltInTools":turn.config["textOnly"] == true});
                let method = if let Some(previous) = turn.native_session_id.as_deref().filter(|id| !id.is_empty()) {
                    if result.pointer("/agentCapabilities/loadSession").and_then(Value::as_bool) != Some(true) {
                        return Err(BackendError::new("unsupported", "agy does not support loading conversations"));
                    }
                    session = previous.to_owned();
                    params["sessionId"] = session.clone().into();
                    "session/load"
                } else { "session/new" };
                transport::request(input, 2, method, params)?;
            }
            2 => {
                if let Some(id) = result["sessionId"].as_str() { session = id.to_owned(); }
                if session.is_empty() { return Err(BackendError::new("agy_protocol", "agy did not return a session ID")); }
                emit(events, turn, BackendEventKind::Session { native_session_id: session.clone() });
                thought_level = result["configOptions"].as_array().is_some_and(|options|
                    options.iter().any(|option| option["id"] == "thought_level"));
                if let Some(model) = turn.config["model"].as_str().filter(|model| !model.is_empty() && *model != "default") {
                    transport::request(input, 3, "session/set_model", json!({"sessionId":session, "modelId":model}))?;
                } else { configure_thinking(input, turn, &session, thought_level)?; }
            }
            3 => configure_thinking(input, turn, &session, thought_level)?,
            4 => configure_mode(input, turn, &session)?,
            5 => {
                let text = format!("{}\n\nBlock context (snapshot; use Block tools for live state): {}\n\n{}",
                    crate::editor::ai::agent::SYSTEM_PROMPT_HINT,
                    serde_json::to_string(&turn.context).map_err(transport::error)?, turn.message);
                transport::request(input, 6, "session/prompt", json!({"sessionId":session,
                    "prompt":[{"type":"text", "text":text}]}))?;
                prompting = true;
            }
            6 => return match result["stopReason"].as_str() {
                Some("cancelled") => Ok(BackendEventKind::Cancelled),
                Some("end_turn" | "max_tokens" | "refusal") => Ok(BackendEventKind::Completed),
                reason => Err(BackendError::new("agy_turn", format!("agy turn ended: {}", reason.unwrap_or("unknown")))),
            },
            _ => {},
        }
    }
}

fn configure_thinking(input: &transport::Input, turn: &TurnRequest, session: &str, supported: bool) -> Result<(), BackendError> {
    if supported {
        transport::request(input, 4, "session/set_config_option", json!({"sessionId":session,
            "configId":"thought_level", "value":turn.config["thinkingLevel"].as_str().unwrap_or("high")}))
    } else { configure_mode(input, turn, session) }
}

fn configure_mode(input: &transport::Input, turn: &TurnRequest, session: &str) -> Result<(), BackendError> {
    let mode = if turn.config["textOnly"] == true { "plan" } else { "default" };
    transport::request(input, 5, "session/set_mode", json!({"sessionId":session, "modeId":mode}))
}

fn permission(connection: &Connection, turn: &TurnRequest, events: &Sender<BackendEvent>, pending: &Arc<Mutex<Pending>>,
    frame: &Value) -> Result<(), BackendError> {
    let id = frame.get("id").filter(|id| id.is_string() || id.is_number())
        .ok_or_else(|| BackendError::new("agy_protocol", "Missing agy permission request ID"))?;
    let params = &frame["params"];
    let permission = Permission { id: id.clone(), options: params["options"].as_array().cloned().unwrap_or_default() };
    if turn.config["textOnly"] == true {
        return transport::send(&connection.input, permission.reply(ApprovalResponse::Deny));
    }
    if turn.config["fullPermissions"] == true {
        return transport::send(&connection.input, permission.reply(ApprovalResponse::AllowOnce));
    }
    let key = id.to_string();
    pending.lock().map_err(|_| transport::error("agy approval lock failed"))?.insert(key.clone(), permission);
    emit(events, turn, BackendEventKind::Approval { request_id: key,
        title: format!("agy · {}", params["toolCall"]["title"].as_str().unwrap_or("tool")),
        detail: params["toolCall"]["rawInput"].to_string() });
    Ok(())
}

fn update(events: &Sender<BackendEvent>, turn: &TurnRequest, update: &Value) {
    let kind = match update["sessionUpdate"].as_str() {
        Some("agent_message_chunk") => update["content"]["text"].as_str().map(|text| BackendEventKind::Delta(text.to_owned())),
        Some("agent_thought_chunk") => update["content"]["text"].as_str().map(|text| BackendEventKind::Thinking(text.to_owned())),
        Some("tool_call") => Some(BackendEventKind::ToolActivity {
            name: update["title"].as_str().unwrap_or("tool").to_owned(), detail: update["rawInput"].to_string() }),
        Some("plan") => update["entries"].as_array().map(|entries| BackendEventKind::PublicSummary(
            entries.iter().filter_map(|entry| entry["content"].as_str()).collect::<Vec<_>>().join("\n"))),
        _ => None,
    };
    if let Some(kind) = kind { emit(events, turn, kind); }
}
