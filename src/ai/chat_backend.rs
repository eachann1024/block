use std::{sync::{atomic::AtomicBool, mpsc, Arc}, time::Duration};

use gpui::{App, Context, Window};
use serde_json::{json, Value};

use super::{agent, AiChat, Msg};
use super::super::backend::{self, ApprovalResponse, BackendError, BackendEvent, BackendEventKind, BackendKind, BackendStatus, NoteContext, ToolBridge, ToolRequest, TurnRequest};

impl AiChat {
    pub(in crate::editor::ai) fn context_scope(&self) -> backend::ContextScope {
        if self.selection_scope { backend::ContextScope::Selection } else { backend::ContextScope::CurrentNote }
    }
    pub(in crate::editor::ai) fn selection_available(&self, cx: &App) -> bool { self.note_context(cx).selection.is_some() }
    pub(in crate::editor::ai) fn set_context_scope(&mut self, scope: backend::ContextScope, cx: &mut Context<Self>) {
        self.selection_scope = scope == backend::ContextScope::Selection && self.selection_available(cx);
        cx.notify();
    }
    pub(in crate::editor::ai) fn dsh_options(&self) -> backend::DshOptions { self.backend_config.dsh_options() }

    pub(in crate::editor::ai) fn set_dsh_options(&mut self, options: backend::DshOptions, cx: &mut Context<Self>) -> Result<(), String> {
        self.backend_config.set_dsh_options(options)?; cx.notify(); Ok(())
    }
    pub(in crate::editor::ai) fn recognized_backends(&self) -> Vec<BackendKind> {
        BackendKind::ALL.into_iter().filter(|&kind| backend::is_recognized(kind, self.backend_status(kind))).collect()
    }
    pub(in crate::editor::ai) fn visible_backends(&self) -> Vec<BackendKind> {
        let recognized = self.recognized_backends();
        let enabled: Vec<BackendKind> = recognized.iter().copied().filter(|&k| self.backend_config.is_enabled(k)).collect();
        if enabled.is_empty() { recognized.into_iter().take(1).collect() } else { enabled }
    }
    pub(in crate::editor::ai) fn selected_backend(&self) -> BackendKind {
        let visible = self.visible_backends();
        let candidate = self.conv().map_or(self.backend_config.selected, |conversation| conversation.backend);
        if visible.contains(&candidate) { candidate } else { visible.first().copied().unwrap_or(BackendKind::Builtin) }
    }

    pub(in crate::editor::ai) fn select_backend(&mut self, kind: BackendKind, window: &mut Window, cx: &mut Context<Self>) {
        if self.selected_backend() == kind { return; }
        if self.current.is_some() {
            let draft = self.input.read(cx).value().to_owned();
            let refs = self.refs.clone();
            let skills = self.skills.clone();
            self.new_conv(window, cx);
            self.refs = refs; self.skills = skills;
            self.input.update(cx, |input, cx| input.set_value(draft, window, cx));
        } else {
            self.error = None; self.error_settings = false; self.retry_draft = None;
        }
        self.backend_config.selected = kind;
        if let Err(error) = self.backend_config.save() { self.error = Some(error); }
        cx.notify();
    }

    pub(in crate::editor::ai) fn backend_status(&self, kind: BackendKind) -> Option<&BackendStatus> {
        self.backend_statuses.iter().find(|(backend, _)| *backend == kind).map(|(_, status)| status)
    }

    pub(in crate::editor::ai) fn probe_backend(&mut self, kind: BackendKind, cx: &mut Context<Self>) {
        if let Some(status) = super::input_smoke::status_override(kind) {
            self.backend_statuses.retain(|(backend, _)| *backend != kind);
            self.backend_statuses.push((kind, status));
            let recognized = self.recognized_backends();
            self.backend_config.ensure_valid_selection(&recognized); cx.notify(); return;
        }
        let (sender, receiver) = mpsc::channel();
        std::thread::spawn(move || {
            let status = match backend::adapter(kind) {
                Ok(adapter) => adapter.probe(),
                Err(_) if kind == BackendKind::Builtin => BackendStatus::Ready { version: "Block".into() },
                Err(error) => BackendStatus::Error { message: error.message },
            };
            let _ = sender.send(status);
        });
        cx.spawn(async move |this, cx| loop {
            cx.background_executor().timer(Duration::from_millis(60)).await;
            match receiver.try_recv() {
                Ok(status) => {
                    let _ = this.update(cx, |this, cx| {
                        let status = super::input_smoke::status_override(kind).unwrap_or(status);
                        if this.backend_statuses.iter().any(|(backend, old)| *backend == kind && *old == status) { return; }
                        this.backend_statuses.retain(|(backend, _)| *backend != kind);
                        this.backend_statuses.push((kind, status));
                        let recognized = this.recognized_backends();
                        this.backend_config.ensure_valid_selection(&recognized); cx.notify();
                    });
                    break;
                }
                Err(mpsc::TryRecvError::Empty) => {},
                Err(mpsc::TryRecvError::Disconnected) => break,
            }
        }).detach();
    }

