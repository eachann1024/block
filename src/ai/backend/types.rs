use std::{path::PathBuf, sync::mpsc::Sender};

use serde::{Deserialize, Serialize};
use serde_json::Value;

use super::ToolBridge;

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq, Ord, PartialOrd, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub(crate) enum BackendKind { #[default] Builtin, Codex, Claude, Dsh, Pi }

impl BackendKind {
    pub(crate) const ALL: [Self; 5] = [Self::Builtin, Self::Codex, Self::Claude, Self::Dsh, Self::Pi];

    pub(crate) fn key(self) -> &'static str {
        match self {
            Self::Builtin => "builtin",
            Self::Codex => "codex",
            Self::Claude => "claude",
            Self::Dsh => "dsh",
            Self::Pi => "pi",
        }
    }
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub(crate) enum ContextScope { #[default] CurrentNote, Selection }

#[derive(Clone, Debug)]
pub(crate) struct ModelOption { pub value: String, pub label: String }

#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) enum BackendStatus {
    Ready { version: String },
    Installed { version: String },
    Missing,
    NotAuthenticated { message: String },
    Unsupported { message: String },
    Error { message: String },
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub(crate) struct NoteContext {
    pub notebook_root: Option<PathBuf>,
    pub page_id: Option<String>,
    pub markdown: Option<String>,
    pub selection: Option<String>,
    pub revision: Option<String>,
}

#[derive(Clone, Debug)]
pub(crate) struct TurnRequest {
    pub backend: BackendKind,
    pub conversation_id: String,
    pub turn_id: String,
    pub native_session_id: Option<String>,
    pub message: String,
    pub context: NoteContext,
    pub config: Value,
}

#[derive(Clone, Debug)]
pub(crate) struct BackendError {
    pub code: String,
    pub message: String,
}

impl BackendError {
    pub(crate) fn new(code: impl Into<String>, message: impl Into<String>) -> Self {
        Self { code: code.into(), message: message.into() }
    }
}

#[derive(Clone, Debug)]
pub(crate) enum ApprovalResponse { AllowOnce, AllowSession, Deny, Cancel }

#[derive(Clone, Debug)]
pub(crate) enum BackendEventKind {
    Delta(String),
    Thinking(String),
    PublicSummary(String),
    ToolActivity { name: String, detail: String },
    Approval { request_id: String, title: String, detail: String },
    Session { native_session_id: String },
    Completed,
    Cancelled,
    Error(BackendError),
}

#[derive(Clone, Debug)]
pub(crate) struct BackendEvent {
    pub conversation_id: String,
    pub turn_id: String,
    pub kind: BackendEventKind,
}

pub(crate) trait RunningTurn: Send {
    fn cancel(&mut self) -> Result<(), BackendError>;
    fn respond(&mut self, request_id: &str, response: ApprovalResponse) -> Result<(), BackendError>;
}

pub(crate) trait BackendAdapter: Send {
    fn probe(&self) -> BackendStatus;
    fn start(&mut self, request: TurnRequest, events: Sender<BackendEvent>, tools: ToolBridge)
        -> Result<Box<dyn RunningTurn>, BackendError>;
}
