//! The AI 助手 side panel, after thinkly's `AIChatPanel`: conversations with history,
//! streamed replies with 停止生成, quoted selections from 加入对话 (⌘⇧U), and ⌘J to
//! toggle. Replies run thinkly's notebook agent (`agent.rs`): Skills, note tools and
//! 笔记变更计划 cards that write back into the open note and can be 撤回. `@` picks
//! notes as references and `/` picks commands and workspace Skills.
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, mpsc};
use std::time::Duration;

use gpui::{App, AppContext, Context, CursorStyle, Entity, FontWeight, Global, IntoElement, MouseButton, ParentElement, Render,
    ScrollHandle, SharedString, Styled, Subscription, WeakEntity, Window, div, prelude::*, px, transparent_black};
use gpui_component::input::{Input, InputEvent, InputState};

use super::agent;
use super::provider::{self, Event};
use crate::editor::view::Editor;
use crate::icons::{Icon, IconColors, icon};
use crate::theme::Theme;

const FIRST_PLACEHOLDER: &str = "向 AI 提问，/ 调用指令或 Skill，@ 引用笔记或本地文件…";
const FOLLOW_PLACEHOLDER: &str = "继续追问…";
const MAX_STEPS: usize = 8;

#[derive(Clone, Default, serde::Serialize, serde::Deserialize)]
struct Msg {
    role: String,
    content: String,
    /// Referenced notes / invoked Skills sent to the model but not shown in the bubble.
    #[serde(default, skip_serializing_if = "Option::is_none")] context: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")] tool_calls: Option<serde_json::Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")] tool_call_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")] plan: Option<agent::Plan>,
}

/// A row of the `@` / `/` picker.
#[derive(Clone)]
enum Pick { Page(String), Skill(String), NewConv }

#[derive(Clone, serde::Serialize, serde::Deserialize)]
struct Conv { id: u64, title: String, messages: Vec<Msg> }

pub(crate) struct AiChat {
    convs: Vec<Conv>,
    current: Option<u64>,
    input: Entity<InputState>,
    quotes: Vec<String>,
    refs: Vec<String>,
    skills: Vec<String>,
    partial: String,
    streaming: Option<(u64, Arc<AtomicBool>)>,
    serial: u64,
    error: Option<String>,
    history: bool,
    open: bool,
    editor: Option<WeakEntity<Editor>>,
    scroll: ScrollHandle,
    _sub: Subscription,
}

struct Holder(Entity<AiChat>);
impl Global for Holder {}

fn store_path() -> Option<std::path::PathBuf> {
    std::env::var_os("HOME").map(|h| std::path::PathBuf::from(h).join("Library/Application Support/thinkly-fast/ai-chats.json"))
}

fn chat(window: &mut Window, cx: &mut App) -> Entity<AiChat> {
    if let Some(h) = cx.try_global::<Holder>() { return h.0.clone(); }
    let entity = cx.new(|cx| AiChat::new(window, cx));
    cx.set_global(Holder(entity.clone()));
    entity
}

/// The panel when it is shown, for the workspace layout.
pub(crate) fn panel(cx: &App) -> Option<Entity<AiChat>> {
    cx.try_global::<Holder>().map(|h| h.0.clone()).filter(|c| c.read(cx).open)
}

/// ⌘J / 关闭 AI.
pub(crate) fn toggle(editor: WeakEntity<Editor>, window: &mut Window, cx: &mut App) {
    let c = chat(window, cx);
    c.update(cx, |this, cx| {
        this.open = !this.open;
        this.editor = Some(editor);
        if this.open { this.input.update(cx, |i, cx| i.focus(window, cx)); }
        cx.notify();
    });
}

/// 加入对话 (⌘⇧U): quotes the editor selection into the composer and shows the panel.
pub(crate) fn add_selection(editor: Entity<Editor>, window: &mut Window, cx: &mut App) {
    let text = editor.read(cx).ai_selected_text();
    let c = chat(window, cx);
    c.update(cx, |this, cx| {
        this.editor = Some(editor.downgrade());
        this.open = true;
        if !text.trim().is_empty() { this.quotes.push(text); }
        this.input.update(cx, |i, cx| i.focus(window, cx));
        cx.notify();
    });
}