    pub(in crate::editor::ai) fn respond_approval(&mut self, response: ApprovalResponse, cx: &mut Context<Self>) {
        let Some((request_id, _, _)) = self.pending_approval.take() else { return };
        if let Some((conversation, turn)) = &self.active_external {
            if let Err(error) = self.backend_runtime.respond(conversation, turn, &request_id, response) { self.error = Some(error.message); }
        }
        cx.notify();
    }

    pub(super) fn note_context(&self, cx: &App) -> NoteContext {
        use std::hash::{Hash, Hasher};
        let ctx = self.agent_ctx(cx);
        let selection = self.editor.as_ref().and_then(|editor| editor.upgrade()).and_then(|editor| {
            let editor = editor.read(cx);
            let text = editor.selection_ranges().iter().filter_map(|(id, range)| {
                editor.doc.get(*id).and_then(|block| block.text.get(range.clone())).map(str::to_owned)
            }).collect::<Vec<_>>().join("\n");
            (!text.is_empty()).then_some(text)
        });
        let markdown = ctx.current.as_ref().map(|_| ctx.current_md.clone());
        let revision = markdown.as_ref().map(|text| {
            let mut hash = std::collections::hash_map::DefaultHasher::new(); text.hash(&mut hash); format!("{:016x}", hash.finish())
        });
        let markdown = if self.selection_scope { selection.clone() } else { markdown };
        NoteContext { notebook_root: (!ctx.root.as_os_str().is_empty()).then_some(ctx.root), page_id: ctx.current, markdown, selection, revision }
    }

    pub(in crate::editor::ai) fn run_external(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let kind = self.selected_backend();
        let Some(conversation) = self.conv() else { return };
        let conversation_id = conversation.id.to_string();
        let native_session_id = conversation.native_session_id.clone();
        let mut message = self.sent_draft.as_ref().and_then(|draft| conversation.messages.get(draft.message_index))
            .map(|message| format!("{}{}", message.content, message.context.as_deref().unwrap_or(""))).unwrap_or_default();
        self.serial += 1;
        let serial = self.serial;
        let turn_id = format!("{}-{serial}", conversation_id);
        let expected_context = self.note_context(cx);
        if self.selection_scope && expected_context.selection.is_none() {
            self.error = Some("Selected text is no longer available".into());
            self.restore_failed_draft(window, cx); cx.notify(); return;
        }
        let catalog = super::chat_skills::catalog(expected_context.notebook_root.as_deref());
        let skill_labels: Vec<_> = catalog.iter().map(|(name, grant)| json!({"name":name,"skillId":grant.stable_id})).collect();
        if !skill_labels.is_empty() { message.push_str(&format!("\n\nAvailable read-only skill resources (readSkillResource uses skillId and relative path; scripts cannot be executed): {skill_labels:?}")); }
        let grants: Vec<_> = catalog.into_iter().map(|(_, grant)| grant).collect();
        let request = TurnRequest { backend: kind, conversation_id: conversation_id.clone(), turn_id: turn_id.clone(), native_session_id,
            message, context: expected_context.clone(), config: self.backend_config.options(kind) };
        let mut adapter = match backend::adapter(kind) { Ok(adapter) => adapter, Err(error) => { self.error = Some(error.message); self.restore_failed_draft(window, cx); cx.notify(); return; } };
        let (events, receiver) = mpsc::channel();
        let (bridge, tool_receiver) = ToolBridge::channel();
        if let Err(error) = self.backend_runtime.start(adapter.as_mut(), request, events, bridge.clone()) {
            self.error = Some(error.message); self.restore_failed_draft(window, cx); cx.notify(); return;
        }
        self.active_external = Some((conversation_id.clone(), turn_id.clone()));
        self.external_tools = Some(bridge);
        self.streaming = Some((serial, Arc::new(AtomicBool::new(false))));
        self.partial.clear();
        cx.spawn_in(window, async move |this, cx| loop {
            cx.background_executor().timer(Duration::from_millis(40)).await;
            let mut finished = false;
            if this.update_in(cx, |this, window, cx| {
                if this.serial != serial || this.active_external.as_ref() != Some(&(conversation_id.clone(), turn_id.clone())) { finished = true; return; }
                while !finished {
                    let request = match backend::priority::next_tool(
                        || this.drain_external_events(&receiver, &conversation_id, &turn_id, cx),
                        || tool_receiver.try_recv().ok()) {
                        Err(()) => { finished = true; break; },
                        Ok(None) => break,
                        Ok(Some(request)) => request,
                    };
                    finished = this.handle_tool_request(request, &conversation_id, &turn_id, &expected_context, &grants, &receiver, cx);
                }
                if finished {
                    let _ = this.backend_runtime.finish(&conversation_id, &turn_id);
                    if let Some(tools) = this.external_tools.take() { tools.cancel(); }
                    this.active_external = None; this.pending_approval = None; this.streaming = None;
                    let text = std::mem::take(&mut this.partial);
                    if !text.trim().is_empty() { this.push("assistant", text); }
                    if this.error.is_some() { this.restore_failed_draft(window, cx); } else { this.sent_draft = None; }
                }
                this.scroll.scroll_to_bottom(); cx.notify();
            }).is_err() { break; }
            if finished { break; }
        }).detach();
    }

