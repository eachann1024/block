//! AI chat provider (OpenAI Chat Completions & Anthropic Messages). Streams through the
//! system `curl`, so the app carries no HTTP stack. Settings come exclusively from
//! `ai.json` beside `state.json` (edited in 偏好设置 › AI 助手).
use std::collections::BTreeMap;
use std::io::Write;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::sync::atomic::AtomicBool;
use std::sync::{Arc, mpsc};

use super::anthropic;
#[path = "provider_validation.rs"] mod validation;
use validation::resolve_model;
pub(crate) use validation::validate_request;
use super::presets::{self, Protocol, ProviderProfile};
use crate::theme::{is_en, tr};

fn yes() -> bool { true }
pub(crate) fn missing_key_msg() -> &'static str {
    tr("请前往「设置 › AI 助手」选择服务商并填写 API 密钥", "Go to Settings › AI Assistant to select a provider and enter an API Key")
}
pub(crate) fn missing_url_msg() -> &'static str {
    tr("请前往「设置 › AI 助手」配置服务商与接口地址", "Go to Settings › AI Assistant to configure a provider and Base URL")
}
pub(crate) fn missing_model_msg() -> &'static str {
    tr("请前往「设置 › AI 助手」选择默认模型", "Go to Settings › AI Assistant to select a default model")
}
fn invalid_key_msg() -> &'static str { tr("密钥无效或未配置", "Invalid or missing API Key") }

#[derive(Clone, Debug, serde::Deserialize, serde::Serialize)]
pub(crate) struct Config {
    #[serde(default = "yes")] pub enabled: bool,
    #[serde(default)] pub provider: String,
    #[serde(default)] pub protocol: Protocol,
    #[serde(default)] pub base_url: String,
    #[serde(default)] pub api_key: String,
    #[serde(default)] pub model: String,
    #[serde(default)] pub providers: BTreeMap<String, ProviderProfile>,
}
impl Default for Config {
    fn default() -> Self {
        Config { enabled: true, provider: String::new(), protocol: Protocol::Openai,
            base_url: String::new(), api_key: String::new(), model: String::new(), providers: BTreeMap::new() }
    }
}
impl Config {
    pub(crate) fn effective_protocol(&self) -> Protocol {
        presets::find(&self.provider).map_or(self.protocol, |p| p.protocol)
    }
    pub(crate) fn key_required(&self) -> bool { presets::find(&self.provider).map_or(true, |p| p.key_required) }
}

fn settings_path() -> Option<PathBuf> {
    Some(crate::files::app_data_dir("Block").join("ai.json"))
}

/// What is saved on disk (the settings form edits this).
pub(crate) fn stored() -> Config {
    let mut c: Config = settings_path().and_then(|p| std::fs::read(p).ok())
        .and_then(|b| serde_json::from_slice(&b).ok()).unwrap_or_default();
    c.provider = presets::normalize_provider_id(&c.provider, c.protocol, &c.base_url).to_owned();
    if let Some(p) = presets::find(&c.provider) {
        if c.base_url.trim().is_empty() { c.base_url = p.base_url.to_owned(); c.protocol = p.protocol; }
    }
    c
}

pub(crate) fn save(config: &Config) -> Result<(), String> {
    #[cfg(target_family = "wasm")]
    { let _ = config; return Ok(()); }
    #[cfg(not(target_family = "wasm"))]
    {
        let path = settings_path().ok_or_else(|| tr("找不到 HOME 目录", "HOME directory not found").to_owned())?;
        if let Some(dir) = path.parent() { std::fs::create_dir_all(dir).map_err(|e| e.to_string())?; }
        std::fs::write(path, serde_json::to_string_pretty(config).map_err(|e| e.to_string())?).map_err(|e| e.to_string())
    }
}

pub(crate) fn normalize(mut c: Config) -> Config {
    c.provider = c.provider.trim().to_owned();
    c.base_url = c.base_url.trim().trim_end_matches('/').to_owned();
    c.api_key = c.api_key.trim().to_owned();
    c.model = c.model.trim().to_owned();
    c.protocol = c.effective_protocol();
    c
}

pub(crate) fn config() -> Config { normalize(stored()) }

#[allow(dead_code)]
pub(crate) enum Event {
    Thinking(String), Delta(String), Done, Failed { message: String, settings: bool },
    /// Function calls the model asked for this turn: (id, name, JSON arguments).
    ToolCalls(Vec<(String, String, String)>),
}

fn curl(config: &Config, path: &str, with_body: bool, max_time: &str) -> Command {
    let mut cmd = Command::new(if cfg!(target_os = "macos") { "/usr/bin/curl" } else { "curl" });
    cmd.args(["-sS", "-N", "--connect-timeout", "5", "--max-time", max_time, "-H", "content-type: application/json"]);
    if !config.api_key.is_empty() { cmd.arg("-H").arg(format!("authorization: Bearer {}", config.api_key)); }
    if with_body { cmd.args(["--data-binary", "@-"]); }
    cmd.arg("-w").arg("\n__HTTP__%{http_code}\n").arg(format!("{}{path}", config.base_url));
    cmd.stdin(if with_body { Stdio::piped() } else { Stdio::null() }).stdout(Stdio::piped()).stderr(Stdio::piped());
    cmd
}

pub(super) fn split_status(out: &str) -> (String, u16) {
    match out.rfind("__HTTP__") {
        Some(at) => (out[..at].trim().to_owned(), out[at + 8..].trim().parse().unwrap_or(0)),
        None => (out.trim().to_owned(), 0),
    }
}

