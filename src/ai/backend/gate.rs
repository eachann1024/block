use std::sync::Mutex;

/// `begin` and `close` have one linearization point. A begun operation may finish.
#[derive(Default)]
pub(crate) struct TurnGate(Mutex<bool>);
impl TurnGate {
    pub(crate) fn begin(&self) -> bool { self.0.lock().map(|closed| !*closed).unwrap_or(false) }
    pub(crate) fn close(&self) { if let Ok(mut closed) = self.0.lock() { *closed = true; } }
}