    fn drain_external_events(&mut self, receiver: &mpsc::Receiver<BackendEvent>, conversation: &str, turn: &str, cx: &mut Context<Self>) -> bool {
        loop {
            match receiver.try_recv() {
                Ok(event) if event.conversation_id == conversation && event.turn_id == turn => {
                    if self.handle_external_event(event, cx) { return true; }
                }
                Ok(_) => {},
                Err(mpsc::TryRecvError::Empty) => return false,
                Err(mpsc::TryRecvError::Disconnected) => { self.error = Some("AI backend disconnected".into()); return true; }
            }
        }
    }

    fn handle_external_event(&mut self, event: BackendEvent, _cx: &mut Context<Self>) -> bool {
        match event.kind {
            BackendEventKind::Delta(text) => self.partial.push_str(&text),
            BackendEventKind::Thinking(_) | BackendEventKind::ToolActivity { .. } => {},
            BackendEventKind::Approval { request_id, title, detail } => {
                if self.pending_approval.is_some() {
                    if let Some((conversation, turn)) = &self.active_external {
                        if let Err(error) = self.backend_runtime.respond(conversation, turn, &request_id, ApprovalResponse::Deny) {
                            self.error = Some(error.message);
                        } else { self.error = Some("Concurrent approval rejected; resolve the pending request first".into()); }
                    }
                } else { self.pending_approval = Some((request_id, title, detail)); }
            }
            BackendEventKind::Session { native_session_id } => {
                if let Some(conversation) = self.current.and_then(|id| self.convs.iter_mut().find(|conversation| conversation.id == id)) {
                    conversation.native_session_id = Some(native_session_id); self.persist();
                }
            }
            BackendEventKind::Completed => {
                let kind = self.selected_backend();
                if let Some((_, status)) = self.backend_statuses.iter_mut().find(|(backend, _)| *backend == kind) {
                    if let BackendStatus::Installed { version } = status { *status = BackendStatus::Ready { version: version.clone() }; }
                }
                return true;
            }
            BackendEventKind::Cancelled => return true,
            BackendEventKind::Error(error) => {
                if error.code == "authentication" {
                    let kind = self.selected_backend();
                    self.backend_statuses.retain(|(backend, _)| *backend != kind);
                    self.backend_statuses.push((kind, BackendStatus::NotAuthenticated { message: error.message.clone() }));
                }
                self.error = Some(error.message); return true;
            }
        }
        false
    }

