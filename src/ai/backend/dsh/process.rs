use std::{fs, io::{BufRead, BufReader, Write}, path::PathBuf, process::{Child, Command, Stdio}, sync::{Arc, Mutex, mpsc::Sender, atomic::{AtomicBool, Ordering}}};
use serde_json::{json, Value};
use super::{bridge, BackendError, BackendEvent, BackendEventKind, ToolBridge, TurnRequest};

struct TempDir(PathBuf);
impl Drop for TempDir { fn drop(&mut self) { let _ = fs::remove_dir_all(&self.0); } }
struct BridgeGuard(Arc<AtomicBool>);
impl Drop for BridgeGuard { fn drop(&mut self) { self.0.store(false, Ordering::Release); } }

type Worker = Box<dyn FnOnce() + Send>;

pub(super) fn start(request: TurnRequest, events: Sender<BackendEvent>, tools: ToolBridge,
    cancelled: Arc<AtomicBool>) -> Result<(Arc<Mutex<Child>>, Arc<AtomicBool>, Worker), BackendError> {
    let provider = request.config.get("provider").and_then(Value::as_str).filter(|value| !value.trim().is_empty())
        .ok_or_else(|| BackendError::new("dsh_provider_required", "Select the configured dsh provider before starting"))?;
    if provider != "deepseek-official" {
        return Err(BackendError::new("dsh_provider_unsupported", format!("The isolated dsh profile does not support provider {provider}")));
    }
    if request.config.get("model").and_then(Value::as_str).is_none_or(|value| value.trim().is_empty()) {
        return Err(BackendError::new("dsh_model_required", "Select the configured dsh model before starting"));
    }
    let native_home = std::env::var_os("DSH_HOME").filter(|value| !value.is_empty()).map(PathBuf::from)
        .or_else(|| dirs::home_dir().map(|home| home.join(".dsh")))
        .ok_or_else(|| BackendError::new("dsh_home", "Cannot locate the native dsh home"))?;
    let native_home = if native_home.is_absolute() { native_home } else {
        std::env::current_dir().map_err(io_error)?.join(native_home)
    };
    let block_key = if request.config.get("authSource").and_then(Value::as_str) == Some("block") {
        let config = super::super::super::provider::config();
        if config.provider != "deepseek" || config.api_key.is_empty() {
            return Err(BackendError::new("dsh_block_credential", "Configure the DeepSeek API key in Block AI settings"));
        }
        Some(config.api_key)
    } else { None };
    let token = bridge::secret()?;
    let dir = TempDir(std::env::temp_dir().join(format!("block-dsh-{}", &token[..24])));
    fs::create_dir(&dir.0).map_err(io_error)?;
    #[cfg(unix)] {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(&dir.0, fs::Permissions::from_mode(0o700)).map_err(io_error)?;
        fs::create_dir(dir.0.join("home")).map_err(io_error)?;
        std::os::unix::fs::symlink(native_home.join(".env"), dir.0.join("home").join(".env")).map_err(io_error)?;
    }
    let (port, bridge_active, bridge_ready) = bridge::listen(&request, tools.clone(), token.clone())?;
    let guard = BridgeGuard(bridge_active.clone());
    let tool_specs = super::super::super::agent::tools().as_array().cloned().unwrap_or_default().into_iter().filter_map(|entry| {
        let function = entry.get("function")?;
        Some(json!({"name": function.get("name")?, "description": function.get("description")?, "inputSchema": function.get("parameters")?}))
    }).collect::<Vec<_>>();
    let tool_file = dir.0.join("mcp.js");
    fs::write(&tool_file, include_str!("mcp.js")).map_err(io_error)?;
    let base = dirs::data_local_dir().ok_or_else(|| BackendError::new("dsh_storage", "No application data directory"))?.join("Block").join("dsh-runtime");
    let storage = base.join("sessions");
    let workspace = base.join("workspace");
    fs::create_dir_all(&storage).map_err(io_error)?;
    fs::create_dir_all(&workspace).map_err(io_error)?;
    let credentials_home = if block_key.is_some() { dir.0.join("home") } else { native_home.clone() };
    let project_skills = request.context.notebook_root.as_ref().map(|root| {
        let project = root.ancestors().find(|path| path.join(".git").exists()).unwrap_or(root);
        vec![project.join(".dsh/skills"), project.join(".agents/skills")]
    }).unwrap_or_default();
    let patch = json!([
        {"id":"tool-fs", "disabled":true},
        {"id":"tool-working-directory", "disabled":true},
        {"id":"persistent-bash", "disabled":true},
        {"id":"persistent-pwsh", "disabled":true},
        {"id":"sessions", "config":{"root":storage,"compression":"none"}},
        {"insert":[{"id":"credentials", "name":"@deepseek-ai/dsh-credentials-local", "config":{"dshHome":credentials_home,"watch":false}},
        {"id":"skills", "name":"@deepseek-ai/dsh-skill"},
        {"id":"skill-filesystem", "name":"@deepseek-ai/dsh-skill-filesystem", "config":{
            "dshHome":native_home,"customSkillDirs":project_skills,"watch":false}},
        {"id":"tool-skill", "name":"@deepseek-ai/dsh-tool-skill"},
        {"id":"block-mcp", "name":"@deepseek-ai/dsh-mcp-client", "config":{
            "serverName":"block", "transport":"stdio", "command":"node", "args":[tool_file],
            "failOnStartupError":true, "env":{"BLOCK_MCP_PORT":port.to_string(),"BLOCK_MCP_TOKEN":token,
                "BLOCK_MCP_TOOLS":serde_json::to_string(&tool_specs).map_err(io_error)?}
        }}]}
    ]);
    let patch_path = dir.0.join("patch.json");
    fs::write(&patch_path, serde_json::to_vec(&patch).map_err(io_error)?).map_err(io_error)?;
    #[cfg(target_os = "macos")]
    let mut command = if block_key.is_none() {
        let (ancestor, canonical) = native_home.ancestors().find_map(|path| fs::canonicalize(path).ok().map(|resolved| (path, resolved)))
            .ok_or_else(|| BackendError::new("dsh_home", "Cannot protect native dsh home"))?;
        let protected_home = canonical.join(native_home.strip_prefix(ancestor).map_err(io_error)?);
        let escaped = protected_home.to_str().ok_or_else(|| BackendError::new("dsh_home", "Cannot protect non-UTF-8 dsh home"))?
            .replace('\\', "\\\\").replace('"', "\\\"");
        let mut command = Command::new("/usr/bin/sandbox-exec");
        command.args(["-p", &format!("(version 1)(allow default)(deny file-write* (subpath \"{escaped}\"))"), "dsh"]);
        command
    } else { Command::new("dsh") };
    #[cfg(not(target_os = "macos"))]
    let mut command = {
        if block_key.is_none() { return Err(BackendError::new("dsh_native_credentials", "Native dsh credentials require read-only protection on this platform")); }
        Command::new("dsh")
    };
    command.args(["--profile", "sdk-minimal", "--patch"]).arg(&patch_path)
        .env("DSH_HOME", dir.0.join("home")).env("DSH_TELEMETRY_DISABLED", "1")
        .current_dir(&workspace).stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::null());
    if let Some(key) = block_key { command.env("DEEPSEEK_API_KEY", key); }
    #[cfg(unix)] {
        use std::os::unix::process::CommandExt;
        command.process_group(0);
    }
    let mut child = command.spawn().map_err(io_error)?;
    let stdin = child.stdin.take().ok_or_else(|| BackendError::new("dsh_stdio", "Missing stdin"))?;
    let stdout = child.stdout.take().ok_or_else(|| BackendError::new("dsh_stdio", "Missing stdout"))?;
    let child = Arc::new(Mutex::new(child));
    let worker_child = child.clone();
    let watchdog_child = child.clone();
    let (initialized, wait_init) = std::sync::mpsc::channel::<()>();
    std::thread::spawn(move || {
        if matches!(wait_init.recv_timeout(std::time::Duration::from_secs(30)), Err(std::sync::mpsc::RecvTimeoutError::Timeout)) {
            if let Ok(mut child) = watchdog_child.lock() { let _ = child.kill(); }
        }
    });
    let worker: Worker = Box::new(move || {
        let result = run(&request, &events, stdin, stdout, &cancelled, &token, &workspace, &bridge_ready, initialized, &tools);
        tools.cancel();
        drop(guard);
        if let Ok(mut child) = worker_child.lock() {
            if result.is_ok() {
                for _ in 0..60 {
                    if child.try_wait().ok().flatten().is_some() { break; }
                    std::thread::sleep(std::time::Duration::from_millis(50));
                }
            }
            if child.try_wait().ok().flatten().is_none() { let _ = child.kill(); }
            let _ = child.wait();
        }
        if let Err(error) = result {
            if !cancelled.load(Ordering::Acquire) { emit(&events, &request, BackendEventKind::Error(error)); }
        }
        drop(dir);
    });
    Ok((child, bridge_active, worker))
}

