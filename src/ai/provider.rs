//! OpenAI-compatible chat provider. Streams through the system `curl`, so the app
//! carries no HTTP stack. Settings come from `ai.json` beside `state.json` (edited in
//! 偏好设置 › AI 助手), then the `THINKLY_AI_*` environment variables, then the local
//! gateway default.
use std::io::{BufRead, BufReader, Read, Write};
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, mpsc};

pub(crate) const DEFAULT_BASE_URL: &str = "http://127.0.0.1:3425/v1";
pub(crate) const MISSING_KEY: &str = "请前往「设置 › AI 助手 › AI 服务」补充 API Key";

fn yes() -> bool { true }

#[derive(Clone, Debug, serde::Deserialize, serde::Serialize)]
pub(crate) struct Config {
    #[serde(default = "yes")] pub enabled: bool,
    #[serde(default)] pub base_url: String,
    #[serde(default)] pub api_key: String,
    #[serde(default)] pub model: String,
}
impl Default for Config {
    fn default() -> Self { Config { enabled: true, base_url: String::new(), api_key: String::new(), model: String::new() } }
}

fn settings_path() -> Option<PathBuf> {
    std::env::var_os("HOME").map(|h| PathBuf::from(h).join("Library/Application Support/thinkly-fast/ai.json"))
}

/// What is saved on disk, without environment overrides (the settings form edits this).
pub(crate) fn stored() -> Config {
    settings_path().and_then(|p| std::fs::read(p).ok())
        .and_then(|b| serde_json::from_slice(&b).ok()).unwrap_or_default()
}

pub(crate) fn save(config: &Config) -> Result<(), String> {
    let path = settings_path().ok_or("找不到 HOME 目录")?;
    if let Some(dir) = path.parent() { std::fs::create_dir_all(dir).map_err(|e| e.to_string())?; }
    let json = serde_json::to_string_pretty(config).map_err(|e| e.to_string())?;
    std::fs::write(path, json).map_err(|e| e.to_string())
}

pub(crate) fn normalize(mut c: Config) -> Config {
    let env = |k: &str| std::env::var(k).ok().filter(|v| !v.trim().is_empty());
    if let Some(v) = env("THINKLY_AI_BASE_URL") { c.base_url = v; }
    if let Some(v) = env("THINKLY_AI_KEY") { c.api_key = v; }
    if let Some(v) = env("THINKLY_AI_MODEL") { c.model = v; }
    if c.base_url.trim().is_empty() { c.base_url = DEFAULT_BASE_URL.into(); }
    c.base_url = c.base_url.trim().trim_end_matches('/').to_owned();
    c
}

pub(crate) fn config() -> Config { normalize(stored()) }

pub(crate) enum Event { Thinking(String), Delta(String), Done, Failed { message: String, settings: bool },
    /// OpenAI function calls the model asked for this turn: (id, name, JSON arguments).
    ToolCalls(Vec<(String, String, String)>) }

fn curl(config: &Config, path: &str, with_body: bool, max_time: &str) -> Command {
    let mut cmd = Command::new("/usr/bin/curl");
    cmd.args(["-sS", "-N", "--connect-timeout", "5", "--max-time", max_time, "-H", "content-type: application/json"]);
    if !config.api_key.is_empty() { cmd.arg("-H").arg(format!("authorization: Bearer {}", config.api_key)); }
    if with_body { cmd.args(["--data-binary", "@-"]); }
    cmd.arg("-w").arg("\n__HTTP__%{http_code}\n").arg(format!("{}{path}", config.base_url));
    cmd.stdin(if with_body { Stdio::piped() } else { Stdio::null() }).stdout(Stdio::piped()).stderr(Stdio::piped());
    cmd
}

fn split_status(out: &str) -> (String, u16) {
    match out.rfind("__HTTP__") {
        Some(at) => (out[..at].trim().to_owned(), out[at + 8..].trim().parse().unwrap_or(0)),
        None => (out.trim().to_owned(), 0),
    }
}