impl Editor {
    pub(crate) fn ai_selected_text(&self) -> String {
        self.selection_ranges().into_iter()
            .filter_map(|(id, r)| self.doc.get(id).map(|b| b.text[r].to_owned()))
            .collect::<Vec<_>>().join("\n")
    }
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
        let input = cx.new(|cx| InputState::new(window, cx).placeholder(FIRST_PLACEHOLDER));
        let sub = cx.subscribe_in(&input, window, |this, _, e: &InputEvent, window, cx| {
            match e {
                InputEvent::PressEnter { secondary: false } => this.send(window, cx),
                InputEvent::Change => cx.notify(),
                _ => {}
            }
        });
        let convs: Vec<Conv> = store_path().and_then(|p| std::fs::read(p).ok()).and_then(|b| serde_json::from_slice(&b).ok()).unwrap_or_default();
        AiChat { convs, current: None, input, quotes: Vec::new(), refs: Vec::new(), skills: Vec::new(), partial: String::new(), streaming: None, serial: 0, error: None,
            history: false, open: false, editor: None, scroll: ScrollHandle::new(), _sub: sub }
    }
    fn persist(&self) {
        let Some(p) = store_path() else { return };
        if let Some(dir) = p.parent() { let _ = std::fs::create_dir_all(dir); }
        let keep: Vec<&Conv> = self.convs.iter().filter(|c| !c.messages.is_empty()).collect();
        if let Ok(json) = serde_json::to_vec_pretty(&keep) { let _ = std::fs::write(p, json); }
    }
    fn conv(&self) -> Option<&Conv> { self.current.and_then(|id| self.convs.iter().find(|c| c.id == id)) }
    fn stop(&mut self) {
        if let Some((_, cancel)) = self.streaming.take() { cancel.store(true, Ordering::Relaxed); }
        self.serial += 1;
        let partial = std::mem::take(&mut self.partial);
        if !partial.trim().is_empty() { self.push("assistant", partial); }
    }
    fn new_conv(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.stop();
        self.current = None; self.error = None; self.history = false; self.quotes.clear(); self.refs.clear(); self.skills.clear();
        self.input.update(cx, |i, cx| { i.set_value("", window, cx); i.set_placeholder(FIRST_PLACEHOLDER, window, cx); i.focus(window, cx); });
        cx.notify();
    }
    fn push(&mut self, role: &str, content: String) { self.push_msg(Msg { role: role.into(), content, ..Default::default() }); }
    fn push_msg(&mut self, msg: Msg) {
        let content = msg.content.clone();
        let id = match self.current {
            Some(id) => id,
            None => {
                let id = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_millis() as u64).unwrap_or(0);
                let title: String = content.lines().find(|l| !l.trim().is_empty() && !l.starts_with('>')).unwrap_or("新会话").chars().take(24).collect();
                self.convs.insert(0, Conv { id, title, messages: Vec::new() });
                self.current = Some(id);
                id
            }
        };
        if let Some(c) = self.convs.iter_mut().find(|c| c.id == id) { c.messages.push(msg); }
        self.persist();
    }
    fn send(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.streaming.is_some() { return; }
        let question = self.input.read(cx).value().trim().to_owned();
        if question.is_empty() && self.quotes.is_empty() { return; }
        let mut content = String::new();
        for q in self.quotes.drain(..) {
            for line in q.lines() { content.push_str("> "); content.push_str(line); content.push('\n'); }
            content.push('\n');
        }
        content.push_str(if question.is_empty() { "请解释这段内容。" } else { &question });
        let ctx = self.agent_ctx(cx);
        let mut context = String::new();
        let mut shown = String::new();
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
        let content = if shown.is_empty() { content } else { format!("{shown}{content}") };
        self.push_msg(Msg { role: "user".into(), content, context: (!context.is_empty()).then_some(context), ..Default::default() });
        self.error = None;
        self.input.update(cx, |i, cx| { i.set_value("", window, cx); i.set_placeholder(FOLLOW_PLACEHOLDER, window, cx); });
        self.run_turn(0, cx);
    }

    fn agent_ctx(&self, cx: &App) -> agent::Ctx {
        let editor = self.editor.as_ref().and_then(|e| e.upgrade());
        let path = editor.as_ref().and_then(|e| e.read(cx).path.clone());
        let root = path.as_ref().and_then(|p| p.parent()).map(|p| p.to_path_buf()).unwrap_or_default();
        let current = path.as_ref().and_then(|p| p.file_stem()).map(|s| s.to_string_lossy().into_owned());
        let current_md = editor.map(|e| crate::editor::markdown::serialize(&e.read(cx).doc)).unwrap_or_default();
        agent::Ctx { root, current, current_md }
    }

    /// One model round of the agent loop: stream, then run any tool calls and go again.
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
                                    let state = this.plan_at(at).map(|p| p.state.clone());
                                    result = if state == Some(agent::PlanState::Applied) { serde_json::json!({ "status": "applied", "message": "已新建页面。" }) }
                                        else { serde_json::json!({ "error": "新建失败：目标页面已存在。" }) };
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
    /// Writes a page: into the open editor (one undo step) or the file on disk.
    fn write_page(&self, ctx: &agent::Ctx, page: &str, text: &str, cx: &mut Context<Self>) -> String {
        if ctx.current.as_deref() == Some(page) {
            if let Some(e) = self.editor.as_ref().and_then(|e| e.upgrade()) { return e.update(cx, |e, cx| e.ai_write_markdown(text, cx)); }
        }
        let path = ctx.path(page);
        if let Some(d) = path.parent() { let _ = std::fs::create_dir_all(d); }
        let fresh = !path.exists();
        let _ = std::fs::write(&path, text);
        if fresh { crate::quick::notify_saved_note(path, cx); }
        text.to_owned()
    }
    /// 应用: every op is checked against the live text first, then all are written.
    fn apply_plan(&mut self, at: usize, cx: &mut Context<Self>) {
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
    /// 撤回本次变更: only when every page still holds exactly what was applied.
    fn undo_plan(&mut self, at: usize, cx: &mut Context<Self>) {
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
    fn set_plan_state(&mut self, at: usize, state: agent::PlanState, cx: &mut Context<Self>) {
        if let Some(mut p) = self.plan_at(at).cloned() { p.state = state; self.set_plan(at, p); }
        cx.notify();
    }
}

fn glyph(i: Icon, size: f32, color: gpui::Hsla) -> impl IntoElement {
    icon(i, px(size), IconColors { line: color, fill: transparent_black(), accent: color })
}

fn icon_btn(id: impl Into<SharedString>, glyph_icon: Icon, tip: &'static str) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id(id.into()).size(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(6.))
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
        .tooltip(move |w, cx| gpui_component::tooltip::Tooltip::new(tip).build(w, cx))
        .child(glyph(glyph_icon, 16., theme.text_secondary))
}

impl Render for AiChat {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let model = provider::config().model;
        let header = div().h(px(44.)).flex_none().px(px(12.)).flex().items_center().gap(px(4.)).border_b_1().border_color(theme.hairline)
            .child(glyph(Icon::Sparkles, 16., theme.accent))
            .child(div().flex_1().pl(px(4.)).text_size(px(14.)).font_weight(FontWeight(600.)).child("AI 助手"))
            .child(icon_btn("ai-chat-new", Icon::AiPlus, "新建会话").on_mouse_down(MouseButton::Left, cx.listener(|this, _, w, cx| this.new_conv(w, cx))))
            .child(icon_btn("ai-chat-history", Icon::History, "历史会话").on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.history = !this.history; cx.notify(); })))
            .child(icon_btn("ai-chat-close", Icon::Close, "关闭 AI").on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.open = false; cx.notify(); })));

        let history = self.history.then(|| {
            let list = if self.convs.iter().all(|c| c.messages.is_empty()) {
                div().p(px(12.)).text_size(px(13.)).text_color(theme.text_secondary).child("暂无历史会话")
            } else {
                self.convs.iter().filter(|c| !c.messages.is_empty()).fold(div().flex().flex_col().p(px(4.)), |d, c| {
                    let (id, active) = (c.id, Some(c.id) == self.current);
                    d.child(div().id(SharedString::from(format!("conv-{id}"))).h(px(32.)).px(px(8.)).flex().items_center().gap(px(6.)).rounded(px(6.))
                        .when(active, |r| r.bg(theme.hover)).hover(move |s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                            this.stop(); this.current = Some(id); this.history = false; this.error = None;
                            this.input.update(cx, |i, cx| i.set_placeholder(FOLLOW_PLACEHOLDER, w, cx));
                            cx.notify();
                        }))
                        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(13.)).child(c.title.clone()))
                        .child(div().id(SharedString::from(format!("conv-del-{id}"))).size(px(22.)).flex().items_center().justify_center().rounded(px(4.))
                            .hover(move |s| s.bg(theme.shell))
                            .tooltip(|w, cx| gpui_component::tooltip::Tooltip::new("删除会话").build(w, cx))
                            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                                cx.stop_propagation();
                                if this.current == Some(id) { this.stop(); this.current = None; }
                                this.convs.retain(|c| c.id != id); this.persist(); cx.notify();
                            }))
                            .child(glyph(Icon::Trash, 13., theme.text_secondary))))
                })
            };
            div().id("ai-chat-history-list").mx(px(8.)).mt(px(6.)).max_h(px(260.)).overflow_y_scroll().rounded(px(8.)).border_1()
                .border_color(theme.menu_border).bg(theme.menu).shadow_md().child(list)
        });

        let mut msgs: Vec<Msg> = self.conv().map(|c| c.messages.clone()).unwrap_or_default();
        if self.streaming.is_some() { msgs.push(Msg { role: "assistant".into(), content: if self.partial.is_empty() { "思考中…".into() } else { self.partial.clone() }, ..Default::default() }); }
        let empty = msgs.is_empty();
        let body = msgs.into_iter().enumerate().fold(div().flex().flex_col().gap(px(14.)).p(px(14.)), |d, (i, m)| {
            let text = m.content.clone();
            if m.role == "user" {
                d.child(div().flex().justify_end().child(div().max_w(px(300.)).px(px(12.)).py(px(8.)).rounded(px(12.)).bg(theme.hover)
                    .text_size(px(14.)).line_height(px(22.)).child(text)))
            } else if m.role == "tool" {
                match m.plan { Some(plan) => d.child(plan_card(i, plan, cx)), None => d }
            } else {
                let tools = m.tool_calls.as_ref().and_then(|t| t.as_array()).map(|a| a.iter()
                    .filter_map(|c| c.pointer("/function/name").and_then(|n| n.as_str()).map(tool_label)).collect::<Vec<_>>().join(" · "));
                let copy = text.clone();
                d.child(div().flex().flex_col().gap(px(4.))
                    .when(!text.trim().is_empty(), |d| d.child(super::md_view::render(&text, &format!("{i}"))))
                    .when_some(tools, |d, t| d.child(div().flex().items_center().gap(px(6.)).text_size(px(12.)).text_color(theme.text_secondary)
                        .child(glyph(Icon::Sparkles, 12., theme.text_secondary)).child(t)))
                    .when(m.tool_calls.is_none(), |d| d.child(div().id(SharedString::from(format!("copy-{i}"))).w_auto().text_size(px(12.)).text_color(theme.text_secondary)
                        .cursor(CursorStyle::PointingHand).hover(move |s| s.text_color(theme.text))
                        .on_mouse_down(MouseButton::Left, move |_, _, cx| cx.write_to_clipboard(gpui::ClipboardItem::new_string(copy.clone())))
                        .child("复制"))))
            }
        });
        let body = if empty { body.child(div().pt(px(40.)).flex().flex_col().items_center().gap(px(8.)).text_color(theme.text_secondary)
            .child(glyph(Icon::Sparkles, 28., theme.accent)).child(div().text_size(px(14.)).child("有什么可以帮你？"))) } else { body };

        let error = self.error.clone().map(|e| div().mx(px(12.)).mb(px(8.)).p(px(10.)).flex().items_start().gap(px(8.)).rounded(px(8.))
            .bg(gpui::hsla(0., 0.75, 0.5, 0.08)).text_size(px(12.))
            .child(div().flex_1().flex().flex_col().gap(px(2.))
                .child(div().font_weight(FontWeight(600.)).text_color(gpui::rgb(0xb42318)).child("本轮失败原因"))
                .child(div().text_color(theme.text).child(e)))
            .child(icon_btn("ai-chat-error-close", Icon::Close, "关闭错误提示").on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.error = None; cx.notify(); }))));

        let quotes = self.quotes.iter().enumerate().fold(div().flex().flex_col().gap(px(4.)), |d, (i, q)| {
            d.child(div().flex().items_center().gap(px(6.)).px(px(8.)).py(px(4.)).rounded(px(6.)).bg(theme.hover).text_size(px(12.))
                .child(div().w(px(2.)).h(px(14.)).bg(theme.accent))
                .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_color(theme.text_secondary).child(q.replace('\n', " ")))
                .child(div().id(SharedString::from(format!("quote-x-{i}"))).cursor(CursorStyle::PointingHand)
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| { if i < this.quotes.len() { this.quotes.remove(i); } cx.notify(); }))
                    .child(glyph(Icon::Close, 12., theme.text_secondary))))
        });
        let chip_list: Vec<(bool, String)> = self.refs.iter().map(|r| (true, r.clone())).chain(self.skills.iter().map(|s| (false, s.clone()))).collect();
        let chips = chip_list.into_iter().enumerate().fold(div().flex().flex_wrap().gap(px(4.)), |d, (i, (is_ref, name))| {
            d.child(div().id(SharedString::from(format!("chip-{i}"))).flex().items_center().gap(px(4.)).px(px(6.)).py(px(2.)).rounded(px(6.))
                .bg(theme.hover).text_size(px(12.))
                .child(glyph(if is_ref { Icon::Note } else { Icon::Sparkles }, 12., theme.accent))
                .child(if is_ref { name.clone() } else { format!("/{name}") })
                .child(div().id(SharedString::from(format!("chip-x-{i}"))).cursor(CursorStyle::PointingHand)
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                        if is_ref { this.refs.retain(|r| r != &name); } else { this.skills.retain(|s| s != &name); }
                        cx.notify();
                    }))
                    .child(glyph(Icon::Close, 10., theme.text_secondary))))
        });
        let value = self.input.read(cx).value().to_string();
        let token = value.rsplit(char::is_whitespace).next().unwrap_or("").to_owned();
        let picks: Vec<(Pick, String, String)> = if let Some(q) = token.strip_prefix('@') {
            let q = q.to_lowercase();
            self.agent_ctx(cx).pages().into_iter().filter(|p| p.to_lowercase().contains(&q) && !self.refs.contains(p))
                .take(8).map(|p| (Pick::Page(p.clone()), p, "笔记".into())).collect()
        } else if let Some(q) = token.strip_prefix('/').filter(|_| value.trim_start().starts_with('/') || value.contains(" /")) {
            let q = q.to_lowercase();
            let mut v: Vec<(Pick, String, String)> = vec![(Pick::NewConv, "/新会话".into(), "开启空白对话，当前会话会留在历史里".into())];
            v.extend(self.agent_ctx(cx).local_skills().into_iter().map(|(n, d, _)| (Pick::Skill(n.clone()), format!("/{n}"), d)));
            v.into_iter().filter(|(_, n, d)| q.is_empty() || n.to_lowercase().contains(&q) || d.to_lowercase().contains(&q) || (q == "new" && n == "/新会话")).collect()
        } else { Vec::new() };
        let picker_open = token.starts_with('@') || (token.starts_with('/') && (value.trim_start().starts_with('/') || value.contains(" /")));
        let picker = picker_open.then(|| {
            let list = if picks.is_empty() {
                div().p(px(10.)).text_size(px(12.)).text_color(theme.text_secondary)
                    .child(if token.starts_with('@') { "没有匹配的笔记" } else { "没有匹配的指令或 Skill" })
            } else {
                picks.into_iter().enumerate().fold(div().flex().flex_col().p(px(4.)), |d, (i, (pick, name, desc))| {
                    let tlen = token.len();
                    d.child(div().id(SharedString::from(format!("pick-{i}"))).px(px(8.)).py(px(6.)).flex().items_center().gap(px(8.)).rounded(px(6.))
                        .hover(move |s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand)
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                            let v = this.input.read(cx).value().to_string();
                            let rest = v[..v.len().saturating_sub(tlen)].to_owned();
                            this.input.update(cx, |i, cx| { i.set_value(rest, w, cx); i.focus(w, cx); });
                            match &pick {
                                Pick::Page(p) => this.refs.push(p.clone()),
                                Pick::Skill(s) => this.skills.push(s.clone()),
                                Pick::NewConv => this.new_conv(w, cx),
                            }
                            cx.notify();
                        }))
                        .child(glyph(if name.starts_with('/') { Icon::Sparkles } else { Icon::Note }, 14., theme.accent))
                        .child(div().text_size(px(13.)).child(name))
                        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(12.)).text_color(theme.text_secondary).child(desc)))
                })
            };
            div().mx(px(12.)).max_h(px(240.)).rounded(px(8.)).border_1().border_color(theme.menu_border).bg(theme.menu).shadow_md()
                .child(div().px(px(10.)).pt(px(6.)).text_size(px(11.)).text_color(theme.text_secondary).child(if token.starts_with('@') { "引用笔记" } else { "指令与 Skill" }))
                .child(list)
        });
        let busy = self.streaming.is_some();
        let action = if busy {
            icon_btn("ai-chat-stop", Icon::Stop, "停止生成").on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.stop(); cx.notify(); }))
        } else {
            div().id("ai-chat-send").size(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(8.)).bg(theme.primary)
                .cursor(CursorStyle::PointingHand).tooltip(|w, cx| gpui_component::tooltip::Tooltip::new("发送消息").build(w, cx))
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, w, cx| this.send(w, cx)))
                .child(glyph(Icon::ArrowUp, 16., gpui::white()))
        };
        let composer = div().flex_none().m(px(12.)).p(px(10.)).flex().flex_col().gap(px(8.)).rounded(px(12.)).border_1().border_color(theme.btn_border).bg(theme.paper)
            .when(!self.quotes.is_empty(), |d| d.child(quotes))
            .when(!self.refs.is_empty() || !self.skills.is_empty(), |d| d.child(chips))
            .child(div().h(px(28.)).text_size(px(14.)).child(Input::new(&self.input).appearance(false).bordered(false).focus_bordered(false).h(px(28.))))
            .child(div().flex().items_center().gap(px(6.))
                .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(12.)).text_color(theme.text_secondary)
                    .child(SharedString::from(if model.is_empty() { "当前模型：默认".to_owned() } else { format!("当前模型：{model}") })))
                .child(action));

        div().id("ai-chat-panel").w(px(380.)).flex_none().h_full().flex().flex_col().relative().bg(theme.paper).border_l_1().border_color(theme.hairline)
            .text_color(theme.text)
            .child(header)
            .child(div().id("ai-chat-scroll").flex_1().min_h_0().overflow_y_scroll().track_scroll(&self.scroll).child(body))
            .children(error)
            .children(picker)
            .child(composer)
            .children(history.map(|h| div().absolute().top(px(44.)).left_0().right_0().child(h)))
    }
}

