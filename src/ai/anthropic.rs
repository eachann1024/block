//! Native Anthropic Messages API client (`/v1/models`, `/v1/messages`) over system `curl`.
use std::io::{BufRead, BufReader, Read, Write};
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, mpsc};

use super::provider::{self, Config, Event};
use crate::theme::{is_en, tr};

const ANTHROPIC_VERSION: &str = "2023-06-01";
const DEFAULT_MAX_TOKENS: u32 = 8192;

fn curl(config: &Config, path: &str, with_body: bool, max_time: &str) -> Command {
    let mut cmd = Command::new(if cfg!(target_os = "macos") { "/usr/bin/curl" } else { "curl" });
    cmd.args([
        "-sS", "-N", "--connect-timeout", "5", "--max-time", max_time,
        "-H", "content-type: application/json",
        "-H", &format!("anthropic-version: {ANTHROPIC_VERSION}"),
    ]);
    if !config.api_key.is_empty() { cmd.arg("-H").arg(format!("x-api-key: {}", config.api_key)); }
    if with_body { cmd.args(["--data-binary", "@-"]); }
    cmd.arg("-w").arg("\n__HTTP__%{http_code}\n").arg(format!("{}{path}", config.base_url));
    cmd.stdin(if with_body { Stdio::piped() } else { Stdio::null() })
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    cmd
}

/// `GET /models?limit=1000`: lists Anthropic model IDs (`data[].id`).
pub(super) fn list_models(config: &Config) -> Result<Vec<String>, String> {
    let out = curl(config, "/models?limit=1000", false, "15").output().map_err(|e| {
        if is_en() { format!("Failed to launch curl: {e}") } else { format!("无法启动 curl：{e}") }
    })?;
    let (body, code) = provider::split_status(&String::from_utf8_lossy(&out.stdout));
    if code == 0 {
        return Err(tr("网络请求失败，请检查网络或接口地址", "Network request failed. Check your connection or Base URL").into());
    }
    if !(200..300).contains(&code) { return Err(provider::service_error(&body, code)); }
    let mut ids: Vec<String> = serde_json::from_str::<serde_json::Value>(&body).ok()
        .and_then(|v| v.get("data").and_then(|d| d.as_array()).cloned()).unwrap_or_default()
        .iter().filter_map(|m| m.get("id").and_then(|i| i.as_str()).map(str::to_owned)).collect();
    ids.sort();
    ids.dedup();
    Ok(ids)
}

/// `POST /messages` with `max_tokens: 8` for connection testing.
pub(super) fn test_connection(config: &Config, model: &str) -> Result<(), String> {
    let body = serde_json::json!({
        "model": model, "max_tokens": 8, "messages": [{ "role": "user", "content": "ping" }]
    }).to_string();
    let mut child = curl(config, "/messages", true, "30").spawn().map_err(|e| {
        if is_en() { format!("Failed to launch curl: {e}") } else { format!("无法启动 curl：{e}") }
    })?;
    if let Some(mut stdin) = child.stdin.take() { let _ = stdin.write_all(body.as_bytes()); }
    let out = child.wait_with_output().map_err(|e| {
        if is_en() { format!("Failed to read curl output: {e}") } else { format!("读取 curl 输出失败：{e}") }
    })?;
    let (body, code) = provider::split_status(&String::from_utf8_lossy(&out.stdout));
    if code == 0 {
        let err = String::from_utf8_lossy(&out.stderr);
        return Err(if err.contains("timed out") {
            tr("连接测试超时（30 秒），请检查配置后重试。", "Connection test timed out (30s). Check settings and try again.").into()
        } else {
            tr("连接失败，请检查配置后重试。", "Connection failed. Check settings and try again.").into()
        });
    }
    if !(200..300).contains(&code) { return Err(provider::keyed(config, provider::service_error(&body, code))); }
    Ok(())
}

