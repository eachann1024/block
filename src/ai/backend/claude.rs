#[path = "claude/mcp.rs"] mod mcp;
#[path = "claude/process.rs"] mod process;
#[path = "claude/models.rs"] pub(crate) mod models;

use std::sync::mpsc::Sender;

use super::{BackendAdapter, BackendError, BackendEvent, BackendStatus, RunningTurn, ToolBridge, TurnRequest, cli_env};

#[derive(Default)]
pub(crate) struct ClaudeAdapter;

impl BackendAdapter for ClaudeAdapter {
    fn probe(&self) -> BackendStatus {
        let output = match cli_env::command("claude").arg("--version").output() {
            Ok(output) => output,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return BackendStatus::Missing,
            Err(error) => return BackendStatus::Error { message: error.to_string() },
        };
        if !output.status.success() {
            return BackendStatus::Error { message: "Claude CLI version check failed".into() };
        }
        let version = String::from_utf8_lossy(&output.stdout).trim().to_owned();
        let numbers: Vec<u32> = version.split_whitespace().next().unwrap_or("").split('.')
            .filter_map(|part| part.parse().ok()).collect();
        if numbers.len() < 3 || (numbers[0], numbers[1], numbers[2]) < (2, 1, 318) {
            return BackendStatus::Unsupported { message: format!("Claude CLI 2.1.318 or newer required; found {version}") };
        }
        let auth = cli_env::command("claude").args(["auth", "status", "--json"]).output();
        match auth {
            Ok(output) => match serde_json::from_slice::<serde_json::Value>(&output.stdout) {
                Ok(value) if value.get("loggedIn").and_then(|v| v.as_bool()) == Some(true) => BackendStatus::Ready { version },
                Ok(value) if value.get("loggedIn").and_then(|v| v.as_bool()) == Some(false)
                    && ["ANTHROPIC_API_KEY", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"]
                        .iter().all(|name| std::env::var_os(name).is_none()) => {
                    BackendStatus::NotAuthenticated { message: "Claude CLI reports no authentication; API environment credentials may still be available".into() }
                }
                _ => BackendStatus::Ready { version },
            },
            Err(_) => BackendStatus::Ready { version },
        }
    }

    fn start(&mut self, request: TurnRequest, events: Sender<BackendEvent>, tools: ToolBridge)
        -> Result<Box<dyn RunningTurn>, BackendError> {
        process::start(request, events, tools)
    }
}
