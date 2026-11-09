use std::{io::{BufRead, BufReader, Read, Write}, net::{TcpListener, TcpStream}, sync::{Arc, atomic::{AtomicBool, Ordering}}, thread, time::Duration};
use serde_json::{json, Value};
use super::{BackendError, ToolBridge, TurnRequest};

pub(crate) fn secret() -> Result<String, BackendError> {
    let mut bytes = [0u8; 32];
    std::fs::File::open("/dev/urandom").and_then(|mut file| file.read_exact(&mut bytes))
        .map_err(|error| BackendError::new("mcp_random", error.to_string()))?;
    Ok(bytes.iter().map(|byte| format!("{byte:02x}")).collect())
}

pub(crate) fn listen(request: &TurnRequest, tools: ToolBridge, token: String)
    -> Result<(u16, Arc<AtomicBool>, Arc<AtomicBool>), BackendError> {
    let listener = TcpListener::bind("127.0.0.1:0").map_err(|error| BackendError::new("mcp_bridge", error.to_string()))?;
    let port = listener.local_addr().map_err(|error| BackendError::new("mcp_bridge", error.to_string()))?.port();
    listener.set_nonblocking(true).map_err(|error| BackendError::new("mcp_bridge", error.to_string()))?;
    let running = Arc::new(AtomicBool::new(true));
    let active = running.clone();
    let ready = Arc::new(AtomicBool::new(false));
    let bridge_ready = ready.clone();
    let request = request.clone();
    thread::spawn(move || while active.load(Ordering::Acquire) {
        match listener.accept() {
            Ok((stream, _)) => {
                let tools = tools.clone();
                let token = token.clone();
                let request = request.clone();
                let active = active.clone();
                let ready = bridge_ready.clone();
                thread::spawn(move || serve(stream, &request, tools, &token, active, ready));
            }
            Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => thread::sleep(Duration::from_millis(20)),
            Err(_) => break,
        }
    });
    Ok((port, running, ready))
}

fn serve(mut stream: TcpStream, request: &TurnRequest, tools: ToolBridge, token: &str, active: Arc<AtomicBool>, ready: Arc<AtomicBool>) {
    if stream.set_nonblocking(false).is_err() { return; }
    let _ = stream.set_read_timeout(Some(Duration::from_secs(5)));
    let Ok(copy) = stream.try_clone() else { return };
    let mut line = String::new();
    if BufReader::new(copy).take(1024 * 1024).read_line(&mut line).is_err() { return; }
    let Ok(value) = serde_json::from_str::<Value>(&line) else { return };
    if value["token"].as_str() != Some(token) || !active.load(Ordering::Acquire) { return; }
    if value["ready"] == true { ready.store(true, Ordering::Release); return; }
    let Some(name) = value["name"].as_str() else { return };
    if !super::super::agent::is_authorized_tool(name) { return; }
    let response = tools.call(request.conversation_id.clone(), request.turn_id.clone(),
        value["id"].as_str().unwrap_or_default().to_owned(), name.to_owned(),
        value["arguments"].clone(), Duration::from_secs(300));
    let result = match response {
        Ok(value) => json!({"ok": true, "value": value}),
        Err(error) => json!({"ok": false, "value": {"code": error.code, "message": error.message}}),
    };
    if active.load(Ordering::Acquire) { let _ = writeln!(stream, "{result}"); }
}