/// Maps raw failures to localized user-facing messages.
pub(crate) fn format_error(raw: &str) -> String {
    if raw == missing_key_msg() || raw == missing_url_msg() || raw == missing_model_msg() {
        return raw.into();
    }
    let lower = raw.to_lowercase();
    if lower.contains("missing block") || raw.contains("目标块") || lower.contains("target block") {
        return tr("目标块已变化，原文未修改。请重新选择后再试", "Target block changed; text was not modified. Reselect and try again").into();
    }
    if lower.contains("tool_choice") || lower.contains("thinking mode") {
        return tr("当前模型的思考模式不支持强制工具调用，请换非思考模型或关闭思考后再试", "Current model's thinking mode does not support forced tool use; switch models or disable thinking").into();
    }
    if lower.contains("401") || lower.contains("403") || lower.contains("unauthorized")
        || lower.contains("authentication_error") || lower.contains("api key") || lower.contains("api_key")
        || raw.contains("密钥无效") || lower.contains("invalid or missing api key")
    {
        return invalid_key_msg().into();
    }
    if lower.contains("network") || lower.contains("econnrefused") || lower.contains("timeout")
        || lower.contains("timed out") || lower.contains("connect") || lower.contains("resolve host")
    {
        return tr("网络请求失败，请检查网络或接口地址", "Network request failed. Check your connection or Base URL").into();
    }
    if lower.contains("abort") || lower.contains("cancel") {
        return tr("已停止", "Stopped").into();
    }
    if lower.contains("invalid json") || lower.contains("expected value") {
        return tr("模型返回格式异常，请换模型或稍后再试", "Malformed model response. Try another model or retry later").into();
    }
    raw.to_owned()
}

pub(super) fn keyed(config: &Config, message: String) -> String {
    if config.api_key.trim().is_empty() && message == invalid_key_msg() { missing_key_msg().into() } else { message }
}

pub(super) fn service_error(body: &str, code: u16) -> String {
    let detail = serde_json::from_str::<serde_json::Value>(body).ok()
        .and_then(|v| v.pointer("/error/message").or_else(|| v.get("message")).or_else(|| v.get("error"))
            .and_then(|m| m.as_str()).map(str::to_owned))
        .unwrap_or_else(|| body.chars().take(160).collect());
    if code == 401 || code == 403 { return invalid_key_msg().into(); }
    if detail.is_empty() {
        if is_en() { format!("AI service returned {code}") } else { format!("AI 服务返回 {code}") }
    } else {
        format_error(&if is_en() { format!("AI service returned {code}: {detail}") } else { format!("AI 服务返回 {code}：{detail}") })
    }
}

/// Lists models offered by the configured provider (`GET /models`).
pub(crate) fn list_models(config: &Config) -> Result<Vec<String>, String> {
    if cfg!(target_family = "wasm") { return Err(tr("网页演示版暂不连接在线 AI 服务", "Online AI is disabled in web demo").into()); }
    if config.base_url.trim().is_empty() { return Err(missing_url_msg().into()); }
    if config.key_required() && config.api_key.trim().is_empty() { return Err(missing_key_msg().into()); }
    if config.effective_protocol() == Protocol::Anthropic { return anthropic::list_models(config); }
    let out = curl(config, "/models", false, "15").output().map_err(|e| {
        if is_en() { format!("Failed to launch curl: {e}") } else { format!("无法启动 curl：{e}") }
    })?;
    let (body, code) = split_status(&String::from_utf8_lossy(&out.stdout));
    if code == 0 {
        return Err(tr("网络请求失败，请检查网络或接口地址", "Network request failed. Check your connection or Base URL").into());
    }
    if !(200..300).contains(&code) { return Err(service_error(&body, code)); }
    let ids: Vec<String> = serde_json::from_str::<serde_json::Value>(&body).ok()
        .and_then(|v| v.get("data").and_then(|d| d.as_array()).cloned()).unwrap_or_default()
        .iter().filter_map(|m| m.get("id").and_then(|i| i.as_str()).map(str::to_owned)).collect();
    Ok(ids)
}

/// Connection test: one tiny completion (`max_tokens: 8`), no notes or local context.
pub(crate) fn test_connection(config: &Config) -> Result<(), String> {
    if cfg!(target_family = "wasm") { return Err(tr("网页演示版暂不连接在线 AI 服务", "Online AI is disabled in web demo").into()); }
    let model = resolve_model(config).map_err(|(m, _)| m)?;
    if config.effective_protocol() == Protocol::Anthropic { return anthropic::test_connection(config, &model); }
    let body = serde_json::json!({ "model": model, "max_tokens": 8, "messages": [{ "role": "user", "content": "ping" }] }).to_string();
    let mut child = curl(config, "/chat/completions", true, "30").spawn().map_err(|e| {
        if is_en() { format!("Failed to launch curl: {e}") } else { format!("无法启动 curl：{e}") }
    })?;
    if let Some(mut stdin) = child.stdin.take() { let _ = stdin.write_all(body.as_bytes()); }
    let out = child.wait_with_output().map_err(|e| {
        if is_en() { format!("Failed to read curl output: {e}") } else { format!("读取 curl 输出失败：{e}") }
    })?;
    let (body, code) = split_status(&String::from_utf8_lossy(&out.stdout));
    if code == 0 {
        let err = String::from_utf8_lossy(&out.stderr);
        return Err(if err.contains("timed out") {
            tr("连接测试超时（30 秒），请检查配置后重试。", "Connection test timed out (30s). Check settings and try again.").into()
        } else {
            tr("连接失败，请检查配置后重试。", "Connection failed. Check settings and try again.").into()
        });
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

#[path = "provider_stream.rs"] mod streaming;
pub(crate) use streaming::{stream_agent, stream_agent_with_config};
