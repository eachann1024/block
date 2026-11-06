//! AI side panel with streamed conversations, notebook tools and reversible plans.
//! ⌘J toggles the panel; @ references notes and / picks commands and Skills.
use std::sync::{Arc, atomic::{AtomicBool, Ordering}};
use gpui::{App, AppContext, Context, Entity, Global, ScrollHandle, Subscription, WeakEntity, Window};
use gpui_component::input::{InputEvent, InputState};
use super::agent;
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
#[path = "chat_turn.rs"] mod chat_turn;

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

}

/// Seeds only the isolated smoke worker; never touches the user's conversations.
pub(crate) fn prepare_selection_smoke(window: &mut Window, cx: &mut App) -> Result<(), String> {
    if !crate::smoke::is_requested() { return Err("Selection fixture requires smoke mode".into()); }
    let config = super::provider::Config { enabled: true, provider: "openai".into(), base_url: "http://127.0.0.1:1/v1".into(),
        api_key: "smoke-fixture".into(), model: "smoke-fixture".into(), ..Default::default() };
    super::provider::save(&config)?;
    chat(window, cx).update(cx, |this, cx| {
        this.new_conv(window, cx);
        this.open = true;
        this.backend_config.selected = super::backend::BackendKind::Builtin;
        this.push("user", "用户拖选 Alpha Beta".into());
        this.push("assistant", "回复 **加粗** 中文 Alpha\n\n第二段 Beta 结束".into());
        this.scroll.set_offset(gpui::point(gpui::px(0.), gpui::px(0.)));
        cx.notify();
    });
    window.refresh();
    Ok(())
}

pub(crate) fn enable_smoke_builtin(window: &mut Window, cx: &mut App) -> Result<(), String> {
    if !crate::smoke::is_requested() { return Err("AI fixture requires smoke mode".into()); }
    let config = super::provider::Config { enabled: true, provider: "openai".into(), base_url: "http://127.0.0.1:1/v1".into(),
        api_key: "smoke-fixture".into(), model: "smoke-fixture".into(), ..Default::default() };
    super::provider::save(&config)?;
    chat(window, cx).update(cx, |_, cx| cx.notify());
    window.refresh();
    Ok(())
}
