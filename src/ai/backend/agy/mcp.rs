use std::{fs, path::PathBuf, sync::{Arc, atomic::{AtomicBool, Ordering}}};
use serde_json::{Value, json};
use super::super::{BackendError, ToolBridge, TurnRequest, cli_env, mcp_bridge};

pub(super) struct Server {
    directory: PathBuf,
    active: Option<Arc<AtomicBool>>,
}

impl Server {
    pub fn start(request: &TurnRequest, tools: ToolBridge) -> Result<(Self, Value), BackendError> {
        let node = cli_env::resolve("node").ok_or_else(|| BackendError::new(
            "agy_mcp", "Node.js is required for agy to use Block note tools"))?;
        let token = mcp_bridge::secret()?;
        let directory = std::env::temp_dir().join(format!("block-agy-{}", &token[..24]));
        fs::create_dir(&directory).map_err(super::transport::error)?;
        let mut server = Self { directory, active: None };
        #[cfg(unix)] {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(&server.directory, fs::Permissions::from_mode(0o700)).map_err(super::transport::error)?;
        }
        let script = server.directory.join("mcp.cjs");
        fs::write(&script, include_str!("../mcp_bridge/server.js")).map_err(super::transport::error)?;
        let specs: Vec<Value> = super::super::super::agent::tools().as_array().into_iter().flatten()
            .filter_map(|entry| entry.get("function")).map(|tool| json!({
                "name":tool["name"], "description":tool["description"], "inputSchema":tool["parameters"]
            })).collect();
        let (port, active, _) = mcp_bridge::listen(request, tools, token.clone())?;
        server.active = Some(active);
        let config = json!([{"name":"block", "command":node, "args":[script], "env":[
            {"name":"BLOCK_MCP_PORT", "value":port.to_string()},
            {"name":"BLOCK_MCP_TOKEN", "value":token},
            {"name":"BLOCK_MCP_TOOLS", "value":serde_json::to_string(&specs).map_err(super::transport::error)?},
        ]}]);
        Ok((server, config))
    }
}

impl Drop for Server {
    fn drop(&mut self) {
        if let Some(active) = &self.active { active.store(false, Ordering::Release); }
        let _ = fs::remove_dir_all(&self.directory);
    }
}