/// thinkly's `formatAiMenuError`: maps raw failures to the short Chinese messages.
pub(crate) fn format_error(raw: &str) -> String {
    if raw == MISSING_KEY { return raw.into(); }
    let lower = raw.to_lowercase();
    if lower.contains("missing block") || raw.contains("目标块") { return "目标块已变化，原文未修改。请重新选择后再试".into(); }
    if lower.contains("tool_choice") || lower.contains("thinking mode") { return "当前模型的思考模式不支持强制工具调用，请换非思考模型或关闭思考后再试".into(); }
    if lower.contains("401") || lower.contains("403") || lower.contains("unauthorized") || lower.contains("api key") || lower.contains("api_key") { return "密钥无效或未配置".into(); }
    if lower.contains("network") || lower.contains("econnrefused") || lower.contains("timeout") || lower.contains("timed out")
        || lower.contains("connect") || lower.contains("resolve host") { return "网络请求失败，请检查网络或 Base URL".into(); }
    if lower.contains("abort") || lower.contains("cancel") { return "已停止".into(); }
    if lower.contains("invalid json") || lower.contains("expected value") { return "模型返回格式异常，请换模型或稍后再试".into(); }
    raw.to_owned()
}

/// thinkly: an auth failure with no key saved points at settings instead of blaming the key.
fn keyed(config: &Config, message: String) -> String {
    if config.api_key.trim().is_empty() && message == "密钥无效或未配置" { MISSING_KEY.into() } else { message }
}

fn service_error(body: &str, code: u16) -> String {
    let detail = serde_json::from_str::<serde_json::Value>(body).ok()
        .and_then(|v| v.pointer("/error/message").or_else(|| v.get("message")).or_else(|| v.get("error"))
            .and_then(|m| m.as_str()).map(str::to_owned))
        .unwrap_or_else(|| body.chars().take(160).collect());
    if code == 401 || code == 403 { return "密钥无效或未配置".into(); }
    if detail.is_empty() { format!("AI 服务返回 {code}") } else { format_error(&format!("AI 服务返回 {code}：{detail}")) }
}

/// `GET /models`: the ids the service offers.
pub(crate) fn list_models(config: &Config) -> Result<Vec<String>, String> {
    let out = curl(config, "/models", false, "15").output().map_err(|e| format!("无法启动 curl：{e}"))?;
    let (body, code) = split_status(&String::from_utf8_lossy(&out.stdout));
    if code == 0 { return Err("网络请求失败，请检查网络或 Base URL".into()); }
    if !(200..300).contains(&code) { return Err(service_error(&body, code)); }
    let mut ids: Vec<String> = serde_json::from_str::<serde_json::Value>(&body).ok()
        .and_then(|v| v.get("data").and_then(|d| d.as_array()).cloned()).unwrap_or_default()
        .iter().filter_map(|m| m.get("id").and_then(|i| i.as_str()).map(str::to_owned)).collect();
    ids.sort(); ids.dedup();
    Ok(ids)
}

/// The configured model, else the gateway's first listed model (preferring a fast one).
fn resolve_model(config: &Config) -> Result<String, (String, bool)> {
    if !config.model.trim().is_empty() { return Ok(config.model.trim().to_owned()); }
    let ids = list_models(config).map_err(|m| { let s = m.contains("密钥"); (keyed(config, m), s) })?;
    ids.iter().find(|id| id.contains("flash")).or(ids.first()).cloned()
        .ok_or_else(|| ("请先选择模型后再使用行内 AI。".to_owned(), true))
}

/// thinkly's 测试连接: one tiny completion, no notes or local context.
pub(crate) fn test_connection(config: &Config) -> Result<(), String> {
    let model = resolve_model(config).map_err(|(m, _)| m)?;
    let body = serde_json::json!({ "model": model, "max_tokens": 8,
        "messages": [{ "role": "user", "content": "ping" }] }).to_string();
    let mut child = curl(config, "/chat/completions", true, "30").spawn().map_err(|e| format!("无法启动 curl：{e}"))?;
    if let Some(mut stdin) = child.stdin.take() { let _ = stdin.write_all(body.as_bytes()); }
    let out = child.wait_with_output().map_err(|e| e.to_string())?;
    let (body, code) = split_status(&String::from_utf8_lossy(&out.stdout));
    if code == 0 {
        let err = String::from_utf8_lossy(&out.stderr);
        return Err(if err.contains("timed out") { "连接测试超时（30 秒），请检查配置后重试。".into() } else { "连接失败，请检查配置后重试。".into() });
    }
    if !(200..300).contains(&code) { return Err(keyed(config, service_error(&body, code))); }
    Ok(())
}

/// Streams one chat completion on a worker thread. Setting `cancel` kills the request.
pub(crate) fn stream(system: String, user: String, cancel: Arc<AtomicBool>) -> mpsc::Receiver<Event> {
    stream_messages(vec![("system".into(), system), ("user".into(), user)], cancel)
}