fn tool_label(name: &str) -> &'static str {
    match name {
        "loadSkill" => "加载 Skill", "listPages" => "列出页面", "searchNotes" => "搜索笔记", "readPage" => "读取页面",
        "executeBatchPlan" => "生成变更计划", _ => "调用工具",
    }
}

/// thinkly's NotebookBatchPlanCard: 笔记变更计划 with 原文 / 新片段, 应用 and 撤回.
fn plan_card(at: usize, plan: agent::Plan, cx: &mut Context<AiChat>) -> impl IntoElement {
    use agent::{OpKind, PlanState};
    let theme = Theme::ocean();
    let n = plan.ops.len();
    let (badge, note) = match plan.state {
        PlanState::Pending => ("等待确认", "方案已生成，尚未修改笔记。".to_owned()),
        PlanState::Applied => ("已应用", format!("已应用 {} 项操作。", n)),
        PlanState::Canceled => ("已取消", "未修改笔记。".to_owned()),
        PlanState::Undone => ("已撤回", "本次变更已撤回。".to_owned()),
        PlanState::UndoConflict => ("撤回冲突", "笔记在应用后发生变化，未覆盖后续编辑。".to_owned()),
        PlanState::Stale => ("已取消", "原文或目标位置已变化，请基于当前笔记重新生成方案。".to_owned()),
    };
    let snippet = |label: &'static str, text: String, del: bool| div().flex().flex_col().gap(px(2.))
        .child(div().text_size(px(11.)).text_color(theme.text_secondary).child(label))
        .child(div().px(px(8.)).py(px(4.)).rounded(px(6.)).text_size(px(12.)).line_height(px(18.))
            .bg(if del { gpui::hsla(0., 0.75, 0.5, 0.08) } else { gpui::hsla(0.38, 0.6, 0.4, 0.10) })
            .when(del, |d| d.line_through())
            .child(if text.is_empty() { "（空）".to_owned() } else { text.chars().take(400).collect() }));
    let ops = plan.ops.iter().fold(div().flex().flex_col().gap(px(8.)), |d, op| {
        let (label, body) = match &op.kind {
            OpKind::Replace { old, new, all } => (if *all { "替换全部匹配" } else { "替换首处匹配" },
                div().flex().flex_col().gap(px(4.)).child(snippet("原文", old.clone(), true)).child(snippet("新片段", new.clone(), false))),
            OpKind::Edit { markdown } => ("替换整篇正文", div().child(snippet("新正文", markdown.clone(), false))),
            OpKind::Create { markdown } => ("新建页面", div().child(snippet("正文", markdown.clone(), false))),
        };
        d.child(div().flex().flex_col().gap(px(4.))
            .child(div().flex().items_center().gap(px(6.)).text_size(px(12.))
                .child(div().font_weight(FontWeight(600.)).child(label))
                .child(div().text_color(theme.text_secondary).child(op.page.clone())))
            .child(body))
    });
    let btn = |id: String, _label: String, primary: bool| div().id(SharedString::from(id)).h(px(28.)).px(px(12.)).flex().items_center().rounded(px(6.))
        .text_size(px(12.)).cursor(CursorStyle::PointingHand)
        .when(primary, |d| d.bg(theme.primary).text_color(gpui::white()))
        .when(!primary, |d| d.border_1().border_color(theme.btn_border).hover(move |s| s.bg(theme.hover)));
    let actions = match plan.state {
        PlanState::Pending => Some(div().flex().justify_end().gap(px(8.))
            .child(btn(format!("plan-cancel-{at}"), "取消".into(), false).child("取消")
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| this.set_plan_state(at, PlanState::Canceled, cx))))
            .child(btn(format!("plan-apply-{at}"), String::new(), true).child(if n > 1 { format!("应用 {n} 项") } else { "应用".into() })
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| this.apply_plan(at, cx))))),
        PlanState::Applied => Some(div().flex().justify_end()
            .child(btn(format!("plan-undo-{at}"), String::new(), false).child("撤回本次变更")
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| this.undo_plan(at, cx))))),
        PlanState::Stale => Some(div().flex().justify_end()
            .child(btn(format!("plan-regen-{at}"), String::new(), false).child("重新生成方案")
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| {
                    this.input.update(cx, |i, cx| i.set_value("请基于当前笔记重新生成方案", w, cx));
                    this.send(w, cx);
                })))),
        _ => None,
    };
    div().flex().flex_col().gap(px(8.)).p(px(12.)).rounded(px(10.)).border_1().border_color(theme.btn_border).bg(theme.paper)
        .child(div().flex().items_center().gap(px(8.))
            .child(glyph(Icon::Sparkles, 14., theme.accent))
            .child(div().flex_1().text_size(px(13.)).font_weight(FontWeight(600.)).child(if plan.title.is_empty() { "笔记变更计划".to_owned() } else { format!("笔记变更计划 · {}", plan.title) }))
            .child(div().px(px(6.)).py(px(1.)).rounded(px(4.)).bg(theme.hover).text_size(px(11.)).text_color(theme.text_secondary).child(badge)))
        .when(!plan.summary.is_empty(), |d| d.child(div().text_size(px(12.)).text_color(theme.text_secondary).child(plan.summary.clone())))
        .child(ops)
        .child(div().text_size(px(12.)).text_color(theme.text_secondary).child(note))
        .children(actions)
}
