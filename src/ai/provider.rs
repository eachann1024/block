//! OpenAI-compatible chat provider. Streams through the system `curl`, so the app
//! carries no HTTP stack. Settings come from `ai.json` beside `state.json`, then
//! the `THINKLY_AI_*` environment variables, then the local gateway default.
use std::io::{BufRead, BufReader, Read, Write};
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, mpsc};

pub(in crate::editor) const DEFAULT_BASE_URL: &str = "http://127.0.0.1:3425/v1";
pub(in crate::editor) const SETTINGS_HINT: &str =
    "在 ~/Library/Application Support/thinkly-fast/ai.json 填写 base_url、api_key、model，或设置环境变量 THINKLY_AI_BASE_URL、THINKLY_AI_KEY、THINKLY_AI_MODEL。";

#[derive(Clone, Debug, Default, serde::Deserialize)]
pub(in crate::editor) struct Config {
    #[serde(default)] pub base_url: String,
    #[serde(default)] pub api_key: String,
    #[serde(default)] pub model: String,
}

fn settings_path() -> Option<PathBuf> {
    std::env::var_os("HOME").map(|h| PathBuf::from(h).join("Library/Application Support/thinkly-fast/ai.json"))
}

pub(in crate::editor) fn config() -> Config {
    let mut c: Config = settings_path().and_then(|p| std::fs::read(p).ok())
        .and_then(|b| serde_json::from_slice(&b).ok()).unwrap_or_default();
    let env = |k: &str| std::env::var(k).ok().filter(|v| !v.trim().is_empty());
    if let Some(v) = env("THINKLY_AI_BASE_URL") { c.base_url = v; }
    if let Some(v) = env("THINKLY_AI_KEY") { c.api_key = v; }
    if let Some(v) = env("THINKLY_AI_MODEL") { c.model = v; }
    if c.base_url.trim().is_empty() { c.base_url = DEFAULT_BASE_URL.into(); }
    c.base_url = c.base_url.trim().trim_end_matches('/').to_owned();
    c
}

pub(in crate::editor) enum Event { Thinking(String), Delta(String), Done, Failed { message: String, settings: bool } }

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

fn service_error(body: &str, code: u16) -> String {
    let detail = serde_json::from_str::<serde_json::Value>(body).ok()
        .and_then(|v| v.pointer("/error/message").or_else(|| v.get("message")).or_else(|| v.get("error"))
            .and_then(|m| m.as_str()).map(str::to_owned))
        .unwrap_or_else(|| body.chars().take(160).collect());
    if detail.is_empty() { format!("AI 服务返回 {code}") } else { format!("AI 服务返回 {code}：{detail}") }
}

/// The configured model, else the gateway's first listed model (preferring a fast one).
fn resolve_model(config: &Config) -> Result<String, (String, bool)> {
    if !config.model.trim().is_empty() { return Ok(config.model.trim().to_owned()); }
    let out = curl(config, "/models", false, "10").output().map_err(|e| (format!("无法启动 curl：{e}"), false))?;
    let (body, code) = split_status(&String::from_utf8_lossy(&out.stdout));
    if code == 0 { return Err((format!("无法连接 AI 服务 {}", config.base_url), true)); }
    if !(200..300).contains(&code) { return Err((service_error(&body, code), code == 401 || code == 403)); }
    let ids: Vec<String> = serde_json::from_str::<serde_json::Value>(&body).ok()
        .and_then(|v| v.get("data").and_then(|d| d.as_array()).cloned()).unwrap_or_default()
        .iter().filter_map(|m| m.get("id").and_then(|i| i.as_str()).map(str::to_owned)).collect();
    ids.iter().find(|id| id.contains("flash")).or(ids.first()).cloned()
        .ok_or_else(|| ("AI 服务没有列出可用模型".to_owned(), true))
}

/// Streams one chat completion on a worker thread. Setting `cancel` kills the request.
pub(in crate::editor) fn stream(system: String, user: String, cancel: Arc<AtomicBool>) -> mpsc::Receiver<Event> {
    let (tx, rx) = mpsc::channel();
    std::thread::spawn(move || {
        let config = config();
        let fail = |message: String, settings: bool| { let _ = tx.send(Event::Failed { message, settings }); };
        let model = match resolve_model(&config) { Ok(m) => m, Err((m, s)) => return fail(m, s) };
        if cancel.load(Ordering::Relaxed) { return; }
        let body = serde_json::json!({ "model": model, "stream": true,
            "messages": [{ "role": "system", "content": system }, { "role": "user", "content": user }] }).to_string();
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
                if let Some(err) = v.pointer("/error/message").and_then(|m| m.as_str()) { let _ = child.kill(); return fail(format!("AI 服务出错：{err}"), false); }
                let delta = v.pointer("/choices/0/delta");
                if let Some(t) = delta.and_then(|d| d.get("reasoning_content").or_else(|| d.get("reasoning"))).and_then(|t| t.as_str()) {
                    let _ = tx.send(Event::Thinking(t.to_owned()));
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
        if code == 0 { return fail(format!("无法连接 AI 服务 {}（{}）", config.base_url, stderr.trim()), true); }
        if !(200..300).contains(&code) { return fail(service_error(other.trim(), code), code == 401 || code == 403 || code == 404); }
        if !got { return fail("AI 没有返回内容".into(), false); }
        let _ = tx.send(Event::Done);
    });
    rx
}
