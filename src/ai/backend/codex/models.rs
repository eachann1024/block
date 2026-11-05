use std::{sync::{Arc, Mutex, mpsc}, time::Duration};
use serde_json::{json, Value};
use super::protocol::{Control, Server};

pub(crate) fn list() -> Result<Vec<String>, String> {
    let control = Arc::new(Mutex::new(Control::default()));
    let state = control.clone();
    let (tx, rx) = mpsc::channel();
    std::thread::spawn(move || {
        let result = (|| {
            let mut server = Server::spawn(&[], &state).map_err(|e| e.message)?;
            let mut ids = Vec::new();
            let mut cursor: Option<String> = None;
            for _ in 0..20 {
                let result = server.request(2, "model/list", json!({"limit":100,"cursor":cursor})).map_err(|e| e.message)?;
                let data = result.get("data").and_then(Value::as_array).ok_or("Codex model list is unavailable")?;
                ids.extend(data.iter().filter(|m| !m.get("hidden").and_then(Value::as_bool).unwrap_or(false))
                    .filter_map(|m| m.get("model").or_else(|| m.get("id")).and_then(Value::as_str)).map(str::to_owned));
                cursor = result.get("nextCursor").and_then(Value::as_str).map(str::to_owned);
                if cursor.is_none() { break; }
            }
            Ok(ids)
        })();
        let _ = tx.send(result);
    });
    match rx.recv_timeout(Duration::from_secs(12)) {
        Ok(result) => result,
        Err(_) => { if let Ok(mut control) = control.lock() { let _ = control.cancel(); } Err("Codex model list timed out".into()) }
    }
}
