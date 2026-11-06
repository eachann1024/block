//! AI side panel with streamed conversations, notebook tools and reversible plans.
//! ⌘J toggles the panel; @ references notes and / picks commands and Skills.
use std::{sync::{Arc, atomic::{AtomicBool, Ordering}, mpsc}, time::Duration};
use gpui::{App, AppContext, Context, Entity, Global, ScrollHandle, Subscription, WeakEntity, Window};
use gpui_component::input::{InputEvent, InputState};
use super::{agent, provider::{self, Event}};
use crate::editor::view::Editor;
#[path = "chat_resize.rs"] mod chat_resize;
pub(crate) use chat_resize::resize_capture;
#[path = "chat_plan.rs"] mod chat_plan;
#[path = "chat_backend.rs"] mod chat_backend;
#[path = "chat_drafts.rs"] mod chat_drafts;
#[path = "chat_roots.rs"] pub(super) mod chat_roots;
#[path = "chat_skills.rs"] pub(super) mod chat_skills;
#[path = "chat_models.rs"] pub(super) mod chat_models;

pub(super) fn first_placeholder() -> &'static str { crate::theme::tr("向 AI 提问，/ 调用技能，@ 引用笔记…", "Ask AI, / for skills, @ to reference notes…") }
pub(super) fn follow_placeholder() -> &'static str { crate::theme::tr("继续追问…", "Ask a follow-up…") }
const MAX_STEPS: usize = 8;

#[derive(Clone, Default, serde::Serialize, serde::Deserialize)]
pub(super) struct Msg {
    pub role: String,
    pub content: String,
    #[serde(default, skip_serializing_if = "Option::is_none")] pub context: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")] pub tool_calls: Option<serde_json::Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")] pub tool_call_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")] pub plan: Option<agent::Plan>,
}

#[derive(Clone)]
pub(super) enum Pick { Page(String), Skill(String), NewConv }
#[derive(Clone, serde::Serialize, serde::Deserialize)]
pub(super) struct SentDraft { pub text: String, pub refs: Vec<String>, pub skills: Vec<String>, pub conversation: u64, pub message_index: usize }
#[derive(Clone, serde::Serialize, serde::Deserialize)]
pub(super) struct Conv {
    pub id: u64, pub title: String, pub messages: Vec<Msg>,
    #[serde(default)] pub backend: super::backend::BackendKind,
    #[serde(default)] pub native_session_id: Option<String>,
}

pub(crate) struct AiChat {
    pub(super) convs: Vec<Conv>,
    pub(super) current: Option<u64>,
    pub(super) input: Entity<InputState>,
    pub(super) refs: Vec<String>,
    pub(super) skills: Vec<String>,
    pub(super) partial: String,
    pub(super) streaming: Option<(u64, Arc<AtomicBool>)>,
    pub(super) backend_config: super::backend::BackendConfig,
    pub(super) backend_runtime: super::backend::BackendRuntime,
    pub(super) active_external: Option<(String, String)>,
    pub(super) external_tools: Option<super::backend::ToolBridge>,
    pub(super) sent_draft: Option<SentDraft>,
    pub(super) deferred_drafts: Vec<SentDraft>,
    pub(super) retry_draft: Option<SentDraft>,
    pub(super) selection_scope: bool,
    pub(super) pending_approval: Option<(String, String, String)>,
    pub(super) backend_statuses: Vec<(super::backend::BackendKind, super::backend::BackendStatus)>,
    pub(super) model_cache: Vec<(super::backend::BackendKind, chat_models::ModelStatus, Vec<super::backend::ModelOption>)>,
    pub(super) serial: u64,
    pub(super) error: Option<String>,
    pub(super) error_settings: bool,
    pub(super) pick_index: usize,
    pub(super) picker_dismissed: bool,
    pub(super) history: bool,
    pub(super) open: bool,
    pub(super) fullscreen: bool,
    pub(super) panel_width: gpui::Pixels,
    pub(super) resizing: Option<(gpui::Pixels, gpui::Pixels)>,
    pub(super) last_en: bool,
    pub(super) editor: Option<WeakEntity<Editor>>,
    pub(super) scroll: ScrollHandle,
    pub(super) deck_scroll: ScrollHandle,
    _sub: Subscription,
}

struct Holder(Entity<AiChat>);
impl Global for Holder {}

fn store_path() -> Option<std::path::PathBuf> {
    Some(crate::files::app_data_dir("Block").join("ai-chats.json"))
}

