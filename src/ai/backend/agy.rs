mod mcp;
mod protocol;
mod transport;

use std::{io::Read, process::Stdio, sync::{Arc, Mutex, atomic::{AtomicBool, Ordering}, mpsc::Sender}, thread, time::{Duration, Instant}};
use serde_json::{Value, json};
use super::{ApprovalResponse, BackendAdapter, BackendError, BackendEvent, BackendStatus, ModelOption,
    RunningTurn, ToolBridge, TurnRequest, cli_env};
use transport::Connection;

pub(crate) struct AgyAdapter;

impl BackendAdapter for AgyAdapter {
    fn probe(&self) -> BackendStatus {
        let result = (|| {
            let connection = Connection::spawn(false)?;
            transport::initialize(&connection.input)?;
            let frame = connection.receive(Duration::from_secs(12))?;
            let result = protocol::response(&frame)?;
            Ok::<_, BackendError>(result.pointer("/agentInfo/version").and_then(Value::as_str).unwrap_or("").to_owned())
        })();
        match result {
            Ok(version) => BackendStatus::Installed { version },
            Err(error) if error.code == "missing" => BackendStatus::Missing,
            Err(error) => BackendStatus::Error { message: error.message },
        }
    }

    fn start(&mut self, request: TurnRequest, events: Sender<BackendEvent>, tools: ToolBridge)
        -> Result<Box<dyn RunningTurn>, BackendError> {
        let text_only = request.config["textOnly"].as_bool() == Some(true);
        let full = !text_only && request.config["fullPermissions"].as_bool() == Some(true);
        let (server, servers) = if text_only { (None, json!([])) } else {
            let (server, config) = mcp::Server::start(&request, tools.clone())?;
            (Some(server), config)
        };
        let connection = Connection::spawn(full)?;
        let pending = Arc::new(Mutex::new(protocol::Pending::new()));
        let cancelled = Arc::new(AtomicBool::new(false));
        let finished = Arc::new(AtomicBool::new(false));
        let turn = Turn { child: connection.child.clone(), input: connection.input.clone(), pending: pending.clone(),
            cancelled: cancelled.clone(), finished: finished.clone(), tools: tools.clone() };
        thread::spawn(move || {
            let outcome = protocol::run(&connection, &request, &events, &pending, &cancelled, servers);
            tools.cancel();
            if let Ok(mut pending) = pending.lock() { pending.clear(); }
            drop(connection);
            drop(server);
            finished.store(true, Ordering::Release);
            let kind = if cancelled.load(Ordering::Acquire) { super::BackendEventKind::Cancelled }
                else { outcome.unwrap_or_else(super::BackendEventKind::Error) };
            protocol::emit(&events, &request, kind);
        });
        Ok(Box::new(turn))
    }
}

struct Turn {
    child: transport::SharedChild,
    input: transport::Input,
    pending: Arc<Mutex<protocol::Pending>>,
    cancelled: Arc<AtomicBool>,
    finished: Arc<AtomicBool>,
    tools: ToolBridge,
}

impl RunningTurn for Turn {
    fn cancel(&mut self) -> Result<(), BackendError> {
        if !self.finished.load(Ordering::Acquire) {
            self.cancelled.store(true, Ordering::Release);
            self.tools.cancel();
            transport::stop(&self.child);
        }
        Ok(())
    }

    fn respond(&mut self, request_id: &str, response: ApprovalResponse) -> Result<(), BackendError> {
        let permission = self.pending.lock().map_err(|_| transport::error("agy approval lock failed"))?
            .remove(request_id).ok_or_else(|| BackendError::new("approval_expired", "agy approval is no longer pending"))?;
        transport::send(&self.input, permission.reply(response))
    }
}

impl Drop for Turn {
    fn drop(&mut self) { let _ = self.cancel(); }
}

pub(crate) fn list_models() -> Result<Vec<ModelOption>, String> {
    let mut child = cli_env::command("agy").arg("models").stdin(Stdio::null())
        .stdout(Stdio::piped()).stderr(Stdio::null()).spawn().map_err(|err| err.to_string())?;
    let output = child.stdout.take().ok_or("agy stdout unavailable")?;
    let reader = thread::spawn(move || {
        let mut text = String::new();
        std::io::BufReader::new(output).read_to_string(&mut text).map(|_| text)
    });
    let deadline = Instant::now() + Duration::from_secs(20);
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(25)),
            result => {
                let _ = child.kill(); let _ = child.wait();
                return Err(match result { Err(err) => err.to_string(), _ => "agy model list timed out".into() });
            }
        }
    };
    if !status.success() { return Err(format!("agy models exited with {status}; check agy login")); }
    let text = reader.join().map_err(|_| "agy model reader failed")?.map_err(|err| err.to_string())?;
    let models: Vec<_> = text.lines().filter_map(|line| {
        let (value, label) = line.split_once('\t')?;
        let value = value.trim();
        (!value.is_empty()).then(|| ModelOption { value: value.into(), label: label.trim().into() })
    }).collect();
    if models.is_empty() { return Err("agy returned no models; check agy login".into()); }
    Ok(models)
}