pub(crate) fn stream_messages(messages: Vec<(String, String)>, cancel: Arc<AtomicBool>) -> mpsc::Receiver<Event> {
    let msgs = messages.iter().map(|(r, c)| serde_json::json!({ "role": r, "content": c })).collect();
    stream_agent(msgs, None, cancel)
}

/// One streamed turn with optional OpenAI `tools`; tool-call deltas are gathered and
/// sent as `Event::ToolCalls` before `Done`.
pub(crate) fn stream_agent(msgs: Vec<serde_json::Value>, tools: Option<serde_json::Value>, cancel: Arc<AtomicBool>) -> mpsc::Receiver<Event> {
    let (tx, rx) = mpsc::channel();
    std::thread::spawn(move || {
        let config = config();
        let fail = |message: String, settings: bool| { let _ = tx.send(Event::Failed { message, settings }); };
        if !config.enabled { return fail("当前页面不可编辑或 AI 未开启。".into(), true); }
        let model = match resolve_model(&config) { Ok(m) => m, Err((m, s)) => return fail(m, s) };
        if cancel.load(Ordering::Relaxed) { return; }
        let mut body = serde_json::json!({ "model": model, "stream": true, "messages": msgs });
        if let Some(t) = tools { body["tools"] = t; }
        let body = body.to_string();
        let mut calls: Vec<(String, String, String)> = Vec::new();
        let mut child = match curl(&config, "/chat/completions", true, "180").spawn() {
            Ok(c) => c, Err(e) => return fail(format!("无法启动 curl：{e}"), false),
        };
        if let Some(mut stdin) = child.stdin.take() { let _ = stdin.write_all(body.as_bytes()); }
        let mut other = String::new(); let mut code = 0u16; let mut got = false;
        let stdout = child.stdout.take().expect("piped stdout");
        for line in BufReader::new(stdout).lines() {
            if cancel.load(Ordering::Relaxed) { let _ = child.kill(); let _ = child.wait(); return; }
            let Ok(line) = line else { break };
            if let Some(data) = line.strip_prefix("data:") {
                let data = data.trim();
                if data == "[DONE]" { continue; }
                let Ok(v) = serde_json::from_str::<serde_json::Value>(data) else { continue };
                if let Some(err) = v.pointer("/error/message").and_then(|m| m.as_str()) { let _ = child.kill(); return fail(format_error(err), false); }
                let delta = v.pointer("/choices/0/delta");
                if let Some(t) = delta.and_then(|d| d.get("reasoning_content").or_else(|| d.get("reasoning"))).and_then(|t| t.as_str()) {
                    let _ = tx.send(Event::Thinking(t.to_owned()));
                }
                for c in delta.and_then(|d| d.get("tool_calls")).and_then(|t| t.as_array()).into_iter().flatten() {
                    let i = c.get("index").and_then(|i| i.as_u64()).unwrap_or(calls.len() as u64) as usize;
                    while calls.len() <= i { calls.push(Default::default()); }
                    if let Some(id) = c.get("id").and_then(|v| v.as_str()) { calls[i].0 = id.into(); }
                    if let Some(n) = c.pointer("/function/name").and_then(|v| v.as_str()) { calls[i].1.push_str(n); }
                    if let Some(a) = c.pointer("/function/arguments").and_then(|v| v.as_str()) { calls[i].2.push_str(a); }
                    got = true;
                }
                if let Some(t) = delta.and_then(|d| d.get("content")).and_then(|t| t.as_str()).filter(|t| !t.is_empty()) {
                    got = true; let _ = tx.send(Event::Delta(t.to_owned()));
                }
            } else if let Some(c) = line.strip_prefix("__HTTP__") { code = c.trim().parse().unwrap_or(0); }
            else { other.push_str(&line); other.push('\n'); }
        }
        let mut stderr = String::new();
        if let Some(mut e) = child.stderr.take() { let _ = e.read_to_string(&mut stderr); }
        let _ = child.wait();
        if cancel.load(Ordering::Relaxed) { return; }
        if code == 0 { return fail("网络请求失败，请检查网络或 Base URL".into(), true); }
        if !(200..300).contains(&code) { return fail(keyed(&config, service_error(other.trim(), code)), code == 401 || code == 403 || code == 404); }
        if !got { return fail("AI 未返回可写入的内容。".into(), false); }
        if !calls.is_empty() {
            for (i, c) in calls.iter_mut().enumerate() { if c.0.is_empty() { c.0 = format!("call_{i}"); } }
            let _ = tx.send(Event::ToolCalls(calls));
        }
        let _ = tx.send(Event::Done);
    });
    rx
}
