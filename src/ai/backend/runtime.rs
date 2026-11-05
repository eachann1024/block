use std::{collections::HashMap, sync::mpsc::Sender};

use super::{ApprovalResponse, BackendAdapter, BackendError, BackendEvent, BackendKind, RunningTurn, ToolBridge, TurnRequest};

#[derive(Default)]
pub(crate) struct BackendRuntime {
    bindings: HashMap<String, BackendKind>,
    active: HashMap<String, (String, Box<dyn RunningTurn>)>,
}

impl BackendRuntime {
    pub(crate) fn start(&mut self, adapter: &mut dyn BackendAdapter, request: TurnRequest,
        events: Sender<BackendEvent>, tools: ToolBridge) -> Result<(), BackendError> {
        if self.bindings.get(&request.conversation_id).is_some_and(|kind| *kind != request.backend) {
            return Err(BackendError::new("backend_changed", "Start a new conversation to change backend"));
        }
        if self.active.contains_key(&request.conversation_id) {
            return Err(BackendError::new("turn_active", "A turn is already active in this conversation"));
        }
        let conversation_id = request.conversation_id.clone();
        let turn_id = request.turn_id.clone();
        let backend = request.backend;
        let running = adapter.start(request, events, tools)?;
        self.bindings.insert(conversation_id.clone(), backend);
        self.active.insert(conversation_id, (turn_id, running));
        Ok(())
    }

    pub(crate) fn cancel(&mut self, conversation_id: &str, turn_id: &str) -> Result<(), BackendError> {
        self.active_turn(conversation_id, turn_id)?.cancel()
    }

    pub(crate) fn respond(&mut self, conversation_id: &str, turn_id: &str,
        request_id: &str, response: ApprovalResponse) -> Result<(), BackendError> {
        self.active_turn(conversation_id, turn_id)?.respond(request_id, response)
    }

    pub(crate) fn finish(&mut self, conversation_id: &str, turn_id: &str) -> Result<(), BackendError> {
        self.active_turn(conversation_id, turn_id)?;
        self.active.remove(conversation_id);
        Ok(())
    }

    fn active_turn(&mut self, conversation_id: &str, turn_id: &str) -> Result<&mut (dyn RunningTurn + 'static), BackendError> {
        match self.active.get_mut(conversation_id) {
            Some((active_id, turn)) if active_id == turn_id => Ok(turn.as_mut()),
            _ => Err(BackendError::new("turn_not_active", "Turn is no longer active")),
        }
    }
}

impl Drop for BackendRuntime {
    fn drop(&mut self) {
        for (_, turn) in self.active.values_mut() { let _ = turn.cancel(); }
    }
}