fn io_error(error: impl std::fmt::Display) -> BackendError { BackendError::new("dsh_io", error.to_string()) }

fn emit(events: &Sender<BackendEvent>, request: &TurnRequest, kind: BackendEventKind) {
    let _ = events.send(BackendEvent { conversation_id: request.conversation_id.clone(), turn_id: request.turn_id.clone(), kind });
}

fn send(stdin: &mut impl Write, id: u32, method: &str, params: Value) -> Result<(), BackendError> {
    writeln!(stdin, "{}", json!({"jsonrpc":"2.0", "id":id, "method":method, "params":params})).map_err(io_error)?;
    stdin.flush().map_err(io_error)
}

fn run(request: &TurnRequest, events: &Sender<BackendEvent>, mut stdin: impl Write,
    stdout: impl std::io::Read, cancelled: &AtomicBool, token: &str, cwd: &PathBuf,
    bridge_ready: &AtomicBool, initialized: Sender<()>, tools: &ToolBridge) -> Result<(), BackendError> {
    let session = request.native_session_id.clone().unwrap_or_else(|| token[..32].to_owned());
    let provider = request.config["provider"].as_str().unwrap_or_default();
    let model = request.config["model"].as_str().unwrap_or_default();
    send(&mut stdin, 1, "initialize", json!({"cwd":cwd,"provider":provider,"model":model}))?;
    let mut terminal: Option<Value> = None;
    for line in BufReader::new(stdout).lines() {
        let line = line.map_err(io_error)?;
        let value: Value = serde_json::from_str(&line).map_err(io_error)?;
        if let Some(id) = value["id"].as_u64() {
            if !value["error"].is_null() { return Err(BackendError::new("dsh_rpc", value["error"].to_string())); }
            if id == 1 {
                for _ in 0..100 {
                    if bridge_ready.load(Ordering::Acquire) { break; }
                    if cancelled.load(Ordering::Acquire) { return Ok(()); }
                    std::thread::sleep(std::time::Duration::from_millis(50));
                }
                if !bridge_ready.load(Ordering::Acquire) {
                    return Err(BackendError::new("dsh_mcp", "Block MCP tools were not discovered"));
                }
                let _ = initialized.send(());
            }
            match id {
                1 if request.native_session_id.is_some() => send(&mut stdin, 2, "session/resume", json!({"sessionId":session}))?,
                1 | 2 => {
                    let context = serde_json::to_string(&request.context).map_err(io_error)?;
                    let text = format!("Block context: {context}\n\n{}", request.message);
                    send(&mut stdin, 3, "session/prompt", json!({"sessionId":session,"contentBlocks":[{"type":"text","text":text}]}))?;
                }
                3 => {
                    emit(events, request, BackendEventKind::Session { native_session_id: session.clone() });
                    send(&mut stdin, 4, "session/wait", json!({"sessionId":session}))?;
                }
                4 => {
                    match terminal.as_ref().and_then(|reason| reason["kind"].as_str()) {
                        Some("completed") | Some("aborted") => send(&mut stdin, 5, "shutdown", json!({}))?,
                        Some(kind) => return Err(BackendError::new("dsh_turn", format!("Turn ended: {kind}: {}", terminal.as_ref().unwrap()))),
                        None => return Err(BackendError::new("dsh_turn", "Session became idle without a turn result")),
                    }
                }
                5 => {
                    let kind = if terminal.as_ref().and_then(|reason| reason["kind"].as_str()) == Some("completed") {
                        BackendEventKind::Completed
                    } else { BackendEventKind::Cancelled };
                    emit(events, request, kind);
                    return Ok(());
                }
                _ => {}
            }
        } else if value["method"] == "session.event" && value["params"]["sessionId"] == session {
            let event = &value["params"]["event"];
            match event["type"].as_str().unwrap_or("") {
                "assistant/message" => if let Some(blocks) = event["data"]["message"]["content"].as_array() {
                    for block in blocks {
                        if let Some(text) = block["text"].as_str() {
                            let kind = match block["type"].as_str() {
                                Some("text") => Some(BackendEventKind::Delta(text.to_owned())),
                                Some("reasoning") => Some(BackendEventKind::Thinking(text.to_owned())),
                                _ => None,
                            };
                            if let Some(kind) = kind { emit(events, request, kind); }
                        }
                    }
                },
                "tool/call" => emit(events, request, BackendEventKind::ToolActivity {
                    name: event["data"]["name"].as_str().unwrap_or("").to_owned(), detail: String::new() }),
                "turn/end" => { tools.cancel(); terminal = Some(event["data"]["reason"].clone()); },
                _ => {}
            }
        }
        if cancelled.load(Ordering::Acquire) { return Ok(()); }
    }
    Err(BackendError::new("dsh_disconnected", "dsh exited before completing the turn"))
}
