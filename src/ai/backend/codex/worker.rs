use std::{sync::{mpsc::Sender, Mutex}, thread, time::Duration};
use serde_json::{Value, json};

use super::protocol::{Server, SharedControl, write};
use super::super::{BackendError, BackendEvent, BackendEventKind, ToolBridge, TurnRequest};

fn emit(events: &Sender<BackendEvent>, request: &TurnRequest, kind: BackendEventKind) {
    let _ = events.send(BackendEvent { conversation_id: request.conversation_id.clone(), turn_id: request.turn_id.clone(), kind });
}

pub(super) fn run(request: TurnRequest, events: Sender<BackendEvent>, tools: ToolBridge, control: SharedControl) {
    if let Ok(mut state) = control.lock() {
        if state.cancelled { tools.cancel(); }
        state.tools = Some(tools.clone());
    }
    let result = run_inner(&request, &events, tools.clone(), &control);
    tools.cancel();
    let cancelled = control.lock().map(|mut state| { state.finished = true; state.cancelled }).unwrap_or(false);
    let kind = if cancelled { BackendEventKind::Cancelled } else { result.unwrap_or_else(BackendEventKind::Error) };
    emit(&events, &request, kind);
}

fn run_inner(request: &TurnRequest, events: &Sender<BackendEvent>, tools: ToolBridge, control: &SharedControl) -> Result<BackendEventKind, BackendError> {
    let cwd = match &request.context.notebook_root {
        Some(path) if path.is_absolute() && path.is_dir() => path.clone(),
        Some(_) => return Err(BackendError::new("codex_cwd", "笔记本目录不可用")),
        None => std::env::current_dir().map_err(|error| BackendError::new("codex_cwd", error.to_string()))?,
    };
    let mut server = Server::spawn(control)?;
    let account = server.request(2, "account/read", json!({"refreshToken":false}))?;
    if account.get("requiresOpenaiAuth").and_then(Value::as_bool) == Some(true) && account.get("account").is_none_or(Value::is_null) {
        return Err(BackendError::new("codex_auth", "请先使用 codex login 登录"));
    }
    let dynamic: Vec<Value> = super::super::super::agent::tools().as_array().into_iter().flatten().filter_map(|tool| {
        let function = tool.get("function")?;
        Some(json!({"type":"function","name":function.get("name")?,"description":function.get("description")?,"inputSchema":function.get("parameters")?}))
    }).collect();
    if dynamic.is_empty() { return Err(BackendError::new("codex_tools", "Block 工具定义不可用")); }
    let mut params = json!({"cwd":cwd,"sandbox":"danger-full-access","approvalPolicy":"never",
        "developerInstructions":"笔记和选区可使用 Block 提供的工具读取，批量修改笔记内容请优先使用 executeBatchPlan。"});
    if let Some(model) = request.config.get("model").and_then(Value::as_str).filter(|model| !model.is_empty()) { params["model"] = json!(model); }
    let method = if let Some(id) = &request.native_session_id { params["threadId"] = json!(id); "thread/resume" }
        else { params["dynamicTools"] = json!(dynamic); "thread/start" };
    let started = server.request(5, method, params)?;
    let thread_id = started.pointer("/thread/id").and_then(Value::as_str)
        .ok_or_else(|| BackendError::new("codex_protocol", "Codex 未返回会话 ID"))?.to_owned();
    emit(events, request, BackendEventKind::Session { native_session_id: thread_id.clone() });
    let context = serde_json::to_string(&request.context).map_err(|error| BackendError::new("codex_context", error.to_string()))?;
    let text = format!("{}\n\n当前 Block 上下文（实时内容，仅供本轮使用）：\n{}", request.message, context);
    write(&server.writer, &json!({"id":6,"method":"turn/start","params":{"threadId":thread_id,
        "input":[{"type":"text","text":text,"text_elements":[]}],"approvalPolicy":"never",
        "sandboxPolicy":{"type":"dangerFullAccess"}}}))?;
    {
        let mut state = control.lock().map_err(|_| BackendError::new("codex_control", "Codex 控制锁已失效"))?;
        state.thread_id = Some(thread_id.clone());
        if state.cancelled { return Err(BackendError::new("cancelled", "已取消")); }
    }
    loop {
        let message = server.next()?;
        if message.get("id") == Some(&json!(6)) && message.get("method").is_none() {
            if let Some(error) = message.get("error") { return Err(BackendError::new("codex_turn", error.to_string())); }
            if let Some(id) = message.pointer("/result/turn/id").and_then(Value::as_str) { set_turn(control, id)?; }
            continue;
        }
        let method = message.get("method").and_then(Value::as_str).unwrap_or("");
        let params = &message["params"];
        match method {
            "turn/started" => if let Some(id) = params.pointer("/turn/id").and_then(Value::as_str) { set_turn(control, id)?; },
            "item/agentMessage/delta" => if let Some(delta) = params.get("delta").and_then(Value::as_str) { emit(events, request, BackendEventKind::Delta(delta.into())); },
            "item/reasoning/summaryTextDelta" | "item/reasoning/textDelta" => if let Some(delta) = params.get("delta").and_then(Value::as_str) { emit(events, request, BackendEventKind::Thinking(delta.into())); },
            "item/started" | "item/completed" => if let Some(kind) = params.pointer("/item/type").and_then(Value::as_str) {
                if kind != "agentMessage" && kind != "reasoning" { emit(events, request, BackendEventKind::ToolActivity { name: kind.into(), detail: method.into() }); }
            },
            "item/tool/call" => tool_call(&server, &message, request, events, &tools, &thread_id)?,
            "turn/completed" => {
                if params.get("threadId").and_then(Value::as_str) != Some(thread_id.as_str()) { continue; }
                return match params.pointer("/turn/status").and_then(Value::as_str) {
                    Some("completed") => Ok(BackendEventKind::Completed),
                    Some("interrupted") => Ok(BackendEventKind::Cancelled),
                    Some("failed") => return Err(BackendError::new("codex_turn_failed", params.pointer("/turn/error/message").and_then(Value::as_str).unwrap_or("Codex 任务失败").to_owned())),
                    _ => return Err(BackendError::new("codex_protocol", "Codex 返回未知结束状态")),
                };
            },
            _ if message.get("id").is_some() && message.get("method").is_some() => {
                write(&server.writer, &json!({"id":message["id"],"result":{"decision":"accept"}}))?;
            },
            _ => {}
        }
    }
}