/// Converts OpenAI-shaped messages into `(system_prompt, anthropic_messages)`.
fn convert_messages(msgs: &[serde_json::Value]) -> (Option<String>, Vec<serde_json::Value>) {
    let mut systems = Vec::new();
    let mut out: Vec<serde_json::Value> = Vec::new();
    let mut pending_tool_results: Vec<serde_json::Value> = Vec::new();

    let flush_tools = |pending: &mut Vec<serde_json::Value>, out: &mut Vec<serde_json::Value>| {
        if !pending.is_empty() {
            let blocks = std::mem::take(pending);
            out.push(serde_json::json!({ "role": "user", "content": blocks }));
        }
    };

    for m in msgs {
        let role = m.get("role").and_then(|r| r.as_str()).unwrap_or("user");
        match role {
            "system" => {
                if let Some(c) = m.get("content").and_then(|c| c.as_str()).filter(|s| !s.trim().is_empty()) {
                    systems.push(c.to_owned());
                }
            }
            "tool" => {
                let id = m.get("tool_call_id").and_then(|v| v.as_str()).unwrap_or("");
                let content = m.get("content").and_then(|v| v.as_str()).unwrap_or("");
                pending_tool_results.push(serde_json::json!({ "type": "tool_result", "tool_use_id": id, "content": content }));
            }
            "assistant" => {
                flush_tools(&mut pending_tool_results, &mut out);
                let text = m.get("content").and_then(|v| v.as_str()).unwrap_or("");
                if let Some(tcs) = m.get("tool_calls").and_then(|v| v.as_array()).filter(|a| !a.is_empty()) {
                    let mut blocks = Vec::new();
                    if !text.is_empty() { blocks.push(serde_json::json!({ "type": "text", "text": text })); }
                    for tc in tcs {
                        let id = tc.get("id").and_then(|v| v.as_str()).unwrap_or("call_0");
                        let name = tc.pointer("/function/name").and_then(|v| v.as_str()).unwrap_or("");
                        let raw_args = tc.pointer("/function/arguments").and_then(|v| v.as_str()).unwrap_or("{}");
                        let input = serde_json::from_str::<serde_json::Value>(raw_args).ok()
                            .filter(|v| v.is_object()).unwrap_or_else(|| serde_json::json!({}));
                        blocks.push(serde_json::json!({ "type": "tool_use", "id": id, "name": name, "input": input }));
                    }
                    out.push(serde_json::json!({ "role": "assistant", "content": blocks }));
                } else {
                    out.push(serde_json::json!({ "role": "assistant", "content": text }));
                }
            }
            _ => {
                flush_tools(&mut pending_tool_results, &mut out);
                let content = m.get("content").cloned().unwrap_or_else(|| serde_json::json!(""));
                out.push(serde_json::json!({ "role": "user", "content": content }));
            }
        }
    }
    flush_tools(&mut pending_tool_results, &mut out);
    let system = if systems.is_empty() { None } else { Some(systems.join("\n\n")) };
    (system, out)
}

/// Converts OpenAI function tools array to Anthropic tools format.
fn convert_tools(tools: serde_json::Value) -> Vec<serde_json::Value> {
    tools.as_array().cloned().unwrap_or_default().into_iter().filter_map(|t| {
        let f = t.get("function")?;
        let name = f.get("name")?.as_str()?;
        let description = f.get("description").and_then(|d| d.as_str()).unwrap_or("");
        let schema = f.get("parameters").cloned()
            .unwrap_or_else(|| serde_json::json!({ "type": "object", "properties": {} }));
        Some(serde_json::json!({ "name": name, "description": description, "input_schema": schema }))
    }).collect()
}

