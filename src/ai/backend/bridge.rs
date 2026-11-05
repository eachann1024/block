use std::{collections::HashMap, sync::{Arc, Mutex, atomic::{AtomicBool, AtomicU64, Ordering}, mpsc::{self, Receiver, Sender}}, time::Duration};
use serde_json::Value;
use super::{BackendError, gate::TurnGate};

type Pending = Arc<Mutex<HashMap<u64, Sender<Result<Value, BackendError>>>>>;

pub(crate) struct ToolRequest {
    pub conversation_id: String,
    pub turn_id: String,
    pub request_id: String,
    pub name: String,
    pub arguments: Value,
    pub responder: ToolResponder,
    cancelled: Arc<AtomicBool>,
    turn_cancelled: Arc<AtomicBool>,
    gate: Arc<TurnGate>,
}
impl ToolRequest {
    pub(crate) fn is_cancelled(&self) -> bool {
        self.cancelled.load(Ordering::Acquire) || self.turn_cancelled.load(Ordering::Acquire)
    }
    pub(crate) fn begin_side_effect(&self) -> bool { !self.is_cancelled() && self.gate.begin() }
}

pub(crate) struct ToolResponder { id: u64, pending: Pending }
impl ToolResponder {
    pub(crate) fn respond(self, result: Result<Value, BackendError>) -> Result<(), BackendError> {
        let sender = self.pending.lock().map_err(|_| BackendError::new("tool_disconnected", "Tool bridge lock failed"))?.remove(&self.id);
        sender.ok_or_else(|| BackendError::new("cancelled", "Tool request is no longer pending"))?
            .send(result).map_err(|_| BackendError::new("tool_disconnected", "Tool caller disconnected"))
    }
}
impl Drop for ToolResponder {
    fn drop(&mut self) { if let Ok(mut pending) = self.pending.lock() { pending.remove(&self.id); } }
}

#[derive(Clone)]
pub(crate) struct ToolBridge {
    sender: Sender<ToolRequest>, cancelled: Arc<AtomicBool>, pending: Pending, next_id: Arc<AtomicU64>, gate: Arc<TurnGate>,
}
impl ToolBridge {
    pub(crate) fn channel() -> (Self, Receiver<ToolRequest>) {
        let (sender, receiver) = mpsc::channel();
        (Self { sender, cancelled: Arc::new(AtomicBool::new(false)), pending: Arc::new(Mutex::new(HashMap::new())), next_id: Arc::new(AtomicU64::new(0)), gate: Arc::new(TurnGate::default()) }, receiver)
    }
    pub(crate) fn cancel(&self) {
        self.gate.close();
        self.cancelled.store(true, Ordering::Release);
        if let Ok(mut pending) = self.pending.lock() {
            for (_, sender) in pending.drain() {
                let _ = sender.send(Err(BackendError::new("cancelled", "Tool request cancelled")));
            }
        }
    }
    pub(crate) fn call(&self, conversation_id: String, turn_id: String, request_id: String,
        name: String, arguments: Value, timeout: Duration) -> Result<Value, BackendError> {
        let (sender, receiver) = mpsc::channel();
        let cancelled = Arc::new(AtomicBool::new(false));
        let id = self.next_id.fetch_add(1, Ordering::Relaxed);
        {
            let mut pending = self.pending.lock().map_err(|_| BackendError::new("tool_disconnected", "Tool bridge lock failed"))?;
            if self.cancelled.load(Ordering::Acquire) { return Err(BackendError::new("cancelled", "Tool request cancelled")); }
            pending.insert(id, sender);
        }
        self.sender.send(ToolRequest { conversation_id, turn_id, request_id, name, arguments,
            responder: ToolResponder { id, pending: self.pending.clone() }, cancelled: cancelled.clone(), turn_cancelled: self.cancelled.clone(), gate: self.gate.clone() })
            .map_err(|_| BackendError::new("tool_disconnected", "Block tool bridge is unavailable"))?;
        match receiver.recv_timeout(timeout) {
            Ok(result) => result,
            Err(error) => {
                cancelled.store(true, Ordering::Release);
                if let Ok(mut pending) = self.pending.lock() { pending.remove(&id); }
                Err(BackendError::new("tool_wait_failed", error.to_string()))
            }
        }
    }
}