fn set_turn(control: &Mutex<super::protocol::Control>, id: &str) -> Result<(), BackendError> {
    let mut state = control.lock().map_err(|_| BackendError::new("codex_control", "Codex 控制锁已失效"))?;
    state.turn_id = Some(id.into());
    if state.cancelled { state.cancel()?; }
    Ok(())
}

fn tool_call(server: &Server, message: &Value, request: &TurnRequest, events: &Sender<BackendEvent>, tools: &ToolBridge, thread_id: &str) -> Result<(), BackendError> {
    let params = &message["params"];
    let name = params.get("tool").and_then(Value::as_str).unwrap_or("");
    let id = message.get("id").cloned().ok_or_else(|| BackendError::new("codex_protocol", "工具请求缺少 ID"))?;
    if params.get("threadId").and_then(Value::as_str) != Some(thread_id) || !["loadSkill", "readSkillResource", "listNotebooks", "listPages", "searchNotes", "readPage", "executeBatchPlan"].contains(&name) || !params.get("namespace").is_none_or(Value::is_null) {
        return write(&server.writer, &json!({"id":id,"result":{"success":false,"contentItems":[{"type":"inputText","text":"此工具未获 Block 授权"}]}}));
    }
    emit(events, request, BackendEventKind::ToolActivity { name: name.into(), detail: "调用 Block 工具".into() });
    let tools = tools.clone();
    let writer = server.writer.clone();
    let conversation_id = request.conversation_id.clone();
    let turn_id = request.turn_id.clone();
    let call_id = params.get("callId").and_then(Value::as_str).unwrap_or("").to_owned();
    let name = name.to_owned();
    let arguments = params.get("arguments").cloned().unwrap_or(Value::Null);
    thread::spawn(move || {
        let result = tools.call(conversation_id, turn_id, call_id, name, arguments, Duration::from_secs(300));
        let (success, text) = match result { Ok(value) => (true, value.to_string()), Err(error) => (false, error.message) };
        let _ = write(&writer, &json!({"id":id,"result":{"success":success,"contentItems":[{"type":"inputText","text":text}]}}));
    });
    Ok(())
}
