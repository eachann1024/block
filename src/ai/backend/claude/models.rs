use std::{io::{BufRead, BufReader, Write}, process::Stdio, sync::mpsc, time::Duration};
use serde_json::{json, Value};

pub(crate) fn list() -> Result<Vec<super::super::ModelOption>, String> {
    let mut child = super::super::cli_env::command("claude")
        .args(["-p", "--verbose", "--dangerously-skip-permissions", "--permission-mode", "bypassPermissions", "--input-format", "stream-json", "--output-format", "stream-json"])
        .env_remove("CLAUDECODE").stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::null()).spawn().map_err(|e| e.to_string())?;
    let mut input = child.stdin.take().ok_or("Claude stdin unavailable")?;
    let output = child.stdout.take().ok_or("Claude stdout unavailable")?;
    writeln!(input, "{}", json!({"type":"control_request","request_id":"block_models","request":{"subtype":"initialize","hooks":null}})).map_err(|e| e.to_string())?;
    let (tx, rx) = mpsc::channel();
    std::thread::spawn(move || {
        for line in BufReader::new(output).lines() {
            let Ok(line) = line else { break };
            let Ok(frame) = serde_json::from_str::<Value>(&line) else { continue };
            if frame.pointer("/response/request_id").and_then(Value::as_str) == Some("block_models") {
                let models = frame.pointer("/response/response/models").and_then(Value::as_array).map(|models| models.iter().filter_map(|model| {
                    let value = model.get("value").and_then(Value::as_str)?;
                    let label = model.get("displayName").and_then(Value::as_str).unwrap_or(value);
                    Some(super::super::ModelOption { value: value.to_owned(), label: label.to_owned() })
                }).collect());
                let _ = tx.send(models.ok_or_else(|| "Claude did not return a model list".to_owned())); return;
            }
        }
        let _ = tx.send(Err("Claude exited before returning models".into()));
    });
    let result = rx.recv_timeout(Duration::from_secs(12)).unwrap_or_else(|_| Err("Claude model list timed out".into()));
    drop(input); let _ = child.kill(); let _ = child.wait(); result
}