/// Streams a single turn against Anthropic `POST /messages`.
pub(super) fn stream_turn(
    config: Config, model: String, msgs: Vec<serde_json::Value>, tools: Option<serde_json::Value>,
    cancel: Arc<AtomicBool>, tx: mpsc::Sender<Event>,
) {
    let fail = |message: String, settings: bool| { let _ = tx.send(Event::Failed { message, settings }); };
    let (system, messages) = convert_messages(&msgs);
    let mut body = serde_json::json!({
        "model": model, "max_tokens": DEFAULT_MAX_TOKENS, "stream": true, "messages": messages,
    });
    if let Some(sys) = system { body["system"] = serde_json::Value::String(sys); }
    if let Some(t) = tools {
        let converted = convert_tools(t);
        if !converted.is_empty() { body["tools"] = serde_json::Value::Array(converted); }
    }
    let body = body.to_string();
    let mut child = match curl(&config, "/messages", true, "180").args(["--speed-time", "60", "--speed-limit", "1"]).spawn() {
        Ok(c) => c,
        Err(e) => return fail(if is_en() { format!("Failed to launch curl: {e}") } else { format!("无法启动 curl：{e}") }, false),
    };
    if let Some(mut stdin) = child.stdin.take() { let _ = stdin.write_all(body.as_bytes()); }
    let mut calls: Vec<(u64, String, String, String)> = Vec::new();
    let mut other = String::new(); let mut code = 0u16; let mut got = false; let mut completed = false;
    let stdout = child.stdout.take().expect("piped stdout");
    for line in BufReader::new(stdout).lines() {
        if cancel.load(Ordering::Relaxed) { let _ = child.kill(); let _ = child.wait(); return; }
        let Ok(line) = line else { break };
        if line.starts_with("event:") { continue; }
        if let Some(data) = line.strip_prefix("data:") {
            let data = data.trim();
            if data.is_empty() || data == "[DONE]" { continue; }
            let Ok(v) = serde_json::from_str::<serde_json::Value>(data) else { continue };
            match v.get("type").and_then(|t| t.as_str()).unwrap_or("") {
                "message_stop" => completed = true,
                "error" => {
                    let err = v.pointer("/error/message").or_else(|| v.get("message")).and_then(|m| m.as_str())
                        .unwrap_or_else(|| tr("Anthropic 服务返回错误", "Anthropic service error"));
                    let _ = child.kill();
                    return fail(provider::format_error(err), false);
                }
                "content_block_start" => {
                    let idx = v.get("index").and_then(|i| i.as_u64()).unwrap_or(calls.len() as u64);
                    if let Some(cb) = v.get("content_block").filter(|cb| cb.get("type").and_then(|t| t.as_str()) == Some("tool_use")) {
                        let id = cb.get("id").and_then(|s| s.as_str()).unwrap_or("").to_owned();
                        let name = cb.get("name").and_then(|s| s.as_str()).unwrap_or("").to_owned();
                        calls.push((idx, id, name, String::new()));
                        got = true;
                    }
                }
                "content_block_delta" => {
                    let idx = v.get("index").and_then(|i| i.as_u64()).unwrap_or(0);
                    if let Some(delta) = v.get("delta") {
                        match delta.get("type").and_then(|t| t.as_str()).unwrap_or("") {
                            "text_delta" => if let Some(t) = delta.get("text").and_then(|t| t.as_str()).filter(|t| !t.is_empty()) {
                                got = true; let _ = tx.send(Event::Delta(t.to_owned()));
                            },
                            "thinking_delta" => if let Some(t) = delta.get("thinking").and_then(|t| t.as_str()).filter(|t| !t.is_empty()) {
                                got = true; let _ = tx.send(Event::Thinking(t.to_owned()));
                            },
                            "input_json_delta" => if let Some(p) = delta.get("partial_json").and_then(|t| t.as_str()) {
                                if let Some(pos) = calls.iter().position(|c| c.0 == idx).or_else(|| calls.len().checked_sub(1)) {
                                    calls[pos].3.push_str(p); got = true;
                                }
                            },
                            _ => {}
                        }
                    }
                }
                _ => {}
            }
        } else if let Some(c) = line.strip_prefix("__HTTP__") { code = c.trim().parse().unwrap_or(0); }
        else { other.push_str(&line); other.push('\n'); }
    }
    let mut stderr = String::new();
    if let Some(mut e) = child.stderr.take() { let _ = e.read_to_string(&mut stderr); }
    let status = child.wait();
    if cancel.load(Ordering::Relaxed) { return; }
    if status.as_ref().is_ok_and(|s| s.code() == Some(28)) {
        crate::log::warn("AI stream timed out (Anthropic)");
        return fail(tr("AI 响应超时，请重试或切换模型。", "AI response timed out. Retry or switch models.").into(), false);
    }
    if code == 0 {
        return fail(tr("网络请求失败，请检查网络或接口地址", "Network request failed. Check your connection or Base URL").into(), true);
    }
    if !(200..300).contains(&code) {
        return fail(provider::keyed(&config, provider::service_error(other.trim(), code)), code == 401 || code == 403 || code == 404);
    }
    if !status.is_ok_and(|s| s.success()) || !completed {
        crate::log::warn("AI stream interrupted before completion (Anthropic)");
        return fail(tr("AI 响应中断，请重试。", "AI response interrupted. Please retry.").into(), false);
    }
    if !got { return fail(tr("AI 未返回可写入的内容。", "AI returned an empty response.").into(), false); }
    if !calls.is_empty() {
        let finalized = calls.into_iter().enumerate().map(|(i, (_, id, name, args))| {
            (if id.is_empty() { format!("call_{i}") } else { id }, name, if args.trim().is_empty() { "{}".into() } else { args })
        }).collect();
        let _ = tx.send(Event::ToolCalls(finalized));
    }
    let _ = tx.send(Event::Done);
}
