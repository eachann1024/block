//! OpenAI streaming transport; only completed responses can execute tools.
use super::*;
use std::io::{BufRead, BufReader, Read};
use std::sync::atomic::Ordering;

/// One streamed turn with optional `tools`; tool-call deltas are sent as `Event::ToolCalls` before `Done`.
pub(crate) fn stream_agent(msgs: Vec<serde_json::Value>, tools: Option<serde_json::Value>, cancel: Arc<AtomicBool>) -> mpsc::Receiver<Event> {
    stream_agent_with_config(config(), msgs, tools, cancel, None)
}

pub(crate) fn stream_agent_with_config(config: Config, msgs: Vec<serde_json::Value>, tools: Option<serde_json::Value>,
    cancel: Arc<AtomicBool>, thinking: Option<String>) -> mpsc::Receiver<Event> {
    let (tx, rx) = mpsc::channel();
    #[cfg(target_family = "wasm")]
    {
        let _ = (config, msgs, tools, cancel, thinking);
        let _ = tx.send(Event::Failed { message: tr("网页演示版暂不连接在线 AI 服务", "Online AI is disabled in web demo").into(), settings: false });
    }
    #[cfg(not(target_family = "wasm"))]
    std::thread::spawn(move || {
        let fail = |message: String, settings: bool| { let _ = tx.send(Event::Failed { message, settings }); };
        if let Err((m, s)) = validate_request(&config) { return fail(m, s); }
        let resolved = if thinking.is_some() && config.model.is_empty() {
            list_models(&config).map_err(|error| (error, false)).and_then(|models| models.into_iter().find(|model| !model.trim().is_empty())
                .ok_or_else(|| (missing_model_msg().into(), false)))
        } else { resolve_model(&config) };
        let model = match resolved { Ok(m) => m, Err((m, s)) => return fail(m, s) };
        if cancel.load(Ordering::Relaxed) { return; }
        if config.effective_protocol() == Protocol::Anthropic {
            return anthropic::stream_turn(config, model, msgs, tools, cancel, tx, thinking);
        }
        let mut body = serde_json::json!({ "model": model, "stream": true, "messages": msgs });
        if let Some(t) = tools { body["tools"] = t; }
        if let Some(level) = thinking {
            body["reasoning_effort"] = level.into();
            if config.provider == "deepseek" { body["thinking"] = serde_json::json!({"type":"enabled"}); }
        }
        let body = body.to_string();
        let mut calls: Vec<(String, String, String)> = Vec::new();
        let mut child = match curl(&config, "/chat/completions", true, "180").args(["--speed-time", "60", "--speed-limit", "1"]).spawn() {
            Ok(c) => c,
            Err(e) => return fail(if is_en() { format!("Failed to launch curl: {e}") } else { format!("无法启动 curl：{e}") }, false),
        };
        if let Some(mut stdin) = child.stdin.take() { let _ = stdin.write_all(body.as_bytes()); }
        let mut other = String::new(); let mut code = 0u16; let mut got = false; let mut completed = false;
        let stdout = child.stdout.take().expect("piped stdout");
        for line in BufReader::new(stdout).lines() {
            if cancel.load(Ordering::Relaxed) { let _ = child.kill(); let _ = child.wait(); return; }
            let Ok(line) = line else { break };
            if let Some(data) = line.strip_prefix("data:") {
                let data = data.trim();
                if data == "[DONE]" { completed = true; continue; }
                let Ok(v) = serde_json::from_str::<serde_json::Value>(data) else { continue };
                if let Some(err) = v.pointer("/error/message").and_then(|m| m.as_str()) { let _ = child.kill(); return fail(format_error(err), false); }
                if v.pointer("/choices/0/finish_reason").is_some_and(|r| r.as_str().is_some()) { completed = true; }
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
        let status = child.wait();
        if cancel.load(Ordering::Relaxed) { return; }
        if status.as_ref().is_ok_and(|s| s.code() == Some(28)) {
            crate::log::warn("AI stream timed out (OpenAI)");
            return fail(tr("AI 响应超时，请重试或切换模型。", "AI response timed out. Retry or switch models.").into(), false);
        }
        if code == 0 {
            return fail(tr("网络请求失败，请检查网络或接口地址", "Network request failed. Check your connection or Base URL").into(), true);
        }
        if !(200..300).contains(&code) { return fail(keyed(&config, service_error(other.trim(), code)), code == 401 || code == 403 || code == 404); }
        if !status.is_ok_and(|s| s.success()) || !completed {
            crate::log::warn("AI stream interrupted before completion (OpenAI)");
            return fail(tr("AI 响应中断，请重试。", "AI response interrupted. Please retry.").into(), false);
        }
        if !got { return fail(tr("AI 未返回可写入的内容。", "AI returned an empty response.").into(), false); }
        if !calls.is_empty() {
            for (i, c) in calls.iter_mut().enumerate() { if c.0.is_empty() { c.0 = format!("call_{i}"); } }
            let _ = tx.send(Event::ToolCalls(calls));
        }
        let _ = tx.send(Event::Done);
    });
    rx
}