fn chat(window: &mut Window, cx: &mut App) -> Entity<AiChat> {
    if let Some(h) = cx.try_global::<Holder>() { return h.0.clone(); }
    let entity = cx.new(|cx| AiChat::new(window, cx));
    cx.set_global(Holder(entity.clone()));
    entity
}

pub(crate) fn panel(cx: &App) -> Option<Entity<AiChat>> {
    cx.try_global::<Holder>().map(|h| h.0.clone()).filter(|c| c.read(cx).open)
}

pub(crate) fn is_open(cx: &App) -> bool { panel(cx).is_some() }
pub(crate) fn is_fullscreen(cx: &App) -> bool { panel(cx).is_some_and(|chat| chat.read(cx).fullscreen) }

pub(crate) fn sync_settings(cx: &mut App) {
    let Some(entity) = cx.try_global::<Holder>().map(|h| h.0.clone()) else { return };
    entity.update(cx, |this, cx| {
        if this.error_settings { this.error = None; this.error_settings = false; }
        this.probe_backend(super::backend::BackendKind::Builtin, cx);
        this.refresh_models_for(super::backend::BackendKind::Builtin, cx);
        cx.notify();
    });
}

pub(crate) fn toggle(editor: WeakEntity<Editor>, window: &mut Window, cx: &mut App) {
    let c = chat(window, cx);
    c.update(cx, |this, cx| {
        this.open = !this.open;
        if !this.open { this.fullscreen = false; }
        this.editor = Some(editor);
        if this.open { this.input.update(cx, |i, cx| i.focus(window, cx)); }
        cx.notify();
    });
    window.refresh();
}

impl Editor {
    /// Replaces the open note with `md` as one undo step; returns the text it now holds.
    fn ai_write_markdown(&mut self, md: &str, cx: &mut Context<Self>) -> String {
        let before = self.snapshot();
        let old = self.doc.blocks.len();
        self.doc = crate::editor::markdown::parse(md);
        if let Some(id) = self.doc.blocks.iter().next().map(|b| b.id) { self.caret = crate::editor::view::selection::Caret::new(id); }
        let after = self.doc.blocks.len();
        self.record_changes(before, vec![crate::editor::input::transactions::ListChange { start: 0, before: old, after }], true, cx);
        crate::editor::markdown::serialize(&self.doc)
    }
}

