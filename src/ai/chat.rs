//! The AI 助手 side panel, after thinkly's `AIChatPanel`: conversations with history,
//! streamed replies with 停止生成, and ⌘J to toggle. Replies run thinkly's notebook
//! agent (`agent.rs`): Skills, note tools and 笔记变更计划 cards that write back into
//! the open note and can be 撤回. `@` picks notes as references and `/` picks commands
//! and workspace Skills.
use std::{sync::{Arc, atomic::{AtomicBool, Ordering}, mpsc}, time::Duration};
use gpui::{App, AppContext, Context, Entity, Global, ScrollHandle, Subscription, WeakEntity, Window};
use gpui_component::input::{InputEvent, InputState};
use super::{agent, provider::{self, Event}};
use crate::editor::view::Editor;

fn first_placeholder() -> &'static str { crate::theme::tr("向 AI 提问，/ 调用指令或 Skill，@ 引用笔记或本地文件…", "Ask AI, / for commands or Skills, @ to reference notes…") }
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
pub(super) struct Conv { pub id: u64, pub title: String, pub messages: Vec<Msg> }

pub(crate) struct AiChat {
    pub(super) convs: Vec<Conv>,
    pub(super) current: Option<u64>,
    pub(super) input: Entity<InputState>,
    pub(super) refs: Vec<String>,
    pub(super) skills: Vec<String>,
    pub(super) partial: String,
    pub(super) streaming: Option<(u64, Arc<AtomicBool>)>,
    pub(super) serial: u64,
    pub(super) error: Option<String>,
    pub(super) history: bool,
    pub(super) open: bool,
    pub(super) editor: Option<WeakEntity<Editor>>,
    pub(super) scroll: ScrollHandle,
    _sub: Subscription,
}

struct Holder(Entity<AiChat>);
impl Global for Holder {}

