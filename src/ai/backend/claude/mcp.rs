use std::{sync::{Arc, Mutex, mpsc::Sender}, thread, time::Duration};

use serde_json::{Value, json};
use super::super::{BackendEvent, BackendEventKind, ToolBridge, TurnRequest};
use super::process::{Input, write_frame};

pub(super) fn handle(frame: &Value, request: &TurnRequest, tools: &ToolBridge,
    input: &Arc<Mutex<Input>>, events: &Sender<BackendEvent>) {
    let id = frame["request_id"].as_str().unwrap_or("").to_owned();
    let data = &frame["request"];
    if data["server_name"] != "block" {
        let _ = write_frame(input, &json!({"type":"control_response","response":{"subtype":"error","request_id":id,"error":"Unknown MCP server"}}));
        return;
    }
    let message = data["message"].clone();
    let rpc_id = message.get("id").cloned();
    let method = message["method"].as_str().unwrap_or("");
    let result = match method {
        "initialize" => json!({"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"block","version":"1.0"}}),
        "tools/list" => json!({"tools":catalog()}),
        "tools/call" => {
            let name = message["params"]["name"].as_str().unwrap_or("").to_owned();
            let arguments = message["params"]["arguments"].clone();
            if !crate::editor::ai::agent::is_authorized_tool(&name) {
                reply(input, &id, json!({"jsonrpc":"2.0","id":rpc_id,"error":{"code":-32601,"message":"Unknown tool"}}));
                return;
            }
            let _ = events.send(BackendEvent { conversation_id: request.conversation_id.clone(), turn_id: request.turn_id.clone(),
                kind: BackendEventKind::ToolActivity { name: name.clone(), detail: "Block".into() } });
            let tools = tools.clone();
            let input = input.clone();
            let conversation_id = request.conversation_id.clone();
            let turn_id = request.turn_id.clone();
            thread::spawn(move || {
                let result = tools.call(conversation_id, turn_id, id.clone(), name, arguments, Duration::from_secs(600));
                let content = match result {
                    Ok(value) => json!({"content":[{"type":"text","text":value.to_string()}]}),
                    Err(error) => json!({"content":[{"type":"text","text":error.message}],"isError":true}),
                };
                reply(&input, &id, json!({"jsonrpc":"2.0","id":rpc_id,"result":content}));
            });
            return;
        }
        _ if rpc_id.is_none() => { reply(input, &id, json!({"jsonrpc":"2.0","result":{}})); return; }
        _ => { reply(input, &id, json!({"jsonrpc":"2.0","id":rpc_id,"error":{"code":-32601,"message":"Method not found"}})); return; }
    };
    reply(input, &id, json!({"jsonrpc":"2.0","id":rpc_id,"result":result}));
}

fn reply(input: &Arc<Mutex<Input>>, id: &str, response: Value) {
    let _ = write_frame(input, &json!({"type":"control_response","response":{"subtype":"success","request_id":id,"response":{"mcp_response":response}}}));
}

fn catalog() -> Vec<Value> {
    let tools = crate::editor::ai::agent::tools();
    tools.as_array().into_iter().flatten().filter_map(|tool| {
        let function = tool.get("function")?;
        Some(json!({"name":function["name"],"description":function["description"],"inputSchema":function["parameters"]}))
    }).collect()
}