impl AiChat {
    fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let active = chat_drafts::load_active();
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(first_placeholder()).default_value(active.text.clone()));
        let sub = cx.subscribe_in(&input, window, |this, _, e: &InputEvent, window, cx| match e {
            InputEvent::PressEnter { secondary: false, .. } => this.send(window, cx),
            InputEvent::Change => { this.pick_index = 0; this.picker_dismissed = false; this.persist_active_draft(cx); cx.notify(); },
            _ => {}
        });
        let convs: Vec<Conv> = store_path().and_then(|p| std::fs::read(p).ok()).and_then(|b| serde_json::from_slice(&b).ok()).unwrap_or_default();
        let current = active.conversation.filter(|id| convs.iter().any(|conversation| conversation.id == *id));
        let mut chat = AiChat { panel_width: chat_resize::load_width(), resizing: None, convs, current, input, refs: active.refs, skills: active.skills, partial: String::new(), streaming: None,
            backend_config: super::backend::BackendConfig::load(), backend_runtime: Default::default(), active_external: None, external_tools: None,
            sent_draft: None, deferred_drafts: chat_drafts::load(), retry_draft: active.retry.filter(|draft| Some(draft.conversation) == current), selection_scope: false, pending_approval: None,
            backend_statuses: vec![(super::backend::BackendKind::Builtin, super::backend::BackendStatus::Ready { version: "Block".into() })],
            model_cache: Vec::new(), serial: 0, error: None,
            error_settings: false, pick_index: 0, picker_dismissed: false, history: false, open: false, fullscreen: false, last_en: crate::theme::is_en(), editor: None, scroll: ScrollHandle::new(), deck_scroll: ScrollHandle::new(), _sub: sub };
        for kind in [super::backend::BackendKind::Builtin, super::backend::BackendKind::Codex, super::backend::BackendKind::Claude, super::backend::BackendKind::Dsh] {
            chat.probe_backend(kind, cx);
            chat.refresh_models_for(kind, cx);
        }
        chat
    }
    pub(super) fn persist(&self) {
        let Some(p) = store_path() else { return };
        if let Some(dir) = p.parent() { let _ = std::fs::create_dir_all(dir); }
        let keep: Vec<&Conv> = self.convs.iter().filter(|c| !c.messages.is_empty()).collect();
        if let Ok(json) = serde_json::to_vec_pretty(&keep) { let _ = std::fs::write(p, json); }
    }
    pub(super) fn conv(&self) -> Option<&Conv> { self.current.and_then(|id| self.convs.iter().find(|c| c.id == id)) }
    pub(super) fn stop(&mut self) {
        self.sent_draft = None;
        if let Some(tools) = self.external_tools.take() { tools.cancel(); }
        if let Some((conversation, turn)) = self.active_external.take() {
            if let Err(error) = self.backend_runtime.cancel(&conversation, &turn) { self.error = Some(error.message); }
            let _ = self.backend_runtime.finish(&conversation, &turn);
        }
        self.pending_approval = None;
        if let Some((_, cancel)) = self.streaming.take() { cancel.store(true, Ordering::Relaxed); }
        self.serial += 1;
        let partial = std::mem::take(&mut self.partial);
        if !partial.trim().is_empty() { self.push("assistant", partial); }
    }
    pub(super) fn new_conv(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.stop();
        self.current = None; self.error = None; self.error_settings = false; self.history = false; self.refs.clear(); self.skills.clear();
        self.pick_index = 0; self.picker_dismissed = false;
        self.input.update(cx, |i, cx| { i.set_value("", window, cx); i.set_placeholder(first_placeholder(), window, cx); i.focus(window, cx); });
        self.retry_draft = None;
        self.persist_active_draft(cx);
        cx.notify();
    }
    fn push(&mut self, role: &str, content: String) { self.push_msg(Msg { role: role.into(), content, ..Default::default() }); }
    pub(super) fn push_msg(&mut self, msg: Msg) {
        let content = msg.content.clone();
        let id = self.current.unwrap_or_else(|| {
            let mut id = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_millis() as u64).unwrap_or(0);
            while self.convs.iter().any(|conversation| conversation.id == id) { id = id.wrapping_add(1); }
            let title: String = content.lines().find(|l| !l.trim().is_empty() && !l.starts_with('>')).unwrap_or(crate::theme::tr("新会话", "New Chat")).chars().take(24).collect();
            self.convs.insert(0, Conv { id, title, messages: Vec::new(), backend: self.backend_config.selected, native_session_id: None });
            self.current = Some(id);
            id
        });
        if let Some(c) = self.convs.iter_mut().find(|c| c.id == id) { c.messages.push(msg); }
        self.persist();
    }

    pub(super) fn agent_ctx(&self, cx: &App) -> agent::Ctx {
        let editor = self.editor.as_ref().and_then(|e| e.upgrade());
        let path = editor.as_ref().and_then(|e| e.read(cx).path.clone());
        let root = path.as_ref().and_then(|p| p.parent()).map(|p| p.to_path_buf()).unwrap_or_default();
        let current = path.as_ref().and_then(|p| p.file_stem()).map(|s| s.to_string_lossy().into_owned());
        let current_md = editor.map(|e| crate::editor::markdown::serialize(&e.read(cx).doc)).unwrap_or_default();
        agent::Ctx { root, current, current_md }
    }

    pub(super) fn run_turn(&mut self, step: usize, window: &mut Window, cx: &mut Context<Self>) {
        let ctx = self.agent_ctx(cx);
        let notebook = ctx.root.file_name().map(|s| s.to_string_lossy().into_owned()).unwrap_or_default();
        let live = self.note_context(cx);
        let system = format!("{}\n\n# 当前上下文\n\n- 当前笔记本：{}\n- 当前打开页面：{}\n- 当前内容范围：{}\n\n{}\n\n# 当前选区\n\n{}", agent::AGENTS,
            if notebook.is_empty() { "（无）" } else { &notebook },
            ctx.current.as_ref().map(|p| format!("[{p}] {p}（pageId: {p}）")).unwrap_or("（无当前打开页面）".into()),
            if self.selection_scope { "选区" } else { "当前笔记" }, live.markdown.unwrap_or_default(), live.selection.unwrap_or_default());
        let mut messages = vec![serde_json::json!({ "role": "system", "content": system })];
        if let Some(c) = self.conv() {
            for m in &c.messages {
                messages.push(match m.role.as_str() {
                    "tool" => serde_json::json!({ "role": "tool", "tool_call_id": m.tool_call_id, "content": m.content }),
                    "assistant" if m.tool_calls.is_some() => serde_json::json!({ "role": "assistant", "content": m.content, "tool_calls": m.tool_calls }),
                    role => serde_json::json!({ "role": role, "content": format!("{}{}", m.content, m.context.as_deref().unwrap_or("")) }),
                });
            }
        }
        let cancel = Arc::new(AtomicBool::new(false));
        self.serial += 1;
        let serial = self.serial;
        self.streaming = Some((serial, cancel.clone()));
        self.partial.clear();
        let rx = provider::stream_agent(messages, Some(agent::tools()), cancel);
        cx.spawn_in(window, async move |this, cx| {
            let mut calls = Vec::new();
            loop {
                cx.background_executor().timer(Duration::from_millis(40)).await;
                let mut finished = false;
                let alive = this.update_in(cx, |this, window, cx| {
                    if this.serial != serial { finished = true; return; }
                    let mut failed = false;
                    loop {
                        match rx.try_recv() {
                            Ok(Event::Thinking(_)) => {}
                            Ok(Event::Delta(t)) => this.partial.push_str(&t),
                            Ok(Event::ToolCalls(c)) => calls = c,
                            Ok(Event::Done) => { finished = true; break; }
                            Ok(Event::Failed { message, settings }) => { this.error = Some(provider::format_error(&message)); this.error_settings = settings; finished = true; failed = true; break; }
                            Err(mpsc::TryRecvError::Empty) => break,
                            Err(mpsc::TryRecvError::Disconnected) => { this.error = Some("AI backend disconnected".into()); finished = true; failed = true; break; }
                        }
                    }
                    if finished {
                        this.streaming = None;
                        if failed { calls.clear(); }
                        let text = std::mem::take(&mut this.partial);
                        if calls.is_empty() {
                            if !text.trim().is_empty() { this.push("assistant", text); }
                        } else {
                            let tc: Vec<serde_json::Value> = calls.iter().map(|(id, name, args)| serde_json::json!({
                                "id": id, "type": "function", "function": { "name": name, "arguments": args } })).collect();
                            this.push_msg(Msg { role: "assistant".into(), content: text, tool_calls: Some(tc.into()), ..Default::default() });
                            for (id, name, args) in std::mem::take(&mut calls) {
                                let root = serde_json::from_str::<serde_json::Value>(&args).ok().and_then(|value| value.get("notebookRoot").and_then(|root| root.as_str()).map(str::to_owned));
                                let ctx = root.as_ref().map(|root| this.context_for_root(std::path::Path::new(root), cx)).unwrap_or_else(|| Ok(this.agent_ctx(cx)));
                                let (mut result, plan) = match ctx {
                                    Ok(ctx) => agent::exec(&name, &args, &ctx),
                                    Err(error) => (serde_json::json!({"error":error}), None),
                                };
                                let auto = plan.as_ref().is_some_and(|p| p.ops.iter().all(|o| matches!(o.kind, agent::OpKind::Create { .. })));
                                this.push_msg(Msg { role: "tool".into(), content: String::new(), tool_call_id: Some(id), plan, ..Default::default() });
                                if auto {
                                    let at = this.conv().map(|c| c.messages.len() - 1).unwrap_or(0);
                                    this.apply_plan(at, cx);
                                    result = if this.plan_at(at).map(|p| p.state.clone()) == Some(agent::PlanState::Applied) {
                                        serde_json::json!({ "status": "applied", "message": "已新建页面。" })
                                    } else { serde_json::json!({ "error": this.error.clone().unwrap_or_else(|| "新建失败：目标页面已变化或不可写。".into()) }) };
                                }
                                if let Some(m) = this.current.and_then(|cid| this.convs.iter_mut().find(|c| c.id == cid)).and_then(|c| c.messages.last_mut()) {
                                    m.content = result.to_string();
                                }
                            }
                            this.persist();
                            if step + 1 < MAX_STEPS { this.run_turn(step + 1, window, cx); }
                        }
                        if failed { this.restore_failed_draft(window, cx); }
                        if !failed && calls.is_empty() && this.streaming.is_none() { this.sent_draft = None; }
                    }
                    this.scroll.scroll_to_bottom();
                    cx.notify();
                });
                if alive.is_err() || finished { break; }
            }
        }).detach();
        self.scroll.scroll_to_bottom();
        cx.notify();
    }

}