fn store_path() -> Option<std::path::PathBuf> {
    Some(crate::files::app_data_dir("thinkly-fast").join("ai-chats.json"))
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

pub(crate) fn toggle(editor: WeakEntity<Editor>, window: &mut Window, cx: &mut App) {
    let c = chat(window, cx);
    c.update(cx, |this, cx| {
        this.open = !this.open;
        this.editor = Some(editor);
        if this.open { this.input.update(cx, |i, cx| i.focus(window, cx)); }
        cx.notify();
    });
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
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(first_placeholder()));
        let sub = cx.subscribe_in(&input, window, |this, _, e: &InputEvent, window, cx| match e {
            InputEvent::PressEnter { secondary: false, .. } => this.send(window, cx),
            InputEvent::Change => cx.notify(),
            _ => {}
        });
        let convs: Vec<Conv> = store_path().and_then(|p| std::fs::read(p).ok()).and_then(|b| serde_json::from_slice(&b).ok()).unwrap_or_default();
        AiChat { convs, current: None, input, refs: Vec::new(), skills: Vec::new(), partial: String::new(), streaming: None, serial: 0, error: None,
            history: false, open: false, editor: None, scroll: ScrollHandle::new(), _sub: sub }
    }
    pub(super) fn persist(&self) {
        let Some(p) = store_path() else { return };
        if let Some(dir) = p.parent() { let _ = std::fs::create_dir_all(dir); }
        let keep: Vec<&Conv> = self.convs.iter().filter(|c| !c.messages.is_empty()).collect();
        if let Ok(json) = serde_json::to_vec_pretty(&keep) { let _ = std::fs::write(p, json); }
    }
    pub(super) fn conv(&self) -> Option<&Conv> { self.current.and_then(|id| self.convs.iter().find(|c| c.id == id)) }
    pub(super) fn stop(&mut self) {
        if let Some((_, cancel)) = self.streaming.take() { cancel.store(true, Ordering::Relaxed); }
        self.serial += 1;
        let partial = std::mem::take(&mut self.partial);
        if !partial.trim().is_empty() { self.push("assistant", partial); }
    }
    pub(super) fn new_conv(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.stop();
        self.current = None; self.error = None; self.history = false; self.refs.clear(); self.skills.clear();
        self.input.update(cx, |i, cx| { i.set_value("", window, cx); i.set_placeholder(first_placeholder(), window, cx); i.focus(window, cx); });
        cx.notify();
    }
    fn push(&mut self, role: &str, content: String) { self.push_msg(Msg { role: role.into(), content, ..Default::default() }); }
    fn push_msg(&mut self, msg: Msg) {
        let content = msg.content.clone();
        let id = self.current.unwrap_or_else(|| {
            let id = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_millis() as u64).unwrap_or(0);
            let title: String = content.lines().find(|l| !l.trim().is_empty() && !l.starts_with('>')).unwrap_or(crate::theme::tr("新会话", "New Chat")).chars().take(24).collect();
            self.convs.insert(0, Conv { id, title, messages: Vec::new() });
            self.current = Some(id);
            id
        });
        if let Some(c) = self.convs.iter_mut().find(|c| c.id == id) { c.messages.push(msg); }
        self.persist();
    }
    pub(super) fn send(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.streaming.is_some() { return; }
        let question = self.input.read(cx).value().trim().to_owned();
        if question.is_empty() { return; }
        let ctx = self.agent_ctx(cx);
        let (mut context, mut shown) = (String::new(), String::new());
        for r in self.refs.drain(..) {
            shown.push_str(&format!("@{r} "));
            let md = ctx.read(&r).unwrap_or_default();
            context.push_str(&format!("\n\n# 引用笔记：{r}（pageId: {r}）\n\n{}", md.chars().take(12000).collect::<String>()));
        }
        let local = ctx.local_skills();
        for name in self.skills.drain(..) {
            shown.push_str(&format!("/{name} "));
            if let Some((_, _, body)) = local.iter().find(|s| s.0 == name) { context.push_str(&format!("\n\n# 用户调用的 Skill：{name}\n\n{body}")); }
        }
        let content = if shown.is_empty() { question } else { format!("{shown}{question}") };
        self.push_msg(Msg { role: "user".into(), content, context: (!context.is_empty()).then_some(context), ..Default::default() });
        self.error = None;
        self.input.update(cx, |i, cx| { i.set_value("", window, cx); i.set_placeholder(follow_placeholder(), window, cx); });
        self.run_turn(0, cx);
    }

    pub(super) fn agent_ctx(&self, cx: &App) -> agent::Ctx {
        let editor = self.editor.as_ref().and_then(|e| e.upgrade());
        let path = editor.as_ref().and_then(|e| e.read(cx).path.clone());
        let root = path.as_ref().and_then(|p| p.parent()).map(|p| p.to_path_buf()).unwrap_or_default();
        let current = path.as_ref().and_then(|p| p.file_stem()).map(|s| s.to_string_lossy().into_owned());
        let current_md = editor.map(|e| crate::editor::markdown::serialize(&e.read(cx).doc)).unwrap_or_default();
        agent::Ctx { root, current, current_md }
    }

    fn run_turn(&mut self, step: usize, cx: &mut Context<Self>) {
        let ctx = self.agent_ctx(cx);
        let notebook = ctx.root.file_name().map(|s| s.to_string_lossy().into_owned()).unwrap_or_default();
        let system = format!("{}\n\n# 当前上下文\n\n- 当前笔记本：{}\n- 当前打开页面：{}", agent::AGENTS,
            if notebook.is_empty() { "（无）" } else { &notebook },
            ctx.current.as_ref().map(|p| format!("[{p}] {p}（pageId: {p}）")).unwrap_or("（无当前打开页面）".into()));
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
        cx.spawn(async move |this, cx| {
            let mut calls = Vec::new();
            loop {
                cx.background_executor().timer(Duration::from_millis(40)).await;
                let mut finished = false;
                let alive = this.update(cx, |this, cx| {
                    if this.serial != serial { finished = true; return; }
                    loop {
                        match rx.try_recv() {
                            Ok(Event::Thinking(_)) => {}
                            Ok(Event::Delta(t)) => this.partial.push_str(&t),
                            Ok(Event::ToolCalls(c)) => calls = c,
                            Ok(Event::Done) => { finished = true; break; }
                            Ok(Event::Failed { message, .. }) => { this.error = Some(provider::format_error(&message)); finished = true; break; }
                            Err(mpsc::TryRecvError::Empty) => break,
                            Err(mpsc::TryRecvError::Disconnected) => { finished = true; break; }
                        }
                    }
                    if finished {
                        this.streaming = None;
                        let text = std::mem::take(&mut this.partial);
                        if calls.is_empty() {
                            if !text.trim().is_empty() { this.push("assistant", text); }
                        } else {
                            let tc: Vec<serde_json::Value> = calls.iter().map(|(id, name, args)| serde_json::json!({
                                "id": id, "type": "function", "function": { "name": name, "arguments": args } })).collect();
                            this.push_msg(Msg { role: "assistant".into(), content: text, tool_calls: Some(tc.into()), ..Default::default() });
                            for (id, name, args) in std::mem::take(&mut calls) {
                                let ctx = this.agent_ctx(cx);
                                let (mut result, plan) = agent::exec(&name, &args, &ctx);
                                let auto = plan.as_ref().is_some_and(|p| p.ops.iter().all(|o| matches!(o.kind, agent::OpKind::Create { .. })));
                                this.push_msg(Msg { role: "tool".into(), content: String::new(), tool_call_id: Some(id), plan, ..Default::default() });
                                if auto {
                                    let at = this.conv().map(|c| c.messages.len() - 1).unwrap_or(0);
                                    this.apply_plan(at, cx);
                                    result = if this.plan_at(at).map(|p| p.state.clone()) == Some(agent::PlanState::Applied) {
                                        serde_json::json!({ "status": "applied", "message": "已新建页面。" })
                                    } else { serde_json::json!({ "error": "新建失败：目标页面已存在。" }) };
                                }
                                if let Some(m) = this.current.and_then(|cid| this.convs.iter_mut().find(|c| c.id == cid)).and_then(|c| c.messages.last_mut()) {
                                    m.content = result.to_string();
                                }
                            }
                            this.persist();
                            if step + 1 < MAX_STEPS { this.run_turn(step + 1, cx); }
                        }
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

    fn plan_at(&self, at: usize) -> Option<&agent::Plan> { self.conv().and_then(|c| c.messages.get(at)).and_then(|m| m.plan.as_ref()) }
    fn set_plan(&mut self, at: usize, plan: agent::Plan) {
        if let Some(m) = self.current.and_then(|cid| self.convs.iter_mut().find(|c| c.id == cid)).and_then(|c| c.messages.get_mut(at)) { m.plan = Some(plan); }
        self.persist();
    }
    fn write_page(&self, ctx: &agent::Ctx, page: &str, text: &str, cx: &mut Context<Self>) -> String {
        if ctx.current.as_deref() == Some(page) {
            if let Some(e) = self.editor.as_ref().and_then(|e| e.upgrade()) { return e.update(cx, |e, cx| e.ai_write_markdown(text, cx)); }
        }
        let path = ctx.path(page);
        if let Some(d) = path.parent() { let _ = std::fs::create_dir_all(d); }
        let fresh = !path.exists();
        let _ = std::fs::write(&path, text);
        if fresh { crate::search::window::notify_saved_note(path, cx); }
        text.to_owned()
    }
    pub(super) fn apply_plan(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(mut plan) = self.plan_at(at).cloned() else { return };
        let ctx = self.agent_ctx(cx);
        let mut work: Vec<(String, Option<String>, String)> = Vec::new();
        for op in &plan.ops {
            let cur = match work.iter().find(|w| w.0 == op.page) { Some(w) => Some(w.2.clone()), None => ctx.read(&op.page) };
            let Some(next) = agent::apply_op(op, cur.as_deref()) else {
                plan.state = agent::PlanState::Stale; self.set_plan(at, plan); cx.notify(); return;
            };
            match work.iter_mut().find(|w| w.0 == op.page) { Some(w) => w.2 = next, None => work.push((op.page.clone(), cur, next)) }
        }
        plan.receipts = work.into_iter().map(|(page, before, after)| {
            let after = self.write_page(&ctx, &page, &after, cx);
            agent::Receipt { created: before.is_none(), before: before.unwrap_or_default(), after, page }
        }).collect();
        plan.state = agent::PlanState::Applied;
        self.set_plan(at, plan);
        cx.notify();
    }
    pub(super) fn undo_plan(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(mut plan) = self.plan_at(at).cloned() else { return };
        let ctx = self.agent_ctx(cx);
        if plan.receipts.iter().any(|r| ctx.read(&r.page).as_deref() != Some(r.after.as_str())) {
            plan.state = agent::PlanState::UndoConflict;
        } else {
            for r in &plan.receipts {
                if r.created && ctx.current.as_deref() != Some(r.page.as_str()) { let _ = std::fs::remove_file(ctx.path(&r.page)); }
                else { self.write_page(&ctx, &r.page, &r.before, cx); }
            }
            plan.state = agent::PlanState::Undone;
        }
        self.set_plan(at, plan);
        cx.notify();
    }
    pub(super) fn set_plan_state(&mut self, at: usize, state: agent::PlanState, cx: &mut Context<Self>) {
        if let Some(mut p) = self.plan_at(at).cloned() { p.state = state; self.set_plan(at, p); }
        cx.notify();
    }
}