    fn handle_tool_request(&mut self, request: ToolRequest, conversation: &str, turn: &str, expected: &NoteContext,
        grants: &[backend::skill_resources::SkillResourceGrant], events: &mpsc::Receiver<BackendEvent>, cx: &mut Context<Self>) -> bool {
        if request.is_cancelled() || self.active_external.as_ref().is_none_or(|(c, t)| c != conversation || t != turn) { return false; }
        if request.conversation_id != conversation || request.turn_id != turn {
            let _ = request.responder.respond(Err(BackendError::new("stale_turn", "Tool request belongs to another turn"))); return false;
        }
        if !matches!(request.name.as_str(), "loadSkill" | "readSkillResource" | "listNotebooks" | "listPages" | "searchNotes" | "readPage" | "executeBatchPlan") {
            let _ = request.responder.respond(Err(BackendError::new("unsupported_tool", "Unknown Block tool"))); return false;
        }
        let current = self.note_context(cx);
        if current.notebook_root != expected.notebook_root || current.page_id != expected.page_id {
            let _ = request.responder.respond(Err(BackendError::new("context_changed", "The active note changed; start a new turn"))); return false;
        }
        if request.name == "readSkillResource" {
            let id = request.arguments.get("skillId").and_then(Value::as_str).unwrap_or("");
            let path = request.arguments.get("path").and_then(Value::as_str).unwrap_or("");
            let result = backend::skill_resources::read(grants, id, path)
                .map(|text| json!({"text":text})).map_err(|error| BackendError::new("skill_resource", error.to_string()));
            let display = result.as_ref().cloned().unwrap_or_else(|error| json!({"error":error.message}));
            super::super::chat_process::record_tool(self, &request, &display, true);
            let _ = request.responder.respond(result); return false;
        }
        if request.name == "listNotebooks" {
            let roots = super::chat_roots::registered_roots();
            let result = json!({"notebooks": roots});
            super::super::chat_process::record_tool(self, &request, &result, true);
            let _ = request.responder.respond(Ok(result));
            return false;
        }
        let ctx = if let Some(root) = request.arguments.get("notebookRoot").and_then(Value::as_str) {
            match self.context_for_root(std::path::Path::new(root), cx) {
                Ok(ctx) => ctx,
                Err(error) => { let _ = request.responder.respond(Err(BackendError::new("notebook_root", error))); return false; }
            }
        } else { self.agent_ctx(cx) };
        let args = request.arguments.to_string();
        let (mut result, plan) = agent::exec(&request.name, &args, &ctx);
        if self.drain_external_events(events, conversation, turn, cx) { return true; }
        if request.is_cancelled() || self.active_external.as_ref().is_none_or(|(c, t)| c != conversation || t != turn) { return false; }
        if request.name == "readPage" && result.get("error").is_none() {
            let live = self.note_context(cx);
            if result.get("pageId").and_then(Value::as_str) == live.page_id.as_deref() {
                result["selection"] = json!(live.selection); result["revision"] = json!(live.revision);
            }
        }
        if let Some(plan) = plan {
            if !request.begin_side_effect() { return false; }
            super::super::chat_process::record_tool(self, &request, &result, false);
            let auto = plan.ops.iter().all(|op| matches!(op.kind, agent::OpKind::Create { .. }));
            self.push_msg(Msg { role: "tool".into(), content: result.to_string(), tool_call_id: Some(request.request_id.clone()), plan: Some(plan), ..Default::default() });
            if !auto { let at = self.conv().map_or(0, |c| c.messages.len() - 1); self.link_plan(at, 0, false, cx); }
            if auto {
                let at = self.conv().map(|conversation| conversation.messages.len() - 1).unwrap_or(0);
                if self.drain_external_events(events, conversation, turn, cx) {
                    self.set_plan_state(at, agent::PlanState::Stale, cx); return true;
                }
                if request.is_cancelled() || self.active_external.as_ref().is_none_or(|(c, t)| c != conversation || t != turn) {
                    self.set_plan_state(at, agent::PlanState::Stale, cx); return false;
                }
                self.apply_plan(at, cx);
                result = if self.plan_at(at).is_some_and(|plan| plan.state == agent::PlanState::Applied) {
                    json!({"status":"applied"})
                } else { json!({"error":self.error.clone().unwrap_or_else(|| "Create failed".into())}) };
            }
        } else { super::super::chat_process::record_tool(self, &request, &result, true); }
        let _ = request.responder.respond(Ok(result));
        false
    }
}
